package context

import (
	"testing"

	"github.com/magicdrive/ark/internal/testfiles"
	"github.com/magicdrive/ark/internal/testfiles/testfilestest"
)

// The Context Engine's test filter is internal/testfiles' answer, nothing else.
func TestIsTestFile_IsTheSharedClassifier(t *testing.T) {
	for _, p := range testfilestest.Paths {
		if got, want := isTestFile(p), testfiles.IsTestFile(p); got != want {
			t.Errorf("context isTestFile(%q) = %v, shared classifier %v", p, got, want)
		}
	}
}
