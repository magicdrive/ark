package language

import (
	"context"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Language is a lower-case language identifier, e.g. "go", "typescript".
type Language string

// DiagnosticSeverity classifies how serious a diagnostic is.
type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
)

// Diagnostic represents a problem encountered while indexing a file.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Message  string
	Location source.Location

	// Code is a stable classification set by whoever produced the
	// diagnostic: the index for its own failures (DiagReadError,
	// DiagExtractionError), a provider for what it states. "" means
	// unclassified — never inferred from the message.
	Code string `json:",omitempty"`
}

// Diagnostic codes of the index's own failures. With either, the file is not
// indexed at all (IndexStats.Skipped).
const (
	DiagReadError       = "read_error"
	DiagExtractionError = "extraction_error"
)

// SymbolDraft is the raw extraction result from a language provider before
// SymbolIDs are assigned.
type SymbolDraft struct {
	Name      string
	Qualified string
	Kind      symbol.SymbolKind
	Location  source.Location

	// StartByte / EndByte are retained from the parser for source-range queries.
	// They are an internal detail; callers should use Location for coordinates.
	StartByte uint32
	EndByte   uint32

	Parent    string // qualified name of enclosing symbol, if any
	Receiver  string // method receiver type name
	Signature string
	Exported  bool

	// Visibility is the member's declared access level when the language has
	// one ("private", "protected", "public"); "" when not applicable. A
	// private member is not inherited: member lookup through a supertype
	// never reaches it.
	Visibility string `json:",omitempty"`

	// MemberScope is set when the language fixes, from this declaration's own
	// text, which declarations its members denote: a member named N, reached
	// through a receiver whose ReceiverTypeQualified is this symbol's
	// Qualified, denotes exactly the declaration whose Qualified is
	// MemberScope+N (plain concatenation; the provider includes any
	// separator). Example: a Terraform module call whose source is a local
	// path — its outputs are the child module's output declarations. ""
	// means "no such evidence"; members are then looked up as usual.
	MemberScope string `json:",omitempty"`

	// ParameterScope is MemberScope's counterpart for named arguments: an
	// argument named N that a reference passes through this declaration
	// (ReferenceDraft.NamedArgument, its ReceiverTypeQualified this symbol's
	// Qualified) denotes exactly the parameter declaration whose Qualified is
	// ParameterScope+N. Example: a Terraform module call whose source is a
	// local path — its arguments are the child module's input variables.
	ParameterScope string `json:",omitempty"`

	// MembersOutside states that the declarations this symbol's members and
	// parameters denote are, by the language's rules, not part of the
	// repository (a Terraform module call whose source is a registry or
	// remote address). A member or argument reference through it is then
	// OutsideRepository. Never set together with MemberScope or
	// ParameterScope.
	MembersOutside bool `json:",omitempty"`
}

