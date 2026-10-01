package syntax

// SymbolKind represents the kind of a symbol
type SymbolKind string

const (
	SymbolFunction  SymbolKind = "function"
	SymbolMethod    SymbolKind = "method"
	SymbolType      SymbolKind = "type"
	SymbolStruct    SymbolKind = "struct"
	SymbolInterface SymbolKind = "interface"
	SymbolClass     SymbolKind = "class"
	SymbolVariable  SymbolKind = "variable"
	SymbolConstant  SymbolKind = "constant"
	SymbolEnum      SymbolKind = "enum"
	SymbolProperty  SymbolKind = "property"
	SymbolTypeAlias SymbolKind = "type_alias"
)

// Symbol represents a code symbol (function, type, class, etc.)
// Line numbers are 1-based for human readability (per PROMPT.md Section 12)
type Symbol struct {
	Name      string     `json:"name"`
	Kind      SymbolKind `json:"kind"`
	StartLine uint32     `json:"startLine"` // 1-based
	EndLine   uint32     `json:"endLine"`   // 1-based
	StartCol  uint32     `json:"startCol"`  // 1-based
	EndCol    uint32     `json:"endCol"`    // 1-based
	StartByte uint32     `json:"startByte"` // 0-based byte offset
	EndByte   uint32     `json:"endByte"`   // 0-based byte offset
	// Optional fields
	Receiver string `json:"receiver,omitempty"` // For methods: receiver type name
	Parent   string `json:"parent,omitempty"`   // For nested symbols: parent name
	Exported bool   `json:"exported,omitempty"` // Whether the symbol is exported/public
}

// FileSymbols holds all symbols found in a file
type FileSymbols struct {
	Path     string   `json:"path"`
	Language string   `json:"language"`
	Symbols  []Symbol `json:"symbols"`
}

// SymbolMatch represents a symbol found during search
type SymbolMatch struct {
	Path   string `json:"path"`
	Symbol Symbol `json:"symbol"`
}

// SymbolSearchResult holds the results of a symbol search
type SymbolSearchResult struct {
	Query   string        `json:"query"`
	Matches []SymbolMatch `json:"matches"`
}
