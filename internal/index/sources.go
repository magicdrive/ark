package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Exclude reports whether the file or directory at path (absolute, below the
// index root; d is its walk entry, a symlink included) is left out of an
// index: an excluded directory is not entered, an excluded file is not read.
// The MCP server passes its .arkignore access policy; nil excludes nothing.
type Exclude func(path string, d fs.DirEntry) bool

// walkSources visits, in lexical (filepath.WalkDir) order, every file an index
// built over root reads: files with a provider extension, outside skipped
// directories (SkipDirName) and not excluded. visit receives the file's
// content, or the error reading it. Building an index and fingerprinting its
// sources use this one walk, so they always see the same set of files — which
// is why a change of what exclude leaves out changes the fingerprint.
func walkSources(ctx context.Context, root string, providers []language.Provider, exclude Exclude,
	visit func(path, rel string, prov language.Provider, src []byte, readErr error)) error {
	extMap := make(map[string]language.Provider)
	for _, p := range providers {
		for _, ext := range p.Extensions() {
			extMap[ext] = p
		}
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable dirs
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if SkipDirName(d.Name()) || (exclude != nil && path != root && exclude(path, d)) {
				return filepath.SkipDir
			}
			return nil
		}
		prov, ok := extMap[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		if exclude != nil && exclude(path, d) {
			return nil
		}
		src, err := os.ReadFile(path)
		relPath, _ := filepath.Rel(root, path)
		visit(path, relPath, prov, src, err)
		return nil
	})
}

// sourceDigest accumulates the fingerprint of an index's inputs: the provider
// configuration and, in walk order, each source file's path and content (or
// the fact that it could not be read). An index is a deterministic function of
// exactly these inputs, so two equal fingerprints denote the same index.
type sourceDigest struct{ h hash.Hash }

func newSourceDigest(providers []language.Provider) *sourceDigest {
	d := &sourceDigest{h: sha256.New()}
	fmt.Fprintf(d.h, "ark %s\n", ArkVersion)
	for _, p := range providers {
		fmt.Fprintf(d.h, "provider %s %s %s\n", p.Language(), p.CacheVersion(), strings.Join(p.Extensions(), ","))
	}
	return d
}

func (d *sourceDigest) add(rel string, src []byte, readErr error) {
	if readErr != nil {
		fmt.Fprintf(d.h, "file %q unreadable\n", rel)
		return
	}
	sum := sha256.Sum256(src)
	fmt.Fprintf(d.h, "file %q %x\n", rel, sum)
}

func (d *sourceDigest) sum() string { return hex.EncodeToString(d.h.Sum(nil)) }

// SourceFingerprint returns the fingerprint an index built now over root with
// providers would carry (RepositoryIndex.Fingerprint). It reads every source
// file — content, not metadata, decides freshness — but parses nothing.
func SourceFingerprint(ctx context.Context, root string, providers []language.Provider) (string, error) {
	return SourceFingerprintExcluding(ctx, root, providers, nil)
}

// SourceFingerprintExcluding is SourceFingerprint of an index built with
// NewWithCacheExcluding and the same exclude.
func SourceFingerprintExcluding(ctx context.Context, root string, providers []language.Provider, exclude Exclude) (string, error) {
	if err := checkRoot(root); err != nil {
		return "", err
	}
	d := newSourceDigest(providers)
	err := walkSources(ctx, root, providers, exclude, func(_, rel string, _ language.Provider, src []byte, readErr error) {
		d.add(rel, src, readErr)
	})
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	return d.sum(), nil
}

// rootDirName is the base name of the repository root directory.
func rootDirName(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		return filepath.Base(abs)
	}
	return filepath.Base(root)
}

func checkRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("index: root %q: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("index: root %q is not a directory", root)
	}
	return nil
}

// fileFailure is the diagnostic of a file the index could not read or a
// provider could not extract. It names the repository-relative file and
// omits the OS path, which may lie outside the repository's text.
func fileFailure(rel, code, what string, err error) language.Diagnostic {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return language.Diagnostic{
		Severity: language.SeverityWarning,
		Code:     code,
		Message:  what + ": " + err.Error(),
		Location: source.Location{File: source.FileID(rel)},
	}
}
