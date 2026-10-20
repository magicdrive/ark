package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
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
	dir    string
	build  string        // the scope its entries carry (Key.Build); "" for NewFileStore
	tidied chan struct{} // closed when the tidying that opened it has ended
	stats  storeStats
}

// Stats counts what a FileStore's lookups and writes did.
type Stats struct {
	Reused      uint64 // Get returned an entry
	Absent      uint64 // no entry for the key
	Corrupt     uint64 // unreadable or undecodable (removed)
	Schema      uint64 // another cache schema (removed)
	OtherBuild  uint64 // carries another build's scope (removed)
	Stale       uint64 // another content, Ark version or provider version
	Written     uint64
	WriteFailed uint64
}

type storeStats struct {
	reused, absent, corrupt, schema, otherBuild, stale, written, writeFailed atomic.Uint64
}

// Stats returns the counts so far.
func (s *FileStore) Stats() Stats {
	return Stats{
		Reused: s.stats.reused.Load(), Absent: s.stats.absent.Load(),
		Corrupt: s.stats.corrupt.Load(), Schema: s.stats.schema.Load(),
		OtherBuild: s.stats.otherBuild.Load(), Stale: s.stats.stale.Load(),
		Written: s.stats.written.Load(), WriteFailed: s.stats.writeFailed.Load(),
	}
}

// Tidied is closed once the tidying OpenBuildStore started in the
// background has ended (at once for a store it did not open).
func (s *FileStore) Tidied() <-chan struct{} { return s.tidied }

// NewFileStore creates a FileStore rooted at dir, creating it if needed. Its
// entries belong to no build scope: it is for tests and tools that own dir.
// A persistent cache shared by builds of Ark is opened with OpenBuildStore.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cache: mkdir %s: %w", dir, err)
	}
	done := make(chan struct{})
	close(done)
	return &FileStore{dir: dir, tidied: done}, nil
}

// keepDevScopes is how many development scopes besides the current one
// OpenBuildStore keeps: those most recently opened.
const keepDevScopes = 3

// usedMark is the file in a scope whose modification time records when a
// build last opened it.
const usedMark = ".used"

// trashPrefix names a scope being removed: renamed out of the way at once,
// deleted in the background.
const trashPrefix = ".trash-"

// staleTemp is how old the temporary file of a write must be to be taken
// for one a process left behind when it ended mid-write.
const staleTemp = time.Hour

// beforeSweep, when set (tests), runs first in the background tidying.
var beforeSweep func()

var (
	devScopeName = regexp.MustCompile(`^dev-[0-9a-f]{32}$`)
	// The flat entries of the layout before build scopes: <sha256>.json and
	// the temporary <sha256>.json.tmp of an interrupted write.
	legacyEntryName = regexp.MustCompile(`^[0-9a-f]{64}\.json(\.tmp)?$`)
	// The temporary file of a write (Put): <sha256>.json.<random>.tmp.
	tempName = regexp.MustCompile(`^[0-9a-f]{64}\.json\.[0-9]+\.tmp$`)
)

// OpenBuildStore opens the persistent cache under dir for the running build
// (CurrentBuild): the store of its scope, dir/<scope>. It also tidies dir:
// it retires the development scopes beyond the current one and the
// keepDevScopes opened most recently, and removes the entries of the layout
// before scopes — written by builds no one can tell apart any more, they are
// reused by none — and the temporary files of writes a process left behind
// in its scope. The release scope and any name it does not recognise are
// left alone. A retired scope is renamed at once and deleted, like the rest,
// in the background (Tidied). When dir or its parent is a symbolic link,
// nothing is tidied: what it points to need not be Ark's.
//
// Removing a scope another server is using costs that server cache hits,
// never correctness: its lookups miss and its writes recreate the scope.
func OpenBuildStore(dir string) (*FileStore, Build, error) {
	b, err := CurrentBuild()
	if err != nil {
		return nil, Build{}, err
	}
	s, err := openScope(dir, b.Scope)
	return s, b, err
}

func openScope(dir, scope string) (*FileStore, error) {
	s, err := NewFileStore(filepath.Join(dir, scope))
	if err != nil {
		return nil, err
	}
	s.build = scope
	now := time.Now()
	mark := filepath.Join(s.dir, usedMark)
	if err := os.Chtimes(mark, now, now); err != nil {
		_ = os.WriteFile(mark, nil, 0o600)
	}
	s.tidied = make(chan struct{})
	if linked(dir) || linked(filepath.Dir(dir)) {
		close(s.tidied)
		return s, nil
	}
	retire(dir, scope)
	hook := beforeSweep
	go func() {
		defer close(s.tidied)
		if hook != nil {
			hook()
		}
		sweep(dir, s.dir, now)
	}()
	return s, nil
}

