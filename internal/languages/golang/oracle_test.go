package golang_test

// An independent oracle for Go reference resolution.
//
// The oracle type-checks the repository with go/types (standard library only:
// go/parser, go/types, go/importer's source importer for the standard
// library; no network, no go/packages) and records, for every call, generic
// conversion and composite literal, the object the type checker says the
// callee denotes. It shares nothing with Ark's providers or resolver; it only
// maps the declaration position of the object it found to the Ark symbol
// declared there.
//
// Ark's side runs the real providers and resolver over the same files. The
// two are joined on the callee's position (Ark records a call at the start of
// its callee expression; for a selector that is the operand's start).

import (
	"context"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// oracleKind classifies what the type checker says a callee denotes.
type oracleKind string

const (
	okDecl      oracleKind = "declaration"  // a repository declaration Ark has a symbol for
	okUnmapped  oracleKind = "unmapped"     // a repository declaration Ark has no symbol for (interface method, field, local type, ...)
	okExternal  oracleKind = "external"     // declared outside the repository (standard library, dependency)
	okLocal     oracleKind = "local"        // a local variable or parameter (function value)
	okField     oracleKind = "field"        // a struct field holding a function
	okBuiltin   oracleKind = "builtin"      // len, append, ...; or a predeclared type conversion
	okInterface oracleKind = "interface"    // an interface method: dynamic dispatch
	okUnknown   oracleKind = "unverifiable" // the type checker has no object for it
)

type oracleSite struct {
	kind   oracleKind
	target symbol.SymbolID // for okDecl
	what   string          // human-readable target
}

// siteKey is "file:line:col" of the callee expression's start.
type siteKey string

func key(file string, line, col int) siteKey {
	return siteKey(fmt.Sprintf("%s:%d:%d", file, line, col))
}

// goOracle type-checks every package under root and classifies every callee.
// symbolsByFile maps a root-relative file to Ark's symbols in it.
func goOracle(root string, symbolsByFile map[string][]symbol.Symbol) (map[siteKey]oracleSite, oracleStats) {
	o := &oracle{
		root:    root,
		fset:    token.NewFileSet(),
		units:   map[string]*unit{},
		checked: map[string]*types.Package{},
		syms:    symbolsByFile,
		sites:   map[siteKey]oracleSite{},
	}
	o.std = importer.ForCompiler(o.fset, "source", nil)
	o.discover()
	paths := make([]string, 0, len(o.units))
	for p := range o.units {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		o.collect(o.units[p])
	}
	return o.sites, o.stats
}

type oracleStats struct {
	Packages, Files, TypeErrors, RedeclaredPackages int
}

// unit is one package of one directory: its import path, and its files with
// and without the in-package test files. External test packages (name_test)
// are units of their own.
type unit struct {
	path, dir, name string
	files, tests    []*ast.File
	external        bool
}

type oracle struct {
	root    string
	fset    *token.FileSet
	std     types.Importer
	units   map[string]*unit // by import path (external tests: path + "_test")
	checked map[string]*types.Package
	busy    map[string]bool
	syms    map[string][]symbol.Symbol
	sites   map[siteKey]oracleSite
	stats   oracleStats
}

// discover parses every directory's Go files (build constraints of the host
// apply) and derives import paths from the nearest go.mod.
func (o *oracle) discover() {
	_ = filepath.WalkDir(o.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != o.root && index.SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			o.discoverDir(path)
		}
		return nil
	})
}

