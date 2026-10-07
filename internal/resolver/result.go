package resolver

import (
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Candidate is a single resolved symbol hypothesis.
type Candidate struct {
	SymbolID   symbol.SymbolID
	Name       string
	Qualified  string
	File       source.FileID
	Kind       symbol.SymbolKind
	Confidence Confidence
	Evidence   []ResolutionEvidence
}

// Resolution is the outcome for one Reference.
type Resolution struct {
	ReferenceID reference.ReferenceID
	// ReferenceName is the raw name from the reference, for convenience.
	ReferenceName string
	Candidates    []Candidate
	Confidence    Confidence
	Evidence      []ResolutionEvidence

	// OutsideRepository is set on an Unresolved resolution whose authoritative
	// evidence — a qualified identity, a declared receiver type or an import
	// binding — places the referent outside the repository's declarations: an
	// external type or module, or a binding with no repository target. Such a
	// reference cannot denote any same-named repository symbol. It is metadata
	// only; it never changes candidates, confidence or evidence.
	OutsideRepository bool
}

// HasUniqueTarget reports whether this resolution has exactly one candidate at
// Strong or higher confidence.  Index builders MUST NOT create a canonical
// graph edge for a resolution that returns false.
func (res Resolution) HasUniqueTarget() bool {
	return len(res.Candidates) == 1 && res.Confidence >= ConfidenceStrong
}
