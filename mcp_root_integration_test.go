package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// These tests drive the real `ark mcp-server` binary over stdio JSON-RPC, the
// way an MCP client launches it, to pin the root contract end to end:
// --root is resolved once against the launch CWD, in-root absolute and relative
// paths are accepted, and anything outside the root is refused.

// rootWorkspace lays out <base>/{project-a,project-b,launcher}.
func rootWorkspace(t *testing.T) (base, projA, projB, launcher string) {
	t.Helper()
	base = t.TempDir()
	projA = filepath.Join(base, "project-a")
	projB = filepath.Join(base, "project-b")
	launcher = filepath.Join(base, "launcher")
	files := map[string]string{
		filepath.Join(projA, "go.mod"):  "module example.com/a\n\ngo 1.22\n",
		filepath.Join(projA, "main.go"): "package main\n\nfunc main() { helper() }\n\nfunc helper() {}\n",
		filepath.Join(projB, "main.go"): "package main\n\nfunc main() {}\n\nfunc secret() {}\n",
	}
	for p, c := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, projA, projB, launcher
}

type rpcClient struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *strings.Builder
	nextID int
}

// startServer launches `command args...` in dir with a clean environment
// apart from HOME. CLAUDE_PROJECT_DIR is removed so the CWD is the only way a
// relative root can be resolved.
func startServer(t *testing.T, dir, home, command string, args ...string) *rpcClient {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	env := []string{"HOME=" + home, "USERPROFILE=" + home}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "USERPROFILE=") || strings.HasPrefix(kv, "CLAUDE_PROJECT_DIR=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &rpcClient{t: t, cmd: cmd, in: in, out: bufio.NewReader(out), stderr: stderr}
	t.Cleanup(func() {
		_ = in.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	return c
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	} `json:"error"`
}

func (c *rpcClient) call(method string, params any) rpcResponse {
	c.t.Helper()
	c.nextID++
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	if _, err := c.in.Write(append(req, '\n')); err != nil {
		c.t.Fatalf("%s: write: %v\nstderr:%s", method, err, c.stderr)
	}
	line, err := c.out.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("%s: read: %v\nstderr:%s", method, err, c.stderr)
	}
	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		c.t.Fatalf("%s: bad response %q: %v", method, line, err)
	}
	if resp.ID != c.nextID {
		c.t.Fatalf("%s: response id %d, want %d", method, resp.ID, c.nextID)
	}
	return resp
}

