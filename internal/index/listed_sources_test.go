package index_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	golang "github.com/magicdrive/ark/internal/languages/golang"
)

// listing is what a full filepath.WalkDir of dir lists, except below
// directories skip names — the shape IgnoreReader.All records.
func listing(t *testing.T, dir string, skip func(string) bool) []index.WalkEntry {
	t.Helper()
	var out []index.WalkEntry
	skipping := ""
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipping != "" && strings.HasPrefix(p, skipping) {
			return nil
		}
		skipping = ""
		out = append(out, index.WalkEntry{Path: p, D: d})
		if d.IsDir() && p != dir && skip(d.Name()) {
			skipping = p + string(filepath.Separator)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The fingerprint computed from a listing is the fingerprint the walk
// computes — same files, same order, same contents — for any exclusion, so a
// freshness check from a listing reuses exactly the indexes a walk would.
func TestSourceFingerprintListed_EqualsWalk(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"main.go", "a/b.go", "a/c.ts", "a/-early/x.go", "a/z/y.go", "a/b-c/w.go", "vendor/v.go", "node_modules/m.ts",
		".hidden/h.go", "excl/e.go", "excl/deeper/d.go", "keep/k.go", "keep/skip.go", "notes.txt", "UPPER.GO",
	} {
		write(f, "package p // "+f+"\n")
	}
	if err := os.Symlink("main.go", filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.go", filepath.Join(root, "dangling.go")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		write("locked.go", "package p\n")
		if err := os.Chmod(filepath.Join(root, "locked.go"), 0o000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(filepath.Join(root, "locked.go"), 0o644)
	}
	providers := languages.Registry().Providers()
	excludes := map[string]index.Exclude{
		"none": nil,
		"dir and file": func(p string, d fs.DirEntry) bool {
			rel, _ := filepath.Rel(root, p)
			return rel == "excl" || rel == filepath.Join("keep", "skip.go") || rel == "link.go"
		},
		"everything": func(string, fs.DirEntry) bool { return true },
	}
	list := listing(t, root, index.SkipDirName)
	for name, exclude := range excludes {
		for _, sub := range []string{".", "a", "keep", "excl"} {
			dir := filepath.Join(root, sub)
			want, err := index.SourceFingerprintExcluding(context.Background(), dir, providers, exclude)
			if err != nil {
				t.Fatal(err)
			}
			var entries []index.WalkEntry
			for _, e := range list {
				if e.Path == dir || strings.HasPrefix(e.Path, dir+string(filepath.Separator)) {
					entries = append(entries, e)
				}
			}
			got, err := index.SourceFingerprintListed(context.Background(), dir, providers, exclude, entries)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("exclude %s, root %s: listed fingerprint differs from the walk's", name, sub)
			}
		}
	}
	// And it is the fingerprint the built index carries.
	idx, err := index.NewWithCacheExcluding(context.Background(), root, providers, cache.NopStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := index.SourceFingerprintListed(context.Background(), root, providers, nil, list); got != idx.Fingerprint() {
		t.Error("listed fingerprint differs from the index's")
	}
	// A listing that does not start at the root is refused, never trusted.
	if _, err := index.SourceFingerprintListed(context.Background(), root, providers, nil, list[1:]); err == nil {
		t.Error("a listing without its root was accepted")
	}
}

// A root a skipped name names lists nothing, as the walk does.
func TestSourceFingerprintListed_SkippedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hidden")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	providers := []language.Provider{golang.NewProvider()}
	want, _ := index.SourceFingerprintExcluding(context.Background(), root, providers, nil)
	got, _ := index.SourceFingerprintListed(context.Background(), root, providers, nil, listing(t, root, index.SkipDirName))
	if got != want {
		t.Error("skipped root: listed fingerprint differs")
	}
}
