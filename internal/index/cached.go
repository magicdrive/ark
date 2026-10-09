package index

import (
	"context"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// ArkVersion is embedded in cache keys. It is a constant, not the release
// version: upgrading Ark keeps the cache, which is sound because an entry is
// an extraction of one file's content and is invalidated by
// cache.CurrentSchemaVersion and each provider's CacheVersion whenever what an
// extraction contains changes (cache/version.go). Change it only to discard
// every entry at once.
var ArkVersion = "0.1.0"

// NewWithCache builds a RepositoryIndex using store to skip re-extraction for
// files whose content has not changed since the last run.
//
// On any cache read error the file is re-extracted (cache-miss semantics).
// Panic is never used for cache failures.
func NewWithCache(ctx context.Context, root string, providers []language.Provider, store cache.Store) (*RepositoryIndex, error) {
	return newWithCache(ctx, root, providers, store, symbol.NewDeclarationID)
}

func newWithCache(ctx context.Context, root string, providers []language.Provider, store cache.Store, ids IDFunc) (*RepositoryIndex, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}

	b := newBuilder()
	b.rootName = rootDirName(root)
	b.ids = ids
	digest := newSourceDigest(providers)

	err := walkSources(ctx, root, providers, func(path, relPath string, prov language.Provider, src []byte, readErr error) {
		digest.add(relPath, src, readErr)
		if readErr != nil {
			b.addDiagnostic(fileFailure(relPath, language.DiagReadError, "read error", readErr))
			b.stats.Skipped++
			return
		}

		fileID := source.FileID(relPath)
		cacheKey := cache.NewCacheKey(relPath, src, ArkVersion, prov.CacheVersion())

		// Try cache hit first.
		if cached, hit, _ := store.Get(cacheKey); hit {
			// A hit must yield the index a miss would: diagnostics included.
			b.addDiagnostics(cached.Diagnostics)
			b.ingestExtraction(fileID, cached.Language, language.Extraction{
				Symbols:      cached.Symbols,
				References:   cached.References,
				Imports:      cached.Imports,
				Diagnostics:  cached.Diagnostics,
				Bindings:     cached.Bindings,
				Exports:      cached.Exports,
				ModuleScoped: cached.ModuleScoped,
				IdentityOnly: cached.IdentityOnly,

				Package:       cached.Package,
				PackageScoped: cached.PackageScoped,
			})
			return
		}

		// Cache miss — extract and store.
		extraction, err := prov.Extract(ctx, fileID, src)
		if err != nil {
			b.addDiagnostic(fileFailure(relPath, language.DiagExtractionError, "extraction error", err))
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
			IdentityOnly: extraction.IdentityOnly,

			Package:       extraction.Package,
			PackageScoped: extraction.PackageScoped,
		})
	})

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		// Walk errors other than context cancellation are soft.
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Distinct declarations sharing a SymbolID stop the build before anything
	// is derived from the merged ID (identity.go).
	if err := b.identityError(); err != nil {
		return nil, err
	}
	b.resolve()
	b.fingerprint = digest.sum()
	return b.freeze(), nil
}
