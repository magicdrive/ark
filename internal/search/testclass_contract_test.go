package search

import (
	"testing"

	"github.com/magicdrive/ark/internal/testfiles"
	"github.com/magicdrive/ark/internal/testfiles/testfilestest"
)

// excludeTests leaves out exactly what internal/testfiles classifies as a test
// file or test fixture data.
func TestExcludeTest_IsTheSharedClassifier(t *testing.T) {
	q := Query{ExcludeTest: true}
	for _, p := range testfilestest.Paths {
		excluded := !matchesFile(p, "", q, nil)
		if want := testfiles.IsTestFile(p) || testfiles.IsTestData(p); excluded != want {
			t.Errorf("search excludeTests(%q) = %v, shared classifiers %v", p, excluded, want)
		}
	}
}