func (o *oracle) discoverDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	modDir, modPath := o.module(dir)
	rel, _ := filepath.Rel(modDir, dir)
	importPath := modPath
	if rel != "." {
		importPath = modPath + "/" + filepath.ToSlash(rel)
	}
	byName := map[string]*unit{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if ok, err := build.Default.MatchFile(dir, e.Name()); err != nil || !ok {
			continue
		}
		f, err := parser.ParseFile(o.fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil || f == nil {
			continue
		}
		name := f.Name.Name
		u := byName[name]
		if u == nil {
			u = &unit{path: importPath, dir: dir, name: name, external: strings.HasSuffix(name, "_test")}
			if u.external {
				u.path = importPath + "_test"
			}
			byName[name] = u
		}
		if strings.HasSuffix(e.Name(), "_test.go") && !u.external {
			u.tests = append(u.tests, f)
		} else {
			u.files = append(u.files, f)
		}
		o.stats.Files++
	}
	// A directory holding two non-test packages (testdata, generators) has
	// no single importable package: keep the one named like the directory,
	// else none, for imports; all are still checked for their own sites.
	for name, u := range byName {
		key := u.path
		if !u.external {
			if prev, dup := o.units[key]; dup && prev.name != name {
				key = u.path + "#" + name
			} else if len(byName) > 2 || (len(byName) == 2 && !hasExternal(byName)) {
				if name != filepath.Base(dir) {
					key = u.path + "#" + name
				}
			}
		}
		o.units[key] = u
	}
}

func hasExternal(m map[string]*unit) bool {
	for _, u := range m {
		if u.external {
			return true
		}
	}
	return false
}

// module returns the directory and path of the nearest go.mod at or above dir
// (within root); a root without one is module "repo".
func (o *oracle) module(dir string) (string, string) {
	for d := dir; ; d = filepath.Dir(d) {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
					return d, strings.Trim(f[1], `"`)
				}
			}
		}
		if d == o.root || d == filepath.Dir(d) {
			return o.root, "repo"
		}
	}
}

// Import implements types.Importer: repository packages are checked from
// their files (without in-package tests), everything else comes from the
// standard library's sources or fails.
func (o *oracle) Import(path string) (*types.Package, error) {
	if p, ok := o.checked[path]; ok {
		if p == nil {
			return nil, fmt.Errorf("%s: not importable", path)
		}
		return p, nil
	}
	if u, ok := o.units[path]; ok && !u.external {
		if o.busy == nil {
			o.busy = map[string]bool{}
		}
		if o.busy[path] {
			return nil, fmt.Errorf("%s: import cycle", path)
		}
		o.busy[path] = true
		p, _ := o.check(u, u.files)
		delete(o.busy, path)
		o.checked[path] = p
		return p, nil
	}
	p, err := o.std.Import(path)
	if err != nil {
		o.checked[path] = nil
		return nil, err
	}
	o.checked[path] = p
	return p, nil
}

func (o *oracle) check(u *unit, files []*ast.File) (*types.Package, *types.Info) {
	redeclared := false
	info := &types.Info{
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Types:      map[ast.Expr]types.TypeAndValue{},
	}
	conf := types.Config{
		Importer:    o,
		FakeImportC: true,
		Error: func(err error) {
			o.stats.TypeErrors++
			if strings.Contains(err.Error(), "redeclared") {
				redeclared = true
			}
		},
	}
	pkg, _ := conf.Check(u.path, o.fset, files, info)
	if redeclared {
		// Several programs in one directory (testdata): which declaration a
		// name denotes depends on which file is compiled with which, so the
		// type checker's answer is no ground truth here.
		info.Uses = map[*ast.Ident]types.Object{}
		o.stats.RedeclaredPackages++
	}
	return pkg, info
}

// collect checks u with its in-package tests and classifies its callees.
func (o *oracle) collect(u *unit) {
	files := append(append([]*ast.File(nil), u.files...), u.tests...)
	pkg, info := o.check(u, files)
	o.stats.Packages++
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				o.callee(pkg, info, n.Fun)
			case *ast.CompositeLit:
				if n.Type != nil {
					o.callee(pkg, info, n.Type)
				}
			}
			return true
		})
	}
}

