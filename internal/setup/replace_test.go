package setup

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReplaceFile_ReplacesExistingDestination verifies the replace boundary
// swaps content when the destination already exists (the --force path relies on
// this on every OS).
func TestReplaceFile_ReplacesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(dst, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cfg.tmp")
	if err := os.WriteFile(src, []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(src, dst); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "NEW" {
		t.Errorf("destination not replaced: got %q", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source should no longer exist after replace")
	}
}
