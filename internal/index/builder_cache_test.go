package index_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/source"
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

// diagProvider emits one diagnostic per file.
type diagProvider struct{ fakeProvider }

func (diagProvider) Extract(_ context.Context, f source.FileID, _ []byte) (language.Extraction, error) {
	return language.Extraction{Diagnostics: []language.Diagnostic{{Severity: language.SeverityError, Message: "syntax error", Location: source.Location{File: f}}}}, nil
}

// A warm cache returns the index a cold one does — diagnostics included.
func TestNewWithCache_WarmKeepsDiagnostics(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.fake"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	providers := []language.Provider{diagProvider{}}
	var got []int
	for i := 0; i < 2; i++ {
		idx, err := index.NewWithCache(context.Background(), dir, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, len(idx.Diagnostics()))
	}
	if got[0] != 1 || got[1] != 1 {
		t.Errorf("diagnostics cold=%d warm=%d, want 1 and 1", got[0], got[1])
	}
}

type failingProvider struct{ fakeProvider }

func (failingProvider) Extract(context.Context, source.FileID, []byte) (language.Extraction, error) {
	return language.Extraction{}, errors.New("boom")
}

// The index's own failures name the repository-relative file, carry a code
// and never the OS path; the file is skipped, not counted as indexed.
func TestIndexFailureDiagnostics(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.fake"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, build := range []func() (*index.RepositoryIndex, error){
		func() (*index.RepositoryIndex, error) {
			return index.New(context.Background(), dir, []language.Provider{failingProvider{}})
		},
		func() (*index.RepositoryIndex, error) {
			return index.NewWithCache(context.Background(), dir, []language.Provider{failingProvider{}}, cache.NopStore{})
		},
	} {
		idx, err := build()
		if err != nil {
			t.Fatal(err)
		}
		ds := idx.Diagnostics()
		if len(ds) != 1 || ds[0].Code != language.DiagExtractionError || ds[0].Location.File != "x.fake" || strings.Contains(ds[0].Message, dir) {
			t.Errorf("diagnostics %+v", ds)
		}
		if st := idx.Stats(); st.Files != 0 || st.Skipped != 1 {
			t.Errorf("a failed file is skipped, not indexed: %+v", st)
		}
		if s := idx.DiagnosticSummary(); s.Files != 1 || s.Warnings != 1 || s.Errors != 0 {
			t.Errorf("summary %+v", s)
		}
	}
	if os.Getuid() == 0 {
		return // root reads anything
	}
	unreadable := filepath.Join(dir, "y.fake")
	if err := os.WriteFile(unreadable, []byte("y"), 0o000); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{fakeProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range idx.Diagnostics() {
		if d.Code == language.DiagReadError {
			found = true
			if d.Location.File != "y.fake" || strings.Contains(d.Message, dir) {
				t.Errorf("read error %+v", d)
			}
		}
	}
	if !found {
		t.Errorf("no read error: %+v", idx.Diagnostics())
	}
}

// A content change re-extracts: a diagnostic appears and disappears with the
// source, through a warm cache.
func TestNewWithCache_DiagnosticsFollowContent(t *testing.T) {
	dir := t.TempDir()
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	providers := []language.Provider{golang.NewProvider()}
	count := func(src string) int {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		idx, err := index.NewWithCache(context.Background(), dir, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		return len(idx.Diagnostics())
	}
	good, bad := "package a\nfunc A() {}\n", "package a\nfunc A( {}\n"
	for i, c := range []struct {
		src  string
		want bool
	}{{good, false}, {bad, true}, {bad, true}, {good, false}, {good, false}} {
		if got := count(c.src) > 0; got != c.want {
			t.Errorf("step %d: has diagnostics %t, want %t", i, got, c.want)
		}
	}
}
