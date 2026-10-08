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
	// EvidenceQualifiedIdentity: resolved by exact match of a provider-determined
	// qualified identity (language.ReferenceDraft.NameQualified /
	// ReceiverTypeQualified) against repository declarations. It is
	// authoritative: when it applies no proximity, uniqueness or suffix
	// heuristic is consulted, whatever the outcome.
	EvidenceQualifiedIdentity EvidenceKind = "qualified_identity"
	// EvidenceConfidenceCap: the provider bounded the reference's confidence
	// (reference.Reference.ConfidenceCap); the resolution was lowered to it.
	EvidenceConfidenceCap EvidenceKind = "confidence_cap"
	// EvidenceDynamicName: the reference's name is computed at run time
	// (reference.Reference.Dynamic); it is Unresolved by construction.
	EvidenceDynamicName EvidenceKind = "dynamic_name"
	// EvidenceMemberScope: the receiver declaration states which declarations
	// its members or parameters denote (symbol.Symbol.MemberScope /
	// ParameterScope / MembersOutside).
	EvidenceMemberScope EvidenceKind = "member_scope"
	// EvidenceIdentityOnly: the reference is written in a file whose names
	// resolve only by qualified identity, and it carries none.
	EvidenceIdentityOnly EvidenceKind = "identity_only"
	// EvidenceTargetKind: the reference can denote only some symbol kinds
	// (reference.Reference.TargetKinds); candidates of others were removed.
	EvidenceTargetKind EvidenceKind = "target_kind"
)

// ResolutionEvidence records why a particular candidate was matched.
type ResolutionEvidence struct {
	Kind   EvidenceKind
	Detail string // human-readable note, e.g. `imported as "fmt" at line 3`
}
