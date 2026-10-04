package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCline_WritesUserLevelConfig(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ark := fakeArk(t)

	res, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Scope != "user" {
		t.Errorf("scope: got %s want user", res.Scope)
	}
	want := filepath.Join(home, ".cline", "mcp.json")
	if res.ConfigPath != want {
		t.Errorf("config path: got %s want %s", res.ConfigPath, want)
	}
	servers := mcpServers(t, readJSON(t, want))
	entry := servers["ark"].(map[string]any)
	if entry["command"] != ark {
		t.Errorf("command: got %v want %v", entry["command"], ark)
	}
	args := entry["args"].([]any)
	if args[2] != root {
		t.Errorf("root arg: got %v want %v", args[2], root)
	}
}

func TestCline_PreservesUnrelated_ConflictAndMalformed(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ark := fakeArk(t)
	path := filepath.Join(home, ".cline", "mcp.json")

	// Existing unrelated server + unknown field.
	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "keep", "customField": true},
		},
	})
	if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	servers := mcpServers(t, readJSON(t, path))
	if servers["other"].(map[string]any)["customField"] != true {
		t.Error("unknown field on unrelated server not preserved")
	}
	if _, ok := servers["ark"]; !ok {
		t.Error("ark not added")
	}

	// Conflict without --force → error, zero mutation.
	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{"ark": map[string]any{"command": "/old", "args": []string{"x"}}},
	})
	before, _ := os.ReadFile(path)
	if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root}); err == nil {
		t.Fatal("expected conflict without --force")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("mutated on conflict without --force")
	}

	// Malformed → error even with --force, file untouched.
	bad := []byte("{broken")
	os.WriteFile(path, bad, 0o644)
	if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root, Force: true}); err == nil {
		t.Fatal("expected malformed error")
	}
	cur, _ := os.ReadFile(path)
	if string(cur) != string(bad) {
		t.Error("malformed file was modified")
	}
}

func TestCline_GlobalFlagStillUserLevel(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ark := fakeArk(t)

	res, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root, Global: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := filepath.Join(home, ".cline", "mcp.json")
	if res.ConfigPath != want {
		t.Errorf("config path: got %s want %s", res.ConfigPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("config not written: %v", err)
	}
}
