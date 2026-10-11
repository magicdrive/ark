package index

import (
	"context"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/language"
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
	return newWithCache(ctx, root, providers, store, symbol.NewDeclarationID, nil)
}

// NewWithCacheExcluding is NewWithCache over the files exclude does not leave
// out: an excluded file is never read, so it is not parsed, not looked up in
// or written to store, and contributes no symbol, reference, edge, diagnostic
// or fingerprint input.
func NewWithCacheExcluding(ctx context.Context, root string, providers []language.Provider, store cache.Store, exclude Exclude) (*RepositoryIndex, error) {
	return newWithCache(ctx, root, providers, store, symbol.NewDeclarationID, exclude)
}

func newWithCache(ctx context.Context, root string, providers []language.Provider, store cache.Store, ids IDFunc, exclude Exclude) (*RepositoryIndex, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}

	b := newBuilder()
	b.rootName = rootDirName(root)
	b.nameSpaces = nameSpaces(providers)
	b.ids = ids
	digest := newSourceDigest(providers)

	// The walk reads the sources in order (and adds them to the fingerprint
	// as it reads them); their extraction — a pure function of the file's
	// name and bytes — runs on workers; the results are ingested in walk
	// order, so the index is the one a sequential build makes (extract.go).
	ex := newExtractor(ctx, store, extractWorkers())
	err := walkSources(ctx, root, providers, exclude, func(path, relPath string, prov language.Provider, src []byte, readErr error) {
		digest.add(relPath, src, readErr)
		ex.add(relPath, prov, src, readErr)
		ex.drain(b, false)
	})
	ex.drain(b, true)
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
