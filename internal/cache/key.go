package cache

import (
	"crypto/sha256"
	"fmt"
)

// CacheKey uniquely identifies a cached extraction.
// Identity is based on content hash, ark version, and schema version —
// never on mtime alone.
type CacheKey struct {
	FilePath      string
	ContentHash   string // hex SHA-256 of file contents
	ArkVersion    string
	SchemaVersion string
}

// NewCacheKey builds a CacheKey for the given file.
func NewCacheKey(repoRelPath string, content []byte, arkVersion string) CacheKey {
	h := sha256.Sum256(content)
	return CacheKey{
		FilePath:      repoRelPath,
		ContentHash:   fmt.Sprintf("%x", h),
		ArkVersion:    arkVersion,
		SchemaVersion: CurrentSchemaVersion,
	}
}

// storageID returns a stable hex identifier used as the cache file name.
func (k CacheKey) storageID() string {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s", k.FilePath, k.ContentHash, k.ArkVersion, k.SchemaVersion)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}
