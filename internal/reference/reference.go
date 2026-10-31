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
	// KindIDReference names an element by its identifier (HTML: an IDREF
	// attribute such as label for="x", a fragment href="#x"). It is a
	// reference, not a call.
	KindIDReference ReferenceKind = "id_reference"
	// KindResourceLink names another file the enclosing one links to by its
	// path (HTML: a href="page.html", iframe src): a resource relation, not
	// a call or an import.
	KindResourceLink ReferenceKind = "resource_link"
	// KindContains names a declaration the enclosing one contains as a
	// part of its own structure, by the language's rules (GitHub Actions: a
	// workflow's jobs, a job's steps): a structural relation, not a call or a
	// use.
	KindContains     ReferenceKind = "contains"
	KindConstruction ReferenceKind = "construction"
	KindUnknown      ReferenceKind = "unknown"
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
	// TargetKinds are the only symbol kinds the reference can denote (see
	// language.ReferenceDraft.TargetKinds); empty when any.
	TargetKinds string
	// Dynamic marks a run-time computed name (see language.ReferenceDraft.
	// Dynamic): Name is display text, never matched against declarations.
	Dynamic bool
	// IdentityInRepository qualifies the qualified identities (see
	// language.ReferenceDraft.IdentityInRepository).
	IdentityInRepository bool
	// TentativeIdentity: NameQualified names the target only if a
	// declaration carries it (language.ReferenceDraft.TentativeIdentity).
	TentativeIdentity bool
	// PathCaseFold: NameQualified's path may name a file written in
	// another case (language.ReferenceDraft.PathCaseFold).
	PathCaseFold bool
	// MayBeAnyFile: unresolved, the reference may denote any file of its
	// language (language.ReferenceDraft.MayBeAnyFile).
	MayBeAnyFile bool
	// FileIdentity: NameQualified is a repository file's path, naming the
	// declaration of that file itself in any language
	// (language.ReferenceDraft.FileIdentity).
	FileIdentity bool
	// NamedArgument: Name is a named argument passed to the receiver (see
	// language.ReferenceDraft.NamedArgument).
	NamedArgument bool
	IsCall        bool
	// ReceiverChain derives the receiver's type from declarations (see
	// language.ReferenceDraft.ReceiverChain); "" when there is none.
	ReceiverChain string
	// Speculative: recorded only in case Name denotes an extracted
	// declaration (see language.ReferenceDraft.Speculative).
	Speculative bool
	// Lookup / ReceiverLookup: the language's search for the declaration
	// Name, or the receiver's type, denotes (see language.ReferenceDraft.
	// Lookup); "" when there is none.
	Lookup         string
	ReceiverLookup string
	// CountsArguments: ArgumentCount is the number of arguments the call
	// passes (see language.ReferenceDraft.CountsArguments).
	CountsArguments bool
	ArgumentCount   int
	// ArgumentTypes: what the provider proves of the arguments' types (see
	// language.ReferenceDraft.ArgumentTypes).
	ArgumentTypes string
}

// NewReferenceID returns a deterministic ID derived from
// (lang, fileID, kind, name, span). The span's end is part of it: two
// references of one name and kind may start at one position — Go
// f(x).Value.f() is a call of f over f(x) and a call of the method f over
// the whole selector — and must not share an ID, which would join one's
// resolution to the other.
func NewReferenceID(lang string, file source.FileID, kind ReferenceKind, name string, loc source.Location) ReferenceID {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d:%d-%d:%d",
		lang, string(file), string(kind), name,
		loc.Range.Start.Line, loc.Range.Start.Column, loc.Range.End.Line, loc.Range.End.Column)
	sum := sha256.Sum256([]byte(raw))
	return ReferenceID(hex.EncodeToString(sum[:8]))
}

// NewReferenceIDIn is NewReferenceID of a reference whose container (the
// qualified name of the declaration it is in) is container: two references
// of one kind and name at one place in different containers — content a YAML
// alias repeats in two steps — are two references. An empty container gives
// NewReferenceID's ID.
func NewReferenceIDIn(lang string, file source.FileID, kind ReferenceKind, name, container string, loc source.Location) ReferenceID {
	if container == "" {
		return NewReferenceID(lang, file, kind, name, loc)
	}
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d:%d-%d:%d\x00%s",
		lang, string(file), string(kind), name,
		loc.Range.Start.Line, loc.Range.Start.Column, loc.Range.End.Line, loc.Range.End.Column, container)
	sum := sha256.Sum256([]byte(raw))
	return ReferenceID(hex.EncodeToString(sum[:8]))
}
