package cache

import (
	"os"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/language"
)

func TestFileStore_PutGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	key := NewCacheKey("pkg/foo.go", []byte("package foo"), "0.1.0", "pv1")
	entry := &CachedExtraction{
		Key:      key,
		Language: "go",
		Symbols: []language.SymbolDraft{
			{Name: "Foo", Qualified: "Foo"},
		},
		CachedAt: time.Now(),
	}

	if err := store.Put(entry); err != nil {
		t.Fatal("Put:", err)
	}

	got, hit, err := store.Get(key)
	if err != nil {
		t.Fatal("Get error:", err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if len(got.Symbols) != 1 || got.Symbols[0].Name != "Foo" {
		t.Fatalf("unexpected symbols: %+v", got.Symbols)
	}
}

func TestFileStore_MissOnContentChange(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)

	key1 := NewCacheKey("a.go", []byte("v1"), "0.1.0", "pv1")
	_ = store.Put(&CachedExtraction{Key: key1, Language: "go"})

	key2 := NewCacheKey("a.go", []byte("v2"), "0.1.0", "pv1") // different content
	_, hit, _ := store.Get(key2)
	if hit {
		t.Fatal("different content should be a cache miss")
	}
}

func TestFileStore_SchemaIncompatible(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)

	key := NewCacheKey("a.go", []byte("x"), "0.1.0", "pv1")
	entry := &CachedExtraction{Key: key, Language: "go"}
	_ = store.Put(entry)

	// Tamper: write a file with a different schema version directly.
	// Simulate by changing the expected constant and checking miss behaviour.
	// We do this by looking up the file and writing corrupt JSON.
	p, _ := store.path(key)
	_ = os.WriteFile(p, []byte(`{"key":{"schemaVersion":"99"}}`), 0o600)

	_, hit, _ := store.Get(key)
	if hit {
		t.Fatal("incompatible schema should be a cache miss")
	}
}

func TestFileStore_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)

	key := NewCacheKey("a.go", []byte("x"), "0.1.0", "pv1")
	p, _ := store.path(key)
	_ = os.WriteFile(p, []byte(`not json`), 0o600)

	_, hit, err := store.Get(key)
	if err != nil {
		t.Fatal("corrupt cache should not return error, just miss")
	}
	if hit {
		t.Fatal("corrupt cache should be a miss")
	}
}

func TestNopStore(t *testing.T) {
	var s NopStore
	key := NewCacheKey("a.go", []byte("x"), "0.1.0", "pv1")
	entry := &CachedExtraction{Key: key}
	_ = s.Put(entry)
	_, hit, _ := s.Get(key)
	if hit {
		t.Fatal("NopStore should never hit")
	}
}

func TestFileStore_Clear(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)

	for i := range 3 {
		k := NewCacheKey("f.go", []byte{byte(i)}, "0.1.0", "pv1")
		_ = store.Put(&CachedExtraction{Key: k, Language: "go"})
	}
	if err := store.Clear(); err != nil {
		t.Fatal("Clear:", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected empty dir after Clear, got %d entries", len(entries))
	}
}
