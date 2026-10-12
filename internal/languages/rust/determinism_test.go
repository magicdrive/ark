package rust

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
)

// TestDeterminism: the fixture's index is the same built afresh, built
// again, restored from the extraction cache, and built from a copy whose
// files were written in reverse order (the order a directory lists them in
// is not the index's).
func TestDeterminism(t *testing.T) {
	providers := []language.Provider{NewProvider()}
	snapshot := func(idx *index.RepositoryIndex, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return golden.IndexSnapshot(idx)
	}
	want := snapshot(index.New(context.Background(), fixtureRepo, providers))
	if got := snapshot(index.New(context.Background(), fixtureRepo, providers)); got != want {
		t.Error("a second build differs")
	}

	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot(index.NewWithCache(context.Background(), fixtureRepo, providers, store)); got != want {
		t.Error("a build that fills the cache differs")
	}
	if got := snapshot(index.NewWithCache(context.Background(), fixtureRepo, providers, store)); got != want {
		t.Error("a build restored from the cache differs")
	}

	var files []string
	filepath.Walk(fixtureRepo, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	copyDir := t.TempDir()
	for i := len(files) - 1; i >= 0; i-- {
		rel, _ := filepath.Rel(fixtureRepo, files[i])
		data, err := os.ReadFile(files[i])
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(copyDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := snapshot(index.New(context.Background(), copyDir, providers)); got != want {
		t.Error("a copy written in another order differs")
	}

	// An edit: the cache serves the unchanged files, and the index equals
	// one built afresh from the edited sources.
	copyStore, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapshot(index.NewWithCache(context.Background(), copyDir, providers, copyStore)) // fill before the edit
	edited := filepath.Join(copyDir, "src", "storage", "memory.rs")
	data, _ := os.ReadFile(edited)
	if err := os.WriteFile(edited, append(data, []byte("\npub fn drain(m: &mut Memory) { m.put(crate::model::User::new(1)); }\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := snapshot(index.New(context.Background(), copyDir, providers))
	if fresh == want {
		t.Fatal("precondition: the edit changes the index")
	}
	if got := snapshot(index.NewWithCache(context.Background(), copyDir, providers, copyStore)); got != fresh {
		t.Error("the cached index after an edit differs from a fresh one")
	}
}
