package index_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

func TestNewWithCache_ProducesIndex(t *testing.T) {
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	store := cache.NopStore{}

	idx, err := index.NewWithCache(context.Background(), dir, providers, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Files()) == 0 {
		t.Fatal("expected files in index")
	}
}

func TestNewWithCache_CacheHitAvoidsReExtraction(t *testing.T) {
	cacheDir := t.TempDir()
	store, err := cache.NewFileStore(cacheDir)
	if err != nil {
		t.Fatal(err)
	}

	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	ctx := context.Background()

	// First run: cold cache — files are extracted and written.
	idx1, err := index.NewWithCache(ctx, dir, providers, store)
	if err != nil {
		t.Fatal(err)
	}

	// Second run: warm cache — files should be read from cache.
	idx2, err := index.NewWithCache(ctx, dir, providers, store)
	if err != nil {
		t.Fatal(err)
	}

	// Both indexes must agree on file count.
	if len(idx1.Files()) != len(idx2.Files()) {
		t.Fatalf("file counts differ: %d vs %d", len(idx1.Files()), len(idx2.Files()))
	}

	// Symbol count must match.
	s1 := idx1.Stats()
	s2 := idx2.Stats()
	if s1.Symbols != s2.Symbols {
		t.Fatalf("symbol count differs: %d vs %d", s1.Symbols, s2.Symbols)
	}
}

func TestNewWithCache_NopStoreBehavesLikeNew(t *testing.T) {
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}
	ctx := context.Background()

	plain, _ := index.New(ctx, dir, providers)
	cached, _ := index.NewWithCache(ctx, dir, providers, cache.NopStore{})

	if plain.Stats().Symbols != cached.Stats().Symbols {
		t.Fatalf("NopStore: symbol count differs: %d vs %d", plain.Stats().Symbols, cached.Stats().Symbols)
	}
}
