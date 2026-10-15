package css_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/css"
)

func siteRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "site")
}

// snapshot lists an index's symbols, edges, unattributed counts,
// references and diagnostics: what a cached extraction must reproduce.
func snapshot(idx *index.RepositoryIndex) []string {
	var out []string
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			out = append(out, fmt.Sprintf("symbol %s %s %v", s.Kind, s.Qualified, s.Location))
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				out = append(out, fmt.Sprintf("edge %s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
			}
			in, outgoing := idx.Unattributed(s.ID)
			out = append(out, fmt.Sprintf("unattributed %s %d %d", s.Qualified, in, outgoing))
		}
		for _, r := range idx.ReferencesByFile(f) {
			out = append(out, fmt.Sprintf("ref %s %s %v", r.Kind, r.Name, r.Location))
		}
	}
	for _, d := range idx.Diagnostics() {
		out = append(out, fmt.Sprintf("diag %s %s %v", d.Severity, d.Code, d.Location))
	}
	sort.Strings(out)
	return out
}

// TestCacheColdWarmEquivalence: an index built without a cache, with a cold
// cache and from it (warm) is the same.
func TestCacheColdWarmEquivalence(t *testing.T) {
	providers := []language.Provider{css.NewProvider()}
	plain, err := index.New(context.Background(), siteRoot(), providers)
	if err != nil {
		t.Fatal(err)
	}
	want := snapshot(plain)
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"cold", "warm", "warm-again"} {
		idx, err := index.NewWithCache(context.Background(), siteRoot(), providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := snapshot(idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s cache run differs from the uncached index:\n%q\n---\n%q", run, got, want)
		}
	}
}
