package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cliConfig(root string) string   { return filepath.Join(root, ".github", "mcp.json") }
func rootMcpJSON(root string) string { return filepath.Join(root, ".mcp.json") }

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

// snapshot captures content + metadata of a file ("" content + exists=false if absent).
type fileSnap struct {
	exists  bool
	content string
	mode    os.FileMode
	mtime   int64
}

func snap(t *testing.T, path string) fileSnap {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return fileSnap{}
	}
	return fileSnap{true, string(readBytes(t, path)), info.Mode(), info.ModTime().UnixNano()}
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && p != root {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func TestClientCopilotCLI_RegisteredAndDistinctFromCopilot(t *testing.T) {
	if got, err := ParseClientID("copilot-cli"); err != nil || got != ClientCopilotCLI {
		t.Fatalf("ParseClientID(copilot-cli) = %v, %v", got, err)
	}
	if ClientCopilot.DisplayName() == ClientCopilotCLI.DisplayName() {
		t.Error("`copilot` and `copilot-cli` must be distinguishable by display name")
	}
	if !strings.Contains(ClientCopilot.DisplayName(), "VS Code") || !strings.Contains(ClientCopilotCLI.DisplayName(), "CLI") {
		t.Errorf("display names must name the surface: %q / %q", ClientCopilot.DisplayName(), ClientCopilotCLI.DisplayName())
	}
	if _, err := ParseClientID("github-copilot-cli"); err == nil {
		t.Error("only `copilot-cli` is a client name")
	}
}

// ark setup copilot-cli → <root>/.github/mcp.json, mcpServers.ark, GitHub's
// documented local-server shape.
func TestCopilotCLI_Absent_CreatesGithubMcpJSON(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	res, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateAbsent || !res.Changed || res.ConfigPath != cliConfig(root) || res.Scope != "project" {
		t.Fatalf("unexpected result: %+v", res)
	}
	doc := readJSON(t, cliConfig(root))
	if _, bad := doc["servers"]; bad {
		t.Error(`Copilot CLI's file uses "mcpServers", not VS Code's "servers"`)
	}
	entry := mcpServers(t, doc)["ark"].(map[string]any)
	if entry["type"] != "local" || entry["command"] != ark {
		t.Errorf("entry = %v", entry)
	}
	args := entry["args"].([]any)
	if len(args) != 3 || args[0] != "mcp-server" || args[1] != "--root" || args[2] != root {
		t.Errorf("args = %v", args)
	}
	if env, ok := entry["env"].(map[string]any); !ok || len(env) != 0 {
		t.Errorf("env = %v", entry["env"])
	}
	if tools, ok := entry["tools"].([]any); !ok || len(tools) != 1 || tools[0] != "*" {
		t.Errorf("tools = %v", entry["tools"])
	}
	if rep := res.Report(); !strings.Contains(rep, "GitHub Copilot CLI") || !strings.Contains(rep, cliConfig(root)) {
		t.Errorf("report does not name the surface/path:\n%s", rep)
	}
	for _, p := range []string{rootMcpJSON(root), filepath.Join(root, ".vscode")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("setup created %s", p)
		}
	}
}

func TestCopilotCLI_Equivalent_ByteForByteNoOp_AndDeterministic(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	first := snap(t, cliConfig(root))
	res, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root})
	if err != nil || res.State != StateEquivalent || res.Changed {
		t.Fatalf("second run: %+v err=%v", res, err)
	}
	if got := snap(t, cliConfig(root)); got != first {
		t.Error("Equivalent run changed the file (content or metadata)")
	}
	other := t.TempDir()
	if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: other}); err != nil {
		t.Fatal(err)
	}
	a := strings.ReplaceAll(first.content, root, "<root>")
	b := strings.ReplaceAll(string(readBytes(t, cliConfig(other))), other, "<root>")
	if a != b {
		t.Errorf("same input, different output:\n%s\n%s", a, b)
	}
}

const cliExisting = `{
  "mcpServers": {
    "other": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {"X-Keep": "yes"},
      "futureServerField": [1, 2]
    }
  },
  "someFutureField": {"nested": true}
}
`