func linked(path string) bool {
	fi, err := os.Lstat(path)
	return err != nil || fi.Mode()&os.ModeSymlink != 0
}

// retire renames out of the way the development scopes OpenBuildStore does
// not keep (best effort).
func retire(dir, current string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type scope struct {
		name string
		used time.Time
	}
	var devs []scope
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !devScopeName.MatchString(name) || name == current {
			continue
		}
		sc := scope{name: name}
		if fi, err := os.Stat(filepath.Join(dir, name, usedMark)); err == nil {
			sc.used = fi.ModTime()
		} else if fi, err := e.Info(); err == nil {
			sc.used = fi.ModTime()
		}
		devs = append(devs, sc)
	}
	sort.Slice(devs, func(i, j int) bool {
		if !devs[i].used.Equal(devs[j].used) {
			return devs[i].used.After(devs[j].used)
		}
		return devs[i].name < devs[j].name
	})
	for i, sc := range devs {
		if i < keepDevScopes {
			continue
		}
		if trash, err := os.MkdirTemp(dir, trashPrefix+sc.name+"-"); err == nil {
			// Into a fresh directory of its own, so that no two processes
			// retiring the same scope collide.
			if os.Rename(filepath.Join(dir, sc.name), filepath.Join(trash, sc.name)) != nil {
				_ = os.Remove(trash)
			}
		}
	}
}

// sweep deletes, in dir, the retired scopes and the flat entries of the old
// layout, and in the current scope the temporary files older than staleTemp
// (best effort).
func sweep(dir, current string, now time.Time) {
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			name := e.Name()
			switch {
			case e.IsDir() && strings.HasPrefix(name, trashPrefix):
				_ = os.RemoveAll(filepath.Join(dir, name))
			case e.Type().IsRegular() && legacyEntryName.MatchString(name):
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
	}
	if entries, err := os.ReadDir(current); err == nil {
		for _, e := range entries {
			if !e.Type().IsRegular() || !tempName.MatchString(e.Name()) {
				continue
			}
			if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > staleTemp {
				_ = os.Remove(filepath.Join(current, e.Name()))
			}
		}
	}
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
		s.stats.absent.Add(1)
		return nil, false, nil // treat as miss
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		s.stats.absent.Add(1)
		return nil, false, nil
	}
	if err != nil {
		s.stats.corrupt.Add(1)
		return nil, false, nil // treat read errors as miss
	}

	var entry CachedExtraction
	if err := json.Unmarshal(data, &entry); err != nil {
		// Corrupt cache — treat as miss, attempt to remove.
		s.stats.corrupt.Add(1)
		_ = os.Remove(p)
		return nil, false, nil
	}

	// Schema incompatibility → miss (never panic).
	if !IsCompatible(entry.Key) {
		s.stats.schema.Add(1)
		_ = os.Remove(p)
		return nil, false, nil
	}

	// Written by another build (a file moved or copied between scopes): its
	// extraction logic is not this build's.
	if entry.Key.Build != s.build {
		s.stats.otherBuild.Add(1)
		_ = os.Remove(p)
		return nil, false, nil
	}

	// Key mismatch (content, ark version, or provider extraction semantics changed).
	if entry.Key.ContentHash != key.ContentHash ||
		entry.Key.ArkVersion != key.ArkVersion ||
		entry.Key.ProviderVersion != key.ProviderVersion {
		s.stats.stale.Add(1)
		return nil, false, nil
	}

	s.stats.reused.Add(1)
	return &entry, true, nil
}

func (s *FileStore) Put(entry *CachedExtraction) error {
	err := s.put(entry)
	if err != nil {
		s.stats.writeFailed.Add(1)
	} else {
		s.stats.written.Add(1)
	}
	return err
}

func (s *FileStore) put(entry *CachedExtraction) error {
	p, err := s.path(entry.Key)
	if err != nil {
		return err
	}
	entry.Key.Build = s.build
	entry.CachedAt = time.Now()
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("cache: marshal: %w", err)
	}
	// Write atomically via a temporary file of its own: two processes
	// writing the same entry never write the same file.
	tmp, err := os.CreateTemp(s.dir, filepath.Base(p)+".*.tmp")
	if errors.Is(err, os.ErrNotExist) {
		// The scope was removed (OpenBuildStore of another build): recreate it.
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			return fmt.Errorf("cache: mkdir %s: %w", s.dir, err)
		}
		tmp, err = os.CreateTemp(s.dir, filepath.Base(p)+".*.tmp")
	}
	if err != nil {
		return fmt.Errorf("cache: write: %w", err)
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("cache: write: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
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
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".json") || tempName.MatchString(e.Name())) {
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
