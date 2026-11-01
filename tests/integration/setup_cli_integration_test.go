package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildArk returns the ark binary for CLI integration tests, built once per
// test process and shared (integration_binary_test.go). It runs on every
// system; a test that needs a POSIX shell skips itself where there is none.
func buildArk(t *testing.T) string {
	t.Helper()
	return arkBinary.get(t)
}

// runArk executes the built binary in dir with HOME overridden to home.
func runArk(t *testing.T, bin, dir, home string, args ...string) (string, string, int) {
	t.Helper()
	return runArkEnv(t, bin, dir, home, nil, args...)
}

// clientLocationVars are the variables that move a client's user-level MCP
// configuration away from HOME. runArkEnv clears them (an empty value is
// unset for both clients), so a developer's own setting never sends a test
// to their real configuration.
var clientLocationVars = []string{"CLAUDE_CONFIG_DIR", "CLAUDE_CODE_CUSTOM_OAUTH_URL", "CLINE_MCP_SETTINGS_PATH", "CLINE_DATA_DIR", "CLINE_DIR"}

// runArkEnv is runArk with extra environment entries ("NAME=value"), applied
// last.
func runArkEnv(t *testing.T, bin, dir, home string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	for _, v := range clientLocationVars {
		cmd.Env = append(cmd.Env, v+"=")
	}
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run ark: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// fakeArkBin is a placeholder for --ark-path: `ark setup` checks that the
// path is a regular, executable file and writes it into the client's config,
// but never runs it — so the "#!/bin/sh" content needs no shell, and Windows
// (no execute bit; an extensionless path is accepted) takes the file as is.
func fakeArkBin(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ark")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLI_SetupCursor_CreatesAndIsIdempotent(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()
	ark := fakeArkBin(t)

	stdout, stderr, code := runArk(t, bin, repo, home, "setup", "cursor", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Ark is ready for Cursor") {
		t.Errorf("missing success message:\n%s", stdout)
	}
	cfg := filepath.Join(repo, ".cursor", "mcp.json")
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config not created: %v", err)
	}

	stdout, _, code = runArk(t, bin, repo, home, "setup", "cursor", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("second run exit %d", code)
	}
	if !strings.Contains(stdout, "No changes required") {
		t.Errorf("second run not a no-op:\n%s", stdout)
	}
}

func TestCLI_SetupUnknownClient_Fails(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()

	_, stderr, code := runArk(t, bin, repo, home, "setup", "vscode")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown client")
	}
	if !strings.Contains(stderr, "unsupported client") {
		t.Errorf("stderr should explain unsupported client:\n%s", stderr)
	}
}

func TestCLI_SetupNoClient_DeprecationWarning(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()
	ark := fakeArkBin(t)

	_, stderr, code := runArk(t, bin, repo, home, "setup", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("exit %d\nstderr:%s", code, stderr)
	}
	if !strings.Contains(stderr, "deprecated") {
		t.Errorf("expected deprecation warning:\n%s", stderr)
	}
}

func TestCLI_SetupCodex_NotInstalled_Fails(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()
	ark := fakeArkBin(t)

	// Run with an empty PATH so `codex` cannot be found.
	cmd := exec.Command(bin, "setup", "codex", "--ark-path", ark, "--root", repo)
	cmd.Dir = repo
	cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH="}
	if gorace, ok := os.LookupEnv("GORACE"); ok {
		// Under -race: where the race detector reports (integration_binary_test.go).
		cmd.Env = append(cmd.Env, "GORACE="+gorace)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure when codex is not installed")
	}
	if !strings.Contains(stderr.String(), "Codex CLI") {
		t.Errorf("stderr should mention Codex CLI:\n%s", stderr.String())
	}
}

// fakeClaudeScript stands in for `claude mcp add-json --scope user`: it
// creates the user-scope file with the one server it is given (it does not
// merge; the tests start from no file) and logs every call.
const fakeClaudeScript = `#!/bin/sh
cfg="${CLAUDE_CONFIG_DIR:-$HOME}/.claude.json"
[ -n "$FAKE_CLAUDE_LOG" ] && echo "$*" >> "$FAKE_CLAUDE_LOG"
case "$1 $2 $3 $4" in
"mcp add-json --scope user")
  if [ -e "$cfg" ]; then echo "fake claude: cannot merge into $cfg" >&2; exit 3; fi
  printf '{"mcpServers":{"%s":%s}}\n' "$5" "$6" > "$cfg" ;;
*) echo "fake claude: unsupported: $*" >&2; exit 2 ;;
esac
`