// tool calls a tool and returns its text and isError flag.
func (c *rpcClient) tool(name string, args map[string]any) (string, bool) {
	c.t.Helper()
	resp := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	if resp.Error != nil {
		c.t.Fatalf("tools/call %s: protocol error %+v", name, resp.Error)
	}
	var r struct {
		Content []struct{ Text string } `json:"content"`
		IsError bool                    `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		c.t.Fatalf("tools/call %s: %v", name, err)
	}
	text := ""
	if len(r.Content) > 0 {
		text = r.Content[0].Text
	}
	return text, r.IsError
}

func (c *rpcClient) handshake() {
	c.t.Helper()
	if resp := c.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "itest", "version": "0"},
	}); resp.Error != nil {
		c.t.Fatalf("initialize: %+v", resp.Error)
	}
	resp := c.call("tools/list", map[string]any{})
	if resp.Error != nil || !strings.Contains(string(resp.Result), `"get_context"`) {
		c.t.Fatalf("tools/list: %+v %s", resp.Error, resp.Result)
	}
}

// assertServesProject checks the root contract against a server that should
// be serving projA.
func assertServesProject(t *testing.T, c *rpcClient, projA, projB string) {
	t.Helper()
	c.handshake()
	accept := []struct {
		tool string
		args map[string]any
	}{
		{"get_file_info", map[string]any{"path": "main.go"}},
		{"get_file_info", map[string]any{"path": filepath.Join(projA, "main.go")}},
		{"get_symbols", map[string]any{"path": filepath.Join(projA, "main.go")}},
		{"get_context", map[string]any{"path": ".", "symbol": "helper"}},
		{"get_context", map[string]any{"path": projA, "symbol": "helper"}},
		{"find_symbol", map[string]any{"pattern": "helper", "path": projA}},
	}
	for _, a := range accept {
		if text, isErr := c.tool(a.tool, a.args); isErr {
			t.Errorf("%s %v: rejected: %s\nstderr:%s", a.tool, a.args, text, c.stderr)
		}
	}
	// The served project is project-a: project-b's symbols are not in scope.
	if text, _ := c.tool("find_symbol", map[string]any{"pattern": "secret"}); !strings.Contains(text, `"matches": null`) {
		t.Errorf("find_symbol sees project-b: %s", text)
	}
	reject := []struct {
		tool string
		args map[string]any
	}{
		{"get_file_info", map[string]any{"path": filepath.Join(projB, "main.go")}},
		{"get_file_content", map[string]any{"path": "../project-b/main.go"}},
		{"get_context", map[string]any{"path": projB, "symbol": "secret"}},
		{"get_context", map[string]any{"path": "../project-b", "symbol": "secret"}},
	}
	for _, r := range reject {
		text, isErr := c.tool(r.tool, r.args)
		if !isErr || !strings.Contains(text, "outside the server root") {
			t.Errorf("%s %v: not rejected: %s", r.tool, r.args, text)
		}
	}
}

func TestMCPRoot_DotRootFromProjectCWD(t *testing.T) {
	bin := buildArk(t)
	_, projA, projB, _ := rootWorkspace(t)
	c := startServer(t, projA, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	assertServesProject(t, c, projA, projB)
}

func TestMCPRoot_AbsoluteRootFromOtherCWD(t *testing.T) {
	bin := buildArk(t)
	_, projA, projB, launcher := rootWorkspace(t)
	c := startServer(t, launcher, t.TempDir(), bin, "mcp-server", "--root", projA, "--no-cache")
	assertServesProject(t, c, projA, projB)
}

func TestMCPRoot_RelativeRootFromOtherCWD(t *testing.T) {
	bin := buildArk(t)
	_, projA, projB, launcher := rootWorkspace(t)
	c := startServer(t, launcher, t.TempDir(), bin, "mcp-server", "--root", "../project-a/", "--no-cache")
	assertServesProject(t, c, projA, projB)
}

// A root that cannot be served stops the server at startup with a message
// naming the problem, instead of serving nothing.
func TestMCPRoot_InvalidRootFailsAtStartup(t *testing.T) {
	bin := buildArk(t)
	_, projA, _, launcher := rootWorkspace(t)
	for _, c := range []struct{ root, want string }{
		{filepath.Join(launcher, "missing"), "does not exist"},
		{filepath.Join(projA, "main.go"), "not a directory"},
		{"${CLAUDE_PROJECT_DIR:-.}/", "does not exist"},
	} {
		_, stderr, code := runArk(t, bin, launcher, t.TempDir(), "mcp-server", "--root", c.root, "--no-cache")
		if code == 0 || !strings.Contains(stderr, c.want) {
			t.Errorf("--root %q: exit %d, stderr %q (want %q)", c.root, code, stderr, c.want)
		}
	}
}

// expandClaude applies Claude Code's documented .mcp.json expansion
// (${VAR} and ${VAR:-default}) using env. Claude Code sets CLAUDE_PROJECT_DIR
// only in the spawned server's environment, not in its own, so at expansion
// time it is unset and the default applies.
func expandClaude(s string, env map[string]string) string {
	re := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		g := re.FindStringSubmatch(m)
		if v, ok := env[g[1]]; ok && v != "" {
			return v
		}
		if g[2] != "" {
			return g[3]
		}
		return m
	})
}

const fakeCodexScript = `#!/bin/sh
state="$FAKE_CODEX_STATE"
case "$1 $2" in
"mcp list") if [ -f "$state" ]; then cat "$state"; else echo '[]'; fi ;;
"mcp add")
  shift 4
  cmd="$1"; shift
  args=""
  for a in "$@"; do args="$args${args:+,}\"$a\""; done
  printf '[{"name":"ark","transport":{"type":"stdio","command":"%s","args":[%s]}}]\n' "$cmd" "$args" > "$state" ;;
