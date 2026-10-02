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
)

// Symbol is the canonical domain representation of a code symbol.
type Symbol struct {
	ID        SymbolID
	Name      string
	Qualified string // "ReceiverType.Name" for methods, "Name" otherwise
	Kind      SymbolKind
	Language  string

	Location        source.Location
	Parent          SymbolID
	ParentQualified string // qualified name of the parent container, preserved for resolver matching

	Receiver  string // method receiver type name
	Signature string // optional human-readable signature
	Exported  bool
}

// NewSymbolID returns a deterministic ID derived from (lang, repoRelPath, kind, qualified).
// Collision behaviour: distinct symbols with identical inputs produce the same ID.
// Callers that need disambiguation should include a structural discriminator in qualified.
func NewSymbolID(lang, repoRelPath string, kind SymbolKind, qualified string) SymbolID {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s", lang, repoRelPath, kind, qualified)
	sum := sha256.Sum256([]byte(raw))
	return SymbolID(hex.EncodeToString(sum[:8]))
}
