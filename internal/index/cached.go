package index

import (
	"context"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// ArkVersion is embedded in cache keys so that upgrading ark invalidates stale
// entries automatically. Override in tests via build tags if needed.
var ArkVersion = "0.1.0"

// NewWithCache builds a RepositoryIndex using store to skip re-extraction for
// files whose content has not changed since the last run.
//
// On any cache read error the file is re-extracted (cache-miss semantics).
// Panic is never used for cache failures.
func NewWithCache(ctx context.Context, root string, providers []language.Provider, store cache.Store) (*RepositoryIndex, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}

	b := newBuilder()
	digest := newSourceDigest(providers)

	err := walkSources(ctx, root, providers, func(path, relPath string, prov language.Provider, src []byte, readErr error) {
		digest.add(relPath, src, readErr)
		if readErr != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  "read error: " + readErr.Error(),
			})
			b.stats.Skipped++
			return
		}

		fileID := source.FileID(relPath)
		cacheKey := cache.NewCacheKey(relPath, src, ArkVersion, prov.CacheVersion())

		// Try cache hit first.
		if cached, hit, _ := store.Get(cacheKey); hit {
			b.ingestExtraction(fileID, cached.Language, language.Extraction{
				Symbols:      cached.Symbols,
				References:   cached.References,
				Imports:      cached.Imports,
				Diagnostics:  cached.Diagnostics,
				Bindings:     cached.Bindings,
				Exports:      cached.Exports,
				ModuleScoped: cached.ModuleScoped,
			})
			return
		}

		// Cache miss — extract and store.
		extraction, err := prov.Extract(ctx, fileID, src)
		if err != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  path + ": extraction error: " + err.Error(),
			})
			b.stats.Skipped++
			return
		}
		b.addDiagnostics(extraction.Diagnostics)

		lang := string(prov.Language())
		b.ingestExtraction(fileID, lang, extraction)

		// Persist to cache (best-effort; errors are ignored).
		_ = store.Put(&cache.CachedExtraction{
			Key:          cacheKey,
			Language:     lang,
			Symbols:      extraction.Symbols,
			References:   extraction.References,
			Imports:      extraction.Imports,
			Diagnostics:  extraction.Diagnostics,
			Bindings:     extraction.Bindings,
			Exports:      extraction.Exports,
			ModuleScoped: extraction.ModuleScoped,
		})
	})

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		// Walk errors other than context cancellation are soft.
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	b.resolve()
	b.fingerprint = digest.sum()
	return b.freeze(), nil
}
