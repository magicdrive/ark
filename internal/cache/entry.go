package cache

import (
	"time"

	"github.com/magicdrive/ark/internal/language"
)

// CachedExtraction is the persisted result of extracting one source file.
type CachedExtraction struct {
	Key         CacheKey                  `json:"key"`
	Language    string                    `json:"language"`
	Symbols     []language.SymbolDraft    `json:"symbols"`
	References  []language.ReferenceDraft `json:"references"`
	Imports     []language.ImportDraft    `json:"imports"`
	Diagnostics []language.Diagnostic     `json:"diagnostics"`
	// Module-binding evidence (schema v2). Absent for providers that do not
	// model module bindings.
	Bindings     []language.BindingDraft `json:"bindings,omitempty"`
	Exports      []language.ExportDraft  `json:"exports,omitempty"`
	ModuleScoped bool                    `json:"moduleScoped,omitempty"`
	// IdentityOnly (language.Extraction.IdentityOnly) is optional like the
	// omitempty draft fields: an entry without it decodes false, which is
	// what every provider that predates it produces.
	IdentityOnly bool `json:"identityOnly,omitempty"`
	// Package / PackageScoped (language.Extraction) follow the same rule:
	// optional, and the provider that emits them bumped its CacheVersion
	// (go-7), so no entry without them is read for it.
	Package       string    `json:"package,omitempty"`
	PackageScoped bool      `json:"packageScoped,omitempty"`
	CachedAt      time.Time `json:"cachedAt"`
}
