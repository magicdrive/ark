package contextquality_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/contextquality"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/java"
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

// TestEvaluate_OverloadsArePinnedByLine: overloads share their qualified name
// and their file. An unpinned target among them is an error; TargetLine
// selects one, and "Qualified@line" / "file#Qualified@line" entries score
// against the declaration starting on that line only.
func TestEvaluate_OverloadsArePinnedByLine(t *testing.T) {
	root := filepath.Join("testdata", "overload")
	providers := []language.Provider{java.NewProvider()}
	_, err := contextquality.Evaluate(root, providers,
		contextquality.Scenario{TargetQualified: "p.A.f", TargetFile: "A.java", Depth: 1, MaxTokens: 4000})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguity error for unpinned overloads, got %v", err)
	}
	res, err := contextquality.Evaluate(root, providers, contextquality.Scenario{
		TargetQualified: "p.A.f", TargetLine: 5, Depth: 1, MaxTokens: 4000,
		Required:   []string{"p.A.f@5", "A.java#p.A.f@5", "p.A.h"},
		Irrelevant: []string{"p.A.f@4", "A.java#p.A.f@4", "p.A.g"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TargetIncluded || res.RequiredRecall != 1 || len(res.IrrelevantIncluded) != 0 {
		t.Errorf("pinned overload: included=%v missing=%v irrelevant=%v selected=%+v",
			res.TargetIncluded, res.MissingRequired, res.IrrelevantIncluded, res.Selected)
	}
	if res, err := contextquality.Evaluate(root, providers,
		contextquality.Scenario{TargetQualified: "p.A.f", TargetLine: 6, Depth: 1, MaxTokens: 4000}); err != nil || res.TargetFound {
		t.Errorf("a line no overload starts on: found=%v err=%v, want target not found", res.TargetFound, err)
	}
}

// TestEvaluate_UnknownEntriesAreReported: an entry that names no symbol (a
// typo, a wrong line, a wrong file) would make its check vacuous.
func TestEvaluate_UnknownEntriesAreReported(t *testing.T) {
	res, err := contextquality.Evaluate(filepath.Join("testdata", "overload"), []language.Provider{java.NewProvider()},
		contextquality.Scenario{
			TargetQualified: "p.A.g", Depth: 1, MaxTokens: 4000,
			Required:   []string{"p.A.g", "A.java#p.A.f@4"},
			Optional:   []string{"p.A.f@6"},
			Irrelevant: []string{"p.A.missing", "B.java#p.A.h", "p.A.h"},
		})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"B.java#p.A.h", "p.A.f@6", "p.A.missing"}
	if strings.Join(res.UnknownEntries, ",") != strings.Join(want, ",") {
		t.Errorf("unknown entries %v, want %v", res.UnknownEntries, want)
	}
}