"mcp remove") rm -f "$state" ;;
*) exit 2 ;;
esac
`

// For every supported client, `ark setup` writes a launch entry; launching
// exactly that command line (after the client's own documented expansion, and
// from the project directory as CWD) serves the project and honours the root
// contract. A second setup is a no-op and other servers are preserved.
func TestMCPRoot_SetupEntriesLaunchTheProject(t *testing.T) {
	bin := buildArk(t)
	type launch struct {
		command string
		args    []string
	}
	jsonEntry := func(t *testing.T, path, key string) launch {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		e, ok := cfg[key]["ark"]
		if !ok {
			t.Fatalf("%s: no %s.ark entry:\n%s", path, key, data)
		}
		if _, ok := cfg[key]["other"]; !ok {
			t.Errorf("%s: unrelated server entry lost:\n%s", path, data)
		}
		return launch{e.Command, e.Args}
	}
	seed := func(t *testing.T, path, key string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{%q:{"other":{"command":"other-server","args":["x"]}}}`, key)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	clients := []struct {
		id     string
		config func(projA, home string) (path, key string) // "" path → codex
		expand func(string) string
	}{
		{"claude", func(p, _ string) (string, string) { return filepath.Join(p, ".mcp.json"), "mcpServers" },
			func(s string) string { return expandClaude(s, nil) }},
		{"cursor", func(p, _ string) (string, string) { return filepath.Join(p, ".cursor", "mcp.json"), "mcpServers" }, nil},
		{"cline", func(_, h string) (string, string) { return filepath.Join(h, ".cline", "mcp.json"), "mcpServers" }, nil},
		{"copilot-cli", func(p, _ string) (string, string) { return filepath.Join(p, ".github", "mcp.json"), "mcpServers" }, nil},
		{"copilot-vscode", func(p, _ string) (string, string) { return filepath.Join(p, ".vscode", "mcp.json"), "servers" }, nil},
		{"codex", nil, nil},
	}
	for _, cl := range clients {
		t.Run(cl.id, func(t *testing.T) {
			_, projA, projB, _ := rootWorkspace(t)
			home := t.TempDir()
			fakeBin := t.TempDir()
			state := filepath.Join(t.TempDir(), "codex.json")
			if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(fakeCodexScript), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FAKE_CODEX_STATE", state)

			var path, key string
			if cl.config != nil {
				path, key = cl.config(projA, home)
				seed(t, path, key)
			}
			setupArgs := []string{"setup", cl.id, "--ark-path", bin, "--root", projA}
			for run := 1; run <= 2; run++ {
				stdout, stderr, code := runArk(t, bin, projA, home, setupArgs...)
				if code != 0 {
					t.Fatalf("setup run %d: exit %d\n%s\n%s", run, code, stdout, stderr)
				}
				if run == 2 && !strings.Contains(stdout, "No changes required") {
					t.Errorf("second setup is not a no-op:\n%s", stdout)
				}
			}

			var l launch
			if cl.config != nil {
				l = jsonEntry(t, path, key)
			} else {
				data, err := os.ReadFile(state)
				if err != nil {
					t.Fatal(err)
				}
				var list []struct {
					Transport struct {
						Command string   `json:"command"`
						Args    []string `json:"args"`
					} `json:"transport"`
				}
				if err := json.Unmarshal(data, &list); err != nil || len(list) != 1 {
					t.Fatalf("codex state %s: %v", data, err)
				}
				l = launch{list[0].Transport.Command, list[0].Transport.Args}
			}
			if l.command != bin || len(l.args) != 3 || l.args[0] != "mcp-server" || l.args[1] != "--root" {
				t.Fatalf("unexpected launch entry %+v", l)
			}
			if cl.expand != nil {
				// Claude project scope serving the CWD writes the portable
				// placeholder; Claude Code expands it to "./".
				if l.args[2] != "${CLAUDE_PROJECT_DIR:-.}/" {
					t.Errorf("--root %q, want the Claude placeholder", l.args[2])
				}
				for i := range l.args {
					l.args[i] = cl.expand(l.args[i])
				}
			} else if l.args[2] != projA {
				t.Errorf("--root %q, want the absolute project root %q", l.args[2], projA)
			}
			c := startServer(t, projA, home, l.command, append(l.args, "--no-cache")...)
			assertServesProject(t, c, projA, projB)
		})
	}
}
