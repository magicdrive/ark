package typescript_test

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages/typescript"
)

// Cache identity: the provider version is part of the cache key, and it moved
// off "1" when extraction semantics changed.
func TestCacheIdentity(t *testing.T) {
	for _, p := range tsProviders() {
		v := p.CacheVersion()
		if v == "" || v == "1" {
			t.Errorf("%s CacheVersion = %q: extraction semantics changed, the old version must not be reused", p.Language(), v)
		}
		old := cache.NewCacheKey("a.ts", []byte("x"), "0.1.0", "1")
		cur := cache.NewCacheKey("a.ts", []byte("x"), "0.1.0", v)
		if old.ProviderVersion == cur.ProviderVersion {
			t.Errorf("%s: provider version does not change the cache key", p.Language())
		}
	}
	if typescript.NewProvider().CacheVersion() != typescript.NewTSXProvider().CacheVersion() {
		t.Error("TS and TSX share one extraction implementation and must share its version")
	}
}

// Cold and warm cache must produce byte-identical graphs, including module
// bindings, exports and ReceiverType persisted through the cache.
func TestCacheColdWarmEquivalence(t *testing.T) {
	for _, fixture := range []string{"cq_ts", "cq_tsx"} {
		t.Run(fixture, func(t *testing.T) {
			root := cqRoot(fixture)
			plain, err := index.New(context.Background(), root, tsProviders())
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Join(edgeSet(t, plain), "\n")
			if want == "" {
				t.Fatal("fixture produced no edges")
			}

			store, err := cache.NewFileStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, run := range []string{"cold", "warm", "warm-again"} {
				idx, err := index.NewWithCache(context.Background(), root, tsProviders(), store)
				if err != nil {
					t.Fatal(err)
				}
				if got := strings.Join(edgeSet(t, idx), "\n"); got != want {
					t.Errorf("%s cache run differs from uncached:\n%s\n---\n%s", run, got, want)
				}
				if idx.Stats().Symbols != plain.Stats().Symbols || idx.Stats().References != plain.Stats().References {
					t.Errorf("%s: stats differ: %+v vs %+v", run, idx.Stats(), plain.Stats())
				}
			}
		})
	}
}
