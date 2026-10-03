package mcp

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

func tsxRepoDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "tsx_repo")
}

// TestIndexHandlesTSX is the Q5 regression test: .tsx files must flow through
// the SAME indexing/relations path as every other supported language, i.e.
// through defaultProviders(). Before Q5 this failed because defaultProviders()
// excluded tsx.
func TestIndexHandlesTSX(t *testing.T) {
	dir := tsxRepoDir(t)
	idx, err := index.New(context.Background(), dir, defaultProviders())
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	if n := idx.Stats().Languages["tsx"]; n == 0 {
		t.Fatalf("no .tsx files indexed via defaultProviders(); Languages=%v", idx.Stats().Languages)
	}

	var tsxSymbols int
	for _, s := range idx.FindSymbols("") {
		if s.Language == "tsx" {
			tsxSymbols++
		}
	}
	if tsxSymbols == 0 {
		t.Fatal("index produced no tsx-language symbols")
	}

	// Relations/graph must also work for tsx: App calls helper (same file).
	syms := idx.FindSymbolsByQualified("App")
	if len(syms) == 0 {
		t.Fatal("symbol App not found in tsx fixture")
	}
	callees := idx.GetCallees(syms[0].ID)
	foundHelper := false
	for _, e := range callees {
		if to, ok := idx.GetSymbol(e.To); ok && to.Qualified == "helper" {
			foundHelper = true
		}
	}
	if !foundHelper {
		t.Errorf("expected tsx edge App -> helper; callees=%v", callees)
	}
}

// TestTSXUnifiedAcrossTools asserts the Q5 end state: tsx is handled uniformly
// by both the indexing/relations path (defaultProviders) and find_references
// (refProviderRegistry). Replaces the PR-1 compatibility lock.
func TestTSXUnifiedAcrossTools(t *testing.T) {
	hasTSX := false
	for _, p := range defaultProviders() {
		if p.Language() == "tsx" {
			hasTSX = true
		}
	}
	if !hasTSX {
		t.Error("defaultProviders() must include tsx")
	}
	if _, ok := refProviderRegistry[".tsx"]; !ok {
		t.Error("find_references must handle .tsx")
	}
}
