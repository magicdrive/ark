package index_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

// TestStats_LanguagesMapIsIsolated verifies that mutating the map returned by
// Stats() does not affect the internal state of the index.
func TestStats_LanguagesMapIsIsolated(t *testing.T) {
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	s1 := idx.Stats()
	before := s1.Languages["go"]

	// Mutate the returned map.
	s1.Languages["go"] = 99999

	s2 := idx.Stats()
	if s2.Languages["go"] != before {
		t.Errorf("internal Languages map was mutated: want %d, got %d", before, s2.Languages["go"])
	}
}

// TestGetCallees_EvidenceIsIsolated verifies that mutating a returned edge's
// Evidence slice does not affect the index.
func TestGetCallees_EvidenceIsIsolated(t *testing.T) {
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	// Find any symbol that has callees.
	syms := idx.FindSymbols("")
	for _, sym := range syms {
		edges := idx.GetCallees(sym.ID)
		if len(edges) == 0 {
			continue
		}
		if len(edges[0].Evidence) == 0 {
			continue
		}
		// Remember original evidence.
		original := edges[0].Evidence[0]

		// Mutate the returned slice's element.
		edges[0].Evidence[0].Detail = "MUTATED"

		// Re-fetch and verify the index is unchanged.
		edges2 := idx.GetCallees(sym.ID)
		if len(edges2) > 0 && len(edges2[0].Evidence) > 0 {
			if edges2[0].Evidence[0].Detail != original.Detail {
				t.Error("internal Evidence slice was mutated through returned GetCallees result")
			}
		}
		return
	}
	t.Skip("no symbol with callees+evidence found in fixture")
}

// TestGetCallers_EvidenceIsIsolated is the symmetric test for GetCallers.
func TestGetCallers_EvidenceIsIsolated(t *testing.T) {
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	syms := idx.FindSymbols("")
	for _, sym := range syms {
		edges := idx.GetCallers(sym.ID)
		if len(edges) == 0 {
			continue
		}
		if len(edges[0].Evidence) == 0 {
			continue
		}
		original := edges[0].Evidence[0]
		edges[0].Evidence[0].Detail = "MUTATED"

		edges2 := idx.GetCallers(sym.ID)
		if len(edges2) > 0 && len(edges2[0].Evidence) > 0 {
			if edges2[0].Evidence[0].Detail != original.Detail {
				t.Error("internal Evidence slice was mutated through returned GetCallers result")
			}
		}
		return
	}
	t.Skip("no symbol with callers+evidence found in fixture")
}
