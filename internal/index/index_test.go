package index_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func goIndex(t *testing.T, fixture string) *index.RepositoryIndex {
	t.Helper()
	dir := fixtureDir(t, fixture)
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

func TestIndex_Files(t *testing.T) {
	idx := goIndex(t, "simple_go")
	files := idx.Files()
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(files), files)
	}
	// Files must be in deterministic (sorted) order.
	if len(files) == 2 && files[0] > files[1] {
		t.Errorf("files not sorted: %v", files)
	}
}

func TestIndex_Stats(t *testing.T) {
	idx := goIndex(t, "simple_go")
	stats := idx.Stats()
	if stats.Files != 2 {
		t.Errorf("Stats.Files: want 2, got %d", stats.Files)
	}
	if stats.Symbols == 0 {
		t.Error("Stats.Symbols: expected > 0")
	}
	if stats.Languages["go"] != 2 {
		t.Errorf("Stats.Languages[go]: want 2, got %d", stats.Languages["go"])
	}
}

func TestIndex_FindSymbols(t *testing.T) {
	idx := goIndex(t, "simple_go")
	syms := idx.FindSymbols("greet")
	if len(syms) == 0 {
		t.Fatal("expected to find symbol 'greet'")
	}
	found := false
	for _, s := range syms {
		if s.Name == "greet" {
			found = true
		}
	}
	if !found {
		t.Error("'greet' not in FindSymbols result")
	}
}

func TestIndex_SymbolsByFile(t *testing.T) {
	idx := goIndex(t, "simple_go")
	syms := idx.SymbolsByFile(source.FileID("greet.go"))
	if len(syms) == 0 {
		t.Fatal("expected symbols in greet.go")
	}
}

func TestIndex_SymbolsByKind(t *testing.T) {
	idx := goIndex(t, "simple_go")
	fns := idx.SymbolsByKind(symbol.KindFunction)
	if len(fns) == 0 {
		t.Error("expected at least one function symbol")
	}
}

func TestIndex_GetSymbol_RoundTrip(t *testing.T) {
	idx := goIndex(t, "simple_go")
	syms := idx.FindSymbols("greet")
	if len(syms) == 0 {
		t.Skip("no greet symbol found")
	}
	id := syms[0].ID
	got, ok := idx.GetSymbol(id)
	if !ok {
		t.Fatalf("GetSymbol(%q) not found", id)
	}
	if got.Name != syms[0].Name {
		t.Errorf("GetSymbol name: want %q, got %q", syms[0].Name, got.Name)
	}
}

func TestIndex_PartialFailure(t *testing.T) {
	// A directory that doesn't exist should return an index with 0 files, not an error.
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), "/nonexistent/path/xyz", providers)
	if err != nil {
		t.Fatalf("unexpected error for missing directory: %v", err)
	}
	if len(idx.Files()) != 0 {
		t.Errorf("expected 0 files for missing dir, got %d", len(idx.Files()))
	}
}

func TestIndex_Deterministic(t *testing.T) {
	dir := fixtureDir(t, "simple_go")
	providers := []language.Provider{golang.NewProvider()}

	idx1, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("first index.New: %v", err)
	}
	idx2, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("second index.New: %v", err)
	}

	files1 := idx1.Files()
	files2 := idx2.Files()
	if len(files1) != len(files2) {
		t.Fatalf("file counts differ: %d vs %d", len(files1), len(files2))
	}
	for i := range files1 {
		if files1[i] != files2[i] {
			t.Errorf("file[%d] differs: %q vs %q", i, files1[i], files2[i])
		}
	}

	syms1 := idx1.FindSymbols("")
	syms2 := idx2.FindSymbols("")
	if len(syms1) != len(syms2) {
		t.Errorf("symbol counts differ: %d vs %d", len(syms1), len(syms2))
	}
}
