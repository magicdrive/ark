package index_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// hiddenReader is a provider of .cfg files that reads the hidden directory
// .github (language.HiddenDirReader).
type hiddenReader struct{}

func (hiddenReader) Language() language.Language     { return "cfg" }
func (hiddenReader) Extensions() []string            { return []string{".cfg"} }
func (hiddenReader) CacheVersion() string            { return "cfg-1" }
func (hiddenReader) ReadsHiddenDir(name string) bool { return name == ".github" }
func (hiddenReader) Extract(context.Context, source.FileID, []byte) (language.Extraction, error) {
	return language.Extraction{}, nil
}

// In a hidden directory the index enters (.github), only the files of a
// provider that reads it are indexed — at any depth, also below a
// subdirectory; every other hidden directory stays unread, and the
// fingerprint walk sees exactly the indexed files.
func TestHiddenDirs_OnlyTheProvidersThatReadThem(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		".github/a.cfg", ".github/workflows/b.cfg", ".github/scripts/c.fake", ".github/.inner/d.cfg",
		"sub/.github/e.cfg", ".other/f.cfg", ".other/g.fake", "h.cfg", "i.fake",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{hiddenReader{}, fakeProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range idx.Files() {
		files = append(files, string(f))
	}
	slices.Sort(files)
	want := []string{".github/a.cfg", ".github/workflows/b.cfg", "h.cfg", "i.fake", "sub/.github/e.cfg"}
	if !slices.Equal(files, want) {
		t.Errorf("indexed %v, want %v", files, want)
	}
	if index.SkipDirName(".github") || !index.SkipDirName(".git") || !index.SkipDirName(".other") {
		t.Error("SkipDirName enters .github only of the hidden directories")
	}
}

// An index of the hidden directory itself reads it as an index of the
// repository does: only the files of the providers that read it.
func TestHiddenDirs_RootInside(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".github/a.cfg", ".github/scripts/c.fake"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), filepath.Join(dir, ".github"), []language.Provider{hiddenReader{}, fakeProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range idx.Files() {
		files = append(files, string(f))
	}
	if !slices.Equal(files, []string{"a.cfg"}) {
		t.Errorf("indexed %v, want [a.cfg]", files)
	}
}

// Below .github, or under an ancestor of that name, a root is indexed as
// any directory is: a JavaScript action's code, an organization's .github
// repository's tools.
func TestHiddenDirs_RootBelowIsAnyDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".github/actions/js/a.cfg", ".github/actions/js/b.fake"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), filepath.Join(dir, ".github", "actions", "js"), []language.Provider{hiddenReader{}, fakeProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range idx.Files() {
		files = append(files, string(f))
	}
	slices.Sort(files)
	if !slices.Equal(files, []string{"a.cfg", "b.fake"}) {
		t.Errorf("indexed %v, want [a.cfg b.fake]", files)
	}
}
