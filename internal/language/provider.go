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

	// DiagSymbolIDCollision: two distinct declarations share a SymbolID.
	// The index is then not built at all (index.IdentityCollisionError).
	DiagSymbolIDCollision = "symbol_id_collision"

	// DiagReferenceIDCollision: a reference whose ReferenceID another
	// reference of the index already has — a provider stating one reference
	// twice, or a hash collision. It is left out of the index (a warning):
	// joining one reference's resolution to the other would be fabrication.
	DiagReferenceIDCollision = "reference_id_collision"
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

	// Placement states, for a member of a language that has both, whether
	// it is static (symbol.PlacementStatic: on the type itself, reached
	// through its name) or an instance member (symbol.PlacementInstance:
	// reached through a value of the type). The resolver then looks a member
	// up only where the receiver can reach it. "" states nothing. In a file
	// whose members share names (Extraction.MembersShareNames), a KindField
	// member a supertype places on instances (a Python `self.x = …`) is an
	// instance's own attribute: it hides a subtype's member of the name, which
	// a member lookup then finds as a Candidate only.
	Placement string `json:",omitempty"`

	// ConfidenceCap bounds how strongly a reference may be resolved to this
	// declaration: "" (no bound), "strong" or "candidate". A provider sets it
	// when the language binds the declaration's name to a value code computes
	// from it, by code the provider does not model — a Python function or
	// class under a decorator it does not know: the name denotes the
	// decorator's result, not proven to be the declaration. The resolver
	// applies it, only downwards, to a resolution whose one candidate this
	// declaration is (ReferenceDraft.ConfidenceCap's values and rules).
	ConfidenceCap string `json:",omitempty"`

	// Parameters states a callable declaration's parameters where the
	// language selects among callables of one name by them (Java
	// overloads): "(T1,T2)", "()" for none, each type as the provider writes
	// it after erasure, the last ending in "..." when the callable is
	// variadic. A type the provider cannot prove to be one type wherever it
	// is written (a simple name the file's imports and package decide)
	// starts with "?". "" states nothing. A call stating its argument count
	// (ReferenceDraft.CountsArguments) is resolved only among declarations
	// that accept that many arguments, and a member lookup (ReferenceDraft.
	// Lookup) leaves out a supertype's callable that a subtype's callable of
	// equal Parameters overrides.
	Parameters string `json:",omitempty"`

	// ImplicitMembers lists the members the language gives this type
	// declaration that no declaration of the repository states, as name and
	// Parameters, comma-separated ("toString(),equals(?Object)"): those of an
	// implicit supertype outside the repository (Java: java.lang.Object of a
	// class without extends, java.lang.Enum of an enum), and, marked with a
	// leading "+", those it declares itself implicitly (an enum's values(), a
	// record's accessors, a default constructor "<init>()"). A member lookup
	// (ReferenceDraft.Lookup) that reaches one has found the member there: it
	// looks no further, and the member, being no symbol, is no target — one
	// of an outside supertype is outside the repository. "" states nothing.
	ImplicitMembers string `json:",omitempty"`

	// Overrides states, by the language's own annotation of the declaration
	// (Java @Override), that a supertype has a member of its name, which it
	// overrides or implements. A member lookup (ReferenceDraft.Lookup) that
	// finds the declaration, and no supertype declaration of the name, while
	// a supertype is outside the repository, knows a member of the name it
	// cannot see there — one that may be an overload the call denotes: a
	// Candidate beside it.
	Overrides bool `json:",omitempty"`
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

	// FileIdentity qualifies NameQualified: it is the path of a repository
	// file (root-relative, slash-separated, clean) that the reference's own
	// text names as a file — an HTML <link rel="stylesheet" href>, a
	// <script src> — whatever the language of that file. The target is the
	// declaration of the file itself: a symbol whose Qualified is its own
	// FileID (an HTML document, a CSS stylesheet, a JavaScript script), of
	// any language. Explicit path evidence names its target itself, so this
	// is the one relation that joins two name spaces; no name of one
	// language ever reaches a declaration of another. TargetKinds restricts
	// the kinds it may denote.
	FileIdentity bool `json:",omitempty"`

	// NamedArgument marks a reference whose Name is a named argument passed
	// to the declaration its ReceiverTypeQualified identifies: it denotes
	// that declaration's parameter (SymbolDraft.ParameterScope), never one of
	// its members. Without a stated parameter scope it is Unresolved.
	NamedArgument bool `json:",omitempty"`

	// ReceiverChain states how the receiver is built from declarations it
	// names (typefacts.go: x := NewServer() makes x "decl:NewServer|call:0"),
	// for a receiver without a proven ReceiverType. It is no inference: each
	// step is what the code writes, and the resolver evaluates it against
	// the declarations' own TypeFacts — authoritative only when every step
	// names exactly one declaration; otherwise the reference resolves as if
	// it carried no chain. A provider sets it only where its language's
	// rules make the derivation the receiver's static type (a variable whose
	// type cannot change, used where its one declaration is in scope).
	ReceiverChain string `json:",omitempty"`

	// TentativeIdentity qualifies NameQualified: the identity names the
	// target only if a declaration carries it. The provider reads the
	// receiver's type from what another file states (the result type a
	// function declares, a member of it) and cannot tell whether Name is a
	// declaration of that type or something no declaration is (a field
	// holding a function): no declaration with the identity leaves the
	// reference Unresolved — neither outside the repository nor excluded
	// from any same-named declaration's completeness (IdentityInRepository
	// claims both, and is not set with it). Such a reference is a member: it
	// carries its ReceiverExpr, which the identity decides before (R0).
	TentativeIdentity bool `json:",omitempty"`

	// PathCaseFold qualifies NameQualified, whose leading part (up to the
	// first "::") is a file's path: a file whose path differs from it in
	// case alone may be the one meant (a case-insensitive file system opens
	// it). No declaration with the identity, the identity read in such a
	// file names Candidates — never more: on a case-sensitive file system
	// the path names no file.
	PathCaseFold bool `json:",omitempty"`

	// MayBeAnyFile states that the reference, unresolved, may denote the
	// declaration of any file of its language, whatever its name: an import
	// of a module a build maps by code Ark does not evaluate (Zig
	// `@import("name")`). Completeness counts it toward every file's
	// declaration, so no file's importers read complete while it exists.
	MayBeAnyFile bool `json:",omitempty"`

	// Speculative marks a reference the provider records only in case Name
	// denotes a declaration Ark extracts: a selector x.f whose receiver has
	// type evidence, where f may equally be a member no provider extracts
	// as a symbol (a Go struct field). Resolved to a candidate it is an
	// ordinary reference; resolved to none — unresolved, or even proven
	// outside the repository (a member of an external type may as well be a
	// field) — it is no reference at all: the index drops it, and it is
	// never counted as unresolved or outside. Its resolution never falls
	// back to name matching.
	Speculative bool `json:",omitempty"`

	// Lookup states how the language finds the declaration Name denotes
	// where the file alone cannot fix it — the scopes it searches in order,
	// some of them other files' (the members a supertype declares, the
	// types of the file's package): steps separated by ";", each a set of
	// alternatives separated by "|":
	//
	//	=K:Q    the declarations of kind class K whose Qualified is Q
	//	@K:T.N  the members named N of kind class K of the type declaration
	//	        whose Qualified is T (T is the text before the last "."):
	//	        its own and those it inherits (below)
	//	?       a scope where the language may find the name, which Ark
	//	        cannot see
	//	!       the name is found here and denotes nothing Ark represents
	//	        (a local variable): the search ends without a target
	//
	// An "=" or "@" alternative may be followed by a member path —
	// "/N" a member type N, "~N" a field or constant N, else a member type N
	// — that the declaration it finds, a type, is followed through: the
	// alternative is found once its own declaration is (the language took
	// the head for a type), and the path's end is what it finds; a value on
	// the way, or a member missing, ends it without a target.
	//
	// K is "T" (a type-like declaration), "V" (a field or constant), "M" (a
	// method) or "C" (a constructor). The resolver takes every declaration
	// the first step that finds any finds; for a call that states its
	// argument count, those that accept it. One is the target; several are
	// Candidates; none is Unresolved. A step passed without finding anything
	// whose scope may hold the name outside what the repository declares —
	// an identity no declaration carries, a member lookup reaching a
	// supertype the repository does not declare, "?" — bounds the result at
	// Strong, and, when no step finds anything, puts the referent outside
	// the repository (unless IdentityInRepository).
	//
	// A member lookup finds, for M, the methods of the name the type
	// declares and those its supertypes have that none of these overrides
	// (equal Parameters; an override proven only by types starting with "?"
	// bounds the result at Strong); for T and V, those the type declares,
	// or else those its supertypes have, each searched the same way (several
	// distinct: ambiguity); for C, the type's own. A supertype's private
	// member is not inherited, nor a member without Visibility from a file
	// of another Extraction.Package. A type's supertypes are its file's
	// inheritance and implements references whose Container is the type's
	// Qualified, identified by NameQualified or by their own Lookup; one the
	// repository does not declare leaves what it may contribute unknown
	// (Strong), several declarations of it leave it ambiguous (Candidate).
	// A static method of a type of kind interface is not inherited. The
	// type's SymbolDraft.ImplicitMembers count as found. Of the callables
	// found, those the reference may not access are no candidates: a
	// private one outside its outermost enclosing declaration, one without
	// Visibility outside its Extraction.Package; a protected one of another
	// Package, and a private one whose or whose reference's enclosing
	// declarations cannot be followed, is kept, but neither ends the phases argument types are tried
	// in nor is the one they narrow the others to. A type whose declaration states a ConfidenceCap (its members
	// cannot all be listed) bounds what is found in it, and what a later
	// step finds past it, at Candidate, and what it does not show is not
	// outside the repository. Lookup is evaluated before, and instead of,
	// every other rule; "" states nothing.
	Lookup string `json:",omitempty"`

	// ReceiverLookup states, in Lookup's notation, how the language finds
	// the type of the receiver: the declaration found must be one type —
	// the receiver itself, when ReceiverType is "" (static access, a
	// qualified type name), or the declared type of the receiver value
	// otherwise. Name is then looked up as its member (kind class M for a
	// call, C for the name "<init>", T for a type use, V otherwise). A field
	// or constant found instead is a value whose type Ark does not know:
	// the reference is Unresolved.
	ReceiverLookup string `json:",omitempty"`

	// CountsArguments states that ArgumentCount is the number of arguments
	// the call passes: it is resolved only among the callables whose
	// SymbolDraft.Parameters accept that many (or that state none).
	CountsArguments bool `json:",omitempty"`
	ArgumentCount   int  `json:",omitempty"`

	// ArgumentTypes states, with CountsArguments, what the provider proves
	// of each argument's type, comma-separated: a primitive (byte, short,
	// char, int, long, float, double, boolean) or an array of one ("int[]"),
	// "null", "S" (a string: never a primitive), "F" (a lambda or a method
	// reference: a functional interface, never a primitive or an array), "N"
	// (a reference type that is no array and no type unboxing converts:
	// never a primitive), "R" (a reference type that is no array) or "R[]"
	// (an array of references), "" nothing. The resolver leaves out the callables an argument can
	// certainly not be passed to — by the conversions method invocation
	// allows (a primitive widens byte < short < int < long < float < double,
	// char < int; boxing and unboxing between a primitive and a reference;
	// an array is a reference) in the phases that try them in turn: strict
	// (no boxing), loose, variadic —; a callable whose parameters are the
	// arguments' known primitive types themselves, applying strictly, is the
	// most specific, and so is, among callables whose parameters are all
	// primitives, of primitive arguments, the one whose parameters widen to
	// every other's (JLS 15.12.2). Several left are Candidates.
	ArgumentTypes string `json:",omitempty"`
}

