package symbol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/magicdrive/ark/internal/source"
)

// SymbolID is a deterministic identifier for a symbol.
// It is stable as long as language, file path, kind, and qualified name are unchanged.
type SymbolID string

// SymbolKind is a language-neutral vocabulary of symbol types.
type SymbolKind string

const (
	KindPackage     SymbolKind = "package"
	KindModule      SymbolKind = "module"
	KindNamespace   SymbolKind = "namespace"
	KindClass       SymbolKind = "class"
	KindInterface   SymbolKind = "interface"
	KindStruct      SymbolKind = "struct"
	KindTrait       SymbolKind = "trait"
	KindEnum        SymbolKind = "enum"
	KindType        SymbolKind = "type"
	KindTypeAlias   SymbolKind = "type_alias"
	KindFunction    SymbolKind = "function"
	KindMethod      SymbolKind = "method"
	KindConstructor SymbolKind = "constructor"
	KindField       SymbolKind = "field"
	KindProperty    SymbolKind = "property"
	KindVariable    SymbolKind = "variable"
	KindConstant    SymbolKind = "constant"
	KindParameter   SymbolKind = "parameter"
	KindUnknown     SymbolKind = "unknown"

	// Declarations of configuration languages (Terraform). A resource or data
	// source is a declared infrastructure object, an output a value a module
	// exposes to its callers, a configuration block a declaration of settings
	// (Terraform's terraform/provider/check blocks). Input variables and local
	// values are KindVariable, module calls KindModule.
	KindResource      SymbolKind = "resource"
	KindDataSource    SymbolKind = "data_source"
	KindOutput        SymbolKind = "output"
	KindConfiguration SymbolKind = "configuration"
)

// Symbol is the canonical domain representation of a code symbol.
type Symbol struct {
	ID        SymbolID
	Name      string
	Qualified string // "ReceiverType.Name" for methods, "Name" otherwise
	Kind      SymbolKind
	Language  string

	Location source.Location
	// Parent is the SymbolID of the declaration the provider names as this
	// symbol's enclosing symbol (ParentQualified), when exactly one
	// declaration of the file carries that name and encloses this one; ""
	// otherwise. It is the declaration hierarchy, not the call graph's
	// container.
	Parent          SymbolID
	ParentQualified string // qualified name of the parent container, preserved for resolver matching

	Receiver  string // method receiver type name
	Signature string // optional human-readable signature
	Exported  bool
	// Visibility is the declared member access level ("private", "protected",
	// "public") for languages that have one; "" otherwise (see
	// language.SymbolDraft.Visibility).
	Visibility string `json:",omitempty"`

	// MemberScope / ParameterScope / MembersOutside state where the
	// declarations this symbol's members and parameters denote live (see
	// language.SymbolDraft.MemberScope).
	MemberScope    string `json:",omitempty"`
	ParameterScope string `json:",omitempty"`
	MembersOutside bool   `json:",omitempty"`
}

// NewSymbolID returns a deterministic ID derived from (lang, repoRelPath, kind, qualified).
// It is the ID of the first (or only) declaration with those inputs; the
// others in the same file get NewDeclarationID's ordinal IDs.
func NewSymbolID(lang, repoRelPath string, kind SymbolKind, qualified string) SymbolID {
	return NewDeclarationID(lang, repoRelPath, kind, qualified, 1)
}

// NewDeclarationID returns the ID of one declaration: the ordinal-th (1-based,
// in source order) of a file's declarations that share (lang, repoRelPath,
// kind, qualified) — several Go init functions, a Python or JavaScript
// function defined twice. A declaration is its own symbol, so they must not
// share an ID: the index would merge them into one.
//
// The first keeps the NewSymbolID of its inputs, so a symbol without a
// namesake has the same ID whatever else the file declares, and no position
// enters the ID: editing other code never changes it. Only the second and
// later declarations of one name carry their ordinal, which changes when a
// namesake is inserted before them.
func NewDeclarationID(lang, repoRelPath string, kind SymbolKind, qualified string, ordinal int) SymbolID {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s", lang, repoRelPath, kind, qualified)
	if ordinal > 1 {
		raw += fmt.Sprintf("\x00#%d", ordinal)
	}
	sum := sha256.Sum256([]byte(raw))
	return SymbolID(hex.EncodeToString(sum[:8]))
}
