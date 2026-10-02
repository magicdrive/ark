package index_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

func benchmarkProviders() []language.Provider {
	return []language.Provider{golang.NewProvider()}
}

func fixtureDirB(b *testing.B, name string) string {
	b.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func buildIndex(b *testing.B, fixture string) *index.RepositoryIndex {
	b.Helper()
	dir := fixtureDirB(b, fixture)
	idx, err := index.New(context.Background(), dir, benchmarkProviders())
	if err != nil {
		b.Fatalf("index.New: %v", err)
	}
	return idx
}

// BenchmarkNew measures the cost of scanning and indexing a small fixture.
func BenchmarkNew(b *testing.B) {
	dir := fixtureDirB(b, "simple_go")
	providers := benchmarkProviders()
	b.ResetTimer()
	for range b.N {
		_, err := index.New(context.Background(), dir, providers)
		if err != nil {
			b.Fatalf("index.New: %v", err)
		}
	}
}

// BenchmarkFindSymbols measures the cost of a symbol lookup by name.
func BenchmarkFindSymbols(b *testing.B) {
	idx := buildIndex(b, "simple_go")
	b.ResetTimer()
	for range b.N {
		_ = idx.FindSymbols("greet")
	}
}

// BenchmarkFindSymbolsAll measures the cost of listing all symbols.
func BenchmarkFindSymbolsAll(b *testing.B) {
	idx := buildIndex(b, "simple_go")
	b.ResetTimer()
	for range b.N {
		_ = idx.FindSymbols("")
	}
}

// BenchmarkGetCallees measures callee graph lookup.
func BenchmarkGetCallees(b *testing.B) {
	idx := buildIndex(b, "simple_go")
	syms := idx.FindSymbols("")
	if len(syms) == 0 {
		b.Skip("no symbols in fixture")
	}
	id := syms[0].ID
	b.ResetTimer()
	for range b.N {
		_ = idx.GetCallees(id)
	}
}

// BenchmarkGetCallers measures caller graph lookup.
func BenchmarkGetCallers(b *testing.B) {
	idx := buildIndex(b, "simple_go")
	syms := idx.FindSymbols("")
	if len(syms) == 0 {
		b.Skip("no symbols in fixture")
	}
	id := syms[0].ID
	b.ResetTimer()
	for range b.N {
		_ = idx.GetCallers(id)
	}
}

// BenchmarkStats measures the cost of Stats() with deep copy.
func BenchmarkStats(b *testing.B) {
	idx := buildIndex(b, "simple_go")
	b.ResetTimer()
	for range b.N {
		_ = idx.Stats()
	}
}
