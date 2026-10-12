package golang_test

// A reference-level extension of the go/types oracle (oracle_test.go).
//
// The call oracle (compareWithOracle) checks the callee of every call and
// composite literal. This one checks every identifier the type checker
// resolves (types.Info.Uses), whatever its role: a call, a construction, a
// type use, a value read, a write, a method value or expression, a field.
// Its ground truth is go/types alone: the role is read from the go/ast
// syntax around the identifier, the target from the object the type checker
// records for it. Nothing comes from Ark's providers or resolver; Ark's
// symbols serve only to name a repository declaration (symbolAt).
//
// Join: an Ark reference and an oracle site are the same use when they name
// the same identifier ending at the same position of the same file — Ark
// records a reference over the expression ending in its name (a selector
// a.b.c ends at c), so the end position and the name identify the
// identifier, where a start position would make a.b and a.b.c collide.
//
// What it cannot judge (reported, never counted as Ark's error nor as its
// success): an identifier the type checker has no object for (an import
// outside the standard library, a type error), a package with
// redeclarations (several programs in a directory), a file the host's build
// constraints exclude (no oracle site at all), and a repository
// declaration Ark has no symbol for (a field, an interface method, a local
// type).

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// refRole is the syntactic role of an identifier use.
type refRole string

const (
	roleCall        refRole = "call"         // callee of a call (a function, a method, a conversion)
	roleConstruct   refRole = "construction" // type of a composite literal
	roleTypeUse     refRole = "type_use"     // any other type name (incl. an embedded field's type)
	roleValue       refRole = "value"        // a function, variable or constant read as a value
	roleWrite       refRole = "write"        // a variable assigned (=, op=, ++, --, range =)
	roleMethodValue refRole = "method_value" // x.M not called
	roleMethodExpr  refRole = "method_expr"  // T.M, (*T).M
	roleFieldRead   refRole = "field_read"
	roleFieldWrite  refRole = "field_write"
	roleFieldKey    refRole = "field_key" // a struct literal's key
)

// targetKind is what the type checker says an identifier denotes.
type targetKind string

const (
	tkDecl       targetKind = "declaration"  // a repository declaration Ark has a symbol for
	tkUnmapped   targetKind = "unmapped"     // a package-level repository declaration Ark has no symbol for
	tkExternal   targetKind = "external"     // declared outside the repository
	tkLocal      targetKind = "local"        // a function-scope variable, constant or type
	tkField      targetKind = "field"        // a struct field
	tkInterface  targetKind = "interface"    // an interface method: dynamic dispatch
	tkTypeParam  targetKind = "type_param"   // a type parameter
	tkBuiltin    targetKind = "builtin"      // predeclared
	tkUnverified targetKind = "unverifiable" // no object: no ground truth
)

type refSite struct {
	role   refRole
	kind   targetKind
	target symbol.SymbolID
	what   string
	inRepo bool // the object is declared in the repository (any kind)
}

// refKey is "file:endLine:endCol:name".
type refKey string

func rkey(file string, line, col int, name string) refKey {
	return refKey(fmt.Sprintf("%s:%d:%d:%s", file, line, col, name))
}

// goRefOracle type-checks root and classifies every resolved identifier.
func goRefOracle(root string, symbolsByFile map[string][]symbol.Symbol) (map[refKey]refSite, oracleStats) {
	o := newOracle(root, symbolsByFile)
	o.refSites = map[refKey]refSite{}
	o.discover()
	paths := make([]string, 0, len(o.units))
	for p := range o.units {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		u := o.units[p]
		files := append(append([]*ast.File(nil), u.files...), u.tests...)
		before := o.stats.RedeclaredPackages
		_, info := o.check(u, files)
		o.stats.Packages++
		if o.stats.RedeclaredPackages > before {
			// Several programs in one directory (testdata): no ground truth
			// for the package, but each file is a program of its own — check
			// it alone. A name another file of its program declares stays
			// without an object (unverifiable).
			o.stats.SingleFilePrograms += len(files)
			for _, f := range files {
				_, alone := o.check(u, []*ast.File{f})
				o.collectRefs(f, alone)
			}
			continue
		}
		for _, f := range files {
			o.collectRefs(f, info)
		}
	}
	return o.refSites, o.stats
}

