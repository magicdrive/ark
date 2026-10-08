package resolver

import (
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

func TestRestrictTargetKinds_RemovesOnlyAndNeverPromotes(t *testing.T) {
	fn := Candidate{SymbolID: "f", Kind: symbol.KindFunction, Confidence: ConfidenceStrong}
	v := Candidate{SymbolID: "v", Kind: symbol.KindVariable, Confidence: ConfidenceStrong}
	ref := reference.Reference{TargetKinds: string(symbol.KindFunction)}

	if got := restrictTargetKinds(Resolution{Candidates: []Candidate{fn}, Confidence: ConfidenceStrong}, ref); !got.HasUniqueTarget() {
		t.Errorf("an allowed unique target must stay: %+v", got)
	}
	if got := restrictTargetKinds(Resolution{Candidates: []Candidate{v}, Confidence: ConfidenceExact}, ref); got.Confidence != ConfidenceUnresolved || len(got.Candidates) != 0 {
		t.Errorf("a disallowed target must leave the reference Unresolved: %+v", got)
	}
	got := restrictTargetKinds(Resolution{Candidates: []Candidate{fn, v}, Confidence: ConfidenceStrong}, ref)
	if got.HasUniqueTarget() || len(got.Candidates) != 1 || got.Confidence > ConfidenceCandidate {
		t.Errorf("narrowing two candidates to one must not make it unique: %+v", got)
	}
	if got := restrictTargetKinds(Resolution{Candidates: []Candidate{v}, Confidence: ConfidenceExact}, reference.Reference{}); got.Confidence != ConfidenceExact {
		t.Errorf("no TargetKinds: unchanged, got %+v", got)
	}
}
