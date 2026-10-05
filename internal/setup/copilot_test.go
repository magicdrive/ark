package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func copilotConfig(root string) string { return filepath.Join(root, ".vscode", "mcp.json") }

func servers(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	s, ok := doc["servers"].(map[string]any)
	if !ok {
		t.Fatalf(`top-level "servers" missing or wrong type in %v`, doc)
	}
	return s
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestClientCopilot_RegisteredAndNamed(t *testing.T) {
	if got, err := ParseClientID("copilot"); err != nil || got != ClientCopilot {
		t.Fatalf("ParseClientID(copilot) = %v, %v", got, err)
	}
	ids := SupportedClientStrings()
	if ids[len(ids)-1] != "copilot" {
		t.Errorf("copilot must follow the existing clients in canonical order: %v", ids)
	}
	if _, err := ParseClientID("github-copilot"); err == nil {
		t.Error("only the name `copilot` is a client; other spellings must be rejected")
	}
}

// ark setup copilot → <root>/.vscode/mcp.json, top-level "servers", stdio entry.
func TestCopilot_Absent_CreatesVSCodeConfig(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	res, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateAbsent || !res.Changed || res.ConfigPath != copilotConfig(root) || res.Scope != "project" {
		t.Fatalf("unexpected result: %+v", res)
	}
	doc := readJSON(t, copilotConfig(root))
	if _, bad := doc["mcpServers"]; bad {
		t.Error(`VS Code's schema uses "servers"; "mcpServers" must not be written`)
	}
	entry := servers(t, doc)["ark"].(map[string]any)
	if entry["type"] != "stdio" || entry["command"] != ark {
		t.Errorf("entry = %v", entry)
	}
	args := entry["args"].([]any)
	if len(args) != 3 || args[0] != "mcp-server" || args[1] != "--root" || args[2] != root {
		t.Errorf("args = %v", args)
	}
	if env, ok := entry["env"].(map[string]any); !ok || len(env) != 0 {
		t.Errorf("env = %v", entry["env"])
	}
	// Report names the surface honestly.
	if rep := res.Report(); !strings.Contains(rep, "GitHub Copilot (VS Code)") || !strings.Contains(rep, copilotConfig(root)) {
		t.Errorf("report does not name the surface/path:\n%s", rep)
	}
}

// Re-running on the file Ark generated is Equivalent and changes nothing;
// determinism: identical input → identical bytes.
func TestCopilot_Equivalent_NoOp_AndDeterministic(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	first := readBytes(t, copilotConfig(root))

	res, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root})
	if err != nil || res.State != StateEquivalent || res.Changed {
		t.Fatalf("second run: %+v err=%v", res, err)
	}
	if string(readBytes(t, copilotConfig(root))) != string(first) {
		t.Error("Equivalent run modified the file")
	}

	other := t.TempDir()
	if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: other}); err != nil {
		t.Fatal(err)
	}
	a := strings.ReplaceAll(string(first), root, "<root>")
	b := strings.ReplaceAll(string(readBytes(t, copilotConfig(other))), other, "<root>")
	if a != b {
		t.Errorf("same input produced different output:\n%s\n%s", a, b)
	}
}

const copilotExisting = `{
  "servers": {
    "other": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {"X-Keep": "yes"}
    }
  },
  "inputs": [
    {"type": "promptString", "id": "api-key", "description": "key", "password": true}
  ],
  "someFutureField": {"nested": [1, 2, 3]}
}
`

