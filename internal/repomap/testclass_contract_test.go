package repomap

import (
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/testfiles"
	"github.com/magicdrive/ark/internal/testfiles/testfilestest"
)

// The repository map's test files and test packages follow internal/testfiles
// alone.
func TestIsTestFile_IsTheSharedClassifier(t *testing.T) {
	for _, p := range testfilestest.Paths {
		if got, want := isTestFileID(p), testfiles.IsTestFile(p); got != want {
			t.Errorf("repomap isTestFileID(%q) = %v, shared classifier %v", p, got, want)
		}
		dir := filepath.ToSlash(filepath.Dir(p))
		want := testfiles.IsTestData(p) || testfiles.IsTestFile(p)
		if got := isTestPackage(dir, []source.FileID{source.FileID(p)}); got != want {
			t.Errorf("repomap isTestPackage(%q, [%s]) = %v, want %v", dir, p, got, want)
		}
	}
}
