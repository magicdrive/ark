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
	CachedAt     time.Time               `json:"cachedAt"`
}