// callee classifies the callee expression e and records it at its start.
func (o *oracle) callee(pkg *types.Package, info *types.Info, e ast.Expr) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.IndexExpr:
		e = x.X
	case *ast.IndexListExpr:
		e = x.X
	}
	var id *ast.Ident
	var sel *ast.SelectorExpr
	switch x := e.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id, sel = x.Sel, x
	default:
		return
	}
	pos := o.fset.Position(e.Pos())
	rel, err := filepath.Rel(o.root, pos.Filename)
	if err != nil {
		return
	}
	k := key(filepath.ToSlash(rel), pos.Line, pos.Column)
	obj := info.Uses[id]
	site := oracleSite{kind: okUnknown}
	switch {
	case obj == nil:
	case isBuiltin(obj):
		site = oracleSite{kind: okBuiltin, what: obj.Name()}
	default:
		site = o.classify(obj, sel, info)
	}
	o.sites[k] = site
}

func isBuiltin(obj types.Object) bool {
	if _, ok := obj.(*types.Builtin); ok {
		return true
	}
	return obj.Pkg() == nil // predeclared types (int, string, error, any) and nil
}

func (o *oracle) classify(obj types.Object, sel *ast.SelectorExpr, info *types.Info) oracleSite {
	what := obj.Name()
	if obj.Pkg() != nil {
		what = obj.Pkg().Path() + "." + obj.Name()
	}
	switch v := obj.(type) {
	case *types.Var:
		if v.IsField() {
			return oracleSite{kind: okField, what: what}
		}
		if v.Parent() != nil && v.Parent() != v.Pkg().Scope() {
			return oracleSite{kind: okLocal, what: v.Name()}
		}
	case *types.Func:
		if sig, ok := v.Type().(*types.Signature); ok && sig.Recv() != nil {
			if types.IsInterface(sig.Recv().Type()) {
				return oracleSite{kind: okInterface, what: what}
			}
		}
		v = v.Origin()
		obj = v
	}
	if obj.Pkg() == nil || !o.inRepo(obj.Pos()) {
		return oracleSite{kind: okExternal, what: what}
	}
	if tn, ok := obj.(*types.TypeName); ok && tn.Parent() != nil && tn.Parent() != tn.Pkg().Scope() {
		return oracleSite{kind: okUnmapped, what: what + " (local type)"}
	}
	if id := o.symbolAt(obj); id != "" {
		return oracleSite{kind: okDecl, target: id, what: what}
	}
	return oracleSite{kind: okUnmapped, what: what}
}

