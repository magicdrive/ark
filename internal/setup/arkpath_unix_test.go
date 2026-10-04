//go:build !windows

package setup

import (
	"path/filepath"
	"syscall"
	"testing"
)

// TestResolveArkPath_NonRegularRejected ensures a non-regular filesystem object
// (here a FIFO) is rejected even if it carries execute bits — Ark must require a
// regular file for an explicit --ark-path (task §2).
func TestResolveArkPath_NonRegularRejected(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "ark")
	if err := syscall.Mkfifo(fifo, 0o755); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	_, _, err := resolveArkPath(fifo)
	if err == nil {
		t.Fatal("a non-regular file (FIFO) with exec bits must be rejected as --ark-path")
	}
}
