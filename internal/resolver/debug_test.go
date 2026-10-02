package resolver_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func makeResolution(name string, conf resolver.Confidence, candidates []resolver.Candidate) resolver.Resolution {
	return resolver.Resolution{
		ReferenceName: name,
		Candidates:    candidates,
		Confidence:    conf,
		Evidence:      nil,
	}
}

func TestDebugResolution_Unique(t *testing.T) {
	res := makeResolution("Save", resolver.ConfidenceStrong, []resolver.Candidate{
		{
			SymbolID:   symbol.SymbolID("abc"),
			Name:       "Save",
			Qualified:  "UserRepository.Save",
			File:       source.FileID("repository.go"),
			Kind:       symbol.KindMethod,
			Confidence: resolver.ConfidenceStrong,
			Evidence: []resolver.ResolutionEvidence{
				{Kind: resolver.EvidenceSamePackage, Detail: "same package"},
			},
		},
	})

	d := resolver.DebugResolution(res)
	if d.Reference != "Save" {
		t.Errorf("Reference = %q, want %q", d.Reference, "Save")
	}
	if !d.Unique {
		t.Error("expected Unique=true for single strong candidate")
	}
	if len(d.Candidates) != 1 {
		t.Errorf("Candidates count = %d, want 1", len(d.Candidates))
	}
	if d.Candidates[0].Qualified != "UserRepository.Save" {
		t.Errorf("Candidates[0].Qualified = %q", d.Candidates[0].Qualified)
	}
}

func TestDebugResolution_Ambiguous(t *testing.T) {
	res := makeResolution("Process", resolver.ConfidenceCandidate, []resolver.Candidate{
		{Name: "Process", Qualified: "ServiceA.Process", File: "a.go", Confidence: resolver.ConfidenceCandidate},
		{Name: "Process", Qualified: "ServiceB.Process", File: "b.go", Confidence: resolver.ConfidenceCandidate},
	})

	d := resolver.DebugResolution(res)
	if d.Unique {
		t.Error("expected Unique=false for ambiguous resolution")
	}
	if len(d.Candidates) != 2 {
		t.Errorf("Candidates count = %d, want 2", len(d.Candidates))
	}
}

func TestFormatResolution_ContainsKeyInfo(t *testing.T) {
	res := makeResolution("Save", resolver.ConfidenceStrong, []resolver.Candidate{
		{
			Name:       "Save",
			Qualified:  "UserRepository.Save",
			File:       source.FileID("repository.go"),
			Confidence: resolver.ConfidenceStrong,
			Evidence: []resolver.ResolutionEvidence{
				{Kind: resolver.EvidenceSamePackage, Detail: "same pkg"},
			},
		},
	})

	out := resolver.FormatResolution(res)
	if !strings.Contains(out, "Save") {
		t.Error("output missing reference name")
	}
	if !strings.Contains(out, "UserRepository.Save") {
		t.Error("output missing qualified name")
	}
	if !strings.Contains(out, "repository.go") {
		t.Error("output missing file name")
	}
	if !strings.Contains(out, "unique") {
		t.Error("output should mention 'unique'")
	}
}

func TestFormatResolution_Deterministic(t *testing.T) {
	res := makeResolution("F", resolver.ConfidenceCandidate, []resolver.Candidate{
		{Name: "F", Qualified: "A.F", File: "a.go", Confidence: resolver.ConfidenceCandidate},
		{Name: "F", Qualified: "B.F", File: "b.go", Confidence: resolver.ConfidenceCandidate},
	})

	out1 := resolver.FormatResolution(res)
	out2 := resolver.FormatResolution(res)
	if out1 != out2 {
		t.Errorf("FormatResolution is non-deterministic:\n%s\nvs\n%s", out1, out2)
	}
}
