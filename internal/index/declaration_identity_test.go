package index_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Declaration identity: every declaration is its own symbol. Namesakes in one
// file (same kind and qualified name) used to share one SymbolID, and the
// index merged them — mixing their sources, callees and callers.

const initsGo = `package example

func init() {
	registerA()
}

func init() {
	registerB()
}

func registerA() {}
func registerB() {}
`

func declarations(idx *index.RepositoryIndex, file source.FileID, qualified string) []symbol.Symbol {
	var out []symbol.Symbol
	for _, s := range idx.SymbolsByFile(file) {
		if s.Qualified == qualified {
			out = append(out, s)
		}
	}
	return out
}

func names(idx *index.RepositoryIndex, edges []index.GraphEdge) []string {
	var out []string
	for _, e := range edges {
		s, _ := idx.GetSymbol(e.To)
		out = append(out, fmt.Sprintf("%s@%d", s.Qualified, s.Location.Range.Start.Line))
	}
	sort.Strings(out)
	return out
}

func TestDeclarationIdentity_GoInits(t *testing.T) {
	root := writeFiles(t, map[string]string{"go.mod": "module x\n\ngo 1.22\n", "boot.go": initsGo})
	idx, err := index.New(context.Background(), root, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	inits := declarations(idx, "boot.go", "init")
	if len(inits) != 2 {
		t.Fatalf("want two init declarations, got %d", len(inits))
	}
	first, second := inits[0], inits[1]
	if first.ID == second.ID {
		t.Fatal("the two init declarations share a SymbolID")
	}
	if first.Location.Range.Start.Line != 3 || second.Location.Range.Start.Line != 7 {
		t.Fatalf("locations %d / %d", first.Location.Range.Start.Line, second.Location.Range.Start.Line)
	}
	// The first declaration keeps the ID it always had.
	if first.ID != symbol.NewSymbolID("go", "boot.go", symbol.KindFunction, "init") {
		t.Error("the first declaration's ID changed")
	}
	for _, s := range inits {
		if got, ok := idx.GetSymbol(s.ID); !ok || got.Location != s.Location {
			t.Errorf("GetSymbol(%s) = %v, want the declaration at line %d", s.ID, got.Location, s.Location.Range.Start.Line)
		}
	}
	if got := names(idx, idx.GetCallees(first.ID)); !reflect.DeepEqual(got, []string{"registerA@11"}) {
		t.Errorf("callees of the first init: %v", got)
	}
	if got := names(idx, idx.GetCallees(second.ID)); !reflect.DeepEqual(got, []string{"registerB@12"}) {
		t.Errorf("callees of the second init: %v", got)
	}
	regA := symbolIn(t, idx, "boot.go", "registerA")
	regB := symbolIn(t, idx, "boot.go", "registerB")
	if got := names(idx, idx.GetCallers(regA.ID)); !reflect.DeepEqual(got, []string{"init@3"}) {
		t.Errorf("callers of registerA: %v", got)
	}
	if got := names(idx, idx.GetCallers(regB.ID)); !reflect.DeepEqual(got, []string{"init@7"}) {
		t.Errorf("callers of registerB: %v", got)
	}
	for _, s := range inits {
		for _, r := range idx.ReferencesByContainer(s.ID) {
			if r.Location.Range.Start.Line < s.Location.Range.Start.Line || r.Location.Range.Start.Line > s.Location.Range.End.Line {
				t.Errorf("init@%d holds a reference at line %d", s.Location.Range.Start.Line, r.Location.Range.Start.Line)
			}
		}
	}
	if n := len(idx.FindSymbolsByQualified("init")); n != 2 {
		t.Errorf("FindSymbolsByQualified(init) = %d, want 2", n)
	}
	if n := len(idx.FindSymbols("ini")); n != 2 {
		t.Errorf("FindSymbols(ini) = %d, want 2", n)
	}
	if st := idx.Stats(); st.Symbols != 4 {
		t.Errorf("symbols %d, want 4", st.Symbols)
	}
}

// A reference to a name declared twice stays ambiguous: two candidates, no
// edge, never a pick — identity is not resolution.
func TestDeclarationIdentity_ReferenceToNamesakesStaysAmbiguous(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"red.py": "def f():\n    return a()\n\ndef f():\n    return b()\n\ndef a(): pass\ndef b(): pass\n\ndef use():\n    return f()\n",
		"red.js": "function g() { return c() }\nfunction g() { return d() }\nfunction c() {}\nfunction d() {}\nfunction useG() { return g() }\n",
	})
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		file             source.FileID
		name, user       string
		calleeA, calleeB string
	}{
		{"red.py", "f", "use", "a@7", "b@8"},
		{"red.js", "g", "useG", "c@3", "d@4"},
	} {
		decls := declarations(idx, c.file, c.name)
		if len(decls) != 2 || decls[0].ID == decls[1].ID {
			t.Fatalf("%s: want two distinct %s declarations", c.file, c.name)
		}
		if got := names(idx, idx.GetCallees(decls[0].ID)); !reflect.DeepEqual(got, []string{c.calleeA}) {
			t.Errorf("%s: callees of the first %s: %v", c.file, c.name, got)
		}
		if got := names(idx, idx.GetCallees(decls[1].ID)); !reflect.DeepEqual(got, []string{c.calleeB}) {
			t.Errorf("%s: callees of the second %s: %v", c.file, c.name, got)
		}
		user := symbolIn(t, idx, c.file, c.user)
		if edges := idx.GetCallees(user.ID); len(edges) != 0 {
			t.Errorf("%s: %s -> %s picked a declaration: %v", c.file, c.user, c.name, names(idx, edges))
		}
		if s := idx.CandidateCalleeSample(user.ID); s.Total != 2 {
			t.Errorf("%s: %s has %d candidate callees, want both declarations", c.file, c.user, s.Total)
		}
		for _, d := range decls {
			if len(idx.GetCallers(d.ID)) != 0 {
				t.Errorf("%s: %s@%d gained a caller edge", c.file, c.name, d.Location.Range.Start.Line)
			}
		}
	}
}

