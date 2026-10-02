package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

// TestNew_MissingRoot expects a hard error when the repository root does not exist.
func TestNew_MissingRoot(t *testing.T) {
	providers := []language.Provider{golang.NewProvider()}
	_, err := index.New(context.Background(), "/nonexistent/path/xyz_hardening", providers)
	if err == nil {
		t.Fatal("expected error for missing root, got nil")
	}
}

// TestNew_RootIsFile expects a hard error when the root is a file, not a directory.
func TestNew_RootIsFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "ark_root_file_*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())
	tmp.Close()

	providers := []language.Provider{golang.NewProvider()}
	_, err = index.New(context.Background(), tmp.Name(), providers)
	if err == nil {
		t.Fatal("expected error when root is a file, got nil")
	}
}

// TestNew_UnreadableChildFile expects a partial result with diagnostics, not a hard error,
// when a single child file is unreadable but the root itself is valid.
func TestNew_UnreadableChildFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root user can read any file")
	}
	dir := t.TempDir()
	// Write one valid Go file.
	valid := filepath.Join(dir, "valid.go")
	if err := os.WriteFile(valid, []byte("package main\nfunc Valid() {}\n"), 0644); err != nil {
		t.Fatalf("write valid.go: %v", err)
	}
	// Write one unreadable Go file.
	unreadable := filepath.Join(dir, "unreadable.go")
	if err := os.WriteFile(unreadable, []byte("package main\nfunc Unreadable() {}\n"), 0000); err != nil {
		t.Fatalf("write unreadable.go: %v", err)
	}
	defer os.Chmod(unreadable, 0644) // clean up

	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	// Soft failure: no error returned, but index is partial.
	if err != nil {
		t.Fatalf("unexpected hard error for unreadable child: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index for partial failure")
	}
	// Valid file should be indexed.
	syms := idx.FindSymbols("Valid")
	if len(syms) == 0 {
		t.Error("expected Valid to be indexed despite unreadable sibling")
	}
	// There should be at least one diagnostic about the skipped file.
	if len(idx.Diagnostics()) == 0 {
		t.Error("expected diagnostics for unreadable file")
	}
}

// TestNewWithCache_MissingRoot expects a hard error when the root is missing.
func TestNewWithCache_MissingRoot(t *testing.T) {
	providers := []language.Provider{golang.NewProvider()}
	_, err := index.NewWithCache(context.Background(), "/nonexistent/path/xyz_hardening", providers, cache.NopStore{})
	if err == nil {
		t.Fatal("expected error for missing root in NewWithCache, got nil")
	}
}
