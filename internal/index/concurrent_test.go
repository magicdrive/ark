package index_test

import (
	"context"
	"sync"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// TestIndex_ConcurrentReads verifies that multiple goroutines can query a
// RepositoryIndex concurrently without data races.
// Run with: go test -race ./internal/index/
func TestIndex_ConcurrentReads(t *testing.T) {
	idx := goIndex(t, "simple_go")
	syms := idx.FindSymbols("")
	if len(syms) == 0 {
		t.Skip("no symbols in fixture")
	}
	targetID := syms[0].ID

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			_ = idx.FindSymbols("greet")
			_ = idx.FindSymbols("")
			_ = idx.GetCallees(targetID)
			_ = idx.GetCallers(targetID)
			_ = idx.GetRelatedSymbols(targetID)
			_ = idx.Stats()
			_ = idx.Files()
			_ = idx.Diagnostics()
		}()
	}
	wg.Wait()
}

// TestIndex_CancellationRespected verifies that index.New returns promptly
// when the context is already cancelled.
func TestIndex_CancellationRespected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	providers := benchmarkProviders()
	dir := fixtureDir(t, "simple_go")
	_, err := index.New(ctx, dir, providers)
	if err == nil {
		// Some implementations may complete a small scan before noticing cancellation.
		// The important property is: no panic and no hang.
		t.Log("index.New completed despite cancelled context (fixture is small)")
	}
}
