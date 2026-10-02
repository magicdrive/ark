package cache

import (
	"time"

	"github.com/magicdrive/ark/internal/language"
)

// CachedExtraction is the persisted result of extracting one source file.
type CachedExtraction struct {
	Key         CacheKey              `json:"key"`
	Language    string                `json:"language"`
	Symbols     []language.SymbolDraft    `json:"symbols"`
	References  []language.ReferenceDraft `json:"references"`
	Imports     []language.ImportDraft    `json:"imports"`
	Diagnostics []language.Diagnostic     `json:"diagnostics"`
	CachedAt    time.Time             `json:"cachedAt"`
}
