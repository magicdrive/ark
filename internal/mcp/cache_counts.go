package mcp

import (
	"sync/atomic"

	"github.com/magicdrive/ark/internal/cache"
)

// countingStore counts the lookups of one index build: the files whose
// extraction came from the persistent cache and those extracted afresh.
type countingStore struct {
	cache.Store
	reused, missed atomic.Uint64
}

func (c *countingStore) Get(key cache.CacheKey) (*cache.CachedExtraction, bool, error) {
	e, hit, err := c.Store.Get(key)
	if hit {
		c.reused.Add(1)
	} else {
		c.missed.Add(1)
	}
	return e, hit, err
}
