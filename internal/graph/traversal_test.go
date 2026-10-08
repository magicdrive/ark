package graph_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// specProvider turns "From->To[:kind[:strong]]" specs into one file of symbols and
// same-file references (Exact edges), so traversal is tested on graphs of
// any shape and edge kind, independent of a real language.
type specProvider struct{ specs []string }

func (specProvider) Language() language.Language { return "spec" }
func (specProvider) Extensions() []string        { return []string{".spec"} }
func (specProvider) CacheVersion() string        { return "t" }
func (p specProvider) Extract(_ context.Context, file source.FileID, _ []byte) (language.Extraction, error) {
	loc := func(line uint32) source.Location {
		return source.Location{File: file, Range: source.Range{Start: source.Position{Line: line, Column: 1}, End: source.Position{Line: line, Column: 2}}}
	}
	var ex language.Extraction
	seen := map[string]bool{}
	addSym := func(n string) {
		if !seen[n] {
			seen[n] = true
			ex.Symbols = append(ex.Symbols, language.SymbolDraft{Name: n, Qualified: n, Kind: symbol.KindFunction, Location: loc(1), EndByte: 1})
		}
	}
	for i, sp := range p.specs {
		edge, rest, _ := strings.Cut(sp, ":")
		kind, capConf, _ := strings.Cut(rest, ":")
		if kind == "" {
			kind = "call"
		}
		from, to, _ := strings.Cut(edge, "->")
		addSym(from)
		addSym(to)
		// A "strong" cap lowers the same-file Exact resolution to Strong.
		ex.References = append(ex.References, language.ReferenceDraft{Name: to, Kind: kind, Container: from, Location: loc(uint32(i + 2)), ConfidenceCap: capConf})
	}
	addSym("Isolated")
	return ex, nil
}

func specGraph(t *testing.T, specs ...string) (*graph.Graph, *index.RepositoryIndex) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "g.spec"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{specProvider{specs}})
	if err != nil {
		t.Fatal(err)
	}
	return graph.New(idx), idx
}

func id(t *testing.T, idx *index.RepositoryIndex, name string) symbol.SymbolID {
	t.Helper()
	s := idx.FindSymbolsByQualified(name)
	if len(s) != 1 {
		t.Fatalf("%s: %d symbols", name, len(s))
	}
	return s[0].ID
}

// reached renders each reached symbol with its depth, in result order.
func reached(idx *index.RepositoryIndex, hops []graph.Hop) []string {
	var out []string
	for _, h := range hops {
		s, _ := idx.GetSymbol(h.Edge.To)
		out = append(out, fmt.Sprintf("%s@%d", s.Qualified, h.Depth))
	}
	return out
}

func names(idx *index.RepositoryIndex, edges []index.GraphEdge) []string {
	var out []string
	for _, e := range edges {
		s, _ := idx.GetSymbol(e.To)
		out = append(out, s.Qualified)
	}
	return out
}

func eq(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestTraversal_Chain(t *testing.T) {
	g, idx := specGraph(t, "A->B", "B->C", "C->D", "D->E")
	eq(t, "callees(A, all)", names(idx, g.TransitiveCallees(id(t, idx, "A"), 0)), []string{"B", "C", "D", "E"})
	eq(t, "callees(A, 1)", names(idx, g.TransitiveCallees(id(t, idx, "A"), 1)), []string{"B"})
	eq(t, "callees(A, 2)", names(idx, g.TransitiveCallees(id(t, idx, "A"), 2)), []string{"B", "C"})
	eq(t, "callers(D, all)", names(idx, g.TransitiveCallers(id(t, idx, "D"), 0)), []string{"C", "B", "A"})
	eq(t, "callers(C, all)", names(idx, g.TransitiveCallers(id(t, idx, "C"), 0)), []string{"B", "A"})
	eq(t, "callers(E, 1)", names(idx, g.TransitiveCallers(id(t, idx, "E"), 1)), []string{"D"})
	eq(t, "callers(E, 2)", names(idx, g.TransitiveCallers(id(t, idx, "E"), 2)), []string{"D", "C"})
	eq(t, "callers(E, 10)", names(idx, g.TransitiveCallers(id(t, idx, "E"), 10)), []string{"D", "C", "B", "A"})
	eq(t, "caller hops(E)", reached(idx, g.TransitiveCallerHops(id(t, idx, "E"), 0)), []string{"D@1", "C@2", "B@3", "A@4"})
	eq(t, "callers(A)", names(idx, g.TransitiveCallers(id(t, idx, "A"), 0)), nil)
	for _, e := range g.TransitiveCallers(id(t, idx, "D"), 0) {
		if e.Kind != index.EdgeCalledBy {
			t.Errorf("caller traversal returned a %s edge", e.Kind)
		}
	}
}

func TestTraversal_DiamondReachesEachSymbolOnce(t *testing.T) {
	g, idx := specGraph(t, "A->B", "A->C", "B->D", "C->D")
	eq(t, "callers(D)", reached(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0)), []string{"B@1", "C@1", "A@2"})
	eq(t, "callees(A)", names(idx, g.TransitiveCallees(id(t, idx, "A"), 0)), []string{"B", "C", "D"})
}