// Other servers, "inputs" and unknown fields survive adding Ark — and survive
// --force replacing a conflicting Ark entry.
func TestCopilot_PreservesOtherServersInputsAndUnknownFields(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	p := copilotConfig(root)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(copilotExisting), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readJSON(t, p)

	check := func(label string) {
		t.Helper()
		doc := readJSON(t, p)
		if _, ok := servers(t, doc)["ark"]; !ok {
			t.Fatalf("%s: ark entry missing", label)
		}
		for _, key := range []string{"inputs", "someFutureField"} {
			if !jsonEqual(t, doc[key], before[key]) {
				t.Errorf("%s: %q changed: %v → %v", label, key, before[key], doc[key])
			}
		}
		if !jsonEqual(t, servers(t, doc)["other"], servers(t, before)["other"]) {
			t.Errorf("%s: other server changed", label)
		}
	}

	if res, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root}); err != nil || res.State != StateAbsent {
		t.Fatalf("add: %+v %v", res, err)
	}
	check("after add")

	// Conflict (different ark path) is refused without --force…
	ark2 := fakeArk(t)
	snapshot := readBytes(t, p)
	if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark2, RootDir: root}); err == nil ||
		!strings.Contains(err.Error(), "--force") {
		t.Fatalf("conflict must be refused with a --force hint, got %v", err)
	}
	if string(readBytes(t, p)) != string(snapshot) {
		t.Fatal("refused conflict modified the file")
	}
	// …and --force replaces ONLY the Ark entry.
	res, err := Run(Options{Client: ClientCopilot, ArkPath: ark2, RootDir: root, Force: true})
	if err != nil || res.State != StateConflict || !res.Changed {
		t.Fatalf("force: %+v %v", res, err)
	}
	check("after force")
	if got := servers(t, readJSON(t, p))["ark"].(map[string]any)["command"]; got != ark2 {
		t.Errorf("force did not replace the Ark entry: %v", got)
	}
}

// Malformed configuration is never overwritten — not even with --force.
func TestCopilot_Malformed_NeverOverwritten(t *testing.T) {
	cases := map[string]string{
		"invalid json":      `{"servers": {`,
		"servers not map":   `{"servers": []}`,
		"servers is string": `{"servers": "x"}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			root, ark := t.TempDir(), fakeArk(t)
			p := copilotConfig(root)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, []byte(content), 0o644)
			for _, force := range []bool{false, true} {
				if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root, Force: force}); err == nil {
					t.Fatalf("force=%v: malformed config accepted", force)
				}
				if string(readBytes(t, p)) != content {
					t.Fatalf("force=%v: malformed file was modified", force)
				}
			}
		})
	}
}

// A top-level "mcpServers" in .vscode/mcp.json is foreign data for this schema:
// it is preserved untouched, and Ark still writes its entry under "servers".
func TestCopilot_ForeignMcpServersKeyPreserved(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	p := copilotConfig(root)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"mcpServers": {"x": {"command": "y"}}}`), 0o644)
	if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	doc := readJSON(t, p)
	if _, ok := servers(t, doc)["ark"]; !ok {
		t.Error("ark not written under servers")
	}
	if ms, _ := doc["mcpServers"].(map[string]any); ms["x"] == nil || ms["ark"] != nil {
		t.Errorf("foreign mcpServers key was altered: %v", doc["mcpServers"])
	}
}

// Unavailable: a symlinked or non-regular target is refused.
func TestCopilot_Unavailable_Symlink(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	p := copilotConfig(root)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	target := filepath.Join(t.TempDir(), "real.json")
	_ = os.WriteFile(target, []byte(`{}`), 0o644)
	if err := os.Symlink(target, p); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root, Force: true}); err == nil ||
		!strings.Contains(err.Error(), "symlink") {
		t.Fatalf("want symlink refusal, got %v", err)
	}
	if string(readBytes(t, target)) != `{}` {
		t.Error("symlink target modified")
	}
}

// --global is an explicit unsupported error with NO filesystem mutation.
func TestCopilot_Global_UnsupportedNoMutation(t *testing.T) {
	root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	_, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root, Global: true, Force: true})
	if err == nil || !strings.Contains(err.Error(), "global setup is not supported for copilot") {
		t.Fatalf("want unsupported error, got %v", err)
	}
	for _, dir := range []string{root, home} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("--global created %v in %s", entries, dir)
		}
	}
}