// collectRefs records a site for every identifier of f in info.Uses.
func (o *oracle) collectRefs(f *ast.File, info *types.Info) {
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if _, ok := n.(*ast.ImportSpec); ok {
			return false // import names are package names, not uses of declarations
		}
		if id, ok := n.(*ast.Ident); ok {
			o.refSite(id, stack, info)
		}
		stack = append(stack, n)
		return true
	})
}

func (o *oracle) refSite(id *ast.Ident, stack []ast.Node, info *types.Info) {
	if id.Name == "_" {
		return
	}
	obj, used := info.Uses[id]
	if !used {
		// Not a use (a definition, a package clause, a label, a blank) — unless
		// the checker failed to resolve it: an identifier in expression
		// position without an object is an unverifiable use.
		if _, def := info.Defs[id]; def || len(stack) == 0 {
			return
		}
		switch stack[len(stack)-1].(type) {
		case *ast.File, *ast.LabeledStmt, *ast.BranchStmt, *ast.KeyValueExpr:
			return
		}
		if sel, ok := stack[len(stack)-1].(*ast.SelectorExpr); ok && sel.Sel != id {
			if _, isPkg := info.Uses[sel.X.(*ast.Ident)].(*types.PkgName); isPkg {
				return
			}
		}
	}
	if _, isPkg := obj.(*types.PkgName); isPkg {
		return
	}
	if _, isLabel := obj.(*types.Label); isLabel {
		return
	}
	pos := o.fset.Position(id.End())
	rel, err := filepath.Rel(o.root, pos.Filename)
	if err != nil {
		return
	}
	k := rkey(filepath.ToSlash(rel), pos.Line, pos.Column, id.Name)
	site := refSite{role: o.roleOf(id, obj, stack, info), kind: tkUnverified}
	if obj != nil {
		site.kind, site.target, site.what = o.targetOf(obj)
		site.inRepo = obj.Pkg() != nil && o.inRepo(obj.Pos())
	}
	o.refSites[k] = site
}

// roleOf reads the identifier's role from the syntax around it.
func (o *oracle) roleOf(id *ast.Ident, obj types.Object, stack []ast.Node, info *types.Info) refRole {
	// e is the expression the identifier ends: id, or a selector x.id.
	var e ast.Expr = id
	i := len(stack) - 1
	var sel *ast.SelectorExpr
	if i >= 0 {
		if s, ok := stack[i].(*ast.SelectorExpr); ok && s.Sel == id {
			sel, e = s, s
			i--
		}
	}
	if i >= 0 {
		if kv, ok := stack[i].(*ast.KeyValueExpr); ok && kv.Key == e {
			if v, isVar := obj.(*types.Var); isVar && v.IsField() {
				return roleFieldKey
			}
		}
	}
	// Climb parentheses and instantiations: f[T](x) calls f.
	for i >= 0 {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			e, i = p, i-1
			continue
		case *ast.IndexExpr:
			if p.X == e {
				if _, isType := obj.(*types.TypeName); isType || isFunc(obj) {
					e, i = p, i-1
					continue
				}
			}
		case *ast.IndexListExpr:
			if p.X == e {
				e, i = p, i-1
				continue
			}
		case *ast.StarExpr:
			if _, isType := obj.(*types.TypeName); isType && p.X == e {
				e, i = p, i-1 // *T in a method expression or a conversion
				continue
			}
		}
		break
	}
	var parent ast.Node
	if i >= 0 {
		parent = stack[i]
	}
	written := false
	switch p := parent.(type) {
	case *ast.CallExpr:
		if p.Fun == e {
			if sel != nil {
				if s := info.Selections[sel]; s != nil && s.Kind() == types.MethodExpr {
					return roleMethodExpr
				}
			}
			return roleCall
		}
	case *ast.CompositeLit:
		if p.Type == e {
			return roleConstruct
		}
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == e {
				written = true
			}
		}
	case *ast.IncDecStmt:
		written = p.X == e
	case *ast.RangeStmt:
		written = p.Tok == token.ASSIGN && (p.Key == e || p.Value == e)
	}
	switch v := obj.(type) {
	case *types.TypeName:
		return roleTypeUse
	case *types.Var:
		if v.IsField() {
			if written {
				return roleFieldWrite
			}
			return roleFieldRead
		}
		if written {
			return roleWrite
		}
		return roleValue
	case *types.Func:
		if sel != nil {
			if s := info.Selections[sel]; s != nil {
				switch s.Kind() {
				case types.MethodVal:
					return roleMethodValue
				case types.MethodExpr:
					return roleMethodExpr
				}
			}
		}
		return roleValue
	}
	if written {
		return roleWrite
	}
	return roleValue
}

