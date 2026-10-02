package resolver

import (
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Candidate is a single resolved symbol hypothesis.
type Candidate struct {
	SymbolID  symbol.SymbolID
	Name      string
	Qualified string
	File      source.FileID
	Kind      symbol.SymbolKind
	Confidence Confidence
	Evidence  []ResolutionEvidence
}

// Resolution is the outcome for one Reference.
type Resolution struct {
	ReferenceID reference.ReferenceID
	// ReferenceName is the raw name from the reference, for convenience.
	ReferenceName string
	Candidates    []Candidate
	Confidence    Confidence
	Evidence      []ResolutionEvidence
}

// HasUniqueTarget reports whether this resolution has exactly one candidate at
// Strong or higher confidence.  Index builders MUST NOT create a canonical
// graph edge for a resolution that returns false.
func (res Resolution) HasUniqueTarget() bool {
	return len(res.Candidates) == 1 && res.Confidence >= ConfidenceStrong
}