// AliasDraft states that the qualified identity Qualified names, by the
// language's own rules, whatever the identity Target names: a declaration
// that re-exports another (Rust `pub use a::b::C;` in module m: m::C names
// a::b::C). Target is fixed as NameQualified is — by the language's lexical
// rules, never by similarity. A reference whose NameQualified (or a prefix of
// it, path segment by path segment) is an alias resolves to what the target
// names (resolver R0); an alias declares nothing itself.
type AliasDraft struct {
	Qualified string
	Target    string
	Location  source.Location

	// Members states that Qualified is itself a declaration — a constant
	// whose value is what Target names (Zig `pub const Vec = vec.Vec;`) —
	// so the alias reads only the identities below it: Qualified::x names
	// Target::x, while Qualified itself names its own declaration and never
	// Target's. Without it the alias reads Qualified too.
	Members bool `json:",omitempty"`

	// ConfidenceCap bounds every resolution that follows the alias: ""
	// (no bound), "strong" or "candidate" (ReferenceDraft.ConfidenceCap's
	// values and rules). A provider sets it when the alias rests on weaker
	// evidence than the language's own rules — a module name a build script
	// maps statically, which a dependency's module of the name may stand
	// for instead (Zig build.zig). It only lowers.
	ConfidenceCap string `json:",omitempty"`

	// Open states that what the alias stands for may also be something no
	// declaration Ark has is (a module a build makes by means Ark does not
	// read): a resolution that follows it is open — its candidates do not
	// bound the referent, and completeness counts the reference as it
	// counts an unresolved one, besides its candidates. Set with a cap.
	Open bool `json:",omitempty"`

	// Fallback states an alias read only where its identity names nothing
	// else — no declaration carries it and no other alias of it leads to
	// anything declared or aliased — and only on a reading that found no
	// declaration otherwise (a name a build registers nowhere Ark reads, a
	// bound file the index does not hold, stands for what Target names).
	Fallback bool `json:",omitempty"`

	// Absent states the alias holds only where no file of the language has
	// this path in the index — a path the walk does not reach (through a
	// symbolic link, under a directory the index skips, generated): what
	// it would name is not a file Ark sees (Zig: a module rooted there may
	// hold any file).
	Absent string `json:",omitempty"`
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

	// Unless lists repository files whose presence voids the candidate: the
	// specifier denotes File only where none of them exists (Python: a
	// directory is a root of absolute imports only when it is no package,
	// i.e. has no __init__.py). Paths are clean and root-relative like File;
	// the resolver checks them against the indexed files.
	Unless []source.FileID `json:",omitempty"`
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

	// Scope is the qualified name of the declaration whose body the binding
	// is local to (a Python import inside a function): it binds Local only
	// for the references whose Container is Scope or a declaration nested in
	// it (Scope + "." + …), and there it shadows the bindings of Local with
	// a shorter Scope. "" binds Local in the whole file. It never makes a
	// module export: an export names a binding of the whole file.
	Scope string `json:",omitempty"`

	// Fallback marks a binding the language applies only where the other
	// bindings of Local reach no name the module holds: Python's
	// `from pkg import n` is the submodule pkg.n unless pkg binds n itself.
	// A lookup through the other bindings that reaches an ExportOpaque name
	// leaves it out; otherwise it is merged with them like any binding (a
	// module that also declares n is ambiguity).
	Fallback bool `json:",omitempty"`
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
	// ExportOpaque states that the module binds Exported to a value no
	// declaration or binding of it proves (a Python name a loop, a del or a
	// mix of a declaration and an import rebinds); Exported "" states that
	// any name may be bound so (a star import, a module __getattr__). A
	// lookup of such a name reaches nothing — the repository's,
	// unrepresented — and the Fallback bindings of the importer do not apply.
	ExportOpaque ExportKind = "opaque"
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
	// Mutable marks an export whose value code may replace at run time by
	// routes no static rule closes (a CommonJS exports object property:
	// module.exports = { a }, exports.a = f). The export maps the name to its
	// source declaration, but the value an importer reads is not proven to
	// be that declaration: the resolver caps a resolution through it at
	// Candidate. An ESM export (a live binding) is not Mutable.
	Mutable  bool `json:",omitempty"`
	Location source.Location
}