func contextBackground() context.Context { return context.Background() }

func isFunc(obj types.Object) bool {
	_, ok := obj.(*types.Func)
	return ok
}

// targetOf classifies what obj is and, for a repository declaration Ark has
// a symbol for, which symbol.
func (o *oracle) targetOf(obj types.Object) (targetKind, symbol.SymbolID, string) {
	what := obj.Name()
	if obj.Pkg() != nil {
		what = obj.Pkg().Path() + "." + obj.Name()
	}
	if obj.Pkg() == nil {
		return tkBuiltin, "", what
	}
	switch v := obj.(type) {
	case *types.Var:
		v = v.Origin()
		if v.IsField() {
			return tkField, "", what
		}
		if v.Parent() != v.Pkg().Scope() {
			return tkLocal, "", what
		}
		obj = v
	case *types.Const:
		if v.Parent() != v.Pkg().Scope() {
			return tkLocal, "", what
		}
	case *types.TypeName:
		if _, ok := v.Type().(*types.TypeParam); ok {
			return tkTypeParam, "", what
		}
		if v.Parent() != v.Pkg().Scope() {
			return tkLocal, "", what
		}
	case *types.Func:
		v = v.Origin()
		if sig, ok := v.Type().(*types.Signature); ok && sig.Recv() != nil && types.IsInterface(sig.Recv().Type()) {
			return tkInterface, "", what
		}
		obj = v
	}
	if !o.inRepo(obj.Pos()) {
		return tkExternal, "", what
	}
	if sid := o.symbolAt(obj); sid != "" {
		return tkDecl, sid, what
	}
	return tkUnmapped, "", what
}

// --- Ark's side -------------------------------------------------------------------

// arkRef is Ark's resolution of one Go reference (any kind but import).
type arkRef struct {
	name, recv, kind string
	conf             resolver.Confidence
	target           symbol.SymbolID // a unique Strong/Exact target (a graph edge)
	candidates       []symbol.SymbolID
	rule             string
	outside          bool
}

// arkGoRefs runs Ark's providers and resolver over root like arkGo and
// returns every Go reference but imports, keyed like the oracle's sites.
func arkGoRefs(root string) (map[refKey][]arkRef, map[string][]symbol.Symbol, int, error) {
	files, err := arkFiles(root)
	if err != nil {
		return nil, nil, 0, err
	}
	syms := map[string][]symbol.Symbol{}
	type at struct {
		fi *resolver.FileIndex
		i  int
	}
	byRef := map[string]at{}
	for i := range files {
		fi := &files[i]
		syms[string(fi.FileID)] = fi.Symbols
		for j, r := range fi.References {
			byRef[string(r.ID)] = at{fi, j}
		}
	}
	out := map[refKey][]arkRef{}
	collisions := 0
	abs, _ := filepath.Abs(root)
	for _, res := range resolver.NewInRoot(files, filepath.Base(abs)).Resolve() {
		a, ok := byRef[string(res.ReferenceID)]
		if !ok || a.fi.Language != string(language.Language("go")) {
			continue
		}
		r := a.fi.References[a.i]
		if r.Kind == "import" {
			continue
		}
		s := arkRef{name: r.Name, recv: r.ReceiverExpr, kind: string(r.Kind), conf: res.Confidence, outside: res.OutsideRepository}
		if len(res.Evidence) > 0 {
			s.rule = string(res.Evidence[0].Kind)
		}
		for _, c := range res.Candidates {
			s.candidates = append(s.candidates, c.SymbolID)
		}
		if res.HasUniqueTarget() {
			s.target = res.Candidates[0].SymbolID
		}
		k := rkey(string(a.fi.FileID), int(r.Location.Range.End.Line), int(r.Location.Range.End.Column), r.Name)
		if len(out[k]) > 0 {
			collisions++ // several references of one identifier: each is scored
		}
		out[k] = append(out[k], s)
	}
	return out, syms, collisions, nil
}

