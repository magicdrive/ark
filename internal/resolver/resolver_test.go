package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func pos(line, col uint32) source.Position { return source.Position{Line: line, Column: col} }

func loc(file, sl, sc, el, ec uint32) source.Location {
	return source.Location{
		File: source.FileID(file2path(file)),
		Range: source.Range{
			Start: pos(sl, sc),
			End:   pos(el, ec),
		},
	}
}

func file2path(n uint32) string {
	switch n {
	case 1:
		return "pkg/a.go"
	case 2:
		return "pkg/b.go"
	case 3:
		return "other/c.go"
	default:
		return "unknown.go"
	}
}

func makeSymbol(name, qualified string, kind symbol.SymbolKind, fileN uint32) symbol.Symbol {
	l := loc(fileN, 1, 1, 5, 1)
	return symbol.Symbol{
		ID:        symbol.NewSymbolID("go", string(l.File), kind, qualified),
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Language:  "go",
		Location:  l,
		Exported:  true,
	}
}

func makeRef(name, container string, kind reference.ReferenceKind, fileN uint32, receiver string) reference.Reference {
	l := loc(fileN, 10, 5, 10, 15)
	return reference.Reference{
		ID:           reference.NewReferenceID("go", l.File, kind, name, l),
		Name:         name,
		Kind:         kind,
		Language:     "go",
		Location:     l,
		Container:    container,
		ReceiverExpr: receiver,
		IsCall:       kind == reference.KindCall,
	}
}

// Test 1: same file call → ConfidenceExact
func TestSameFileMatch(t *testing.T) {
	save := makeSymbol("Save", "Save", symbol.KindFunction, 1)
	ref := makeRef("Save", "", reference.KindCall, 1, "")

	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{save}, References: []reference.Reference{ref}},
	})
	results := r.Resolve()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Confidence != resolver.ConfidenceExact {
		t.Errorf("expected Exact, got %s", res.Confidence)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Name != "Save" {
		t.Errorf("unexpected candidates: %v", res.Candidates)
	}
}

// Test 2: same-package call (different file, same directory) → ConfidenceStrong
func TestSamePackageMatch(t *testing.T) {
	save := makeSymbol("Save", "Save", symbol.KindFunction, 2) // pkg/b.go
	ref := makeRef("Save", "", reference.KindCall, 1, "")       // pkg/a.go

	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", References: []reference.Reference{ref}},
		{FileID: "pkg/b.go", Language: "go", Symbols: []symbol.Symbol{save}},
	})
	results := r.Resolve()

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Confidence != resolver.ConfidenceStrong {
		t.Errorf("expected Strong, got %s", results[0].Confidence)
	}
}

// Test 3: explicit import alias match → ConfidenceExact
func TestExplicitImportMatch(t *testing.T) {
	println := makeSymbol("Println", "Println", symbol.KindFunction, 3)
	println.Location.File = "fmt/print.go"
	// Override FileID
	fi2 := resolver.FileIndex{
		FileID:   "fmt/print.go",
		Language: "go",
		Symbols:  []symbol.Symbol{println},
	}

	ref := reference.Reference{
		ID:           "ref1",
		Name:         "fmt.Println",
		Kind:         reference.KindCall,
		Language:     "go",
		Location:     loc(1, 10, 1, 10, 15),
		ReceiverExpr: "fmt",
		IsCall:       true,
	}
	imp := language.ImportDraft{Path: "fmt"}

	fi1 := resolver.FileIndex{
		FileID:     "pkg/a.go",
		Language:   "go",
		References: []reference.Reference{ref},
		Imports:    []language.ImportDraft{imp},
	}

	r := resolver.New([]resolver.FileIndex{fi1, fi2})
	results := r.Resolve()

	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Confidence != resolver.ConfidenceExact {
		t.Errorf("expected Exact via import, got %s", results[0].Confidence)
	}
}

// Test 4: name collision across files in different packages → ConfidenceCandidate, multiple candidates
func TestNameCollision(t *testing.T) {
	save1 := makeSymbol("Save", "Save", symbol.KindFunction, 1) // pkg/a.go
	save2 := makeSymbol("Save", "Save", symbol.KindFunction, 3) // other/c.go

	// ref is in a third, unrelated directory so same-package stage finds nothing.
	refLoc := source.Location{
		File:  "third/d.go",
		Range: source.Range{Start: source.Position{Line: 10, Column: 5}, End: source.Position{Line: 10, Column: 15}},
	}
	ref := reference.Reference{
		ID:       reference.NewReferenceID("go", "third/d.go", reference.KindCall, "Save", refLoc),
		Name:     "Save",
		Kind:     reference.KindCall,
		Language: "go",
		Location: refLoc,
		IsCall:   true,
	}

	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{save1}},
		{FileID: "other/c.go", Language: "go", Symbols: []symbol.Symbol{save2}},
		{FileID: "third/d.go", Language: "go", References: []reference.Reference{ref}},
	})
	results := r.Resolve()

	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	res := results[0]
	if res.Confidence != resolver.ConfidenceCandidate {
		t.Errorf("expected Candidate for ambiguous name, got %s", res.Confidence)
	}
	if len(res.Candidates) < 2 {
		t.Errorf("expected ≥2 candidates, got %d", len(res.Candidates))
	}
}

// Test 5: no matching symbol → ConfidenceUnresolved
func TestUnresolved(t *testing.T) {
	ref := makeRef("GhostFunc", "", reference.KindCall, 1, "")
	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", References: []reference.Reference{ref}},
	})
	results := r.Resolve()

	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Confidence != resolver.ConfidenceUnresolved {
		t.Errorf("expected Unresolved, got %s", results[0].Confidence)
	}
}

// Test 6: determinism — two identical calls produce identical results.
func TestDeterminism(t *testing.T) {
	save := makeSymbol("Save", "Save", symbol.KindFunction, 1)
	ref1 := makeRef("Save", "", reference.KindCall, 2, "")
	ref2 := makeRef("Save", "", reference.KindCall, 3, "")

	files := []resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{save}},
		{FileID: "pkg/b.go", Language: "go", References: []reference.Reference{ref1}},
		{FileID: "other/c.go", Language: "go", References: []reference.Reference{ref2}},
	}

	r1 := resolver.New(files)
	r2 := resolver.New(files)
	res1 := r1.Resolve()
	res2 := r2.Resolve()

	if len(res1) != len(res2) {
		t.Fatalf("non-deterministic length: %d vs %d", len(res1), len(res2))
	}
	for i := range res1 {
		if res1[i].ReferenceID != res2[i].ReferenceID {
			t.Errorf("result[%d] ReferenceID mismatch", i)
		}
		if res1[i].Confidence != res2[i].Confidence {
			t.Errorf("result[%d] Confidence mismatch", i)
		}
	}
}