func TestCopilotCLI_PreservesOtherServersAndUnknownFields_ConflictAndForce(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	p := cliConfig(root)
	mustWrite(t, p, cliExisting)
	before := readJSON(t, p)

	check := func(label string) {
		t.Helper()
		doc := readJSON(t, p)
		if _, ok := mcpServers(t, doc)["ark"]; !ok {
			t.Fatalf("%s: ark missing", label)
		}
		if !jsonEqual(t, doc["someFutureField"], before["someFutureField"]) {
			t.Errorf("%s: unknown top-level field changed", label)
		}
		if !jsonEqual(t, mcpServers(t, doc)["other"], mcpServers(t, before)["other"]) {
			t.Errorf("%s: other server (incl. its unknown fields) changed", label)
		}
	}
	if res, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root}); err != nil || res.State != StateAbsent {
		t.Fatalf("add: %+v %v", res, err)
	}
	check("after add")

	ark2 := fakeArk(t)
	snapshot := snap(t, p)
	if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark2, RootDir: root}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("conflict must be refused with a --force hint, got %v", err)
	}
	if snap(t, p) != snapshot {
		t.Fatal("refused conflict modified the file")
	}
	res, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark2, RootDir: root, Force: true})
	if err != nil || res.State != StateConflict || !res.Changed {
		t.Fatalf("force: %+v %v", res, err)
	}
	check("after force")
	if got := mcpServers(t, readJSON(t, p))["ark"].(map[string]any)["command"]; got != ark2 {
		t.Errorf("force did not replace the Ark entry: %v", got)
	}
}

func TestCopilotCLI_Malformed_NeverOverwritten(t *testing.T) {
	for name, content := range map[string]string{
		"invalid json":      `{"mcpServers": {`,
		"mcpServers array":  `{"mcpServers": []}`,
		"mcpServers string": `{"mcpServers": "x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			root, ark := t.TempDir(), fakeArk(t)
			mustWrite(t, cliConfig(root), content)
			for _, force := range []bool{false, true} {
				if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root, Force: force}); err == nil {
					t.Fatalf("force=%v: malformed accepted", force)
				}
				if string(readBytes(t, cliConfig(root))) != content {
					t.Fatalf("force=%v: malformed file modified", force)
				}
			}
		})
	}
}

func TestCopilotCLI_Unavailable_Symlink(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	target := filepath.Join(t.TempDir(), "real.json")
	mustWrite(t, target, `{}`)
	_ = os.MkdirAll(filepath.Dir(cliConfig(root)), 0o755)
	if err := os.Symlink(target, cliConfig(root)); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root, Force: true}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("want symlink refusal, got %v", err)
	}
	if string(readBytes(t, target)) != `{}` {
		t.Error("symlink target modified")
	}
}

// ---- root .mcp.json precedence -------------------------------------------------

const claudeLikeMcpJSON = `{"mcpServers":{"ark":{"type":"stdio","command":"ark","args":["mcp-server","--root","${CLAUDE_PROJECT_DIR:-.}/"],"env":{}},"keep":{"command":"x"}}}`

// A root .mcp.json that defines mcpServers.ark outranks .github/mcp.json in
// Copilot CLI: setup is refused (also with --force) and NOTHING changes.
func TestCopilotCLI_RootMcpJSONArk_RefusedEvenWithForce_NothingChanges(t *testing.T) {
	for _, preexisting := range []bool{false, true} {
		root, ark := t.TempDir(), fakeArk(t)
		mustWrite(t, rootMcpJSON(root), claudeLikeMcpJSON)
		mustWrite(t, filepath.Join(root, "unrelated.txt"), "keep")
		if preexisting { // even an Equivalent .github/mcp.json is not a success
			if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, cliConfig(root), cliExisting)
		}
		mcpBefore, ghBefore, unrelated := snap(t, rootMcpJSON(root)), snap(t, cliConfig(root)), snap(t, filepath.Join(root, "unrelated.txt"))
		treeBefore := listTree(t, root)

		for _, force := range []bool{false, true} {
			_, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root, Force: force})
			if err == nil {
				t.Fatalf("preexisting=%v force=%v: precedence conflict accepted", preexisting, force)
			}
			msg := err.Error()
			for _, want := range []string{".mcp.json", "precedence", ".github/mcp.json", "did not change anything", "--force does not override"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error does not say %q:\n%s", want, msg)
				}
			}
			if strings.Contains(strings.ToLower(msg), "claude") {
				t.Errorf("error must not assert the entry is Claude's:\n%s", msg)
			}
			if snap(t, rootMcpJSON(root)) != mcpBefore || snap(t, cliConfig(root)) != ghBefore ||
				snap(t, filepath.Join(root, "unrelated.txt")) != unrelated {
				t.Fatalf("preexisting=%v force=%v: files changed despite the refusal", preexisting, force)
			}
			if got := listTree(t, root); strings.Join(got, ",") != strings.Join(treeBefore, ",") {
				t.Fatalf("refusal created files: %v → %v", treeBefore, got)
			}
		}
	}
}

// A .mcp.json without mcpServers.ark is none of Ark's business.
func TestCopilotCLI_RootMcpJSONWithoutArk_DoesNotBlock(t *testing.T) {
	for name, content := range map[string]string{
		"other server only":        `{"mcpServers":{"other":{"command":"x"}}}`,
		"empty object":             `{}`,
		"no mcpServers":            `{"foo":1}`,
		"mcpServers wrongly typed": `{"mcpServers":[]}`,
		"ark under other key":      `{"servers":{"ark":{"command":"x"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			root, ark := t.TempDir(), fakeArk(t)
			mustWrite(t, rootMcpJSON(root), content)
			before := snap(t, rootMcpJSON(root))
			if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root}); err != nil {
				t.Fatalf("setup must not be blocked: %v", err)
			}
			if snap(t, rootMcpJSON(root)) != before {
				t.Error(".mcp.json modified")
			}
		})
	}
}

