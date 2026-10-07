package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
)

// Repository index reuse.
//
// Every graph tool needs the RepositoryIndex of the directory it is pointed at.
// Building one parses every source file and resolves every reference, so the
// handler keeps completed indexes and serves later requests from them.
//
// An index is a deterministic function of (the canonical directory it is built
// over, the provider configuration, the path and content of every source file
// it reads). The cache key is the first two; the third is the index's
// Fingerprint. A request reuses a completed index only after recomputing the
// fingerprint of the files as they are now (index.SourceFingerprint — content,
// not timestamps) and finding it equal: any edit, addition, removal or rename
// of a source file means a rebuild. Nothing query-specific (maxResults,
// maxDepth, maxTokens, format, filePattern, ...) is part of the key: those
// options only read the index.
//
// Concurrent requests for the same key share one build (single-flight); builds
// for different keys run in parallel. A build that fails or panics is never
// published. A waiter that gives up (its context ends) leaves the shared build
// running for the others. At most maxIndexes completed indexes are retained,
// least recently used first out; an entry being built is never evicted.

const maxIndexes = 3

// indexKey identifies what an index is built over and how.
type indexKey struct {
	root   string // canonical (absolute, symlink-free) directory
	config string // provider set, provider cache versions, Ark version
}

type indexEntry struct {
	done    chan struct{} // closed when the build ends
	started time.Time
	idx     *index.RepositoryIndex
	err     error
	used    uint64 // LRU clock value of the last use
}

type indexCache struct {
	build       func(ctx context.Context, root string) (*index.RepositoryIndex, error)
	fingerprint func(ctx context.Context, root string) (string, error)
	config      string
	max         int

	mu      sync.Mutex
	entries map[indexKey]*indexEntry
	clock   uint64

	// Counters for tests and diagnostics.
	builds, reuses, rebuilds uint64
}

func newIndexCache(providers []language.Provider, build func(ctx context.Context, root string) (*index.RepositoryIndex, error)) *indexCache {
	var cfg []string
	for _, p := range providers {
		cfg = append(cfg, fmt.Sprintf("%s:%s:%s", p.Language(), p.CacheVersion(), strings.Join(p.Extensions(), ",")))
	}
	return &indexCache{
		build: build,
		fingerprint: func(ctx context.Context, root string) (string, error) {
			return index.SourceFingerprint(ctx, root, providers)
		},
		config:  index.ArkVersion + "|" + strings.Join(cfg, ";"),
		max:     maxIndexes,
		entries: make(map[indexKey]*indexEntry),
	}
}

// get returns a current index of canonicalRoot, building it when there is no
// completed index of the current sources.
func (c *indexCache) get(ctx context.Context, canonicalRoot string) (*index.RepositoryIndex, error) {
	key := indexKey{root: canonicalRoot, config: c.config}
	requested := time.Now()
	for {
		c.mu.Lock()
		e := c.entries[key]
		if e == nil {
			e = &indexEntry{done: make(chan struct{}), started: time.Now()}
			c.entries[key] = e
			c.builds++
			c.mu.Unlock()
			go c.run(key, e)
		} else {
			c.mu.Unlock()
		}

		select {
		case <-e.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err != nil {
			return nil, e.err
		}
		// A build that started after this request arrived read the sources
		// as they were at or after that moment: current by construction.
		if !e.started.Before(requested) {
			c.touch(e)
			return e.idx, nil
		}
		fp, err := c.fingerprint(ctx, canonicalRoot)
		if err != nil {
			return nil, err
		}
		if fp == e.idx.Fingerprint() {
			c.mu.Lock()
			c.reuses++
			c.mu.Unlock()
			c.touch(e)
			return e.idx, nil
		}
		// Stale: retire this generation (unless another request already
		// did) and build again.
		c.mu.Lock()
		if c.entries[key] == e {
			delete(c.entries, key)
			c.rebuilds++
		}
		c.mu.Unlock()
	}
}

// run builds e. It is detached from every requester's context so that one
// waiter giving up does not abort the build the others wait for.
func (c *indexCache) run(key indexKey, e *indexEntry) {
	defer func() {
		if r := recover(); r != nil {
			e.idx, e.err = nil, fmt.Errorf("building the index of %s panicked: %v", key.root, r)
		}
		failed := e.err != nil || e.idx == nil
		if failed {
			if e.err == nil {
				e.err = fmt.Errorf("building the index of %s produced no index", key.root)
			}
			c.mu.Lock()
			if c.entries[key] == e {
				delete(c.entries, key) // never publish a failed build
			}
			c.mu.Unlock()
		} else {
			c.touch(e)
		}
		close(e.done)
		if !failed {
			c.evict() // after done: this entry now counts as completed
		}
	}()
	e.idx, e.err = c.build(context.Background(), key.root)
}

func (c *indexCache) touch(e *indexEntry) {
	c.mu.Lock()
	c.clock++
	e.used = c.clock
	c.mu.Unlock()
}

// evict drops least recently used completed entries beyond the bound.
func (c *indexCache) evict() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		completed := 0
		var oldest indexKey
		var oldestUse uint64
		found := false
		for k, e := range c.entries {
			select {
			case <-e.done:
			default:
				continue // in flight: never evicted
			}
			completed++
			if !found || e.used < oldestUse || (e.used == oldestUse && k.root < oldest.root) {
				oldest, oldestUse, found = k, e.used, true
			}
		}
		if completed <= c.max {
			return
		}
		delete(c.entries, oldest)
	}
}

// stats reports the cache counters (tests and diagnostics).
func (c *indexCache) stats() (builds, reuses, rebuilds uint64, entries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.builds, c.reuses, c.rebuilds, len(c.entries)
}
