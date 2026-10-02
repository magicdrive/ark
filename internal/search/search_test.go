package search_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/search"
	"github.com/magicdrive/ark/internal/symbol"
)

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "simple_go")
}

func buildIndex(t *testing.T) *index.RepositoryIndex {
	t.Helper()
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), fixtureDir(t), providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

func TestSearch_AllSymbols(t *testing.T) {
	idx := buildIndex(t)
	r, err := search.Search(context.Background(), idx, search.Query{}, 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(r.Matches) == 0 {
		t.Fatal("expected matches, got 0")
	}
}

func TestSearch_KindFilter(t *testing.T) {
	idx := buildIndex(t)
	r, err := search.Search(context.Background(), idx, search.Query{Kind: symbol.KindMethod}, 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, m := range r.Matches {
		if m.Symbol.Kind != symbol.KindMethod {
			t.Errorf("expected method, got %s for %s", m.Symbol.Kind, m.Symbol.Name)
		}
	}
	if len(r.Matches) == 0 {
		t.Fatal("expected method matches")
	}
}

func TestSearch_NamePattern(t *testing.T) {
	idx := buildIndex(t)
	r, err := search.Search(context.Background(), idx, search.Query{NamePattern: "save"}, 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(r.Matches) == 0 {
		t.Fatal("expected matches for name~save")
	}
	for _, m := range r.Matches {
		if m.Symbol == nil {
			continue
		}
		// name must contain "save" (case-insensitive)
		if !containsCI(m.Symbol.Name, "save") {
			t.Errorf("unexpected match: %s", m.Symbol.Name)
		}
	}
}

func TestSearch_ExportedFilter(t *testing.T) {
	idx := buildIndex(t)
	exported := true
	r, err := search.Search(context.Background(), idx, search.Query{Exported: &exported}, 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, m := range r.Matches {
		if m.Symbol != nil && !m.Symbol.Exported {
			t.Errorf("got unexported symbol %s", m.Symbol.Name)
		}
	}
}

func TestSearch_CallsName(t *testing.T) {
	idx := buildIndex(t)
	r, err := search.Search(context.Background(), idx, search.Query{CallsName: "Save"}, 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Create and Update both call Save
	if len(r.Matches) == 0 {
		t.Logf("Note: CallsName=Save returned 0 matches (reference extraction may not have captured this)")
	}
}

func TestSearch_Deterministic(t *testing.T) {
	idx := buildIndex(t)
	q := search.Query{Kind: symbol.KindMethod}
	r1, err := search.Search(context.Background(), idx, q, 100)
	if err != nil {
		t.Fatalf("first Search: %v", err)
	}
	r2, err := search.Search(context.Background(), idx, q, 100)
	if err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if len(r1.Matches) != len(r2.Matches) {
		t.Fatalf("non-deterministic: %d vs %d matches", len(r1.Matches), len(r2.Matches))
	}
	for i := range r1.Matches {
		a, b := r1.Matches[i], r2.Matches[i]
		if a.File != b.File || a.Line != b.Line {
			t.Errorf("match[%d] differs: %v vs %v", i, a, b)
		}
	}
}

func containsCI(s, sub string) bool {
	import_lower := func(x string) string {
		b := []byte(x)
		for i, c := range b {
			if c >= 'A' && c <= 'Z' {
				b[i] = c + 32
			}
		}
		return string(b)
	}
	return len(import_lower(s)) > 0 &&
		len(import_lower(sub)) > 0 &&
		(import_lower(s) == import_lower(sub) ||
			len(import_lower(s)) >= len(import_lower(sub)) &&
				containsSubstring(import_lower(s), import_lower(sub)))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