// An unparsable .mcp.json cannot be read by Copilot CLI either: setup proceeds,
// says it could not verify, and leaves the file alone.
func TestCopilotCLI_RootMcpJSONUnparsable_WarnsAndProceeds(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	mustWrite(t, rootMcpJSON(root), `{ not json`)
	before := snap(t, rootMcpJSON(root))
	res, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, " "), "could not verify") {
		t.Errorf("want a could-not-verify warning, got %v", res.Warnings)
	}
	if snap(t, rootMcpJSON(root)) != before {
		t.Error(".mcp.json modified")
	}
}

// ---- isolation between clients ----------------------------------------------------

// Claude first: its root .mcp.json ark entry makes copilot-cli refuse; Claude's
// configuration stays byte-for-byte identical.
func TestCopilotCLI_AfterClaude_RefusedAndClaudeUntouched(t *testing.T) {
	root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(root)
	if _, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	claude := snap(t, rootMcpJSON(root))
	if !strings.Contains(claude.content, "${CLAUDE_PROJECT_DIR") {
		t.Fatalf("fixture assumption: Claude entry should carry its placeholder:\n%s", claude.content)
	}
	for _, force := range []bool{false, true} {
		if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root, Force: force}); err == nil {
			t.Fatalf("force=%v: copilot-cli accepted next to Claude's .mcp.json ark entry", force)
		}
		if snap(t, rootMcpJSON(root)) != claude {
			t.Fatal("Claude's .mcp.json changed")
		}
		if _, err := os.Stat(cliConfig(root)); err == nil {
			t.Fatal(".github/mcp.json was created despite the refusal")
		}
	}
}

// Reverse order: copilot-cli first, Claude second. Neither adapter edits the
// other's file; Claude's later entry shadows the Copilot CLI one (documented
// limitation), which re-running copilot-cli then reports instead of hiding.
func TestCopilotCLI_ThenClaude_ReverseOrder(t *testing.T) {
	root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(root)
	if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	cli := snap(t, cliConfig(root))
	if _, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatalf("Claude setup must stay unaffected by an existing .github/mcp.json: %v", err)
	}
	if snap(t, cliConfig(root)) != cli {
		t.Fatal("Claude setup changed .github/mcp.json")
	}
	if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root}); err == nil {
		t.Fatal("re-running copilot-cli after Claude must report the now-shadowing .mcp.json entry")
	}
	if snap(t, cliConfig(root)) != cli {
		t.Fatal("refused re-run changed .github/mcp.json")
	}
}

