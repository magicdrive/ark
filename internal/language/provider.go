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

// Diagnostic represents a problem encountered during extraction.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Message  string
	Location source.Location
}

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
}

// ReferenceDraft is a raw syntactic reference before ReferenceIDs are assigned.
type ReferenceDraft struct {
	Name         string
	Kind         string // use reference.ReferenceKind values
	Container    string // qualified name of enclosing symbol, empty if unknown
	Location     source.Location
	ReceiverExpr string // e.g. "repo" in repo.Save()
	IsCall       bool
}

// ImportDraft is a raw import extracted from a source file.
type ImportDraft struct {
	Path     string         // import path, e.g. "fmt" or "github.com/foo/bar"
	Alias    string         // "" = use base name, "." = dot-import, "_" = blank
	Location source.Location
}

// Extraction is the output of a single-file extraction pass.
type Extraction struct {
	Symbols     []SymbolDraft
	References  []ReferenceDraft
	Imports     []ImportDraft
	Diagnostics []Diagnostic
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