// Isolation: .mcp.json (shared with the Claude adapter) is never read or
// written, whatever it contains — including a Claude-owned Ark entry.
func TestCopilot_NeverTouchesDotMcpJSON(t *testing.T) {
	for name, content := range map[string]string{
		"claude ark entry": `{"mcpServers":{"ark":{"type":"stdio","command":"ark","args":["mcp-server","--root","${CLAUDE_PROJECT_DIR:-.}/"],"env":{}},"keep":{"command":"x"}}}`,
		"malformed":        `{ this is not json`,
	} {
		t.Run(name, func(t *testing.T) {
			root, ark := t.TempDir(), fakeArk(t)
			mcpJSON := filepath.Join(root, ".mcp.json")
			if err := os.WriteFile(mcpJSON, []byte(content), 0o640); err != nil {
				t.Fatal(err)
			}
			before, _ := os.Stat(mcpJSON)
			for _, force := range []bool{false, true} {
				if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root, Force: force}); err != nil {
					t.Fatalf("copilot setup must not depend on .mcp.json: %v", err)
				}
			}
			if string(readBytes(t, mcpJSON)) != content {
				t.Fatal(".mcp.json content changed")
			}
			after, _ := os.Stat(mcpJSON)
			if !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
				t.Error(".mcp.json metadata changed")
			}
			entries, _ := os.ReadDir(root)
			for _, e := range entries {
				if e.Name() != ".mcp.json" && e.Name() != ".vscode" {
					t.Errorf("unexpected file created in the repository root: %s", e.Name())
				}
			}
		})
	}
}

// Claude's own setup in the same repository is unaffected by Copilot setup and
// vice versa (no shared ownership).
func TestCopilot_AndClaude_AreIndependent(t *testing.T) {
	root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(root)
	if _, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	claudeBefore := readBytes(t, filepath.Join(root, ".mcp.json"))

	if _, err := Run(Options{Client: ClientCopilot, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	if string(readBytes(t, filepath.Join(root, ".mcp.json"))) != string(claudeBefore) {
		t.Fatal("Copilot setup changed Claude's .mcp.json")
	}
	if _, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatalf("Claude setup must stay Equivalent after Copilot setup: %v", err)
	}
	if _, err := os.Stat(copilotConfig(root)); err != nil {
		t.Error("copilot config missing")
	}
}

// Explicit --ark-path keeps the shared validation contract.
func TestCopilot_ArkPathValidation(t *testing.T) {
	root := t.TempDir()
	for name, path := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "nope"),
		"directory": t.TempDir(),
	} {
		if _, err := Run(Options{Client: ClientCopilot, ArkPath: path, RootDir: root}); err == nil {
			t.Errorf("%s: invalid --ark-path accepted", name)
		}
		if _, err := os.Stat(copilotConfig(root)); err == nil {
			t.Errorf("%s: config written despite invalid --ark-path", name)
		}
	}
	nonexec := filepath.Join(t.TempDir(), "ark")
	_ = os.WriteFile(nonexec, []byte("x"), 0o644)
	if _, err := Run(Options{Client: ClientCopilot, ArkPath: nonexec, RootDir: root}); err == nil {
		t.Error("non-executable --ark-path accepted")
	}
}

// Lost-update protection and verify/rollback come from the shared layer: a
// file changed between load and write is not clobbered.
func TestCopilot_LostUpdateProtection(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	p := copilotConfig(root)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(copilotExisting), 0o644)

	cfg, _, err := loadJSONConfigKey(p, copilotServersKey, "ark")
	if err != nil {
		t.Fatal(err)
	}
	concurrent := `{"servers":{"someone":{"command":"else"}}}`
	_ = os.WriteFile(p, []byte(concurrent), 0o644)
	entry := stdioEntry(ark, []string{"mcp-server", "--root", root})
	entry["type"] = "stdio"
	if err := cfg.write(entry); err == nil || !strings.Contains(err.Error(), "changed while Ark was updating") {
		t.Fatalf("want lost-update error, got %v", err)
	}
	if string(readBytes(t, p)) != concurrent {
		t.Error("concurrent change was clobbered")
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	eq, err := semanticEqual(a, b)
	if err != nil {
		t.Fatal(err)
	}
	return eq
}