func TestTraversal_CycleTerminatesWithoutTheStart(t *testing.T) {
	g, idx := specGraph(t, "A->B", "B->C", "C->A")
	eq(t, "callers(A)", names(idx, g.TransitiveCallers(id(t, idx, "A"), 0)), []string{"C", "B"})
	eq(t, "callees(A)", names(idx, g.TransitiveCallees(id(t, idx, "A"), 0)), []string{"B", "C"})
	g2, idx2 := specGraph(t, "A->A", "B->A")
	eq(t, "self loop callers(A)", names(idx2, g2.TransitiveCallers(id(t, idx2, "A"), 0)), []string{"B"})
}

func TestTraversal_FollowsEveryEdgeKind(t *testing.T) {
	g, idx := specGraph(t, "A->B:type_use", "B->C:value_reference", "C->D:depends_on", "D->E:inheritance")
	eq(t, "callers(E)", reached(idx, g.TransitiveCallerHops(id(t, idx, "E"), 0)), []string{"D@1", "C@2", "B@3", "A@4"})
	eq(t, "callees(A)", names(idx, g.TransitiveCallees(id(t, idx, "A"), 0)), []string{"B", "C", "D", "E"})
}

func TestTraversal_UnknownAndIsolatedSymbols(t *testing.T) {
	g, idx := specGraph(t, "A->B")
	if got := g.TransitiveCallers("no-such-symbol", 0); len(got) != 0 {
		t.Errorf("unknown symbol: %v", got)
	}
	if got := g.TransitiveCallees(id(t, idx, "Isolated"), 0); len(got) != 0 {
		t.Errorf("isolated symbol: %v", got)
	}
}

func TestTraversal_Deterministic(t *testing.T) {
	specs := []string{"A->D", "B->D", "C->D", "E->A", "F->B", "E->C", "G->E", "G->F"}
	g, idx := specGraph(t, specs...)
	want := reached(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0))
	for i := 0; i < 20; i++ {
		g2, idx2 := specGraph(t, specs...)
		if got := reached(idx2, g2.TransitiveCallerHops(id(t, idx2, "D"), 0)); !slices.Equal(got, want) {
			t.Fatalf("run %d: %v, want %v", i, got, want)
		}
	}
	// Breadth-first: every depth-1 caller before any depth-2 one; within a
	// depth the index's edge order (by SymbolID) decides.
	got := slices.Clone(want)
	slices.Sort(got)
	eq(t, "callers(D) as a set", got, []string{"A@1", "B@1", "C@1", "E@2", "F@2", "G@3"})
	depth := func(h string) byte { return h[len(h)-1] }
	for i := 1; i < len(want); i++ {
		if depth(want[i]) < depth(want[i-1]) {
			t.Errorf("not breadth-first: %v", want)
		}
	}
}

// confs renders each reached symbol with its depth and path confidence.
func confs(idx *index.RepositoryIndex, hops []graph.Hop) []string {
	var out []string
	for _, h := range hops {
		s, _ := idx.GetSymbol(h.Edge.To)
		out = append(out, fmt.Sprintf("%s@%d:%s", s.Qualified, h.Depth, h.Confidence))
	}
	slices.Sort(out)
	return out
}

func TestPathConfidence_IsTheWeakestEdge(t *testing.T) {
	g, idx := specGraph(t, "A->B", "B->C", "C->D")
	eq(t, "exact chain", confs(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0)), []string{"A@3:exact", "B@2:exact", "C@1:exact"})

	g, idx = specGraph(t, "A->B::strong", "B->C", "C->D")
	eq(t, "strong at the far end", confs(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0)), []string{"A@3:strong", "B@2:exact", "C@1:exact"})

	g, idx = specGraph(t, "A->B", "B->C::strong", "C->D")
	eq(t, "strong in the middle", confs(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0)), []string{"A@3:strong", "B@2:strong", "C@1:exact"})

	g, idx = specGraph(t, "A->B::strong", "B->C::strong", "C->D")
	eq(t, "two strong", confs(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0)), []string{"A@3:strong", "B@2:strong", "C@1:exact"})

	// Callee direction: the same rule.
	g, idx = specGraph(t, "A->B", "B->C::strong", "C->D")
	callees := g.TransitiveCallees(id(t, idx, "A"), 0)
	if len(callees) != 3 {
		t.Fatalf("callees %v", callees)
	}
}

