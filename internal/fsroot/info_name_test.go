package fsroot

import (
	"os"
	"path/filepath"
	"testing"
)

// A directory described through its own handle is named as os.Lstat names
// it — the root by its base name, not "." (Windows: os.Root's Stat(".")
// says "."), so a walk's start reports the name filepath.Walk reports.
func TestDirectoryHandleNames(t *testing.T) {
	ws := t.TempDir()
	root := filepath.Join(ws, "repo")
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := pin(t, root, Options{})
	want := func(rel string) string {
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return fi.Name()
	}
	if want(".") != "repo" {
		t.Fatalf("os.Lstat names the root %q", want("."))
	}
	for _, rel := range []string{".", "a", "a/b"} {
		if fi, err := tr.Lstat(rel); err != nil || fi.Name() != want(rel) || !fi.IsDir() {
			t.Errorf("Lstat(%q) = %v %v, want %q", rel, fi, err, want(rel))
		}
		if fi, err := tr.StatChecked(rel, nil); err != nil || fi.Name() != want(rel) || !fi.IsDir() {
			t.Errorf("StatChecked(%q) = %v %v, want %q", rel, fi, err, want(rel))
		}
		var starts int
		err := tr.Walk(rel, func(e Entry) error {
			fi, err := e.Info()
			if err != nil {
				t.Errorf("walk from %q: Info(%s): %v", rel, e.Rel(), err)
				return nil
			}
			if e.IsStart() {
				starts++
				if fi.Name() != want(rel) {
					t.Errorf("walk from %q: start named %q, want %q", rel, fi.Name(), want(rel))
				}
			} else if fi.Name() != want(e.Rel()) {
				t.Errorf("walk from %q: %s named %q, want %q", rel, e.Rel(), fi.Name(), want(e.Rel()))
			}
			return nil
		})
		if err != nil || starts != 1 {
			t.Errorf("walk from %q: %v, %d starts", rel, err, starts)
		}
	}
}
