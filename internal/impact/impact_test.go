package impact_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/impact"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

func buildIndex(t *testing.T) *index.RepositoryIndex {
	t.Helper()
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), "testdata/repo", providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

func findSymbol(t *testing.T, idx *index.RepositoryIndex, name string) symbol.SymbolID {
	t.Helper()
	syms := idx.FindSymbolsByQualified(name)
	if len(syms) == 0 {
		syms = idx.FindSymbols(name)
	}
	if len(syms) == 0 {
		t.Fatalf("symbol %q not found", name)
	}
	return syms[0].ID
}

func TestAnalyze_DirectDependent(t *testing.T) {
	idx := buildIndex(t)
	g := graph.New(idx)

	// Service.Create calls Repository.Save — Create should appear as direct_dependent of Save.
	targetID := findSymbol(t, idx, "Save")
	result, err := impact.Analyze(context.Background(), idx, g, targetID, 3)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	var found bool
	for _, e := range result.Entries {
		if e.Category == impact.CategoryDirectDependent && e.Symbol.Name == "Create" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Create as direct_dependent of Save; entries: %v", result.Entries)
	}
}

func TestAnalyze_AffectedFiles(t *testing.T) {
	idx := buildIndex(t)
	g := graph.New(idx)

	targetID := findSymbol(t, idx, "Save")
	result, err := impact.Analyze(context.Background(), idx, g, targetID, 3)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(result.AffectedFiles) == 0 {
		t.Error("expected at least one affected file")
	}
	// repository.go must be included (contains the target)
	found := false
	for _, f := range result.AffectedFiles {
		if containsStr(string(f), "repository") {
			found = true
		}
	}
	if !found {
		t.Errorf("repository file not in AffectedFiles: %v", result.AffectedFiles)
	}
}

func TestAnalyze_PossibleDependent_SameName(t *testing.T) {
	idx := buildIndex(t)
	g := graph.New(idx)

	// cache.Cache.Save has the same name "Save" — should appear as possible_dependent
	// only if the resolver returns Candidate confidence (it shares the name).
	targetID := findSymbol(t, idx, "Save")
	result, err := impact.Analyze(context.Background(), idx, g, targetID, 3)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// We just verify no panic and result is valid.
	_ = result
}

func TestAnalyze_Deterministic(t *testing.T) {
	idx := buildIndex(t)
	g := graph.New(idx)
	targetID := findSymbol(t, idx, "Save")

	r1, _ := impact.Analyze(context.Background(), idx, g, targetID, 3)
	r2, _ := impact.Analyze(context.Background(), idx, g, targetID, 3)

	if len(r1.Entries) != len(r2.Entries) {
		t.Fatalf("non-deterministic entry count: %d vs %d", len(r1.Entries), len(r2.Entries))
	}
	for i := range r1.Entries {
		if r1.Entries[i].Symbol.ID != r2.Entries[i].Symbol.ID {
			t.Errorf("entry[%d] differs: %v vs %v", i, r1.Entries[i].Symbol.ID, r2.Entries[i].Symbol.ID)
		}
	}
}

func TestAnalyze_ConfidenceCategory(t *testing.T) {
	idx := buildIndex(t)
	g := graph.New(idx)
	targetID := findSymbol(t, idx, "Save")

	result, _ := impact.Analyze(context.Background(), idx, g, targetID, 3)

	for _, e := range result.Entries {
		if e.Confidence == resolver.ConfidenceCandidate &&
			e.Category == impact.CategoryTransitiveDependent {
			t.Errorf("candidate-confidence entry incorrectly labelled transitive_dependent: %v", e)
		}
	}
}

func TestAnalyze_Test_Category(t *testing.T) {
	idx := buildIndex(t)
	g := graph.New(idx)

	targetID := findSymbol(t, idx, "Save")
	result, _ := impact.Analyze(context.Background(), idx, g, targetID, 3)

	// TestCreate in service_test.go: might appear as test category if resolver connects it.
	// We verify at minimum: if any test entry exists, it comes from a _test.go file.
	for _, e := range result.Entries {
		if e.Category == impact.CategoryTest {
			if !containsStr(string(e.Symbol.Location.File), "_test") &&
				!containsStr(string(e.Symbol.Location.File), "test_") {
				t.Errorf("test category entry not from test file: %v", e.Symbol.Location.File)
			}
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
