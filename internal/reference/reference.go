package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/magicdrive/ark/internal/source"
)

// ReferenceID is a deterministic identifier for a syntactic reference.
type ReferenceID string

// ReferenceKind classifies how a name is used at the call site.
type ReferenceKind string

const (
	KindRead        ReferenceKind = "read"
	KindWrite       ReferenceKind = "write"
	KindCall        ReferenceKind = "call"
	KindTypeUse     ReferenceKind = "type_use"
	KindInheritance ReferenceKind = "inheritance"
	KindImplements  ReferenceKind = "implementation"
	KindUsesTrait   ReferenceKind = "uses_trait"
	// KindTraitAdaptation names a member mentioned in a trait adaptation
	// (PHP `A::foo insteadof B;`, `foo as bar;`): the class's set of members
	// with that name is not simply the union of its traits'. It forms no edge.
	KindTraitAdaptation ReferenceKind = "trait_adaptation"
	KindImport          ReferenceKind = "import"
	// KindValueReference names another declaration whose value or
	// configuration the enclosing declaration uses (Terraform: an expression
	// `aws_vpc.main.id`, `var.region`, a `provider = aws.west`
	// meta-argument). It is a dependency, not a call.
	KindValueReference ReferenceKind = "value_reference"
	// KindExplicitDependency names a declaration the enclosing one is
	// explicitly ordered after without using its value (Terraform
	// `depends_on`).
	KindExplicitDependency ReferenceKind = "depends_on"
	KindConstruction       ReferenceKind = "construction"
	KindUnknown            ReferenceKind = "unknown"
)

// Reference is a syntactic use of a name in source code.
// Target resolution (Phase 3) is intentionally absent here.
type Reference struct {
	ID           ReferenceID
	Name         string // raw identifier text, e.g. "Save"
	Kind         ReferenceKind
	Language     string
	Location     source.Location
	Container    string // qualified name of enclosing symbol, empty if unknown
	ReceiverExpr string // e.g. "repo" in repo.Save()
	// ReceiverType is provider-proven declared type evidence for the receiver
	// (see language.ReferenceDraft.ReceiverType); "" when not proven.
	ReceiverType string
	// NameQualified / ReceiverTypeQualified are provider-determined qualified
	// identities (see language.ReferenceDraft); "" when there is no such
	// evidence.
	NameQualified         string
	ReceiverTypeQualified string
	// ConfidenceCap bounds the confidence of this reference's resolution (see
	// language.ReferenceDraft.ConfidenceCap); "" when unbounded.
	ConfidenceCap string
	// Dynamic marks a run-time computed name (see language.ReferenceDraft.
	// Dynamic): Name is display text, never matched against declarations.
	Dynamic bool
	// IdentityInRepository qualifies the qualified identities (see
	// language.ReferenceDraft.IdentityInRepository).
	IdentityInRepository bool
	IsCall               bool
}

// NewReferenceID returns a deterministic ID derived from
// (lang, fileID, kind, name, start position).
func NewReferenceID(lang string, file source.FileID, kind ReferenceKind, name string, loc source.Location) ReferenceID {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d:%d",
		lang, string(file), string(kind), name,
		loc.Range.Start.Line, loc.Range.Start.Column)
	sum := sha256.Sum256([]byte(raw))
	return ReferenceID(hex.EncodeToString(sum[:8]))
}
