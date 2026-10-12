package rust

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestOracle_Tasks measures, on the oracle of TestOracle_Compiler (same
// preparation, same variables), what an agent's code investigation asks of
// the index, against the compiler's answer:
//
//	A  the implementations of each trait of the crate (impl blocks)
//	B  the inherent methods of each type of the crate, apart from its trait
//	   implementations' methods
//	C  what each `use` of the crate imports (re-exports included)
//	D  which files each file depends on (resolved references across files)
//	E  references to a name the crate declares more than once
//
// Each is counted as found / wrong / missed over the compiler's set, with the
// compiler-invisible part (code the configuration does not compile) apart.
func TestOracle_Tasks(t *testing.T) {
	dir, srcRoot := os.Getenv("ARK_RUST_ORACLE"), os.Getenv("ARK_RUST_ORACLE_SRC")
	if dir == "" || srcRoot == "" {
		t.Skip("ARK_RUST_ORACLE / ARK_RUST_ORACLE_SRC not set")
	}
	entries, _ := os.ReadDir(dir)
	total := map[string]*taskCount{}
	for _, e := range entries {
		jsons, _ := filepath.Glob(filepath.Join(dir, e.Name(), "save-analysis-temp", "*.json"))
		if !e.IsDir() || len(jsons) != 1 {
			continue
		}
		for task, c := range measureTasks(t, filepath.Join(srcRoot, e.Name()), jsons[0]) {
			if total[task] == nil {
				total[task] = &taskCount{}
			}
			total[task].add(c)
		}
	}
	names := make([]string, 0, len(total))
	for k := range total {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		t.Logf("%s", total[k].report(k))
		for _, w := range total[k].wrongSamples {
			t.Logf("  WRONG %s", w)
		}
	}
	for _, k := range names {
		if total[k].wrong > 0 {
			t.Errorf("%s: %d wrong answers", k, total[k].wrong)
		}
	}
}

type taskCount struct {
	expected, found, wrong, missed, candidate int
	extra                                     int // found by the index, unknown to the compiler (other configurations)
	wrongSamples                              []string
}

func (c *taskCount) add(o taskCount) {
	c.expected += o.expected
	c.found += o.found
	c.wrong += o.wrong
	c.missed += o.missed
	c.candidate += o.candidate
	c.extra += o.extra
	if len(c.wrongSamples) < 10 {
		c.wrongSamples = append(c.wrongSamples, o.wrongSamples...)
	}
}

func (c *taskCount) report(task string) string {
	p, r := 0.0, 0.0
	if c.found+c.wrong > 0 {
		p = 100 * float64(c.found) / float64(c.found+c.wrong)
	}
	if c.expected > 0 {
		r = 100 * float64(c.found) / float64(c.expected)
	}
	return fmt.Sprintf("%s: expected=%d found=%d wrong=%d missed=%d (of which candidate=%d) unknownToCompiler=%d precision=%.2f%% (found/(found+wrong)) recall=%.1f%% (found/expected)",
		task, c.expected, c.found, c.wrong, c.missed, c.candidate, c.extra, p, r)
}

type taskAnalysis struct {
	Defs []struct {
		Kind     string `json:"kind"`
		ID       saID   `json:"id"`
		Name     string `json:"name"`
		Span     saSpan `json:"span"`
		Children []saID `json:"children"`
		Parent   *saID  `json:"parent"`
	} `json:"defs"`
	Impls []struct {
		ID       uint32 `json:"id"`
		Kind     string `json:"kind"`
		Span     saSpan `json:"span"`
		Children []saID `json:"children"`
	} `json:"impls"`
	Relations []struct {
		Span saSpan       `json:"span"`
		Kind relationKind `json:"kind"`
		From saID         `json:"from"`
		To   saID         `json:"to"`
	} `json:"relations"`
	Imports []struct {
		Kind  string `json:"kind"`
		RefID *saID  `json:"ref_id"`
		Span  saSpan `json:"span"`
		Name  string `json:"name"`
	} `json:"imports"`
	Refs []struct {
		Kind  string `json:"kind"`
		Span  saSpan `json:"span"`
		RefID saID   `json:"ref_id"`
	} `json:"refs"`
}

func measureTasks(t *testing.T, crateDir, analysis string) map[string]taskCount {
	t.Helper()
	data, err := os.ReadFile(analysis)
	if err != nil {
		t.Fatal(err)
	}
	var sa taskAnalysis
	if err := json.Unmarshal(data, &sa); err != nil {
		t.Fatal(err)
	}
	rel := func(f string) (string, bool) {
		r, err := filepath.Rel(crateDir, f)
		if err != nil || strings.HasPrefix(r, "..") {
			return "", false
		}
		return filepath.ToSlash(r), true
	}
	pos := func(sp saSpan) (string, source.Position, source.Position, bool) {
		f, ok := rel(sp.File)
		return f, source.Position{Line: sp.LineStart, Column: sp.ColStart}, source.Position{Line: sp.LineEnd, Column: sp.ColEnd}, ok
	}

	// The index's view.
	p := NewProvider()
	var files []resolver.FileIndex
	var symbols []symbol.Symbol
	refsAt := map[spanKey][]reference.Reference{}
	filepath.Walk(filepath.Join(crateDir, "src"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".rs") {
			return nil
		}
		r, _ := rel(path)
		src, _ := os.ReadFile(path)
		ex, err := p.Extract(context.Background(), source.FileID(r), src)
		if err != nil {
			return nil
		}
		fi := index.NewFileIndex(p, source.FileID(r), ex)
		files = append(files, fi)
		symbols = append(symbols, fi.Symbols...)
		for _, ref := range fi.References {
			k := spanKey{r, ref.Location.Range.End.Line, ref.Location.Range.End.Column}
			refsAt[k] = append(refsAt[k], ref)
		}
		return nil
	})
	byID := map[symbol.SymbolID]symbol.Symbol{}
	for _, s := range symbols {
		byID[s.ID] = s
	}
	resolutions := map[reference.ReferenceID]resolver.Resolution{}
	for _, res := range resolver.New(files).Resolve() {
		resolutions[res.ReferenceID] = res
	}
	// containing returns the innermost symbol of kind whose range holds the span.
	containing := func(f string, start, end source.Position, kinds ...symbol.SymbolKind) (symbol.Symbol, bool) {
		var best symbol.Symbol
		found := false
		for _, s := range symbols {
			if string(s.Location.File) != f || before(start, s.Location.Range.Start) || before(s.Location.Range.End, end) {
				continue
			}
			ok := len(kinds) == 0
			for _, k := range kinds {
				ok = ok || s.Kind == k
			}
			if ok && (!found || !before(s.Location.Range.Start, best.Location.Range.Start)) {
				best, found = s, true
			}
		}
		return best, found
	}
	defByID := map[saID]int{}
	for i, d := range sa.Defs {
		defByID[d.ID] = i
	}
	defSymbol := func(id saID, kinds ...symbol.SymbolKind) (symbol.Symbol, bool) {
		i, ok := defByID[id]
		if !ok {
			return symbol.Symbol{}, false
		}
		f, s, e, ok := pos(sa.Defs[i].Span)
		if !ok {
			return symbol.Symbol{}, false
		}
		return containing(f, s, e, kinds...)
	}
	out := map[string]taskCount{}

	// A: trait implementations. The compiler's: a relation Impl from a type
	// to a trait of the crate, at the impl header; the index's: an impl
	// symbol with an `implementation` reference resolved to the trait.
	var a taskCount
	implemented := map[symbol.SymbolID]map[symbol.SymbolID]bool{} // trait -> impls (index)
	for _, f := range files {
		for _, ref := range f.References {
			if ref.Kind != reference.KindImplements {
				continue
			}
			res := resolutions[ref.ID]
			if !res.HasUniqueTarget() {
				continue
			}
			impl, ok := containing(string(f.FileID), ref.Location.Range.Start, ref.Location.Range.End, symbol.KindImpl)
			if !ok {
				continue
			}
			tr := res.Candidates[0].SymbolID
			if implemented[tr] == nil {
				implemented[tr] = map[symbol.SymbolID]bool{}
			}
			implemented[tr][impl.ID] = true
		}
	}
	seenImpl := map[symbol.SymbolID]bool{}
	for _, r := range sa.Relations {
		if _, ok := r.Kind["Impl"]; !ok || r.To.Krate != 0 {
			continue
		}
		trait, ok := defSymbol(r.To, symbol.KindTrait)
		if !ok {
			continue
		}
		f, s, e, ok := pos(r.Span)
		if !ok {
			continue
		}
		a.expected++
		impl, ok := containing(f, s, e, symbol.KindImpl)
		switch {
		case ok && implemented[trait.ID][impl.ID]:
			a.found++
			seenImpl[impl.ID] = true
		default:
			a.missed++
			dump("MISSED-A", fmt.Sprintf("%s:%d impl=%q (found %v) trait=%s", f, s.Line, impl.Qualified, ok, trait.Qualified))
		}
	}
	// The trait each impl block of the compiler's implements (save-analysis
	// records the relation only when the implementing type is an item of the
	// crate: an implementation for a primitive or another crate's type is
	// not in it).
	compilerTrait := map[symbol.SymbolID]saID{}
	for _, r := range sa.Relations {
		if _, ok := r.Kind["Impl"]; !ok {
			continue
		}
		if f, s, e, ok := pos(r.Span); ok {
			if impl, ok := containing(f, s, e, symbol.KindImpl); ok {
				compilerTrait[impl.ID] = r.To
			}
		}
	}
	for tr, impls := range implemented {
		for impl := range impls {
			if seenImpl[impl] {
				continue
			}
			s := byID[impl]
			if to, ok := compilerTrait[impl]; ok {
				// The compiler records this impl implementing another trait.
				a.wrong++
				a.wrongSamples = append(a.wrongSamples, fmt.Sprintf("%s implements %s (%s:%d); compiler: trait %v", s.Qualified, byID[tr].Qualified, s.Location.File, s.Location.Range.Start.Line, to))
			} else {
				a.extra++ // not recorded: another configuration's, or for a type the relation omits
			}
		}
	}
	out["A trait implementations"] = a

	// B: inherent methods of each type. The compiler's: the children of its
	// inherent impls; the index's: the methods of `impl TYPE` blocks of the
	// type's identity. A trait implementation's method listed there would
	// be wrong.
	var b taskCount
	inherentOf := map[string]map[symbol.SymbolID]bool{} // type identity -> methods (index)
	for _, s := range symbols {
		if s.Kind == symbol.KindMethod && strings.HasPrefix(s.ParentQualified, "impl ") {
			ty := strings.TrimPrefix(s.ParentQualified, "impl ")
			if inherentOf[ty] == nil {
				inherentOf[ty] = map[symbol.SymbolID]bool{}
			}
			inherentOf[ty][s.ID] = true
		}
	}
	implTypes := map[uint32]saID{}
	for _, r := range sa.Relations {
		var v struct {
			ID uint32 `json:"id"`
		}
		if raw, ok := r.Kind["Impl"]; ok && json.Unmarshal(raw, &v) == nil {
			implTypes[v.ID] = r.From
		}
	}
	listed := map[symbol.SymbolID]bool{}
	for _, im := range sa.Impls {
		ty, ok := implTypes[im.ID]
		if !ok || ty.Krate != 0 {
			continue
		}
		tsym, ok := defSymbol(ty, symbol.KindStruct, symbol.KindEnum, symbol.KindTypeAlias)
		if !ok {
			continue
		}
		for _, child := range im.Children {
			m, ok := defSymbol(child, symbol.KindMethod, symbol.KindConstant, symbol.KindTypeAlias)
			if !ok || m.Kind != symbol.KindMethod {
				continue
			}
			if im.Kind == "Inherent" {
				b.expected++
				if inherentOf[tsym.Qualified][m.ID] {
					b.found++
					listed[m.ID] = true
				} else {
					b.missed++
				}
			} else if inherentOf[tsym.Qualified][m.ID] {
				b.wrong++
				b.wrongSamples = append(b.wrongSamples, fmt.Sprintf("%s listed as an inherent method of %s", m.Qualified, tsym.Qualified))
			}
		}
	}
	out["B inherent methods"] = b

	// C: imports. The compiler's: each `use` name bound to an item of the
	// crate; the index's: the import reference at that span, resolved.
	var c taskCount
	for _, im := range sa.Imports {
		if im.Kind != "Use" || im.RefID == nil || im.RefID.Krate != 0 {
			continue
		}
		i, ok := defByID[*im.RefID]
		if !ok {
			continue
		}
		f, _, e, ok := pos(im.Span)
		if !ok {
			continue
		}
		df, ds, de, ok := pos(sa.Defs[i].Span)
		if !ok {
			continue
		}
		c.expected++
		var ref *reference.Reference
		for _, r := range refsAt[spanKey{f, e.Line, e.Column}] {
			if r.Kind == reference.KindImport {
				r := r
				ref = &r
			}
		}
		if ref == nil {
			c.missed++
			continue
		}
		res := resolutions[ref.ID]
		switch {
		case res.HasUniqueTarget():
			tgt := byID[res.Candidates[0].SymbolID]
			if string(tgt.Location.File) == df && !before(ds, tgt.Location.Range.Start) && !before(tgt.Location.Range.End, de) || isFileModule(tgt, sa.Defs[i].Kind, df) {
				c.found++
			} else {
				c.wrong++
				c.wrongSamples = append(c.wrongSamples, fmt.Sprintf("%s:%d use %s -> %s; compiler %s:%d", f, e.Line, im.Name, tgt.Qualified, df, ds.Line))
			}
		case res.Confidence == resolver.ConfidenceCandidate:
			c.candidate++
			c.missed++
		default:
			c.missed++
		}
	}
	out["C imports"] = c

	// D: file dependencies. The compiler's: a reference in file F to an item
	// defined in file G ≠ F; the index's: a reference of F resolved to one
	// declaration in G.
	var d taskCount
	compilerDeps := map[[2]string]bool{}
	for _, r := range sa.Refs {
		i, ok := defByID[r.RefID]
		if !ok || r.RefID.Krate != 0 || sa.Defs[i].Kind == "Local" || sa.Defs[i].Kind == "Field" {
			continue
		}
		f, _, _, ok1 := pos(r.Span)
		g, _, _, ok2 := pos(sa.Defs[i].Span)
		if ok1 && ok2 && f != g {
			compilerDeps[[2]string{f, g}] = true
		}
	}
	indexDeps := map[[2]string]bool{}
	for _, f := range files {
		for _, ref := range f.References {
			if res := resolutions[ref.ID]; res.HasUniqueTarget() {
				if g := string(byID[res.Candidates[0].SymbolID].Location.File); g != string(f.FileID) {
					indexDeps[[2]string{string(f.FileID), g}] = true
				}
			}
		}
	}
	for dep := range compilerDeps {
		d.expected++
		if indexDeps[dep] {
			d.found++
		} else {
			d.missed++
		}
	}
	for dep := range indexDeps {
		if !compilerDeps[dep] {
			d.extra++ // another configuration's, a `use` only, or wrong: see A–C and the reference precision
		}
	}
	out["D file dependencies"] = d

	// E: references to a name declared more than once in the crate: the
	// compiler's reference resolved by the index to the same definition.
	var e taskCount
	names := map[string]int{}
	for _, def := range sa.Defs {
		if def.Kind != "Local" && def.Kind != "Field" && def.Kind != "Mod" {
			names[def.Name]++
		}
	}
	for _, r := range sa.Refs {
		i, ok := defByID[r.RefID]
		if !ok || r.RefID.Krate != 0 || names[sa.Defs[i].Name] < 2 || sa.Defs[i].Kind == "Local" || sa.Defs[i].Kind == "Field" {
			continue
		}
		f, _, end, ok := pos(r.Span)
		if !ok {
			continue
		}
		refs := refsAt[spanKey{f, end.Line, end.Column}]
		if len(refs) == 0 {
			continue // not a site the index observes (TestOracle_Compiler counts those)
		}
		e.expected++
		res := resolutions[refs[0].ID]
		df, ds, de, _ := pos(sa.Defs[i].Span)
		switch {
		case res.HasUniqueTarget():
			tgt := byID[res.Candidates[0].SymbolID]
			if string(tgt.Location.File) == df && !before(ds, tgt.Location.Range.Start) && !before(tgt.Location.Range.End, de) || isFileModule(tgt, sa.Defs[i].Kind, df) {
				e.found++
			} else if parent, ok := defSymbol(sa.Defs[i].ID); ok && tgt.Kind == symbol.KindConstant && tgt.ParentQualified == parent.Qualified {
				e.found++ // a braced variant (see TestOracle_Compiler)
			} else {
				e.wrong++
				e.wrongSamples = append(e.wrongSamples, fmt.Sprintf("%s:%d %s -> %s; compiler %s:%d", f, end.Line, sa.Defs[i].Name, tgt.Qualified, df, ds.Line))
			}
		case res.Confidence == resolver.ConfidenceCandidate:
			e.candidate++
			e.missed++
		default:
			e.missed++
		}
	}
	out["E ambiguous names"] = e
	return out
}

// relationKind is a save-analysis relation kind: a string ("SuperTrait") or
// an object ({"Impl": {"id": N}}).
type relationKind map[string]json.RawMessage

func (k *relationKind) UnmarshalJSON(b []byte) error {
	var name string
	if json.Unmarshal(b, &name) == nil {
		*k = relationKind{name: nil}
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*k = m
	return nil
}

// isFileModule reports whether target is the module whose file defines it:
// save-analysis places a file module's definition at the top of its file,
// the index at its `mod` declaration.
func isFileModule(target symbol.Symbol, defKind, defFile string) bool {
	if target.Kind != symbol.KindModule || defKind != "Mod" {
		return false
	}
	lay := fileLayout(source.FileID(defFile))
	return lay.ok && modulePath(lay.crate, lay.modPath) == target.Qualified
}
