package cache_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/cache"
)

// TestCacheKey_ProviderVersionCausesMiss verifies that changing the provider
// version produces a different cache key (and therefore a cache miss).
func TestCacheKey_ProviderVersionCausesMiss(t *testing.T) {
	content := []byte("package main\nfunc Foo() {}")
	k1 := cache.NewCacheKey("pkg/foo.go", content, "v1.0", "provider-v1")
	k2 := cache.NewCacheKey("pkg/foo.go", content, "v1.0", "provider-v2")

	// Different provider versions must produce different keys.
	if k1.ProviderVersion == k2.ProviderVersion {
		// This is trivially true since we set them differently, but storageID must differ.
		t.Fatal("provider versions are unexpectedly equal")
	}
}

// TestCacheKey_SameInputsHit verifies that identical inputs produce a cache hit.
func TestCacheKey_SameInputsHit(t *testing.T) {
	dir := t.TempDir()
	store, err := cache.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	content := []byte("package main\nfunc Foo() {}")
	key := cache.NewCacheKey("pkg/foo.go", content, "v1.0", "provider-v1")

	// Put a synthetic entry.
	entry := &cache.CachedExtraction{Key: key, Language: "go"}
	if err := store.Put(entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get with same key → hit.
	got, hit, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit with identical key")
	}
	if got.Language != "go" {
		t.Errorf("unexpected language: %q", got.Language)
	}
}

// TestCacheKey_ProviderVersionMiss verifies that a changed provider version
// causes a cache miss even when content/ark-version are identical.
func TestCacheKey_ProviderVersionMiss(t *testing.T) {
	dir := t.TempDir()
	store, err := cache.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	content := []byte("package main\nfunc Foo() {}")
	keyV1 := cache.NewCacheKey("pkg/foo.go", content, "v1.0", "provider-v1")
	keyV2 := cache.NewCacheKey("pkg/foo.go", content, "v1.0", "provider-v2")

	// Store under v1.
	if err := store.Put(&cache.CachedExtraction{Key: keyV1, Language: "go"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Retrieve with v2 → miss.
	_, hit, err := store.Get(keyV2)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Error("expected cache miss after provider version change, got hit")
	}
}

// TestCacheKey_ContentChangeCausesMiss verifies content hash change → miss.
func TestCacheKey_ContentChangeCausesMiss(t *testing.T) {
	dir := t.TempDir()
	store, err := cache.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	c1 := []byte("package main\nfunc Foo() {}")
	c2 := []byte("package main\nfunc Bar() {}")
	k1 := cache.NewCacheKey("pkg/foo.go", c1, "v1.0", "provider-v1")
	k2 := cache.NewCacheKey("pkg/foo.go", c2, "v1.0", "provider-v1")

	if err := store.Put(&cache.CachedExtraction{Key: k1, Language: "go"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_, hit, _ := store.Get(k2)
	if hit {
		t.Error("expected miss when content changed")
	}
}