// Of several shortest paths the strongest decides, whatever the traversal
// order; a longer path never replaces a shorter one, however strong.
func TestPathConfidence_MultiplePaths(t *testing.T) {
	for _, specs := range [][]string{
		{"A->B::strong", "B->D", "A->C", "C->D"},
		{"A->C", "C->D", "A->B::strong", "B->D"},
		{"A->B", "B->D::strong", "A->C", "C->D"},
	} {
		g, idx := specGraph(t, specs...)
		eq(t, fmt.Sprint("same distance ", specs), confs(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0)),
			[]string{"A@2:exact", "B@1:" + map[bool]string{true: "strong", false: "exact"}[specs[1] == "B->D::strong"], "C@1:exact"})
	}
	g, idx := specGraph(t, "A->D::strong", "A->B", "B->D")
	eq(t, "shortest wins", confs(idx, g.TransitiveCallerHops(id(t, idx, "D"), 0)), []string{"A@1:strong", "B@1:exact"})

	// Two paths of length 3 from A: the Exact one is chosen.
	g, idx = specGraph(t, "A->B::strong", "B->C", "C->E", "A->D", "D->F", "F->E")
	eq(t, "equal lengths", confs(idx, g.TransitiveCallerHops(id(t, idx, "E"), 0)),
		[]string{"A@3:exact", "B@2:exact", "C@1:exact", "D@2:exact", "F@1:exact"})
	// A Strong path of length 3 and an Exact one of length 4: distance stays
	// 3, so the Strong path decides.
	g, idx = specGraph(t, "A->B::strong", "B->C", "C->E", "A->D", "D->G", "G->F", "F->E")
	eq(t, "different lengths", confs(idx, g.TransitiveCallerHops(id(t, idx, "E"), 0)),
		[]string{"A@3:strong", "B@2:exact", "C@1:exact", "D@3:exact", "F@1:exact", "G@2:exact"})
}

func TestPathConfidence_ParallelEdges(t *testing.T) {
	// Two edges B->A of different kinds: the stronger one carries the hop.
	g, idx := specGraph(t, "B->A::strong", "B->A:type_use", "C->B")
	eq(t, "parallel", confs(idx, g.TransitiveCallerHops(id(t, idx, "A"), 0)), []string{"B@1:exact", "C@2:exact"})
	g, idx = specGraph(t, "B->A::strong", "B->A:type_use:strong", "C->B")
	eq(t, "parallel both strong", confs(idx, g.TransitiveCallerHops(id(t, idx, "A"), 0)), []string{"B@1:strong", "C@2:strong"})
}

func TestPathConfidence_CyclesAndDepth(t *testing.T) {
	g, idx := specGraph(t, "A->B::strong", "B->C", "C->A", "C->C")
	eq(t, "cycle", confs(idx, g.TransitiveCallerHops(id(t, idx, "C"), 0)), []string{"A@2:strong", "B@1:exact"})
	g, idx = specGraph(t, "A->B::strong", "B->C", "C->D", "D->E")
	for depth, want := range map[int][]string{
		1: {"D@1:exact"},
		2: {"C@2:exact", "D@1:exact"},
		3: {"B@3:exact", "C@2:exact", "D@1:exact"},
		0: {"A@4:strong", "B@3:exact", "C@2:exact", "D@1:exact"},
	} {
		eq(t, fmt.Sprint("depth ", depth), confs(idx, g.TransitiveCallerHops(id(t, idx, "E"), depth)), want)
	}
	// The chosen edge belongs to the chosen path: its own confidence is never
	// above the path's, and its To is the reached symbol.
	for _, h := range g.TransitiveCallerHops(id(t, idx, "E"), 0) {
		if h.Edge.Confidence < h.Confidence {
			t.Errorf("hop %+v: edge weaker than its path", h)
		}
	}
}

// Among equally strong paths the first in traversal order is kept: the
// chosen edge comes from the first-reached symbol of the previous layer.
func TestPathConfidence_TieBreakIsFirstReached(t *testing.T) {
	g, idx := specGraph(t, "A->B", "A->C", "B->D", "C->D")
	hops := g.TransitiveCallerHops(id(t, idx, "D"), 0)
	if len(hops) != 3 {
		t.Fatalf("hops %v", hops)
	}
	first := hops[0].Edge.To // B or C, whichever the index orders first
	if hops[2].Edge.From != first {
		t.Errorf("A reached through %s, want the first-reached %s", hops[2].Edge.From, first)
	}
}
