package contextquality_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/contextquality"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

func ambigRoot() string { return filepath.Join("testdata", "ambig") }

// TestEvaluate_AmbiguousTargetIsError: the harness must never evaluate an
// arbitrary one of several same-named targets (candidate zero).
func TestEvaluate_AmbiguousTargetIsError(t *testing.T) {
	_, err := contextquality.Evaluate(ambigRoot(), []language.Provider{golang.NewProvider()},
		contextquality.Scenario{Name: "ambig", TargetQualified: "Create", Depth: 1, MaxTokens: 4000})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguity error, got %v", err)
	}
}

// TestEvaluate_TargetFilePinsAndFileScopedRequired: TargetFile selects one target
// and "file#Qualified" entries score against that exact symbol.
func TestEvaluate_TargetFilePinsAndFileScopedRequired(t *testing.T) {
	res, err := contextquality.Evaluate(ambigRoot(), []language.Provider{golang.NewProvider()},
		contextquality.Scenario{
			Name: "pinned", TargetQualified: "Create", TargetFile: "b/svc.go", Depth: 1, MaxTokens: 4000,
			Required:   []string{"b/svc.go#Create", "b/svc.go#helperB"},
			Irrelevant: []string{"a/svc.go#Create", "a/svc.go#helperA"},
		})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TargetFound || !res.TargetIncluded {
		t.Fatalf("target not found/included: %+v", res)
	}
	if res.RequiredRecall != 1 {
		t.Errorf("recall = %v, missing %v", res.RequiredRecall, res.MissingRequired)
	}
	if len(res.IrrelevantIncluded) != 0 {
		t.Errorf("irrelevant included: %v", res.IrrelevantIncluded)
	}
}