func draftAt(name string, kind symbol.SymbolKind, startLine, endLine uint32) language.SymbolDraft {
	return language.SymbolDraft{
		Name: name, Qualified: name, Kind: kind,
		Location: source.Location{File: "x.fake", Range: source.Range{
			Start: source.Position{Line: startLine, Column: 1}, End: source.Position{Line: endLine, Column: 1}}},
	}
}

func callAt(name, container string, line uint32) language.ReferenceDraft {
	return language.ReferenceDraft{
		Name: name, Kind: "call", Container: container,
		Location: source.Location{File: "x.fake", Range: source.Range{
			Start: source.Position{Line: line, Column: 3}, End: source.Position{Line: line, Column: 4}}},
	}
}

// IDs are a function of the declarations, not of the provider's emission
// order; a draft stated twice is one declaration.
func TestDeclarationIdentity_OrderIndependentAndDeduplicated(t *testing.T) {
	a := draftAt("Run", symbol.KindFunction, 1, 3)
	b := draftAt("Run", symbol.KindFunction, 5, 7)
	ta, tb := draftAt("TA", symbol.KindFunction, 9, 9), draftAt("TB", symbol.KindFunction, 10, 10)
	refs := []language.ReferenceDraft{callAt("TA", "Run", 2), callAt("TB", "Run", 6)}
	type view struct {
		ID      symbol.SymbolID
		Line    uint32
		Callees []string
	}
	snapshot := func(syms []language.SymbolDraft) []view {
		idx := buildFakeIndex(t, fakeProvider{symbols: syms, refs: refs})
		var out []view
		for _, s := range declarations(idx, "x.fake", "Run") {
			out = append(out, view{s.ID, s.Location.Range.Start.Line, names(idx, idx.GetCallees(s.ID))})
		}
		return out
	}
	want := snapshot([]language.SymbolDraft{a, b, ta, tb})
	if len(want) != 2 || want[0].ID == want[1].ID ||
		!reflect.DeepEqual(want[0].Callees, []string{"TA@9"}) || !reflect.DeepEqual(want[1].Callees, []string{"TB@10"}) {
		t.Fatalf("baseline: %+v", want)
	}
	if got := snapshot([]language.SymbolDraft{tb, b, ta, a}); !reflect.DeepEqual(got, want) {
		t.Errorf("emission order changed identities:\n got %+v\nwant %+v", got, want)
	}
	if got := snapshot([]language.SymbolDraft{a, b, a, ta, tb, b}); !reflect.DeepEqual(got, want) {
		t.Errorf("a repeated draft became a namesake:\n got %+v\nwant %+v", got, want)
	}
}

