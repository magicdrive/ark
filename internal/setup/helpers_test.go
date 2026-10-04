package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeArk creates an executable stub and returns its path, so ark-path
// validation (which checks the file is executable) passes deterministically
// without depending on a real ark on PATH.
func fakeArk(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "ark")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake ark: %v", err)
	}
	return p
}

// readJSON reads and decodes a JSON file into a generic map.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, data)
	}
	return m
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mcpServers(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	s, ok := m[mcpServersKey].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or wrong type in %v", m)
	}
	return s
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("read-only file assertions do not hold when running as root")
	}
}
