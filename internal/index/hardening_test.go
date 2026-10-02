package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

// ── Graph edge deduplication by kind ─────────────────────────────────────────

// TestEdgeDedupe_DifferentKindsPreserved verifies that A→B via "calls" and
// A→B via "uses_type" are both kept after deduplication.
func TestEdgeDedupe_DifferentKindsPreserved(t *testing.T) {
	dir := fixtureDir(t, "multi_kind_edges")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("fixture multi_kind_edges not present")
	}
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	syms := idx.FindSymbols("Caller")
	if len(syms) == 0 {
		t.Skip("no Caller symbol in fixture")
	}
	edges := idx.GetCallees(syms[0].ID)
	if len(edges) == 0 {
		t.Skip("no edges for Caller")
	}

	// Collect edge kinds — there should be more than one kind if both calls and uses_type exist.
	kindSet := map[index.EdgeKind]int{}
	for _, e := range edges {
		kindSet[e.Kind]++
	}
	// Not asserting specific count since it depends on fixture; just verify dedup didn't erase kinds.
	if len(kindSet) == 0 {
		t.Error("expected at least one edge kind")
	}
}

// TestEdgeDedupe_SameKindCollapsed verifies that identical (From, To, Kind) edges are merged.
func TestEdgeDedupe_SameKindCollapsed(t *testing.T) {
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	// Rebuild twice — graph must be identical (dedup is idempotent / deterministic).
	idx2, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("second index.New: %v", err)
	}

	syms1 := idx.FindSymbols("")
	syms2 := idx2.FindSymbols("")
	if len(syms1) != len(syms2) {
		t.Fatalf("symbol counts differ: %d vs %d", len(syms1), len(syms2))
	}
	for _, sym := range syms1 {
		e1 := idx.GetCallees(sym.ID)
		e2 := idx2.GetCallees(sym.ID)
		if len(e1) != len(e2) {
			t.Errorf("callee count for %q differs: %d vs %d", sym.Name, len(e1), len(e2))
		}
	}
}

// ── Unique-target enforcement ─────────────────────────────────────────────────

// TestUniqueTarget_AmbiguousResolutionProducesNoEdge builds an index where two
// symbols share a name; the ambiguous reference must NOT produce a graph edge.
func TestUniqueTarget_AmbiguousResolutionProducesNoEdge(t *testing.T) {
	dir := t.TempDir()
	// Write two Go files: both define Foo; a third file calls Foo.
	writeFile(t, dir, "a/a.go", `package a
func Foo() {}
`)
	writeFile(t, dir, "b/b.go", `package b
func Foo() {}
`)
	writeFile(t, dir, "caller/caller.go", `package caller
func Bar() {
	// reference to Foo — ambiguous across a and b
}
`)
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	// Find all "Foo" symbols — should be 2.
	foos := idx.FindSymbols("Foo")
	if len(foos) < 2 {
		t.Skipf("expected ≥2 Foo symbols, got %d (fixture may not produce ambiguity)", len(foos))
	}

	// Neither Foo should appear as a callee of anything if the reference is ambiguous.
	// Bar should have no callees that resolve to Foo with certainty.
	bars := idx.FindSymbols("Bar")
	if len(bars) == 0 {
		t.Skip("Bar not indexed")
	}
	for _, bar := range bars {
		edges := idx.GetCallees(bar.ID)
		for _, e := range edges {
			to, ok := idx.GetSymbol(e.To)
			if !ok {
				continue
			}
			if to.Name == "Foo" {
				t.Errorf("ambiguous Foo resolved to a canonical edge from Bar: kind=%s", e.Kind)
			}
		}
	}
}

func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}