// When containment cannot single out the enclosing namesake (ranges that do
// not hold the reference, or both hold it), the container stays unidentified:
// no edge, an unattributed caller, an unidentified source.
func TestDeclarationIdentity_UnprovableContainerFailsClosed(t *testing.T) {
	for _, c := range []struct {
		name string
		syms []language.SymbolDraft
	}{
		{"no range holds it", []language.SymbolDraft{draftAt("Run", symbol.KindFunction, 1, 1), draftAt("Run", symbol.KindFunction, 3, 3)}},
		{"both ranges hold it", []language.SymbolDraft{draftAt("Run", symbol.KindFunction, 1, 9), draftAt("Run", symbol.KindVariable, 1, 9)}},
	} {
		idx := buildFakeIndex(t, fakeProvider{
			symbols: append(c.syms, draftAt("Target", symbol.KindFunction, 20, 20)),
			refs:    []language.ReferenceDraft{callAt("Target", "Run", 2)},
		})
		target := symbolIn(t, idx, "x.fake", "Target")
		if edges := idx.GetCallers(target.ID); len(edges) != 0 {
			t.Errorf("%s: caller edge %v", c.name, edges)
		}
		if in, _ := idx.Unattributed(target.ID); in != 1 {
			t.Errorf("%s: unattributed callers %d, want 1", c.name, in)
		}
		if u := idx.UnidentifiedSources(target.ID); len(u.Resolved)+len(u.Candidates) != 1 {
			t.Errorf("%s: unidentified sources %+v", c.name, u)
		}
		for _, s := range declarations(idx, "x.fake", "Run") {
			if len(idx.GetCallees(s.ID)) != 0 || len(idx.ReferencesByContainer(s.ID)) != 0 {
				t.Errorf("%s: Run@%d (%s) was given the reference", c.name, s.Location.Range.Start.Line, s.Kind)
			}
		}
	}
}

// A cache hit yields the index a miss would: same declarations, IDs, edges.
// The cache stores extraction drafts, never IDs, so an entry written by an
// older identity scheme cannot carry old IDs into the index.
func TestDeclarationIdentity_ColdAndWarmCacheAgree(t *testing.T) {
	root := writeFiles(t, map[string]string{"go.mod": "module x\n\ngo 1.22\n", "boot.go": initsGo})
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	type edge struct{ From, To symbol.SymbolID }
	view := func(idx *index.RepositoryIndex) ([]symbol.Symbol, []edge) {
		syms := idx.SymbolsByFile("boot.go")
		var edges []edge
		for _, s := range syms {
			for _, e := range idx.GetCallees(s.ID) {
				edges = append(edges, edge{s.ID, e.To})
			}
		}
		slices.SortFunc(edges, func(a, b edge) int {
			if a.From != b.From {
				return map[bool]int{true: -1, false: 1}[a.From < b.From]
			}
			return map[bool]int{true: -1, false: 1}[a.To < b.To]
		})
		return syms, edges
	}
	providers := []language.Provider{golang.NewProvider()}
	plain, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	wantSyms, wantEdges := view(plain)
	for _, run := range []string{"cold", "warm"} {
		idx, err := index.NewWithCache(context.Background(), root, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		syms, edges := view(idx)
		if !reflect.DeepEqual(syms, wantSyms) || !reflect.DeepEqual(edges, wantEdges) {
			t.Errorf("%s cache: index differs from an uncached build", run)
		}
	}
	if len(wantEdges) != 2 {
		t.Errorf("edges %v", wantEdges)
	}
}

// Resolution evidence for a namesake keeps its confidence: identity does not
// promote anything.
func TestDeclarationIdentity_NoConfidencePromotion(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"red.py": "def f():\n    return 1\n\ndef f():\n    return 2\n\ndef use():\n    return f()\n",
	})
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	user := symbolIn(t, idx, "red.py", "use")
	for _, r := range idx.CandidateCalleeSample(user.ID).Relations {
		if r.Confidence > resolver.ConfidenceCandidate {
			t.Errorf("candidate callee %s promoted to %s", r.Symbol, r.Confidence)
		}
	}
}