// installFakeClaude puts fakeClaudeScript first on PATH and returns the file
// its calls are logged to.
func installFakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(fakeClaudeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	log := filepath.Join(t.TempDir(), "claude.log")
	t.Setenv("FAKE_CLAUDE_LOG", log)
	return log
}

// ark setup claude --global registers Ark in Claude Code's user scope,
// ~/.claude.json, through the claude CLI; ~/.claude/settings.json (which
// Claude Code does not read MCP servers from) is reported, never written.
func TestCLI_SetupClaudeGlobal_ThroughClaudeCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude CLI is a POSIX shell script")
	}
	bin := buildArk(t)
	repo, home, ark := t.TempDir(), t.TempDir(), fakeArkBin(t)
	log := installFakeClaude(t)
	legacy := filepath.Join(home, ".claude", "settings.json")
	legacyBody := []byte(`{"theme":"dark","mcpServers":{"ark":{"command":"ark","args":["mcp-server","--root","/old"]}}}`)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, legacyBody, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runArk(t, bin, repo, home, "setup", "claude", "--global", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	cfg := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("%s not written by claude: %v", cfg, err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	e := doc.MCPServers["ark"]
	if e.Type != "stdio" || e.Command != ark || len(e.Args) != 3 || e.Args[2] != repo {
		t.Errorf("unexpected entry %+v (want the absolute root %s)", e, repo)
	}
	if !strings.Contains(stdout, "Config: "+cfg) {
		t.Errorf("report does not name %s:\n%s", cfg, stdout)
	}
	if !strings.Contains(stderr, "Warning: "+legacy) || !strings.Contains(stderr, "can delete") {
		t.Errorf("no notice about the old entry in %s:\n%s", legacy, stderr)
	}
	if got, _ := os.ReadFile(legacy); !bytes.Equal(got, legacyBody) {
		t.Errorf("%s changed: %s", legacy, got)
	}

	stdout, _, code = runArk(t, bin, repo, home, "setup", "claude", "--global", "--ark-path", ark, "--root", repo)
	if code != 0 || !strings.Contains(stdout, "No changes required") {
		t.Errorf("second run: exit %d\n%s", code, stdout)
	}
	calls, _ := os.ReadFile(log)
	if n := strings.Count(string(calls), "mcp add-json --scope user ark "); n != 1 || strings.Count(string(calls), "\n") != 1 {
		t.Errorf("want exactly one add-json call, got:\n%s", calls)
	}
}

// Without claude on PATH, --global fails with the command to run by hand and
// writes nothing.
func TestCLI_SetupClaudeGlobal_NoClaudeCLI(t *testing.T) {
	bin := buildArk(t)
	repo, home, ark := t.TempDir(), t.TempDir(), fakeArkBin(t)
	empty := t.TempDir()
	_, stderr, code := runArkEnv(t, bin, repo, home, []string{"PATH=" + empty}, "setup", "claude", "--global", "--ark-path", ark, "--root", repo)
	if code == 0 || !strings.Contains(stderr, "claude mcp add-json --scope user ark") {
		t.Fatalf("exit %d, want guidance naming add-json:\n%s", code, stderr)
	}
	for _, p := range []string{filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", "settings.json")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s was written: %v", p, err)
		}
	}
}

// A malformed ~/.claude.json is never handed to claude (which would reset it
// and drop the user's servers).
func TestCLI_SetupClaudeGlobal_MalformedConfigNotTouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude CLI is a POSIX shell script")
	}
	bin := buildArk(t)
	repo, home, ark := t.TempDir(), t.TempDir(), fakeArkBin(t)
	log := installFakeClaude(t)
	cfg := filepath.Join(home, ".claude.json")
	bad := []byte(`{"mcpServers": {"other": `)
	if err := os.WriteFile(cfg, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runArk(t, bin, repo, home, "setup", "claude", "--global", "--force", "--ark-path", ark, "--root", repo)
	if code == 0 || !strings.Contains(stderr, "Ark did not run `claude`") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if got, _ := os.ReadFile(cfg); !bytes.Equal(got, bad) {
		t.Errorf("%s changed", cfg)
	}
	if calls, err := os.ReadFile(log); err == nil && len(calls) > 0 {
		t.Errorf("claude was started:\n%s", calls)
	}
}

