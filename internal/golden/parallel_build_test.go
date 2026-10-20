package golden_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
)

// The build the MCP server runs (NewWithCacheExcluding: extraction on
// workers, results taken back in walk order) is the index the sequential
// build (index.New) makes: the same snapshot — symbols, references, edges
// with confidence, diagnostics — and the same fingerprint, for every
// baseline language and for all of them together (and the Python graph
// fixture), with the extraction cache off, cold and warm — also through the
// build-scoped store the server opens (cache.OpenBuildStore), cold, warm and
// opened again as a restarted server opens it.
func TestParallelBuildMatchesSequential(t *testing.T) {
	dirs := map[string]string{"all": testdataDir(), "python-graph": filepath.Join("..", "languages", "python", "testdata", "repo")}
	for _, lang := range baselineLanguages {
		dirs[lang] = filepath.Join(testdataDir(), lang)
	}
	for name, dir := range dirs {
		t.Run(name, func(t *testing.T) {
			want, err := index.New(context.Background(), dir, allProviders())
			if err != nil {
				t.Fatal(err)
			}
			wantSnap := golden.IndexSnapshot(want)
			store, err := cache.NewFileStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			scopedDir := t.TempDir()
			scoped, _, err := cache.OpenBuildStore(scopedDir)
			if err != nil {
				t.Fatal(err)
			}
			var restarted *cache.FileStore
			for _, c := range []struct {
				name  string
				store func() cache.Store
			}{
				{"nocache", func() cache.Store { return cache.NopStore{} }},
				{"cold", func() cache.Store { return store }},
				{"warm", func() cache.Store { return store }},
				{"scoped cold", func() cache.Store { return scoped }},
				{"scoped warm", func() cache.Store { return scoped }},
				{"scoped restarted", func() cache.Store {
					restarted, _, err = cache.OpenBuildStore(scopedDir)
					if err != nil {
						t.Fatal(err)
					}
					return restarted
				}},
			} {
				st := c.store()
				got, err := index.NewWithCacheExcluding(context.Background(), dir, allProviders(), st, nil)
				if err != nil {
					t.Fatal(err)
				}
				if got.Fingerprint() != want.Fingerprint() {
					t.Errorf("%s: fingerprint %s, sequential %s", c.name, got.Fingerprint(), want.Fingerprint())
				}
				if snap := golden.IndexSnapshot(got); snap != wantSnap {
					t.Errorf("%s: snapshot differs from the sequential build's (%d vs %d bytes)", c.name, len(snap), len(wantSnap))
				}
				if g, w := got.Diagnostics(), want.Diagnostics(); len(g) != len(w) {
					t.Errorf("%s: %d diagnostics, sequential %d", c.name, len(g), len(w))
				} else {
					for i := range g {
						if g[i].Message != w[i].Message || g[i].Location != w[i].Location || g[i].Code != w[i].Code {
							t.Errorf("%s: diagnostic %d (in order) %+v, sequential %+v", c.name, i, g[i], w[i])
							break
						}
					}
				}
			}
			// The tidying the stores started has ended before the
			// directory is removed.
			<-scoped.Tidied()
			<-restarted.Tidied()
			if st := scoped.Stats(); st.Reused == 0 || st.Reused != st.Absent {
				t.Errorf("scoped store: %+v, want the warm build to reuse every entry the cold one wrote", st)
			}
			if st := restarted.Stats(); st.Reused == 0 || st.Absent+st.Corrupt+st.Schema+st.OtherBuild+st.Stale != 0 || st.Written != 0 {
				t.Errorf("restarted store: %+v, want every entry reused and none written", st)
			}
		})
	}
}