func (o *oracle) inRepo(p token.Pos) bool {
	if !p.IsValid() {
		return false
	}
	f := o.fset.Position(p).Filename
	rel, err := filepath.Rel(o.root, f)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// symbolAt maps a declaration to the Ark symbol declared at it: same file,
// same name, the narrowest symbol range holding the declaring identifier.
func (o *oracle) symbolAt(obj types.Object) symbol.SymbolID {
	pos := o.fset.Position(obj.Pos())
	rel, err := filepath.Rel(o.root, pos.Filename)
	if err != nil {
		return ""
	}
	var best symbol.Symbol
	found := false
	for _, s := range o.syms[filepath.ToSlash(rel)] {
		r := s.Location.Range
		if s.Name != obj.Name() || pos.Line < int(r.Start.Line) || pos.Line > int(r.End.Line) {
			continue
		}
		if !found || (r.End.Line-r.Start.Line) < (best.Location.Range.End.Line-best.Location.Range.Start.Line) {
			best, found = s, true
		}
	}
	if !found {
		return ""
	}
	return best.ID
}

// --- Ark's side -----------------------------------------------------------------

// arkSite is Ark's resolution of one Go call / construction reference.
type arkSite struct {
	name, recv string
	conf       resolver.Confidence
	target     symbol.SymbolID // set when the resolution has a unique Strong/Exact target
	rule       string          // evidence kind of the resolution
	candidates int
}

// arkGo extracts and resolves root with Ark's providers and resolver (as the
// index builder does) and returns every Go call / construction reference by
// site, plus every file's symbols.
func arkGo(root string) (map[siteKey]arkSite, map[string][]symbol.Symbol, error) {
	reg := languages.Registry()
	var files []resolver.FileIndex
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
		ex, err := prov.Extract(context.Background(), fid, src)
		if err != nil {
			return nil
		}
		files = append(files, index.NewFileIndex(prov, fid, ex))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	syms := map[string][]symbol.Symbol{}
	refs := map[string]struct {
		file string
		r    int
	}{}
	byRef := map[string]*resolver.FileIndex{}
	for i := range files {
		fi := &files[i]
		syms[string(fi.FileID)] = fi.Symbols
		for j, r := range fi.References {
			refs[string(r.ID)] = struct {
				file string
				r    int
			}{string(fi.FileID), j}
			byRef[string(r.ID)] = fi
		}
	}
	out := map[siteKey]arkSite{}
	abs, _ := filepath.Abs(root)
	for _, res := range resolver.NewInRoot(files, filepath.Base(abs)).Resolve() {
		fi := byRef[string(res.ReferenceID)]
		if fi == nil || fi.Language != string(language.Language("go")) {
			continue
		}
		r := fi.References[refs[string(res.ReferenceID)].r]
		if r.Kind != "call" && r.Kind != "construction" {
			continue
		}
		s := arkSite{name: r.Name, recv: r.ReceiverExpr, conf: res.Confidence, candidates: len(res.Candidates)}
		if len(res.Evidence) > 0 {
			s.rule = string(res.Evidence[0].Kind)
		}
		if res.HasUniqueTarget() {
			s.target = res.Candidates[0].SymbolID
		}
		out[key(string(fi.FileID), int(r.Location.Range.Start.Line), int(r.Location.Range.Start.Column))] = s
	}
	return out, syms, nil
}

// --- comparison -------------------------------------------------------------------

// oracleReport is the join of Ark's resolutions with the oracle.
type oracleReport struct {
	ArkSites, OracleSites, Joined int
	// Strong+ resolutions (a unique Strong or Exact target: an edge if the
	// container is identified).
	Strong, StrongTP, StrongFP, StrongUnverifiable int
	FPByKind                                       map[oracleKind]int
	ByRule                                         map[string][3]int // TP, FP, unverifiable
	// Recall over oracle sites whose target is an Ark symbol.
	Representable, Recovered int
	FNByArk                  map[string]int // how Ark answered a representable site it missed
	FPSamples                []string
	FPByRuleKind             map[string][]string // rule|kind → samples
	FNSamples                []string
	FNByCategory             map[string][]string
	Stats                    oracleStats
}

func compareWithOracle(root string, samples int) (*oracleReport, error) {
	ark, syms, err := arkGo(root)
	if err != nil {
		return nil, err
	}
	sites, stats := goOracle(root, syms)
	rep := &oracleReport{ArkSites: len(ark), OracleSites: len(sites), FPByKind: map[oracleKind]int{}, ByRule: map[string][3]int{}, FNByArk: map[string]int{}, FPByRuleKind: map[string][]string{}, FNByCategory: map[string][]string{}, Stats: stats}
	keys := make([]string, 0, len(ark))
	for k := range ark {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, ks := range keys {
		k := siteKey(ks)
		a := ark[k]
		o, ok := sites[k]
		if ok {
			rep.Joined++
		}
		if a.target == "" {
			continue
		}
		rep.Strong++
		r := rep.ByRule[a.rule]
		switch {
		case !ok || o.kind == okUnknown || o.kind == okUnmapped:
			// No ground truth, or a declaration Ark has no symbol for: not
			// counted as wrong (it may be the very declaration Ark found).
			rep.StrongUnverifiable++
			r[2]++
		case o.kind == okDecl && o.target == a.target:
			rep.StrongTP++
			r[0]++
		default:
			rep.StrongFP++
			r[1]++
			fk := o.kind
			if o.kind == okDecl {
				fk = "other declaration"
			}
			rep.FPByKind[fk]++
			line := fmt.Sprintf("%s %s%s [%s %s] -> oracle %s %s", k, recvPrefix(a.recv), a.name, a.rule, a.conf, o.kind, o.what)
			if len(rep.FPSamples) < samples {
				rep.FPSamples = append(rep.FPSamples, line)
			}
			rk := a.rule + "|" + string(fk)
			if len(rep.FPByRuleKind[rk]) < 4 {
				rep.FPByRuleKind[rk] = append(rep.FPByRuleKind[rk], line)
			}
		}
		rep.ByRule[a.rule] = r
	}
	okeys := make([]string, 0, len(sites))
	for k := range sites {
		okeys = append(okeys, string(k))
	}
	sort.Strings(okeys)
	for _, ks := range okeys {
		o := sites[siteKey(ks)]
		if o.kind != okDecl {
			continue
		}
		rep.Representable++
		a, ok := ark[siteKey(ks)]
		switch {
		case ok && a.target == o.target:
			rep.Recovered++
			continue
		}
		cat := "not extracted"
		switch {
		case !ok:
		case a.target != "":
			cat = "wrong target"
		default:
			cat = fmt.Sprintf("%s (%s)", a.conf, a.rule)
		}
		rep.FNByArk[cat]++
		if len(rep.FNByCategory[cat]) < 4 {
			line := ks + " -> " + o.what
			if ok {
				line = fmt.Sprintf("%s %s%s -> %s", ks, recvPrefix(a.recv), a.name, o.what)
			}
			rep.FNByCategory[cat] = append(rep.FNByCategory[cat], line)
		}
		if len(rep.FNSamples) < samples {
			rep.FNSamples = append(rep.FNSamples, fmt.Sprintf("%s -> %s", ks, o.what))
		}
	}
	return rep, nil
}

func recvPrefix(r string) string {
	if r == "" {
		return ""
	}
	return r + "."
}

func (r *oracleReport) String() string {
	var b strings.Builder
	pct := func(a, n int) string {
		if n == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.2f%%", 100*float64(a)/float64(n))
	}
	fmt.Fprintf(&b, "oracle: %d packages, %d files, %d type errors (unresolved imports, testdata), %d packages with redeclarations (no ground truth)\n", r.Stats.Packages, r.Stats.Files, r.Stats.TypeErrors, r.Stats.RedeclaredPackages)
	fmt.Fprintf(&b, "sites: Ark %d, oracle %d, joined %d\n", r.ArkSites, r.OracleSites, r.Joined)
	fmt.Fprintf(&b, "Strong+ resolutions: %d — TP %d, FP %d, unverifiable %d; precision %s (over verifiable)\n",
		r.Strong, r.StrongTP, r.StrongFP, r.StrongUnverifiable, pct(r.StrongTP, r.StrongTP+r.StrongFP))
	fmt.Fprintf(&b, "recall: %d of %d representable sites resolved to the oracle's declaration (%s)\n", r.Recovered, r.Representable, pct(r.Recovered, r.Representable))
	var kinds []string
	for k, n := range r.FPByKind {
		kinds = append(kinds, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(kinds)
	fmt.Fprintf(&b, "FP by what the oracle found: %s\n", strings.Join(kinds, " "))
	var rules []string
	for k, v := range r.ByRule {
		rules = append(rules, fmt.Sprintf("  %-22s TP %6d  FP %6d  unverifiable %6d", k, v[0], v[1], v[2]))
	}
	sort.Strings(rules)
	fmt.Fprintf(&b, "by rule:\n%s\n", strings.Join(rules, "\n"))
	var fns []string
	for k, n := range r.FNByArk {
		fns = append(fns, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(fns)
	fmt.Fprintf(&b, "missed representable sites by Ark's answer: %s\n", strings.Join(fns, " "))
	var rks []string
	for k := range r.FPByRuleKind {
		rks = append(rks, k)
	}
	sort.Strings(rks)
	for _, k := range rks {
		fmt.Fprintf(&b, "FP samples %s:\n", k)
		for _, s := range r.FPByRuleKind[k] {
			fmt.Fprintf(&b, "    %s\n", s)
		}
	}
	var cats []string
	for k := range r.FNByCategory {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	for _, k := range cats {
		fmt.Fprintf(&b, "FN samples %s:\n", k)
		for _, s := range r.FNByCategory[k] {
			fmt.Fprintf(&b, "    %s\n", s)
		}
	}
	return b.String()
}
