//go:build !windows

package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsExecutableFile_Unix(t *testing.T) {
	dir := t.TempDir()

	execPath := filepath.Join(dir, "ark")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	nonExecPath := filepath.Join(dir, "plain")
	if err := os.WriteFile(nonExecPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	execInfo, _ := os.Stat(execPath)
	if !isExecutableFile(execPath, execInfo) {
		t.Error("0755 file should be executable")
	}
	nonExecInfo, _ := os.Stat(nonExecPath)
	if isExecutableFile(nonExecPath, nonExecInfo) {
		t.Error("0644 file should not be executable")
	}
}
