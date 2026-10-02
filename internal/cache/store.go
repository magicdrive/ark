package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store is the persistence interface for cached extractions.
type Store interface {
	Get(key CacheKey) (*CachedExtraction, bool, error)
	Put(entry *CachedExtraction) error
	Delete(key CacheKey) error
	Clear() error
}

// FileStore persists cache entries as JSON files under a directory.
type FileStore struct {
	dir string
}

// NewFileStore creates a FileStore rooted at dir, creating it if needed.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cache: mkdir %s: %w", dir, err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) path(key CacheKey) (string, error) {
	name := key.storageID() + ".json"
	// Guard against path traversal — the name is a hex SHA-256 so this should
	// never trigger in practice, but we validate defensively.
	if strings.ContainsAny(name, "/\\") {
		return "", errors.New("cache: invalid storage id")
	}
	return filepath.Join(s.dir, name), nil
}

func (s *FileStore) Get(key CacheKey) (*CachedExtraction, bool, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, false, nil // treat as miss
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, nil // treat read errors as miss
	}

	var entry CachedExtraction
	if err := json.Unmarshal(data, &entry); err != nil {
		// Corrupt cache — treat as miss, attempt to remove.
		_ = os.Remove(p)
		return nil, false, nil
	}

	// Schema incompatibility → miss (never panic).
	if !IsCompatible(entry.Key) {
		_ = os.Remove(p)
		return nil, false, nil
	}

	// Key mismatch (e.g. content changed).
	if entry.Key.ContentHash != key.ContentHash || entry.Key.ArkVersion != key.ArkVersion {
		return nil, false, nil
	}

	return &entry, true, nil
}

func (s *FileStore) Put(entry *CachedExtraction) error {
	p, err := s.path(entry.Key)
	if err != nil {
		return err
	}
	entry.CachedAt = time.Now()
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("cache: marshal: %w", err)
	}
	// Write atomically via temp file.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("cache: write: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cache: rename: %w", err)
	}
	return nil
}

func (s *FileStore) Delete(key CacheKey) error {
	p, err := s.path(key)
	if err != nil {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *FileStore) Clear() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
	return nil
}

// NopStore is a no-op cache that never stores anything.
// Use it to disable caching entirely.
type NopStore struct{}

func (NopStore) Get(_ CacheKey) (*CachedExtraction, bool, error) { return nil, false, nil }
func (NopStore) Put(_ *CachedExtraction) error                   { return nil }
func (NopStore) Delete(_ CacheKey) error                         { return nil }
func (NopStore) Clear() error                                    { return nil }
