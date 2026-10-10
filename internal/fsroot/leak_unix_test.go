//go:build unix

package fsroot

import (
	"os"
	"runtime"
	"testing"
)

// openFDs counts this process's open descriptors.
func openFDs(t *testing.T) int {
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skip("cannot count descriptors:", err)
	}
	return len(entries)
}

// Every handle is closed: successful and failing operations, walks stopped
// early, entries kept past their walk, Trees closed.
func TestNoDescriptorLeak(t *testing.T) {
	f := newFixture(t)
	f.write(t, "A/deep/a/b/c/file.txt", "DEEP")
	runtime.GC()
	before := openFDs(t)
	for i := 0; i < 300; i++ {
		tr, err := Pin(f.path("root"), Options{AllowExternalSymlinks: i%2 == 0})
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{"s.txt", "hop1", "dirlink/f.txt", "deep/a/b/c/file.txt", "abs-external",
			"loop1", "dangling", "excluded.txt", "sub", "../x", "deep/a/missing/x"} {
			tr.ReadFile(rel, excludedBy("excluded.txt"))
		}
		if r, err := tr.Resolve("deep/a/b/c/file.txt"); err == nil {
			if fh, err := tr.Open(r); err == nil {
				fh.Close()
			}
		}
		tr.Walk(".", func(e Entry) error {
			if e.Rel() == "deep/a" {
				return SkipAll
			}
			if !e.IsDir() && !e.IsSymlink() {
				if b, _, err := e.ReadAll(); err != nil || len(b) == 0 {
					return nil
				}
			}
			return nil
		})
		tr.DirIdentity("deep/a/b")
		tr.DirIdentity("dirlink")
		tr.Close()
	}
	runtime.GC()
	if after := openFDs(t); after != before {
		t.Errorf("descriptors: %d before, %d after", before, after)
	}
}
