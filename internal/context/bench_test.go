package context_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"

	arkctx "github.com/magicdrive/ark/internal/context"
)

func buildBenchIndex(b *testing.B) (*index.RepositoryIndex, string) {
	b.Helper()
	dir := fixtureDir
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		b.Fatalf("index.New: %v", err)
	}
	return idx, dir
}

// BenchmarkBuild_SmallBudget measures context building with a constrained token budget.
func BenchmarkBuild_SmallBudget(b *testing.B) {
	idx, dir := buildBenchIndex(b)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		b.Skip("Create not found in fixture")
	}
	eng := arkctx.New(idx, dir)
	req := arkctx.Request{Target: syms[0].ID, MaxTokens: 2000, MaxDepth: 2}
	b.ResetTimer()
	for range b.N {
		_, err := eng.Build(context.Background(), req)
		if err != nil {
			b.Fatalf("Build: %v", err)
		}
	}
}

// BenchmarkBuild_LargeBudget measures context building with a generous token budget.
func BenchmarkBuild_LargeBudget(b *testing.B) {
	idx, dir := buildBenchIndex(b)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		b.Skip("Create not found in fixture")
	}
	eng := arkctx.New(idx, dir)
	req := arkctx.Request{Target: syms[0].ID, MaxTokens: 32000, MaxDepth: 3}
	b.ResetTimer()
	for range b.N {
		_, err := eng.Build(context.Background(), req)
		if err != nil {
			b.Fatalf("Build: %v", err)
		}
	}
}