// ReferenceDraft is a raw syntactic reference before ReferenceIDs are assigned.
type ReferenceDraft struct {
	Name         string
	Kind         string // use reference.ReferenceKind values
	Container    string // qualified name of enclosing symbol, empty if unknown
	Location     source.Location
	ReceiverExpr string // e.g. "repo" in repo.Save()
	IsCall       bool

	// ReceiverType is the declared type of the receiver expression, set ONLY
	// when the provider can prove it from local, structural evidence: an
	// explicit type annotation, a method receiver, the enclosing class of
	// this/$this/self, a construction assigned to a variable whose type cannot
	// change, or a same-file field with such a declared type. It is the type
	// name as written in the reference's scope and is resolved like any other
	// type name. It is never a type-inference result; "" means "not proven".
	ReceiverType string `json:",omitempty"`

	// NameQualified is the qualified identity of the declaration that Name
	// denotes (the same string a repository symbol of that declaration carries
	// as Symbol.Qualified), fixed by the language's own lexical name-resolution
	// rules — scopes, imports, aliases, explicit qualification — and by nothing
	// else: the provider never consults the repository. It is set only when
	// those rules yield exactly one identity, only for a reference whose Name
	// is itself a declaration name (ReceiverExpr == ""), and never for a member
	// name: a member's home (the type itself, a parent, a trait, ...) is not
	// lexically knowable. "" means "no qualified identity evidence"; the
	// reference then resolves exactly as before.
	NameQualified string `json:",omitempty"`

	// ReceiverTypeQualified is the qualified identity of the receiver's type,
	// determined like NameQualified: for a value receiver, that of its proven
	// ReceiverType; for static member access (`Type::member`), where the
	// receiver expression is itself a type name, that of the named type; for
	// a receiver that names a declaration stating its members' home
	// (SymbolDraft.MemberScope, e.g. a Terraform module call), that of the
	// declaration. It identifies the receiver only; looking up Name among its
	// members is the resolver's job.
	ReceiverTypeQualified string `json:",omitempty"`

	// ConfidenceCap bounds how strongly the resolver may claim this
	// reference's target: "" (no bound), "strong" or "candidate". A provider
	// sets it when its evidence identifies WHICH target but cannot exclude
	// every alternative — e.g. a receiver type proven only from the code of
	// one class, while other code may also write the receiver. The resolver
	// applies it only downwards, min(resolved, cap); it never raises a
	// resolution. Any other non-empty value is treated as "candidate".
	ConfidenceCap string `json:",omitempty"`

	// TargetKinds, when set, are the only symbol kinds the reference can
	// denote: the language's syntax fixes the kind but not the target (Go
	// `f[x](...)` calls f only if f is a generic function or type; were it
	// a variable, the call would be of one of its elements). The resolver
	// removes candidates of any other kind — a resolution left with none is
	// Unresolved — and never raises a confidence for it. It is a comma-separated
	// list of symbol.SymbolKind values (a string keeps the draft comparable);
	// empty means any.
	TargetKinds string `json:",omitempty"`

	// Dynamic marks a reference whose name is computed at run time — the
	// language's syntax fixes that something is called or constructed here,
	// but not which name (PHP `$obj->$m()`, `$fn()`, `new $cls()`; JavaScript
	// `obj[k]()`). Name is then the name expression as written, for display
	// only. The resolver never matches it against declarations: the reference
	// is observed but Unresolved, and completeness reports it as an
	// unresolved reference of its container. A provider sets it only when the
	// syntax itself is a call/construction; it never sets it to hide a name
	// it could have extracted.
	Dynamic bool `json:",omitempty"`

	// IdentityInRepository qualifies NameQualified / ReceiverTypeQualified:
	// the identity names a scope of the repository itself (Terraform: the
	// module directory a reference is written in), so no declaration with
	// that identity means the name is undeclared, or declared in syntax no
	// provider extracts — Unresolved, never OutsideRepository — and no
	// declaration with another identity can be its target, whatever its
	// name. Without it a qualified identity with no declaration is taken to
	// lie outside the repository.
	IdentityInRepository bool `json:",omitempty"`

	// NamedArgument marks a reference whose Name is a named argument passed
	// to the declaration its ReceiverTypeQualified identifies: it denotes
	// that declaration's parameter (SymbolDraft.ParameterScope), never one of
	// its members. Without a stated parameter scope it is Unresolved.
	NamedArgument bool `json:",omitempty"`
}

// ImportDraft is a raw import extracted from a source file.
type ImportDraft struct {
	Path     string // import path, e.g. "fmt" or "github.com/foo/bar"
	Alias    string // "" = use base name, "." = dot-import, "_" = blank
	Location source.Location
}

// ModuleCandidate is one repository-local file that a module specifier may
// denote, with the provider-assigned resolution priority.
//
// Priority is explicit provider evidence, not incidental ordering: a provider
// assigns distinct priorities only within the deterministic, config-independent
// lexical subset it explicitly supports (e.g. an extension-substitution order).
// That subset is the provider's contract; it is not a claim of compiler- or
// runtime-equivalent module resolution. Candidates the provider does not rank
// MUST share a priority. Lower values win.
type ModuleCandidate struct {
	File     source.FileID
	Priority int `json:",omitempty"`
}

// ModuleSpec is a module specifier exactly as written plus the
// repository-local files it may denote. Candidates are computed purely
// lexically by the provider from the importing FileID (no filesystem access),
// are root-relative, slash-separated and cleaned, never escape the repository
// root, and are sorted by (Priority, File) without duplicates.
//
// A nil Candidates means the provider's policy says the specifier is not
// repository-resolvable (external package, unsupported path alias, ...). Such a
// module is never matched against repository symbols by name.
type ModuleSpec struct {
	Specifier  string
	Candidates []ModuleCandidate `json:",omitempty"`
}

