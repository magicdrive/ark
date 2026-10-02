package index

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
	extMap := make(map[string]language.Provider)
	for _, p := range providers {
		for _, ext := range p.Extensions() {
			extMap[ext] = p
		}
	}

	b := newBuilder()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		prov, ok := extMap[ext]
		if !ok {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  "read error: " + err.Error(),
			})
			b.stats.Skipped++
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		fileID := source.FileID(relPath)
		cacheKey := cache.NewCacheKey(relPath, src, ArkVersion)

		// Try cache hit first.
		if cached, hit, _ := store.Get(cacheKey); hit {
			b.ingestExtraction(fileID, cached.Language, language.Extraction{
				Symbols:     cached.Symbols,
				References:  cached.References,
				Imports:     cached.Imports,
				Diagnostics: cached.Diagnostics,
			})
			return nil
		}

		// Cache miss — extract and store.
		extraction, err := prov.Extract(ctx, fileID, src)
		if err != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  path + ": extraction error: " + err.Error(),
			})
			b.stats.Skipped++
			return nil
		}
		b.addDiagnostics(extraction.Diagnostics)

		lang := string(prov.Language())
		b.ingestExtraction(fileID, lang, extraction)

		// Persist to cache (best-effort; errors are ignored).
		_ = store.Put(&cache.CachedExtraction{
			Key:         cacheKey,
			Language:    lang,
			Symbols:     extraction.Symbols,
			References:  extraction.References,
			Imports:     extraction.Imports,
			Diagnostics: extraction.Diagnostics,
		})
		return nil
	})

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		// Walk errors other than context cancellation are soft.
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	b.resolve()
	return b.freeze(), nil
}
