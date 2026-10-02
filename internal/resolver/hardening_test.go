package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// ── Ambiguity invariant tests ─────────────────────────────────────────────────

// Same-file duplicate names must yield Candidate, never Exact.
func TestAmbiguity_SameFileDuplicateNames(t *testing.T) {
	sym1 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "pkg/a.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "pkg/a.go"},
	}
	sym2 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "pkg/a.go", symbol.KindMethod, "Bar.Foo"),
		Name: "Foo", Qualified: "Bar.Foo", Kind: symbol.KindMethod, Language: "go",
		Location: source.Location{File: "pkg/a.go"},
	}
	refLoc := source.Location{
		File:  "pkg/a.go",
		Range: source.Range{Start: source.Position{Line: 20}},
	}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "pkg/a.go", reference.KindCall, "Foo", refLoc),
		Name: "Foo", Kind: reference.KindCall, Language: "go", Location: refLoc,
	}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{sym1, sym2}, References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	res := results[0]
	// Multiple candidates in same file must NOT produce Exact.
	if res.Confidence == resolver.ConfidenceExact {
		t.Errorf("multiple same-file candidates must not be Exact, got %s", res.Confidence)
	}
	if res.Confidence != resolver.ConfidenceCandidate {
		t.Errorf("expected Candidate for ambiguous same-file, got %s", res.Confidence)
	}
	if len(res.Candidates) < 2 {
		t.Errorf("expected ≥2 candidates, got %d", len(res.Candidates))
	}
}

// Same-package duplicate names must yield Candidate, never Strong.
func TestAmbiguity_SamePackageDuplicateNames(t *testing.T) {
	sym1 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "pkg/a.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "pkg/a.go"},
	}
	sym2 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "pkg/b.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "pkg/b.go"},
	}
	refLoc := source.Location{
		File:  "pkg/c.go",
		Range: source.Range{Start: source.Position{Line: 5}},
	}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "pkg/c.go", reference.KindCall, "Foo", refLoc),
		Name: "Foo", Kind: reference.KindCall, Language: "go", Location: refLoc,
	}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{sym1}},
		{FileID: "pkg/b.go", Language: "go", Symbols: []symbol.Symbol{sym2}},
		{FileID: "pkg/c.go", Language: "go", References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	res := results[0]
	// Multiple same-package candidates must NOT be Strong or Exact.
	if res.Confidence == resolver.ConfidenceStrong || res.Confidence == resolver.ConfidenceExact {
		t.Errorf("multiple same-package candidates must not be Strong/Exact, got %s", res.Confidence)
	}
	if res.Confidence != resolver.ConfidenceCandidate {
		t.Errorf("expected Candidate, got %s", res.Confidence)
	}
}

// Multiple import candidates must yield Candidate.
func TestAmbiguity_MultipleImportedCandidates(t *testing.T) {
	sym1 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "fmt/print.go", symbol.KindFunction, "Println"),
		Name: "Println", Qualified: "Println", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "fmt/print.go"},
	}
	sym2 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "fmt/format.go", symbol.KindFunction, "Println"),
		Name: "Println", Qualified: "Println", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "fmt/format.go"},
	}
	refLoc := source.Location{
		File:  "pkg/main.go",
		Range: source.Range{Start: source.Position{Line: 10}},
	}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "pkg/main.go", reference.KindCall, "fmt.Println", refLoc),
		Name: "fmt.Println", Kind: reference.KindCall, Language: "go",
		Location: refLoc, ReceiverExpr: "fmt",
	}
	imp := language.ImportDraft{Path: "fmt"}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/main.go", Language: "go", References: []reference.Reference{ref}, Imports: []language.ImportDraft{imp}},
		{FileID: "fmt/print.go", Language: "go", Symbols: []symbol.Symbol{sym1}},
		{FileID: "fmt/format.go", Language: "go", Symbols: []symbol.Symbol{sym2}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	res := results[0]
	if res.Confidence == resolver.ConfidenceExact {
		t.Errorf("multiple imported candidates must not be Exact, got %s", res.Confidence)
	}
	if len(res.Candidates) < 2 {
		t.Errorf("expected ≥2 candidates, got %d", len(res.Candidates))
	}
}

