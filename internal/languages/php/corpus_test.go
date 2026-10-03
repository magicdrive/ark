package php_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/php"
)

// TestCorpus_RealisticMedium certifies the medium realistic corpus: it indexes
// a multi-namespace repository (controller/service/repository/domain/support +
// tests + modern syntax + broken source) and asserts cross-file connectivity,
// typed relations, safety on modern/broken syntax, and determinism.
func TestCorpus_RealisticMedium(t *testing.T) {
	dir := testdataDir(t, "corpus")
	idx, err := index.New(context.Background(), dir, []language.Provider{php.NewProvider()})
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	st := idx.Stats()
	if st.Symbols == 0 {
		t.Fatal("no symbols extracted from corpus")
	}
	if st.Skipped != 0 {
		t.Errorf("corpus skipped %d files (broken/modern syntax must degrade safely, not skip)", st.Skipped)
	}

	// Typed cross-file relations certified on realistic code.
	wantEdge := func(from, to string, kind index.EdgeKind) {
		t.Helper()
		if !hasEdgeTo(idx, from, to) || edgeKind(idx, from, to) != kind {
			t.Errorf("expected edge %s -%s-> %s", from, kind, to)
		}
	}
	wantEdge("App\\Repository\\DbUserRepository", "App\\Repository\\UserRepository", index.EdgeImplements)
	wantEdge("App\\Repository\\DbUserRepository", "App\\Support\\LogsActivity", index.EdgeUsesTrait)

	// Determinism: three builds produce an identical serialized index snapshot.
	first := golden.IndexSnapshot(idx)
	for i := 0; i < 3; i++ {
		idx2, err := index.New(context.Background(), dir, []language.Provider{php.NewProvider()})
		if err != nil {
			t.Fatalf("rebuild %d: %v", i, err)
		}
		if golden.IndexSnapshot(idx2) != first {
			t.Fatalf("non-deterministic corpus index on rebuild %d", i)
		}
	}
}

// TestCorpus_ScalingSanity generates N / 2N / 4N PHP files and verifies index
// build stays well-behaved: symbol count scales roughly linearly (no explosion)
// and no file is skipped. It is a sanity check, not a complexity proof.
func TestCorpus_ScalingSanity(t *testing.T) {
	genDir := func(n int) string {
		dir := t.TempDir()
		for i := 0; i < n; i++ {
			src := fmt.Sprintf("<?php\nnamespace App\\Gen;\nclass C%d {\n    public function m%d(): void { $this->m%d(); }\n}\n", i, i, i)
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("C%d.php", i)), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	count := func(n int) int {
		idx, err := index.New(context.Background(), genDir(n), []language.Provider{php.NewProvider()})
		if err != nil {
			t.Fatalf("index.New(n=%d): %v", n, err)
		}
		if idx.Stats().Skipped != 0 {
			t.Errorf("n=%d: %d files skipped", n, idx.Stats().Skipped)
		}
		return idx.Stats().Symbols
	}
	// Each generated file has 3 symbols (namespace + class + method). Expect
	// strictly linear growth (no super-linear symbol explosion).
	for _, n := range []int{50, 100, 200} {
		got := count(n)
		if got != n*3 {
			t.Errorf("n=%d: symbols=%d, want %d (linear 3/file)", n, got, n*3)
		}
	}
}
