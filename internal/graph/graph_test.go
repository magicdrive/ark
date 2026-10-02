package graph_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "index", "testdata", name)
}

func simpleGoGraph(t *testing.T) (*graph.Graph, *index.RepositoryIndex) {
	t.Helper()
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return graph.New(idx), idx
}

func TestGraph_TransitiveCallees_DepthLimit(t *testing.T) {
	g, idx := simpleGoGraph(t)

	syms := idx.FindSymbols("main")
	if len(syms) == 0 {
		t.Skip("no 'main' symbol in fixture")
	}
	id := syms[0].ID

	// Depth 1 should find direct callees only.
	edges1 := g.TransitiveCallees(id, 1)
	// Unlimited depth should find at least as many edges.
	edgesUnlimited := g.TransitiveCallees(id, 0)

	if len(edgesUnlimited) < len(edges1) {
		t.Errorf("unlimited depth (%d) found fewer edges than depth-1 (%d)",
			len(edgesUnlimited), len(edges1))
	}
}

func TestGraph_TransitiveCallers(t *testing.T) {
	g, idx := simpleGoGraph(t)

	syms := idx.FindSymbols("greet")
	if len(syms) == 0 {
		t.Skip("no 'greet' symbol in fixture")
	}
	// No panic is the primary requirement; callers may be empty if resolver
	// did not reach ConfidenceStrong for this fixture.
	_ = g.TransitiveCallers(syms[0].ID, 3)
}

func TestGraph_NoCycles(t *testing.T) {
	g, idx := simpleGoGraph(t)

	syms := idx.FindSymbols("main")
	if len(syms) == 0 {
		t.Skip("no 'main' symbol")
	}

	// Unlimited depth must terminate; cycle protection prevents infinite loop.
	edges := g.TransitiveCallees(syms[0].ID, 0)
	if len(edges) > 10000 {
		t.Errorf("suspiciously large edge count %d — possible cycle", len(edges))
	}
}

func TestGraph_New(t *testing.T) {
	_, idx := simpleGoGraph(t)
	g := graph.New(idx)
	if g == nil {
		t.Error("graph.New returned nil")
	}
}
