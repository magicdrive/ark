//go:build unix

package accesspolicy

import (
	"os"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/fsroot"
)

func openFDs(t *testing.T) int {
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skip(err)
	}
	return len(entries)
}

// Builds — successful, retried and failed — and checks close every handle.
func TestNoDescriptorLeak(t *testing.T) {
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	runtime.GC()
	before := openFDs(t)
	n := 0
	withHook(t, func(event, rel string) {
		if event == "rule" && rel == "d/.arkignore" && n%3 != 0 {
			w.write(t, "repo/d/.arkignore", []string{"x.txt\n", "y.txt\n"}[n%2])
		}
		if event == "rule" && rel == "d/.arkignore" {
			n++
		}
	})
	for i := 0; i < 200; i++ {
		s, err := Build(tr, Options{})
		if err == nil {
			s.ReadFile("d/ok.txt")
			s.ReadFile("d/x.txt")
			s.Close()
		}
	}
	runtime.GC()
	if after := openFDs(t); after != before {
		t.Errorf("descriptors: %d before, %d after", before, after)
	}
}