// arkFiles extracts every file under root with Ark's providers, as the index
// builder does.
func arkFiles(root string) ([]resolver.FileIndex, error) {
	reg := languages.Registry()
	var files []resolver.FileIndex
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && index.SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		desc, ok := reg.DetectByExtension(strings.ToLower(filepath.Ext(path)))
		if !ok {
			return nil
		}
		prov, ok := reg.Provider(desc.Language)
		if !ok {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fid := source.FileID(filepath.ToSlash(rel))
		ex, err := prov.Extract(contextBackground(), fid, src)
		if err != nil {
			return nil
		}
		files = append(files, index.NewFileIndex(prov, fid, ex))
		return nil
	})
	return files, err
}

// --- comparison -------------------------------------------------------------------

// refCount is one cell of the report.
type refCount struct {
	Sites         int `json:"sites"`          // oracle sites of this role
	Representable int `json:"representable"`  // ... whose target is an Ark symbol
	Recovered     int `json:"recovered"`      // ... Ark resolved to that symbol (unique Strong+)
	ExactTP       int `json:"exact_tp"`       // Ark Exact, oracle agrees
	ExactFP       int `json:"exact_fp"`       // Ark Exact, oracle disagrees: a false Exact
	StrongTP      int `json:"strong_tp"`      // Ark Strong, oracle agrees
	StrongFP      int `json:"strong_fp"`      // Ark Strong, oracle disagrees
	Unverifiable  int `json:"unverifiable"`   // Ark Strong+ where the oracle has no ground truth
	CandHit       int `json:"candidate_hit"`  // Ark Candidate on a representable site, target among the candidates
	CandMiss      int `json:"candidate_miss"` // ... target not among them
	CandSize      int `json:"candidate_size"` // total candidates over CandHit+CandMiss
	CandSpurious  int `json:"candidate_spurious"`
	Unresolved    int `json:"unresolved"`    // Ark Unresolved on a representable site
	FalseOutside  int `json:"false_outside"` // Ark OutsideRepository where the target is a repository declaration
	NotExtracted  int `json:"not_extracted"` // representable site Ark has no reference for
}

type refReport struct {
	Corpus       string                `json:"corpus"`
	Stats        oracleStats           `json:"oracle"`
	ArkRefs      int                   `json:"ark_refs"`
	ArkUnjoined  map[string]int        `json:"ark_unjoined_by_kind"` // Ark references at no oracle site
	Collisions   int                   `json:"ark_key_collisions"`
	OracleSites  int                   `json:"oracle_sites"`
	ByRole       map[refRole]*refCount `json:"by_role"`
	Total        refCount              `json:"total"`
	ByTargetKind map[string]int        `json:"sites_by_target_kind"`
	FPSamples    []string              `json:"fp_samples"`
	OutsideFP    []string              `json:"false_outside_samples"`
	FNSamples    map[string][]string   `json:"fn_samples"`
	// Sites: one line per representable site — "key\trole\ttarget\tanswer",
	// answer "ok" when recovered — for comparing two runs site by site
	// (ARK_GO_ORACLE_SITES).
	Sites []string `json:"-"`
	// FNCounts: missed representable sites by role and Ark's answer.
	FNCounts map[string]int `json:"fn_counts"`
	// Unverifiable: Ark's Strong+ answers without ground truth, by why.
	Unverifiable        map[string]int      `json:"unverifiable_by_reason"`
	UnverifiableSamples map[string][]string `json:"-"`
}

