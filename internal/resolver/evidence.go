package resolver

// EvidenceKind names the rule that produced a resolution match.
type EvidenceKind string

const (
	EvidenceSameLexicalScope  EvidenceKind = "same_lexical_scope"
	EvidenceSameFile          EvidenceKind = "same_file"
	EvidenceSamePackage       EvidenceKind = "same_package"
	EvidenceExplicitImport    EvidenceKind = "explicit_import"
	EvidenceQualifiedReceiver EvidenceKind = "qualified_receiver"
	EvidenceUniqueName        EvidenceKind = "unique_repo_match"
	EvidenceCandidateSet      EvidenceKind = "candidate_set"
)

// ResolutionEvidence records why a particular candidate was matched.
type ResolutionEvidence struct {
	Kind   EvidenceKind
	Detail string // human-readable note, e.g. `imported as "fmt" at line 3`
}
