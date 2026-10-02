package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// shadowing: symbol defined in same file beats same-package symbol.
func TestShadowing(t *testing.T) {
	localSave := symbol.Symbol{
		ID:        symbol.NewSymbolID("go", "pkg/a.go", symbol.KindFunction, "Save"),
		Name:      "Save",
		Qualified: "Save",
		Kind:      symbol.KindFunction,
		Language:  "go",
		Location:  source.Location{File: "pkg/a.go", Range: source.Range{Start: source.Position{Line: 1, Column: 1}, End: source.Position{Line: 5, Column: 1}}},
	}
	pkgSave := symbol.Symbol{
		ID:        symbol.NewSymbolID("go", "pkg/b.go", symbol.KindFunction, "Save"),
		Name:      "Save",
		Qualified: "Save",
		Kind:      symbol.KindFunction,
		Language:  "go",
		Location:  source.Location{File: "pkg/b.go", Range: source.Range{Start: source.Position{Line: 1, Column: 1}, End: source.Position{Line: 5, Column: 1}}},
	}

	callLoc := source.Location{
		File:  "pkg/a.go",
		Range: source.Range{Start: source.Position{Line: 20, Column: 2}, End: source.Position{Line: 20, Column: 8}},
	}
	ref := reference.Reference{
		ID:       reference.NewReferenceID("go", "pkg/a.go", reference.KindCall, "Save", callLoc),
		Name:     "Save",
		Kind:     reference.KindCall,
		Language: "go",
		Location: callLoc,
		IsCall:   true,
	}

	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{localSave}, References: []reference.Reference{ref}},
		{FileID: "pkg/b.go", Language: "go", Symbols: []symbol.Symbol{pkgSave}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	// Same-file match wins over same-package.
	if results[0].Confidence != resolver.ConfidenceExact {
		t.Errorf("expected Exact (same file wins), got %s", results[0].Confidence)
	}
	if results[0].Candidates[0].File != "pkg/a.go" {
		t.Errorf("expected same-file candidate, got %s", results[0].Candidates[0].File)
	}
}

// unrelated same-name: symbol in a different package with same name → Candidate only.
func TestUnrelatedSameName(t *testing.T) {
	sym1 := symbol.Symbol{
		ID:        symbol.NewSymbolID("go", "pkg/a.go", symbol.KindFunction, "Handle"),
		Name:      "Handle",
		Qualified: "Handle",
		Kind:      symbol.KindFunction,
		Language:  "go",
		Location:  source.Location{File: "pkg/a.go"},
	}
	sym2 := symbol.Symbol{
		ID:        symbol.NewSymbolID("go", "other/b.go", symbol.KindFunction, "Handle"),
		Name:      "Handle",
		Qualified: "Handle",
		Kind:      symbol.KindFunction,
		Language:  "go",
		Location:  source.Location{File: "other/b.go"},
	}
	refLoc := source.Location{
		File:  "third/c.go",
		Range: source.Range{Start: source.Position{Line: 5, Column: 1}},
	}
	ref := reference.Reference{
		ID:       reference.NewReferenceID("go", "third/c.go", reference.KindCall, "Handle", refLoc),
		Name:     "Handle",
		Kind:     reference.KindCall,
		Language: "go",
		Location: refLoc,
		IsCall:   true,
	}

	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{sym1}},
		{FileID: "other/b.go", Language: "go", Symbols: []symbol.Symbol{sym2}},
		{FileID: "third/c.go", Language: "go", References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Confidence != resolver.ConfidenceCandidate {
		t.Errorf("expected Candidate for cross-package ambiguity, got %s", results[0].Confidence)
	}
	if len(results[0].Candidates) < 2 {
		t.Errorf("expected ≥2 candidates, got %d", len(results[0].Candidates))
	}
}
