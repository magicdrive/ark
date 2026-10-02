package cache

import (
	"testing"
)

func TestNewCacheKey_Deterministic(t *testing.T) {
	content := []byte("package main\nfunc main() {}\n")
	k1 := NewCacheKey("cmd/main.go", content, "0.1.0")
	k2 := NewCacheKey("cmd/main.go", content, "0.1.0")
	if k1.ContentHash != k2.ContentHash {
		t.Fatal("content hash not deterministic")
	}
	if k1.storageID() != k2.storageID() {
		t.Fatal("storage ID not deterministic")
	}
}

func TestNewCacheKey_DifferentContent(t *testing.T) {
	k1 := NewCacheKey("a.go", []byte("foo"), "0.1.0")
	k2 := NewCacheKey("a.go", []byte("bar"), "0.1.0")
	if k1.ContentHash == k2.ContentHash {
		t.Fatal("different content produced same hash")
	}
}

func TestNewCacheKey_DifferentVersion(t *testing.T) {
	content := []byte("hello")
	k1 := NewCacheKey("a.go", content, "0.1.0")
	k2 := NewCacheKey("a.go", content, "0.2.0")
	if k1.storageID() == k2.storageID() {
		t.Fatal("different versions should produce different storage IDs")
	}
}

func TestIsCompatible(t *testing.T) {
	k := NewCacheKey("a.go", []byte("x"), "0.1.0")
	if !IsCompatible(k) {
		t.Fatal("fresh key should be compatible")
	}
	k.SchemaVersion = "99"
	if IsCompatible(k) {
		t.Fatal("wrong schema version should be incompatible")
	}
}
