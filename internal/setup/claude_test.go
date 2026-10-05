package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/instruction"
)

// chdir switches to dir for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

func TestClaude_Project_MatchesLegacyEntry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	chdir(t, tmp)
	ark := fakeArk(t)

	res, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: tmp})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State != StateAbsent || !res.Changed {
		t.Fatalf("expected absent+changed, got %s changed=%v", res.State, res.Changed)
	}

	cfg := readJSON(t, filepath.Join(tmp, ".mcp.json"))
	entry := mcpServers(t, cfg)["ark"].(map[string]any)
	if entry["type"] != "stdio" {
		t.Errorf("type: got %v want stdio (legacy behavior)", entry["type"])
	}
	args := entry["args"].([]any)
	// Root == cwd → portable placeholder (legacy behavior).
	if len(args) != 3 || args[2] != "${CLAUDE_PROJECT_DIR:-.}/" {
		t.Errorf("args: got %v want placeholder root", args)
	}
}

func TestClaude_Equivalent_SecondRunNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	chdir(t, tmp)
	ark := fakeArk(t)

	if _, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: tmp}); err != nil {
		t.Fatalf("first: %v", err)
	}
	path := filepath.Join(tmp, ".mcp.json")
	data1, _ := os.ReadFile(path)

	res, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: tmp})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.State != StateEquivalent {
		t.Errorf("expected equivalent, got %s", res.State)
	}
	data2, _ := os.ReadFile(path)
	if string(data1) != string(data2) {
		t.Error("MCP config changed on equivalent re-run")
	}
}

func TestClaude_Conflict_Force_PreservesUnrelated(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	chdir(t, tmp)
	ark := fakeArk(t)
	path := filepath.Join(tmp, ".mcp.json")

	writeJSON(t, path, map[string]any{
		"topLevel": "keep",
		"mcpServers": map[string]any{
			"ark":   map[string]any{"type": "stdio", "command": "/old/ark", "args": []string{"mcp-server"}},
			"other": map[string]any{"command": "keep-me"},
		},
	})

	// Without --force → conflict error, zero mutation.
	before, _ := os.ReadFile(path)
	if _, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: tmp}); err == nil {
		t.Fatal("expected conflict without --force")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("config mutated on conflict without --force")
	}

	// With --force → only Ark replaced, unrelated preserved.
	if _, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: tmp, Force: true}); err != nil {
		t.Fatalf("Run --force: %v", err)
	}
	cfg := readJSON(t, path)
	if cfg["topLevel"] != "keep" {
		t.Error("top-level key lost")
	}
	servers := mcpServers(t, cfg)
	if servers["other"].(map[string]any)["command"] != "keep-me" {
		t.Error("unrelated server damaged")
	}
	if servers["ark"].(map[string]any)["command"] != ark {
		t.Error("ark entry not replaced")
	}
}

func TestClaude_Global_UsesClaudeSettings(t *testing.T) {
	tmp := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	chdir(t, root)
	ark := fakeArk(t)

	res, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: root, Global: true})
	if err != nil {
		t.Fatalf("Run global: %v", err)
	}
	want := filepath.Join(tmp, ".claude", "settings.json")
	if res.ConfigPath != want {
		t.Errorf("global config path: got %s want %s", res.ConfigPath, want)
	}
}

// The Ark usage instruction shown after a successful `ark setup claude` is the
// canonical `ark instruction claude` text (single source), and setup itself
// still writes only its usual artifacts.
func TestClaude_SetupSuccessShowsCanonicalInstruction(t *testing.T) {
	root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(root)

	res, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	want, err := instruction.Render("claude")
	if err != nil {
		t.Fatal(err)
	}
	advisory := strings.Join(res.ExtraLines, "\n")
	for k, l := range strings.Split(want, "\n") {
		if l == "" {
			continue
		}
		if !strings.Contains(advisory, "   "+l+"\n") && !strings.Contains(advisory, "   "+l) {
			t.Fatalf("setup advisory is missing canonical line %d: %q\n%s", k, l, advisory)
		}
	}
	if !strings.Contains(advisory, "   ---  CLAUDE.md  ---") {
		t.Errorf("advisory block markers missing:\n%s", advisory)
	}
	// CLAUDE.md is advised, never written.
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Error("setup must not write CLAUDE.md")
	}
}