// BindingKind classifies how an import binding attaches a local name.
type BindingKind string

const (
	// BindingNamed binds Local to one exported name (Imported) of Module.
	// Providers normalise language-specific forms (e.g. an ES default import
	// binds the export named "default").
	BindingNamed BindingKind = "named"
	// BindingNamespace binds Local to the whole export table of Module.
	BindingNamespace BindingKind = "namespace"
)

// BindingDraft is a module import binding: it introduces one local name into
// the importing file's scope. It is distinct from ImportDraft (a module-level
// dependency) and from ExportDraft (what a module offers to importers).
type BindingDraft struct {
	Local    string // name introduced into this file's scope (required)
	Kind     BindingKind
	Module   ModuleSpec
	Imported string // BindingNamed: export name in Module (required); BindingNamespace: ""
	TypeOnly bool   `json:",omitempty"` // usable only in type positions
	Location source.Location
}

// ExportKind classifies an export binding.
type ExportKind string

const (
	// ExportLocal exports a name bound in this file's own scope (a top-level
	// declaration or an import binding) under Exported.
	ExportLocal ExportKind = "local"
	// ExportFrom re-exports one export (Local) of Module under Exported
	// without creating a local binding.
	ExportFrom ExportKind = "from"
	// ExportAll re-exports every export of Module except the names in Except.
	ExportAll ExportKind = "all"
	// ExportNamespace re-exports Module's whole export table as one name.
	ExportNamespace ExportKind = "namespace"
)

// ExportDraft declares one name a module offers to importers. When a provider
// emits ExportDrafts for a file, they are that module's complete export table:
// SymbolDraft.Exported is NOT consulted for module export resolution.
type ExportDraft struct {
	Kind     ExportKind
	Exported string     // name visible to importers; "" for ExportAll
	Local    string     // ExportLocal: name in this scope; ExportFrom: export name in Module; else ""
	Module   ModuleSpec // zero value for ExportLocal
	Except   []string   `json:",omitempty"` // ExportAll only: names never forwarded
	TypeOnly bool       `json:",omitempty"`
	Location source.Location
}

// Extraction is the output of a single-file extraction pass.
type Extraction struct {
	Symbols     []SymbolDraft
	References  []ReferenceDraft
	Imports     []ImportDraft
	Diagnostics []Diagnostic

	// Bindings and Exports are the file's module bindings (see BindingDraft /
	// ExportDraft). Providers that do not model module bindings leave them nil.
	Bindings []BindingDraft
	Exports  []ExportDraft

	// ModuleScoped declares that names in this file resolve ONLY through local
	// declarations, explicit Bindings and other structural evidence. For such a
	// file, directory proximity and repository-wide name uniqueness are not
	// evidence (a free name is capped at Candidate) and the legacy implicit
	// ImportDraft alias match is not applied.
	ModuleScoped bool

	// IdentityOnly declares that the symbols of this file are denoted only by
	// qualified identity (NameQualified / ReceiverTypeQualified, R0): no
	// name-based resolution stage — same directory, import, receiver-name,
	// unique name, qualified-name suffix — may resolve any reference to them.
	// A provider sets it when its names are scoped by rules a name match
	// cannot see (Terraform: an address is unique only within its module
	// directory), so a reference of another file, or of another language,
	// can never reach them by similarity. Conversely a reference of this file
	// without qualified identity evidence is Unresolved.
	IdentityOnly bool
}

// Provider extracts code intelligence from a single source file.
// Implementations own their parser state; Tree-sitter (or any other parser)
// must not escape through this interface.
type Provider interface {
	Language() Language
	Extensions() []string
	Extract(ctx context.Context, file source.FileID, src []byte) (Extraction, error)
	// CacheVersion returns a version string that must change whenever the
	// provider's extraction semantics change (grammar upgrade, query change, etc.).
	// It is embedded in cache keys to invalidate stale extraction results.
	CacheVersion() string
}
