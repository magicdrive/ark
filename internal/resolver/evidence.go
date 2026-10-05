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

	// EvidenceModuleBinding: resolved through an explicit module binding
	// (import binding → repository module → export table, incl. re-exports).
	EvidenceModuleBinding EvidenceKind = "module_binding"
	// EvidenceReceiverType: member resolved under provider-proven declared
	// receiver type evidence (language.ReferenceDraft.ReceiverType).
	EvidenceReceiverType EvidenceKind = "receiver_type"
	// EvidenceUntypedReceiver: the receiver is a variable/expression with no
	// type evidence; name matches are capped at Candidate.
	EvidenceUntypedReceiver EvidenceKind = "untyped_receiver"
	// EvidenceModuleScope: the file is module-scoped and the name has no local
	// declaration or binding; proximity/uniqueness matches are capped at Candidate.
	EvidenceModuleScope EvidenceKind = "module_scope"
)

// ResolutionEvidence records why a particular candidate was matched.
type ResolutionEvidence struct {
	Kind   EvidenceKind
	Detail string // human-readable note, e.g. `imported as "fmt" at line 3`
}
