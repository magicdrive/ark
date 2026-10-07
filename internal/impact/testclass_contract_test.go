package impact

import (
	"testing"

	"github.com/magicdrive/ark/internal/testfiles"
	"github.com/magicdrive/ark/internal/testfiles/testfilestest"
)

// Impact analysis classifies tests (and leaves out fixture data) by
// internal/testfiles alone.
func TestIsTestFile_IsTheSharedClassifier(t *testing.T) {
	for _, p := range testfilestest.Paths {
		if got, want := isTestFile(p), testfiles.IsTestFile(p) || testfiles.IsTestData(p); got != want {
			t.Errorf("impact isTestFile(%q) = %v, shared classifiers %v", p, got, want)
		}
	}
}
