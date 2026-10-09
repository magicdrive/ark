package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Synthetic secrets the repository-dump rules detect, assembled at run time so
// no literal in this repository looks like a credential.
var (
	maskAWS    = "AKIA" + strings.Repeat("Q", 16)
	maskAWS2   = "AKIA" + strings.Repeat("Z", 16)
	maskAWS3   = "AKIA" + strings.Repeat("W", 16)
	maskGitHub = "ghp_" + strings.Repeat("a", 36)
	maskSlack  = "xoxb-" + strings.Repeat("1", 12)
	maskStripe = "sk_live_" + strings.Repeat("b", 24)
	maskJWT    = "eyJ" + strings.Repeat("a", 12) + "." + strings.Repeat("b", 12) + "." + strings.Repeat("c", 12)
	maskValue  = "hunter2-dummy-value"
	maskEnv    = "envtoken-dummy-123"
	maskPEM    = "MIIEdummyPEMbodyLINE"
	maskUTF8   = "秘密の値-dummy"
)

func maskFixture() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/sec\n\ngo 1.22\n",
		"cfg/cfg.go": `package cfg

// Key is the deploy key ` + maskAWS2 + ` (do not share).
const Key = "` + maskAWS + `"

// Load returns a client. 日本語のコメント: password = "` + maskUTF8 + `"
func Load() string {
	password := "` + maskValue + `"
	return Connect("` + maskGitHub + `", password)
}

// Connect joins its arguments.
func Connect(tok, pw string) string { return tok + pw + "` + maskStripe + `" }
`,
		"web/app.ts": `export function call(): string { return "` + maskSlack + `".trim() }
export function signed(k = "` + maskAWS3 + `") { return k + "` + maskJWT + `" }
`,
		".env":         "API_TOKEN=" + maskEnv + "\n",
		"keys/dev.pem": "-----BEGIN RSA PRIVATE KEY-----\n" + maskPEM + "\n-----END RSA PRIVATE KEY-----\n",
	}
}

var maskSecretsAll = []string{maskAWS, maskAWS2, maskAWS3, maskGitHub, maskSlack, maskStripe, maskJWT, maskValue, maskEnv, maskPEM, maskUTF8}

// leaked returns the synthetic secrets in s.
func leaked(s string) []string {
	var out []string
	for _, secret := range maskSecretsAll {
		if strings.Contains(s, secret) {
			out = append(out, secret)
		}
	}
	return out
}

func writeMaskFixture(t *testing.T) string {
	t.Helper()
	proj := t.TempDir()
	for p, c := range maskFixture() {
		full := filepath.Join(proj, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return proj
}

// maskCalls reaches every tool with arguments that put the fixture's secrets
// within reach of its output, plus a resource read and failing calls.
var maskCalls = []struct {
	method string
	params map[string]any
}{
	{"tools/call", map[string]any{"name": "get_directory_tree", "arguments": map[string]any{"path": "."}}},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "cfg/cfg.go"}}},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": ".env", "maskSecrets": true}}},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "keys/dev.pem"}}},
	{"tools/call", map[string]any{"name": "list_files", "arguments": map[string]any{"path": "."}}},
	{"tools/call", map[string]any{"name": "search_in_files", "arguments": map[string]any{"path": ".", "query": "A"}}},
	{"tools/call", map[string]any{"name": "get_file_info", "arguments": map[string]any{"path": "cfg/cfg.go"}}},
	{"tools/call", map[string]any{"name": "get_project_stats", "arguments": map[string]any{"path": "."}}},
	{"tools/call", map[string]any{"name": "get_files_arklite", "arguments": map[string]any{"paths": []string{"cfg/cfg.go", "web/app.ts", ".env"}}}},
	{"tools/call", map[string]any{"name": "get_symbols", "arguments": map[string]any{"path": "web/app.ts"}}},
	{"tools/call", map[string]any{"name": "find_symbol", "arguments": map[string]any{"pattern": "."}}},
	{"tools/call", map[string]any{"name": "get_symbol", "arguments": map[string]any{"path": "cfg/cfg.go", "name": "Load"}}},
	{"tools/call", map[string]any{"name": "get_symbol", "arguments": map[string]any{"path": "web/app.ts", "name": "signed"}}},
	{"tools/call", map[string]any{"name": "find_references", "arguments": map[string]any{"path": ".", "name": "trim"}}},
	{"tools/call", map[string]any{"name": "get_relations", "arguments": map[string]any{"path": ".", "symbol": "call"}}},
	{"tools/call", map[string]any{"name": "get_callers", "arguments": map[string]any{"path": ".", "symbol": "Connect"}}},
	{"tools/call", map[string]any{"name": "get_callees", "arguments": map[string]any{"path": ".", "symbol": "call"}}},
	{"tools/call", map[string]any{"name": "get_repository_map", "arguments": map[string]any{"path": ".", "format": "json", "detail": "verbose"}}},
	{"tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "Load"}}},
	{"tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "Load", "format": "json"}}},
	{"tools/call", map[string]any{"name": "search_context", "arguments": map[string]any{"query": "Load", "contextLimit": 3}}},
	{"tools/call", map[string]any{"name": "analyze_change_impact", "arguments": map[string]any{"path": ".", "symbol": "call"}}},
	{"tools/call", map[string]any{"name": "analyze_change_impact", "arguments": map[string]any{"path": ".", "symbol": "call", "format": "json"}}},
	{"tools/call", map[string]any{"name": "search_code", "arguments": map[string]any{"path": ".", "format": "json"}}},
	{"tools/call", map[string]any{"name": "get_language_support", "arguments": map[string]any{}}},
	{"tools/call", map[string]any{"name": "get_diagnostics", "arguments": map[string]any{"path": "."}}},
	{"resources/read", map[string]any{"uri": "file://cfg/cfg.go"}},
	{"resources/read", map[string]any{"uri": "file://.env"}},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "../outside"}}},
	{"tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "NoSuchSymbol"}}},
}