func compareRefs(root, corpus string) (*refReport, error) {
	ark, syms, collisions, err := arkGoRefs(root)
	if err != nil {
		return nil, err
	}
	sites, stats := goRefOracle(root, syms)
	rep := &refReport{Corpus: corpus, Stats: stats, ArkRefs: len(ark), Collisions: collisions, OracleSites: len(sites),
		ArkUnjoined: map[string]int{}, ByRole: map[refRole]*refCount{}, ByTargetKind: map[string]int{}, FNSamples: map[string][]string{}, FNCounts: map[string]int{}, Unverifiable: map[string]int{}, UnverifiableSamples: map[string][]string{}}
	cell := func(r refRole) *refCount {
		c := rep.ByRole[r]
		if c == nil {
			c = &refCount{}
			rep.ByRole[r] = c
		}
		return c
	}
	keys := make([]string, 0, len(sites))
	for k := range sites {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, ks := range keys {
		k := refKey(ks)
		o := sites[k]
		c := cell(o.role)
		c.Sites++
		rep.ByTargetKind[string(o.kind)]++
		answers := ark[k]
		ok := len(answers) > 0
		// Precision: every Strong+ answer of every reference at the site.
		for _, a := range answers {
			if a.target != "" {
				switch {
				case o.kind == tkUnverified || o.kind == tkUnmapped:
					c.Unverifiable++
					why := string(o.kind) + " " + string(o.role)
					rep.Unverifiable[why]++
					if len(rep.UnverifiableSamples[why]) < 4 {
						rep.UnverifiableSamples[why] = append(rep.UnverifiableSamples[why], fmt.Sprintf("%s [%s %s %s] -> %s", k, a.kind, recvPrefix(a.recv)+a.name, a.conf, o.what))
					}
				case o.kind == tkDecl && o.target == a.target:
					if a.conf == resolver.ConfidenceExact {
						c.ExactTP++
					} else {
						c.StrongTP++
					}
				default:
					if a.conf == resolver.ConfidenceExact {
						c.ExactFP++
					} else {
						c.StrongFP++
					}
					if len(rep.FPSamples) < 60 {
						rep.FPSamples = append(rep.FPSamples, fmt.Sprintf("%s [%s %s %s %s] -> oracle %s %s %s", k, a.kind, recvPrefix(a.recv)+a.name, a.rule, a.conf, o.role, o.kind, o.what))
					}
				}
			}
			// OutsideRepository is wrong for any object the repository
			// declares — a declaration, a field, a local, an interface method.
			if a.outside && o.inRepo {
				c.FalseOutside++
				if len(rep.OutsideFP) < 40 {
					rep.OutsideFP = append(rep.OutsideFP, fmt.Sprintf("%s [%s %s %s] -> %s %s", k, a.kind, recvPrefix(a.recv)+a.name, a.rule, o.kind, o.what))
				}
			}
			if a.target == "" && len(a.candidates) > 0 && o.kind != tkDecl && o.kind != tkUnverified && o.kind != tkUnmapped {
				c.CandSpurious++
			}
		}
		if o.kind != tkDecl {
			continue
		}
		c.Representable++
		// Recall: the site is recovered when one of its references resolves
		// to the declaration; otherwise the best answer is reported.
		var a arkRef
		recovered := false
		for _, x := range answers {
			if x.target == o.target {
				recovered = true
			}
		}
		if ok {
			a = answers[0]
			for _, x := range answers {
				if x.target != "" || (a.target == "" && len(x.candidates) > len(a.candidates)) {
					a = x
				}
			}
		}
		answer := "missed"
		if recovered {
			answer = "ok"
		}
		rep.Sites = append(rep.Sites, fmt.Sprintf("%s\t%s\t%s\t%s", k, o.role, o.what, answer))
		var cat string
		switch {
		case !ok:
			c.NotExtracted++
			cat = "not extracted"
		case recovered:
			c.Recovered++
			continue
		case a.target != "":
			cat = "wrong target"
		case len(a.candidates) > 0:
			hit := false
			for _, id := range a.candidates {
				hit = hit || id == o.target
			}
			if hit {
				c.CandHit++
			} else {
				c.CandMiss++
			}
			c.CandSize += len(a.candidates)
			cat = fmt.Sprintf("candidate (%s)", a.rule)
		default:
			c.Unresolved++
			cat = fmt.Sprintf("unresolved (%s)", a.rule)
		}
		fk := string(o.role) + " | " + cat
		rep.FNCounts[fk]++
		if len(rep.FNSamples[fk]) < 3 {
			line := fmt.Sprintf("%s -> %s", k, o.what)
			if ok {
				line = fmt.Sprintf("%s [%s %s] -> %s", k, a.kind, recvPrefix(a.recv)+a.name, o.what)
			}
			rep.FNSamples[fk] = append(rep.FNSamples[fk], line)
		}
	}
	for k, as := range ark {
		if _, ok := sites[k]; !ok {
			for _, a := range as {
				rep.ArkUnjoined[a.kind]++
			}
		}
	}
	for _, c := range rep.ByRole {
		t := &rep.Total
		t.Sites += c.Sites
		t.Representable += c.Representable
		t.Recovered += c.Recovered
		t.ExactTP += c.ExactTP
		t.ExactFP += c.ExactFP
		t.StrongTP += c.StrongTP
		t.StrongFP += c.StrongFP
		t.Unverifiable += c.Unverifiable
		t.CandHit += c.CandHit
		t.CandMiss += c.CandMiss
		t.CandSize += c.CandSize
		t.CandSpurious += c.CandSpurious
		t.Unresolved += c.Unresolved
		t.FalseOutside += c.FalseOutside
		t.NotExtracted += c.NotExtracted
	}
	return rep, nil
}

func pct(a, n int) string {
	if n == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f%%", 100*float64(a)/float64(n))
}

func (r *refReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "corpus %s — oracle: %d packages, %d files, %d type errors, %d packages with redeclarations, %d dependency packages from the module cache\n",
		r.Corpus, r.Stats.Packages, r.Stats.Files, r.Stats.TypeErrors, r.Stats.RedeclaredPackages, r.Stats.DepPackages)
	fmt.Fprintf(&b, "sites: oracle %d, Ark references %d (key collisions %d), Ark references at no oracle site %v\n", r.OracleSites, r.ArkRefs, r.Collisions, r.ArkUnjoined)
	fmt.Fprintf(&b, "oracle targets: %v\n", r.ByTargetKind)
	row := func(name string, c refCount) {
		fmt.Fprintf(&b, "%-13s sites %7d repr %7d recall %8s | Exact TP %6d FP %3d | Strong TP %6d FP %3d | unverif %5d | cand hit %5d miss %4d avg %.1f spurious %5d | unres %5d falseOutside %4d | notExtracted %6d\n",
			name, c.Sites, c.Representable, pct(c.Recovered, c.Representable), c.ExactTP, c.ExactFP, c.StrongTP, c.StrongFP, c.Unverifiable,
			c.CandHit, c.CandMiss, avg(c.CandSize, c.CandHit+c.CandMiss), c.CandSpurious, c.Unresolved, c.FalseOutside, c.NotExtracted)
	}
	roles := make([]string, 0, len(r.ByRole))
	for k := range r.ByRole {
		roles = append(roles, string(k))
	}
	sort.Strings(roles)
	for _, k := range roles {
		row(k, *r.ByRole[refRole(k)])
	}
	row("TOTAL", r.Total)
	fmt.Fprintf(&b, "Exact precision %s, Strong precision %s (over verifiable)\n",
		pct(r.Total.ExactTP, r.Total.ExactTP+r.Total.ExactFP), pct(r.Total.StrongTP, r.Total.StrongTP+r.Total.StrongFP))
	for _, s := range r.FPSamples {
		fmt.Fprintf(&b, "FP %s\n", s)
	}
	for why, n := range r.Unverifiable {
		fmt.Fprintf(&b, "UNVERIFIABLE %s: %d %v\n", why, n, r.UnverifiableSamples[why])
	}
	for _, s := range r.OutsideFP {
		fmt.Fprintf(&b, "FALSE-OUTSIDE %s\n", s)
	}
	cats := make([]string, 0, len(r.FNSamples))
	for k := range r.FNSamples {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	for _, k := range cats {
		fmt.Fprintf(&b, "FN %s: %d\n", k, r.FNCounts[k])
		for _, s := range r.FNSamples[k] {
			fmt.Fprintf(&b, "FN %s: %s\n", k, s)
		}
	}
	return b.String()
}

func avg(a, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(a) / float64(n)
}

// TestGoRefOracleMeasure compares every Go reference Ark resolves with the
// reference-level oracle. Measurement only:
//
//	ARK_GO_ORACLE_ROOT=/path/to/repo [ARK_GO_ORACLE_NAME=name] [ARK_GO_ORACLE_JSON=out.json] \
//	  go test -run TestGoRefOracleMeasure -v ./internal/languages/golang/
func TestGoRefOracleMeasure(t *testing.T) {
	root := os.Getenv("ARK_GO_ORACLE_ROOT")
	if root == "" {
		t.Skip("set ARK_GO_ORACLE_ROOT to measure")
	}
	name := os.Getenv("ARK_GO_ORACLE_NAME")
	if name == "" {
		name = filepath.Base(root)
	}
	rep, err := compareRefs(root, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + rep.String())
	if out := os.Getenv("ARK_GO_ORACLE_SITES"); out != "" {
		if err := os.WriteFile(out, []byte(strings.Join(rep.Sites, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out := os.Getenv("ARK_GO_ORACLE_JSON"); out != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