// ark mcp-init --global is refused with guidance and writes nothing.
func TestCLI_MCPInitGlobal_Refused(t *testing.T) {
	bin := buildArk(t)
	repo, home := t.TempDir(), t.TempDir()
	_, stderr, code := runArk(t, bin, repo, home, "mcp-init", "--global", "--ark-path", fakeArkBin(t), "--root", repo)
	if code == 0 || !strings.Contains(stderr, "ark setup claude --global") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	for _, p := range []string{filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".claude.json"), filepath.Join(repo, ".mcp.json")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s was written: %v", p, err)
		}
	}
}

// ark setup cline writes the file the Cline CLI reads, following
// CLINE_DATA_DIR, and reports (never changes) the old ~/.cline/mcp.json.
func TestCLI_SetupCline_FollowsClineDataDir(t *testing.T) {
	bin := buildArk(t)
	repo, home, ark := t.TempDir(), t.TempDir(), fakeArkBin(t)
	legacy := filepath.Join(home, ".cline", "mcp.json")
	legacyBody := []byte(`{"mcpServers":{"ark":{"command":"ark"},"github":{"command":"gh"}}}`)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, legacyBody, 0o644); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "cline-data")
	env := []string{"CLINE_DATA_DIR=" + data}
	stdout, stderr, code := runArkEnv(t, bin, repo, home, env, "setup", "cline", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	cfg := filepath.Join(data, "settings", "cline_mcp_settings.json")
	if !strings.Contains(stdout, "Config: "+cfg) {
		t.Errorf("report does not name %s:\n%s", cfg, stdout)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cline", "data")); !os.IsNotExist(err) {
		t.Errorf("the default location was used despite CLINE_DATA_DIR: %v", err)
	}
	if !strings.Contains(stderr, "Warning: "+legacy) || !strings.Contains(stderr, "github") {
		t.Errorf("no notice about %s:\n%s", legacy, stderr)
	}
	if got, _ := os.ReadFile(legacy); !bytes.Equal(got, legacyBody) {
		t.Errorf("%s changed", legacy)
	}
	stdout, _, code = runArkEnv(t, bin, repo, home, env, "setup", "cline", "--ark-path", ark, "--root", repo)
	if code != 0 || !strings.Contains(stdout, "No changes required") {
		t.Errorf("second run: exit %d\n%s", code, stdout)
	}
}

// An existing "ark" entry that is not of the form Ark writes may hold
// credentials anywhere (env, headers, URL, unknown fields). setup refuses it,
// with and without --force, before running claude: nothing of it reaches a
// command line, Ark's output or the file.
func TestCLI_SetupClaudeGlobal_SecretEntryNeverExposed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude CLI is a POSIX shell script")
	}
	const secret = "DUMMY-SECRET" // not shaped like any real token
	bin := buildArk(t)
	repo, home, ark := t.TempDir(), t.TempDir(), fakeArkBin(t)
	log := installFakeClaude(t)
	cfg := filepath.Join(home, ".claude.json")
	body := []byte(`{"userID":"u","mcpServers":{` +
		`"ark":{"type":"stdio","command":"/x/ark","args":["mcp-server","--root","/r"],"env":{"API_KEY":"` + secret + `-ENV"},"headers":{"Authorization":"Bearer ` + secret + `-BEARER"},"x-extra":"` + secret + `-UNKNOWN"},` +
		`"other":{"type":"http","url":"https://u:` + secret + `-URL@mcp.example.invalid/mcp"}}}`)
	if err := os.WriteFile(cfg, body, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		args := []string{"setup", "claude", "--global", "--ark-path", ark, "--root", repo}
		if force {
			args = append(args, "--force")
		}
		stdout, stderr, code := runArk(t, bin, repo, home, args...)
		if code == 0 {
			t.Fatalf("force=%v: exit 0\n%s\n%s", force, stdout, stderr)
		}
		if !strings.Contains(stderr, "not of the form Ark writes") {
			t.Errorf("force=%v: no refusal in stderr:\n%s", force, stderr)
		}
		if strings.Contains(stdout+stderr, secret) {
			t.Errorf("force=%v: a secret is in Ark's output:\n%s\n%s", force, stdout, stderr)
		}
	}
	if calls, err := os.ReadFile(log); err == nil {
		t.Errorf("claude ran: %s", calls)
	}
	if after, _ := os.ReadFile(cfg); string(after) != string(body) {
		t.Errorf("%s changed:\n%s", cfg, after)
	}
}