// TestMCPSecretMasking_NoDetectableSecretLeaves drives every tool through a
// real stdio session with the default settings (and once with an explicit
// --mask-secrets on) and checks the raw JSON-RPC lines a client receives:
// none contains a secret the masking rules detect, every line is a valid
// response with its request's id, every tool result is unchanged in shape
// (JSON results stay JSON), and no masking warning is logged.
func TestMCPSecretMasking_NoDetectableSecretLeaves(t *testing.T) {
	for _, flags := range [][]string{nil, {"--mask-secrets", "on"}} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) { checkNoLeak(t, flags) })
	}
}

func checkNoLeak(t *testing.T, flags []string) {
	bin := buildArk(t)
	proj := writeMaskFixture(t)
	c := startServer(t, proj, t.TempDir(), bin, append([]string{"mcp-server", "--root", "./", "--no-cache"}, flags...)...)
	c.handshake()
	defer func() {
		if stderr := stopServer(c); strings.Contains(stderr, "masking is disabled") {
			t.Errorf("masking warning logged while masking is on:\n%s", stderr)
		}
	}()

	tools := map[string]bool{}
	for _, call := range maskCalls {
		c.nextID++
		req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": call.method, "params": call.params})
		if _, err := c.in.Write(append(req, '\n')); err != nil {
			t.Fatal(err)
		}
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			t.Fatalf("%v: %v", call.params, err)
		}
		if s := leaked(string(line)); len(s) > 0 {
			t.Errorf("%s %v leaks %q:\n%s", call.method, call.params, s, line)
		}
		var resp struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Result  json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(line, &resp); err != nil || resp.JSONRPC != "2.0" || resp.ID != c.nextID {
			t.Fatalf("invalid response to %v: %s (%v)", call.params, line, err)
		}
		if name, ok := call.params["name"].(string); ok {
			tools[name] = true
			var r struct {
				Content []struct{ Text string } `json:"content"`
			}
			if err := json.Unmarshal(resp.Result, &r); err != nil || len(r.Content) == 0 {
				t.Fatalf("%s: result %s", name, resp.Result)
			}
			if text := strings.TrimSpace(r.Content[0].Text); strings.HasPrefix(text, "{") && !json.Valid([]byte(text)) {
				t.Errorf("%s: JSON result no longer valid:\n%s", name, text)
			}
		}
	}
	if len(tools) != 21 {
		t.Errorf("covered %d tools, want all 21", len(tools))
	}
}

