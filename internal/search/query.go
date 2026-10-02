package search

import (
	"github.com/magicdrive/ark/internal/symbol"
)

// Query describes structural search predicates applied to a RepositoryIndex.
// All non-zero fields are combined with AND semantics.
type Query struct {
	// Symbol filters
	Kind        symbol.SymbolKind // "" = any kind
	NamePattern string            // case-insensitive substring match on Name
	Exported    *bool             // nil = any

	// Reference filters — restrict to symbols whose body contains matching refs
	CallsName string // symbol calls a function/method whose Name contains this
	UsesType  string // symbol references a type whose Name contains this

	// File filters
	Language         string // "" = any language
	FilePattern      string // case-insensitive substring match on FileID
	ExcludeTest      bool
	ExcludeGenerated bool
}