// Method names shared by unrelated types must yield Candidate.
func TestAmbiguity_MethodNameSharedByUnrelatedTypes(t *testing.T) {
	sym1 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "svc/service.go", symbol.KindMethod, "UserService.Save"),
		Name: "Save", Qualified: "UserService.Save", Kind: symbol.KindMethod, Language: "go",
		Location: source.Location{File: "svc/service.go"},
		Receiver: "UserService",
	}
	sym2 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "repo/repository.go", symbol.KindMethod, "UserRepository.Save"),
		Name: "Save", Qualified: "UserRepository.Save", Kind: symbol.KindMethod, Language: "go",
		Location: source.Location{File: "repo/repository.go"},
		Receiver: "UserRepository",
	}
	refLoc := source.Location{
		File:  "main/main.go",
		Range: source.Range{Start: source.Position{Line: 5}},
	}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "main/main.go", reference.KindCall, "Save", refLoc),
		Name: "Save", Kind: reference.KindCall, Language: "go", Location: refLoc,
	}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "svc/service.go", Language: "go", Symbols: []symbol.Symbol{sym1}},
		{FileID: "repo/repository.go", Language: "go", Symbols: []symbol.Symbol{sym2}},
		{FileID: "main/main.go", Language: "go", References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	res := results[0]
	if res.Confidence == resolver.ConfidenceStrong || res.Confidence == resolver.ConfidenceExact {
		t.Errorf("ambiguous method name must not be Strong/Exact, got %s", res.Confidence)
	}
}

// Candidate ordering must be stable across repeated calls.
func TestAmbiguity_CandidateOrderingIsStable(t *testing.T) {
	sym1 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "a/a.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "a/a.go"},
	}
	sym2 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "b/b.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "b/b.go"},
	}
	refLoc := source.Location{
		File:  "c/c.go",
		Range: source.Range{Start: source.Position{Line: 5}},
	}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "c/c.go", reference.KindCall, "Foo", refLoc),
		Name: "Foo", Kind: reference.KindCall, Language: "go", Location: refLoc,
	}
	files := []resolver.FileIndex{
		{FileID: "a/a.go", Language: "go", Symbols: []symbol.Symbol{sym1}},
		{FileID: "b/b.go", Language: "go", Symbols: []symbol.Symbol{sym2}},
		{FileID: "c/c.go", Language: "go", References: []reference.Reference{ref}},
	}
	for i := 0; i < 5; i++ {
		res := resolver.New(files).Resolve()
		if len(res) != 1 {
			t.Fatalf("iter %d: expected 1 result", i)
		}
		cands := res[0].Candidates
		if len(cands) < 2 {
			t.Fatalf("iter %d: expected ≥2 candidates", i)
		}
		if i == 0 {
			continue
		}
		prevRes := resolver.New(files).Resolve()
		prev := prevRes[0].Candidates
		for j := range cands {
			if cands[j].SymbolID != prev[j].SymbolID {
				t.Errorf("iter %d, cand[%d]: non-deterministic order", i, j)
			}
		}
	}
}

// HasUniqueTarget must return false for multiple candidates.
func TestHasUniqueTarget_Multiple(t *testing.T) {
	sym1 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "a/a.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "a/a.go"},
	}
	sym2 := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "b/b.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "b/b.go"},
	}
	refLoc := source.Location{File: "c/c.go", Range: source.Range{Start: source.Position{Line: 5}}}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "c/c.go", reference.KindCall, "Foo", refLoc),
		Name: "Foo", Kind: reference.KindCall, Language: "go", Location: refLoc,
	}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "a/a.go", Language: "go", Symbols: []symbol.Symbol{sym1}},
		{FileID: "b/b.go", Language: "go", Symbols: []symbol.Symbol{sym2}},
		{FileID: "c/c.go", Language: "go", References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1 result")
	}
	if results[0].HasUniqueTarget() {
		t.Error("HasUniqueTarget must be false when multiple candidates exist")
	}
}