// VS Code Copilot and Copilot CLI use different files and never touch each
// other, in either order.
func TestCopilotCLI_AndCopilotVSCode_AreIndependent(t *testing.T) {
	for _, order := range [][]ClientID{{ClientCopilot, ClientCopilotCLI}, {ClientCopilotCLI, ClientCopilot}} {
		root, ark := t.TempDir(), fakeArk(t)
		var vscodeAfterFirst, cliAfterFirst fileSnap
		for i, c := range order {
			if _, err := Run(Options{Client: c, ArkPath: ark, RootDir: root}); err != nil {
				t.Fatalf("%v: %v", order, err)
			}
			if i == 0 {
				vscodeAfterFirst, cliAfterFirst = snap(t, copilotConfig(root)), snap(t, cliConfig(root))
			}
		}
		first := order[0]
		if first == ClientCopilot && snap(t, copilotConfig(root)) != vscodeAfterFirst {
			t.Errorf("%v: copilot-cli changed .vscode/mcp.json", order)
		}
		if first == ClientCopilotCLI && snap(t, cliConfig(root)) != cliAfterFirst {
			t.Errorf("%v: copilot changed .github/mcp.json", order)
		}
		if _, err := os.Stat(rootMcpJSON(root)); err == nil {
			t.Errorf("%v: .mcp.json was created", order)
		}
		if vs := string(readBytes(t, copilotConfig(root))); !strings.Contains(vs, `"servers"`) || strings.Contains(vs, "mcpServers") {
			t.Errorf("%v: .vscode/mcp.json has the wrong schema:\n%s", order, vs)
		}
	}
}

// ---- global, ark-path, lost update --------------------------------------------------

func TestCopilotCLI_Global_UnsupportedNoMutation(t *testing.T) {
	root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("COPILOT_HOME", filepath.Join(home, "ch"))
	_, err := Run(Options{Client: ClientCopilotCLI, ArkPath: ark, RootDir: root, Global: true, Force: true})
	if err == nil || !strings.Contains(err.Error(), "global setup is not supported for copilot-cli") {
		t.Fatalf("want unsupported error, got %v", err)
	}
	for _, dir := range []string{root, home} {
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("--global created %v in %s", entries, dir)
		}
	}
}

func TestCopilotCLI_ArkPathValidation(t *testing.T) {
	root := t.TempDir()
	nonexec := filepath.Join(t.TempDir(), "ark")
	_ = os.WriteFile(nonexec, []byte("x"), 0o644)
	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "nope"), "directory": t.TempDir(), "non-executable": nonexec,
	} {
		if _, err := Run(Options{Client: ClientCopilotCLI, ArkPath: path, RootDir: root}); err == nil {
			t.Errorf("%s: invalid --ark-path accepted", name)
		}
		if _, err := os.Stat(cliConfig(root)); err == nil {
			t.Errorf("%s: config written despite invalid --ark-path", name)
		}
	}
}

func TestCopilotCLI_LostUpdateProtection(t *testing.T) {
	root, ark := t.TempDir(), fakeArk(t)
	p := cliConfig(root)
	mustWrite(t, p, cliExisting)
	cfg, _, err := loadJSONConfig(p, "ark")
	if err != nil {
		t.Fatal(err)
	}
	concurrent := `{"mcpServers":{"someone":{"command":"else"}}}`
	mustWrite(t, p, concurrent)
	entry := stdioEntry(ark, []string{"mcp-server", "--root", root})
	entry["type"], entry["tools"] = "local", []any{"*"}
	if err := cfg.write(entry); err == nil || !strings.Contains(err.Error(), "changed while Ark was updating") {
		t.Fatalf("want lost-update error, got %v", err)
	}
	if string(readBytes(t, p)) != concurrent {
		t.Error("concurrent change was clobbered")
	}
}