// Extraction is the output of a single-file extraction pass.
type Extraction struct {
	Symbols     []SymbolDraft
	References  []ReferenceDraft
	Imports     []ImportDraft
	Diagnostics []Diagnostic

	// Aliases are qualified identities the file declares to name other
	// identities (see AliasDraft). Providers without such declarations
	// leave them nil.
	Aliases []AliasDraft

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

	// Package names the package the file's top-level declarations belong to
	// within its directory ("" when the language has no such notion). Two
	// files of one directory with different Packages share no scope (Go: a
	// package and its external test package foo_test).
	Package string

	// PackageScoped declares package scoping by the language's rules: a name
	// without a receiver denotes a declaration of the file's own package
	// (same directory and Package) or of a package imported with Alias "."
	// — never a declaration elsewhere in the repository, however unique its
	// name — and an ImportDraft path names the repository directory whose
	// path ends the import path, under the repository's import-path prefix.
	// A receiver naming an import denotes that package's declarations only.
	PackageScoped bool

	// LexicalNesting declares that a declaration of the file nested in
	// another one's body (a symbol with a Parent and no Receiver: a Python
	// function or class defined in a function) is visible by its name only
	// to the references within that body (Container Parent or nested in
	// it). A name-based stage never offers it to a reference elsewhere.
	LexicalNesting bool

	// MembersShareNames declares that the language gives a type's methods
	// and properties one name space (ECMAScript): a call of a member name
	// denotes whichever member of that name is nearest — a function-typed
	// property hides a supertype's method. Without it, a call names a
	// callable member only (PHP keeps methods and properties apart).
	MembersShareNames bool

	// SourceRoot names the directory the file's qualified identities are laid
	// out under (Java: where the path of its package starts; "src/main/java"
	// of src/main/java/a/b/C.java in package a.b). A qualified identity one
	// of the file's references names (ReferenceDraft.Lookup) denotes first
	// the declarations of the files of the same SourceRoot — the sources a
	// build compiles together hide a namesake elsewhere on its path —; a
	// declaration only another root holds is at most Strong (that root may
	// be no part of the build), and several only other roots hold are
	// ambiguous. "" states nothing: every declaration of the identity is
	// alike.
	SourceRoot string

	// TypeFacts are the declared-type facts of the file's declarations
	// (typefacts.go), the evidence ReceiverChain and promoted-member lookup
	// are evaluated against. A provider states only what a declaration's own
	// text writes.
	TypeFacts []TypeFact `json:",omitempty"`
}

// Dialect is implemented by a provider whose language is a dialect of
// another language: the files of both share one name space (TSX of
// TypeScript). Without it a provider's language is its own name space.
type Dialect interface {
	DialectOf() Language
}

// NameSpace returns the name space of p's files. A reference is resolved by
// name only among the declarations of files of its own name space: a name
// shared with another language's declaration is no evidence. Explicit
// evidence (bindings, qualified identity) names its targets itself.
func NameSpace(p Provider) Language {
	if d, ok := p.(Dialect); ok {
		return d.DialectOf()
	}
	return p.Language()
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
