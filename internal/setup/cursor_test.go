package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cursorProjectConfig returns the project-scope config path for a root.
func cursorProjectConfig(root string) string {
	return filepath.Join(root, ".cursor", "mcp.json")
}

func TestCursor_Absent_CreatesConfig(t *testing.T) {
	root := t.TempDir()
	ark := fakeArk(t)

	res, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State != StateAbsent || !res.Changed {
		t.Fatalf("expected absent+changed, got %s changed=%v", res.State, res.Changed)
	}

	cfg := readJSON(t, cursorProjectConfig(root))
	entry := mcpServers(t, cfg)["ark"].(map[string]any)
	if entry["command"] != ark {
		t.Errorf("command: got %v want %v", entry["command"], ark)
	}
	args := entry["args"].([]any)
	if len(args) != 3 || args[0] != "mcp-server" || args[1] != "--root" || args[2] != root {
		t.Errorf("args: unexpected %v", args)
	}
}

func TestCursor_PreservesOtherServersAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	ark := fakeArk(t)
	path := cursorProjectConfig(root)

	writeJSON(t, path, map[string]any{
		"someTopLevel": "keep-me",
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "other-bin", "args": []string{"x"}, "customField": true},
		},
	})

	if _, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cfg := readJSON(t, path)
	if cfg["someTopLevel"] != "keep-me" {
		t.Error("top-level key not preserved")
	}
	servers := mcpServers(t, cfg)
	other, ok := servers["other"].(map[string]any)
	if !ok {
		t.Fatal("other server removed")
	}
	if other["customField"] != true {
		t.Error("unknown field on other server not preserved")
	}
	if _, ok := servers["ark"]; !ok {
		t.Error("ark server not added")
	}
}

func TestCursor_Equivalent_NoOp(t *testing.T) {
	root := t.TempDir()
	ark := fakeArk(t)
	path := cursorProjectConfig(root)

	if _, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	info1, _ := os.Stat(path)
	data1, _ := os.ReadFile(path)

	res, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.State != StateEquivalent || res.Changed {
		t.Fatalf("expected equivalent no-op, got %s changed=%v", res.State, res.Changed)
	}
	info2, _ := os.Stat(path)
	data2, _ := os.ReadFile(path)
	if string(data1) != string(data2) {
		t.Error("file content changed on equivalent re-run")
	}
	if info1.ModTime() != info2.ModTime() {
		t.Error("mtime changed on equivalent re-run (file should not be rewritten)")
	}
}

func TestCursor_Idempotent_ThreeRuns(t *testing.T) {
	root := t.TempDir()
	ark := fakeArk(t)
	for i := 0; i < 3; i++ {
		if _, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	servers := mcpServers(t, readJSON(t, cursorProjectConfig(root)))
	if len(servers) != 1 {
		t.Errorf("expected exactly one server after 3 runs, got %d", len(servers))
	}
}

func TestCursor_Conflict_NoForce_Errors_ZeroMutation(t *testing.T) {
	root := t.TempDir()
	ark := fakeArk(t)
	path := cursorProjectConfig(root)

	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{
			"ark": map[string]any{"command": "/old/ark", "args": []string{"mcp-server"}},
		},
	})
	before, _ := os.ReadFile(path)

	_, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root})
	if err == nil {
		t.Fatal("expected conflict error without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("file was mutated on conflict without --force")
	}
}

func TestCursor_Conflict_Force_ReplacesOnlyArk(t *testing.T) {
	root := t.TempDir()
	ark := fakeArk(t)
	path := cursorProjectConfig(root)

	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{
			"ark":   map[string]any{"command": "/old/ark", "args": []string{"mcp-server"}},
			"other": map[string]any{"command": "keep"},
		},
	})

	res, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root, Force: true})
	if err != nil {
		t.Fatalf("Run --force: %v", err)
	}
	if res.State != StateConflict || !res.Changed {
		t.Fatalf("expected conflict+changed, got %s changed=%v", res.State, res.Changed)
	}
	servers := mcpServers(t, readJSON(t, path))
	if servers["other"].(map[string]any)["command"] != "keep" {
		t.Error("unrelated server was damaged by --force")
	}
	if servers["ark"].(map[string]any)["command"] != ark {
		t.Error("ark entry not replaced")
	}
}

func TestCursor_Malformed_Errors_EvenWithForce(t *testing.T) {
	root := t.TempDir()
	ark := fakeArk(t)
	path := cursorProjectConfig(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := []byte("{not valid json")
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, force := range []bool{false, true} {
		_, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root, Force: force})
		if err == nil {
			t.Fatalf("force=%v: expected malformed error", force)
		}
		cur, _ := os.ReadFile(path)
		if string(cur) != string(bad) {
			t.Errorf("force=%v: malformed file was modified", force)
		}
	}
}

func TestCursor_Global_UsesHome(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	ark := fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	res, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root, Global: true})
	if err != nil {
		t.Fatalf("Run global: %v", err)
	}
	if res.Scope != "global" {
		t.Errorf("scope: got %s want global", res.Scope)
	}
	want := filepath.Join(home, ".cursor", "mcp.json")
	if res.ConfigPath != want {
		t.Errorf("config path: got %s want %s", res.ConfigPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("global config not written: %v", err)
	}
}

func TestCursor_SpacesAndUnicodeInRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "my project 日本語")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	ark := fakeArk(t)

	if _, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	servers := mcpServers(t, readJSON(t, cursorProjectConfig(root)))
	args := servers["ark"].(map[string]any)["args"].([]any)
	if args[2] != root {
		t.Errorf("root with spaces/unicode not preserved: got %v want %v", args[2], root)
	}
}
