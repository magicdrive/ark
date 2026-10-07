package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/testfiles"
	"github.com/magicdrive/ark/internal/testfiles/testfilestest"
)

// The analyzer finds tests exactly where internal/testfiles does.
func TestHasTests_IsTheSharedClassifier(t *testing.T) {
	for _, p := range testfilestest.Paths {
		root := t.TempDir()
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, want := NewAnalyzer(root).hasTests(), testfiles.IsTestFile(p); got != want {
			t.Errorf("analyzer hasTests with only %q = %v, shared classifier %v", p, got, want)
		}
	}
}
