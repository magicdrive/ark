package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSemanticEqual_IgnoresTypeAndOrder(t *testing.T) {
	a := map[string]any{
		"command": "ark",
		"args":    []string{"mcp-server", "--root", "/x"},
		"env":     map[string]any{},
	}
	// Same content, different Go types / key order (as if JSON-decoded).
	b := map[string]any{
		"env":     map[string]any{},
		"args":    []any{"mcp-server", "--root", "/x"},
		"command": "ark",
	}
	eq, err := semanticEqual(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !eq {
		t.Error("expected semantic equality across type/order differences")
	}

	c := map[string]any{"command": "ark", "args": []any{"mcp-server", "--root", "/y"}}
	eq, _ = semanticEqual(a, c)
	if eq {
		t.Error("expected inequality for different root")
	}
}

func TestAtomicReplace_LostUpdateDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	orig := []byte("{\"a\":1}\n")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}

	// Another process changed the file after we captured origBytes.
	changed := []byte("{\"a\":2}\n")
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}

	err := atomicReplace(path, []byte("{\"a\":3}\n"), orig, true, 0o644)
	if err == nil {
		t.Fatal("expected lost-update error")
	}
	cur, _ := os.ReadFile(path)
	if string(cur) != string(changed) {
		t.Error("file was clobbered despite concurrent change")
	}
}

func TestAtomicReplace_NewFileAppearedDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	// We believe the file does not exist (existed=false) but it now does.
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := atomicReplace(path, []byte("{\"a\":1}\n"), nil, false, 0o644)
	if err == nil {
		t.Fatal("expected error when a file appeared concurrently")
	}
}

func TestAtomicReplace_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "cfg.json")
	if err := atomicReplace(path, []byte("{}\n"), nil, false, 0o644); err != nil {
		t.Fatalf("atomicReplace: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestLoadJSONConfig_RefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, state, err := loadJSONConfig(link, "ark")
	if err == nil {
		t.Fatal("expected refusal for symlink target")
	}
	if state != StateUnavailable {
		t.Errorf("state: got %s want unavailable", state)
	}
}

func TestCursor_ReadOnlyFile_Errors_OriginalPreserved(t *testing.T) {
	skipIfRoot(t)
	root := t.TempDir()
	ark := fakeArk(t)
	path := cursorProjectConfig(root)
	writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"other": map[string]any{"command": "x"}}})
	before, _ := os.ReadFile(path)

	// Make the containing directory read-only so the rename cannot happen.
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	_, err := Run(Options{Client: ClientCursor, ArkPath: ark, RootDir: root})
	if err == nil {
		t.Fatal("expected error writing into read-only directory")
	}
	os.Chmod(dir, 0o755)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("original file not preserved after failed write")
	}
}