// Property over every provider: distinct declarations (file, kind, qualified
// name, location) never share a SymbolID, and GetSymbol returns each one.
// The snippets cover the constructs that declare one name twice where a
// provider extracts them (Go init and blank, Python and JavaScript
// redefinition) and the ones providers fold into one symbol (TypeScript
// declaration merging and overloads, Terraform duplicates, PHP namespaces).
func TestDeclarationIdentity_DistinctDeclarationsDistinctIDsAllLanguages(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"go.mod":  "module x\n\ngo 1.22\n",
		"a.go":    "package x\nfunc init() {}\nfunc init() {}\nvar _ = 1\nvar _ = 2\nfunc _() {}\nfunc _() {}\ntype A struct{}\ntype B struct{}\nfunc (A) M() {}\nfunc (B) M() {}\n",
		"b/a.go":  "package b\nfunc init() {}\n",
		"a.py":    "def f():\n    return 1\n\ndef f():\n    return 2\n",
		"a.js":    "function g() {}\nfunction g() {}\n",
		"a.ts":    "interface I { a: string }\ninterface I { b: string }\nexport function o(a: string): void;\nexport function o(a: any) {}\nfunction g() {}\n",
		"a.tsx":   "export function C() { return null }\n",
		"a.php":   "<?php\nnamespace A { class U {} }\nnamespace B { class U {} }\n",
		"main.tf": "resource \"aws_vpc\" \"main\" {}\nresource \"aws_vpc\" \"main\" {}\nvariable \"v\" {}\n",
	})
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	type decl struct {
		file      source.FileID
		kind      symbol.SymbolKind
		qualified string
		start     source.Position
	}
	byID := map[symbol.SymbolID]decl{}
	total := 0
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			total++
			d := decl{s.Location.File, s.Kind, s.Qualified, s.Location.Range.Start}
			if prev, ok := byID[s.ID]; ok && prev != d {
				t.Errorf("%s: %+v and %+v share an ID", s.ID, prev, d)
			}
			byID[s.ID] = d
			if got, ok := idx.GetSymbol(s.ID); !ok || got.Location != s.Location {
				t.Errorf("GetSymbol(%s) does not return %s@%v", s.ID, s.Qualified, s.Location)
			}
		}
	}
	if len(byID) != total || idx.Stats().Symbols != total {
		t.Errorf("%d IDs for %d declarations (stats %d)", len(byID), total, idx.Stats().Symbols)
	}
}

// FuzzDeclarationIdentity: arbitrary namesake layouts (kind, name and line of
// each draft) in any emission order give the same ID per declaration, and
// distinct declarations distinct IDs.
func FuzzDeclarationIdentity(f *testing.F) {
	f.Add([]byte{0, 1, 0, 2, 1, 1, 0, 1})
	f.Add([]byte{3, 3, 3, 3, 3, 3})
	f.Fuzz(func(t *testing.T, layout []byte) {
		if len(layout) > 40 {
			layout = layout[:40]
		}
		var drafts []language.SymbolDraft
		for i := 0; i+1 < len(layout); i += 2 {
			name := []string{"init", "f", "g"}[int(layout[i])%3]
			kind := []symbol.SymbolKind{symbol.KindFunction, symbol.KindVariable}[int(layout[i]/3)%2]
			line := uint32(layout[i+1]%16) + 1
			drafts = append(drafts, draftAt(name, kind, line, line))
		}
		ids := func(ds []language.SymbolDraft) map[string]symbol.SymbolID {
			idx := buildFakeIndex(t, fakeProvider{symbols: ds})
			out := map[string]symbol.SymbolID{}
			seen := map[symbol.SymbolID]string{}
			for _, s := range idx.SymbolsByFile("x.fake") {
				key := fmt.Sprintf("%s|%s|%d", s.Kind, s.Qualified, s.Location.Range.Start.Line)
				if other, ok := seen[s.ID]; ok && other != key {
					t.Fatalf("%s and %s share %s", other, key, s.ID)
				}
				seen[s.ID] = key
				out[key] = s.ID
			}
			return out
		}
		want := ids(drafts)
		reversed := slices.Clone(drafts)
		slices.Reverse(reversed)
		if got := ids(reversed); !reflect.DeepEqual(got, want) {
			t.Fatalf("emission order changed IDs")
		}
	})
}