// HasUniqueTarget must return true for a single strong match.
func TestHasUniqueTarget_Single(t *testing.T) {
	sym := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "pkg/a.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "pkg/a.go"},
	}
	refLoc := source.Location{File: "pkg/a.go", Range: source.Range{Start: source.Position{Line: 10}}}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "pkg/a.go", reference.KindCall, "Foo", refLoc),
		Name: "Foo", Kind: reference.KindCall, Language: "go", Location: refLoc,
	}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{sym}, References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1 result")
	}
	if !results[0].HasUniqueTarget() {
		t.Errorf("HasUniqueTarget must be true for single Exact match, confidence=%s", results[0].Confidence)
	}
}

// ── Lexical/container identity tests ─────────────────────────────────────────

// Top-level symbol must not be treated as a lexical container match.
func TestContainer_TopLevelNotLexical(t *testing.T) {
	// topFoo is top-level (no parent); ref.Container is "SomeMethod"
	topFoo := symbol.Symbol{
		ID:   symbol.NewSymbolID("go", "pkg/a.go", symbol.KindFunction, "Foo"),
		Name: "Foo", Qualified: "Foo", Kind: symbol.KindFunction, Language: "go",
		Location: source.Location{File: "pkg/a.go"},
		// ParentQualified deliberately empty — top-level symbol
	}
	refLoc := source.Location{
		File:  "pkg/a.go",
		Range: source.Range{Start: source.Position{Line: 20}},
	}
	// Reference inside "SomeMethod" to "Foo"
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "pkg/a.go", reference.KindCall, "Foo", refLoc),
		Name: "Foo", Kind: reference.KindCall, Language: "go", Location: refLoc,
		Container: "SomeMethod", // The reference is inside SomeMethod
	}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{topFoo}, References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1 result")
	}
	res := results[0]
	// Stage 1 should NOT match (topFoo has no parent, ref.Container = "SomeMethod")
	// Stage 2 (same file) should match with ConfidenceExact
	if res.Confidence != resolver.ConfidenceExact {
		t.Errorf("expected Exact via same-file (not same-container), got %s", res.Confidence)
	}
	// The evidence kind must be same_file, not same_lexical_scope
	if len(res.Evidence) > 0 && res.Evidence[0].Kind == "same_lexical_scope" {
		t.Error("top-level symbol incorrectly matched as same_lexical_scope")
	}
}

// Symbol defined in the matching container IS a lexical match.
func TestContainer_NestedSymbolMatchesContainer(t *testing.T) {
	// innerFoo is defined inside "SomeMethod"
	innerFoo := symbol.Symbol{
		ID:              symbol.NewSymbolID("go", "pkg/a.go", symbol.KindFunction, "SomeMethod.Foo"),
		Name:            "Foo",
		Qualified:       "SomeMethod.Foo",
		Kind:            symbol.KindFunction,
		Language:        "go",
		Location:        source.Location{File: "pkg/a.go"},
		ParentQualified: "SomeMethod",
	}
	refLoc := source.Location{
		File:  "pkg/a.go",
		Range: source.Range{Start: source.Position{Line: 20}},
	}
	ref := reference.Reference{
		ID:   reference.NewReferenceID("go", "pkg/a.go", reference.KindCall, "Foo", refLoc),
		Name: "Foo", Kind: reference.KindCall, Language: "go", Location: refLoc,
		Container: "SomeMethod",
	}
	r := resolver.New([]resolver.FileIndex{
		{FileID: "pkg/a.go", Language: "go", Symbols: []symbol.Symbol{innerFoo}, References: []reference.Reference{ref}},
	})
	results := r.Resolve()
	if len(results) != 1 {
		t.Fatalf("expected 1 result")
	}
	res := results[0]
	// Stage 1 should match (innerFoo.ParentQualified == "SomeMethod" == ref.Container)
	if res.Confidence != resolver.ConfidenceExact {
		t.Errorf("expected Exact via lexical scope, got %s", res.Confidence)
	}
	if len(res.Evidence) > 0 && res.Evidence[0].Kind != "same_lexical_scope" {
		t.Errorf("expected same_lexical_scope evidence, got %s", res.Evidence[0].Kind)
	}
}