// Identifiers survive masking: a file whose path looks like a secret
// assignment is still reported, and reachable, under its real path.
func TestMCPSecretMasking_IdentifiersAreNotMasked(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	path := filepath.Join(proj, "auth", "token-store.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package auth\n\nfunc Rotate() string { return \""+maskAWS+"\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	text, _ := c.tool("get_context", map[string]any{"path": ".", "symbol": "Rotate", "format": "json"})
	var got struct {
		Items []struct{ File, Source string }
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil || len(got.Items) == 0 {
		t.Fatalf("get_context: %s (%v)", text, err)
	}
	if got.Items[0].File != "auth/token-store.go" {
		t.Errorf("file path masked: %q", got.Items[0].File)
	}
	if strings.Contains(got.Items[0].Source, maskAWS) {
		t.Errorf("source not masked: %q", got.Items[0].Source)
	}
	if again, isErr := c.tool("get_symbols", map[string]any{"path": got.Items[0].File}); isErr {
		t.Errorf("the reported path cannot be used again: %s", again)
	}
}

// The HTTP transport applies the same masking under concurrent requests.
func TestMCPSecretMasking_HTTPConcurrent(t *testing.T) {
	bin := buildArk(t)
	proj := writeMaskFixture(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache", "--type", "http", "--http-port", fmt.Sprint(port))
	url := fmt.Sprintf("http://localhost:%d/mcp", port)
	post := func(id int, method string, params any) (string, error) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		_, err = b.ReadFrom(resp.Body)
		return b.String(), err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := post(0, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("HTTP server did not start: %s", c.stderr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 4*len(maskCalls))
	for round := 0; round < 4; round++ {
		for i, call := range maskCalls {
			wg.Add(1)
			go func(id int, method string, params map[string]any) {
				defer wg.Done()
				out, err := post(id, method, params)
				if err != nil {
					errs <- err.Error()
					return
				}
				if s := leaked(out); len(s) > 0 {
					errs <- fmt.Sprintf("%v leaks %q", params, s)
				}
				var resp struct{ ID int }
				if json.Unmarshal([]byte(out), &resp) != nil || resp.ID != id {
					errs <- fmt.Sprintf("bad response for id %d: %s", id, out)
				}
			}(1000*round+i+1, call.method, call.params)
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// stopServer ends a stdio server (closing its input) and returns its stderr,
// read only after the process has exited.
func stopServer(c *rpcClient) string {
	_ = c.in.Close()
	_ = c.cmd.Wait()
	return c.stderr.String()
}

// rawCall sends one request and returns the raw response line.
func rawCall(t *testing.T, c *rpcClient, method string, params map[string]any) string {
	t.Helper()
	c.nextID++
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	if _, err := c.in.Write(append(req, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := c.out.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
	}
	if err := json.Unmarshal(line, &resp); err != nil || resp.JSONRPC != "2.0" || resp.ID != c.nextID {
		t.Fatalf("invalid response to %s %v: %s", method, params, line)
	}
	return string(line)
}

// --mask-secrets off returns the source as written and says so once on
// stderr, never on the protocol stream and without content. The setting is
// the server's: a call's maskSecrets argument changes nothing, in either
// direction (TestFileContent_SecurityOverridesStillIgnored).
func TestMCPSecretMasking_Settings(t *testing.T) {
	bin := buildArk(t)
	proj := writeMaskFixture(t)
	fileCall := func(mask any) map[string]any {
		args := map[string]any{"path": "cfg/cfg.go"}
		if mask != nil {
			args["maskSecrets"] = mask
		}
		return map[string]any{"name": "get_file_content", "arguments": args}
	}
	contextCall := map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "Load", "format": "json"}}
	for _, tc := range []struct {
		name         string
		flags        []string
		call         map[string]any
		calls        int // how often the call is sent
		wantLeak     bool
		wantWarnings int
	}{
		{"server off: every response unmasked", []string{"--mask-secrets", "off"}, contextCall, 3, true, 1},
		{"server off: resource unmasked", []string{"--mask-secrets", "off"}, nil, 1, true, 1},
		{"server off: maskSecrets true changes nothing", []string{"--mask-secrets", "off"}, fileCall(true), 1, true, 1},
		{"default: maskSecrets false changes nothing", nil, fileCall(false), 3, false, 0},
		{"server on: maskSecrets false changes nothing", []string{"--mask-secrets", "on"}, fileCall(false), 1, false, 0},
		{"default: maskSecrets true", nil, fileCall(true), 1, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := startServer(t, proj, t.TempDir(), bin, append([]string{"mcp-server", "--root", "./", "--no-cache"}, tc.flags...)...)
			c.handshake()
			var out strings.Builder
			for i := 0; i < tc.calls; i++ {
				if tc.call == nil {
					out.WriteString(rawCall(t, c, "resources/read", map[string]any{"uri": "file://cfg/cfg.go"}))
					continue
				}
				out.WriteString(rawCall(t, c, "tools/call", tc.call))
			}
			if got := len(leaked(out.String())) > 0; got != tc.wantLeak {
				t.Errorf("leak = %v, want %v:\n%s", got, tc.wantLeak, out.String())
			}
			stderr := stopServer(c)
			if n := strings.Count(stderr, "WARNING: MCP secret masking is disabled."); n != tc.wantWarnings {
				t.Errorf("%d masking warnings, want %d:\n%s", n, tc.wantWarnings, stderr)
			}
			if s := leaked(stderr); len(s) > 0 {
				t.Errorf("stderr contains secrets %q", s)
			}
		})
	}
}

// The HTTP transport honours --mask-secrets off and logs the warning once.
func TestMCPSecretMasking_HTTPOff(t *testing.T) {
	bin := buildArk(t)
	proj := writeMaskFixture(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache", "--type", "http", "--http-port", fmt.Sprint(port), "--mask-secrets", "off")
	url := fmt.Sprintf("http://localhost:%d/mcp", port)
	post := func(id int, method string, params any) (string, error) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		_, err = b.ReadFrom(resp.Body)
		return b.String(), err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := post(0, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("HTTP server did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var out strings.Builder
	for i := 1; i <= 3; i++ {
		s, err := post(i, "tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "Load"}})
		if err != nil {
			t.Fatal(err)
		}
		out.WriteString(s)
	}
	if len(leaked(out.String())) == 0 {
		t.Errorf("--mask-secrets off over HTTP still masks:\n%s", out.String())
	}
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	if n := strings.Count(c.stderr.String(), "WARNING: MCP secret masking is disabled."); n != 1 {
		t.Errorf("%d warnings, want 1:\n%s", n, c.stderr.String())
	}
}
