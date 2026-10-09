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
	"testing"
	"time"
)

// Symlinks that lead outside the repository, through the real binary: by
// default nothing outside the root is read, directly or through a walk, the
// index or the cache; with --allow-external-symlinks on, the links in the
// repository may be read through — and nothing else outside the root, and
// nothing .arkignore excludes. Secret masking does not change either.

// externalFixture builds a repository with symlinks to an outside directory:
//
//	src/visible.go     calls ExternalHelper
//	src/extlink.go     → ext/lib.go         (file link; ExternalHelper)
//	src/hidden_link.go → ext/hidden.go      (file link; excluded by .arkignore)
//	src/chain.go       → links/chain → ext/chained.go (link chain)
//	shared             → ext/dir            (directory link; shared/private/ excluded)
//	loop_a ↔ loop_b, broken.go → ext/missing.go
//
// ext/unlinked.txt has no link to it.
func externalFixture(t *testing.T) (proj, ext string) {
	t.Helper()
	proj, ext = t.TempDir(), t.TempDir()
	write := func(dir, rel, body string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, rel string) {
		full := filepath.Join(proj, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
	}
	write(proj, "go.mod", "module example.com/ext\n\ngo 1.22\n")
	write(proj, ".arkignore", "src/hidden_link.go\nshared/private/\n")
	write(proj, "src/visible.go", "package src\n\n// Visible uses the linked helper.\nfunc Visible() string { return ExternalHelper() + \"VISIBLE_BODY_MARKER\" }\n")
	write(ext, "lib.go", "package src\n\n// ExternalHelper lives outside the repository.\nfunc ExternalHelper() string { return \"EXTERNAL_BODY_MARKER\" }\n")
	write(ext, "hidden.go", "package src\n\nfunc HiddenExternal() string { return \"HIDDEN_EXT_MARKER\" }\n")
	write(ext, "chained.go", "package src\n\nfunc ChainedHelper() string { return \"CHAINED_MARKER\" }\n")
	write(ext, "dir/pub.txt", "SHARED_PUBLIC_MARKER\n")
	write(ext, "dir/private/key.txt", "SHARED_PRIVATE_MARKER\n")
	write(ext, "unlinked.txt", "UNLINKED_MARKER\n")
	link(filepath.Join(ext, "lib.go"), "src/extlink.go")
	link(filepath.Join(ext, "hidden.go"), "src/hidden_link.go")
	link(filepath.Join(ext, "chained.go"), "links/chain")
	link("../links/chain", "src/chain.go")
	link(filepath.Join(ext, "dir"), "shared")
	link("loop_b", "loop_a")
	link("loop_a", "loop_b")
	link(filepath.Join(ext, "missing.go"), "broken.go")
	return proj, ext
}

var (
	// allowedMarkers are external content reachable through a link only when
	// external symlinks are allowed.
	allowedMarkers = []string{"EXTERNAL_BODY_MARKER", "CHAINED_MARKER", "SHARED_PUBLIC_MARKER"}
	// neverMarkers are never reachable: excluded by .arkignore, or outside
	// the root with no link to them.
	neverMarkers = []string{"HIDDEN_EXT_MARKER", "SHARED_PRIVATE_MARKER", "UNLINKED_MARKER"}
)

// externalCalls reach every tool, the resources and the external files by
// every spelling.
func externalCalls(ext string) []struct {
	method string
	params map[string]any
} {
	file := func(path string) map[string]any {
		return map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": path}}
	}
	calls := []struct {
		method string
		params map[string]any
	}{}
	add := func(method string, params map[string]any) {
		calls = append(calls, struct {
			method string
			params map[string]any
		}{method, params})
	}
	for _, p := range []string{
		"src/extlink.go", "src/chain.go", "links/chain", "shared/pub.txt", // through links
		"src/hidden_link.go", "shared/private/key.txt", // excluded
		filepath.Join(ext, "unlinked.txt"), filepath.Join(ext, "lib.go"), filepath.Join(ext, "dir", "private", "key.txt"), // outside, by absolute path
		"../" + filepath.Base(ext) + "/unlinked.txt", "shared/../../" + filepath.Base(ext) + "/unlinked.txt", "shared/private/../private/key.txt",
		"loop_a", "broken.go",
	} {
		add("tools/call", file(p))
	}
	for _, uri := range []string{"file://src/extlink.go", "file://shared/private/key.txt", "file://src/hidden_link.go", "directory://.", "directory://shared"} {
		add("resources/read", map[string]any{"uri": uri})
	}
	for _, call := range []map[string]any{
		{"name": "get_files_arklite", "arguments": map[string]any{"paths": []string{"src/extlink.go", "src/hidden_link.go", "shared/pub.txt", "shared/private/key.txt"}}},
		{"name": "get_files_arklite", "arguments": map[string]any{"paths": []string{"shared"}}},
		{"name": "get_file_info", "arguments": map[string]any{"path": "src/extlink.go"}},
		{"name": "get_symbols", "arguments": map[string]any{"path": "src/extlink.go"}},
		{"name": "get_symbols", "arguments": map[string]any{"path": "src/hidden_link.go"}},
		{"name": "get_symbol", "arguments": map[string]any{"path": "src/extlink.go", "name": "ExternalHelper"}},
		{"name": "list_files", "arguments": map[string]any{"path": "."}},
		{"name": "list_files", "arguments": map[string]any{"path": "shared"}},
		{"name": "get_directory_tree", "arguments": map[string]any{"path": "."}},
		{"name": "get_directory_tree", "arguments": map[string]any{"path": ".", "maxDepth": 5}},
		{"name": "get_project_stats", "arguments": map[string]any{"path": "."}},
		{"name": "search_in_files", "arguments": map[string]any{"path": ".", "query": "MARKER"}},
		{"name": "search_in_files", "arguments": map[string]any{"path": "shared", "query": "MARKER"}},
		{"name": "find_symbol", "arguments": map[string]any{"pattern": "Helper"}},
		{"name": "find_symbol", "arguments": map[string]any{"pattern": "HiddenExternal"}},
		{"name": "find_references", "arguments": map[string]any{"path": ".", "name": "ExternalHelper"}},
		{"name": "get_callers", "arguments": map[string]any{"path": ".", "symbol": "ExternalHelper"}},
		{"name": "get_callees", "arguments": map[string]any{"path": ".", "symbol": "Visible"}},
		{"name": "get_relations", "arguments": map[string]any{"path": ".", "symbol": "ExternalHelper"}},
		{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "ExternalHelper"}},
		{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "HiddenExternal"}},
		{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "ChainedHelper"}},
		{"name": "search_context", "arguments": map[string]any{"query": "Helper External Chained", "contextLimit": 5}},
		{"name": "analyze_change_impact", "arguments": map[string]any{"path": ".", "symbol": "ExternalHelper", "format": "json"}},
		{"name": "search_code", "arguments": map[string]any{"path": ".", "format": "json"}},
		{"name": "get_repository_map", "arguments": map[string]any{"path": ".", "format": "json", "detail": "verbose"}},
		{"name": "get_diagnostics", "arguments": map[string]any{"path": "."}},
		{"name": "get_language_support", "arguments": map[string]any{}},
	} {
		add("tools/call", call)
	}
	return calls
}

// externalSession sends every call and returns the responses by call.
func externalSession(t *testing.T, c *rpcClient, ext string) map[string]string {
	t.Helper()
	out := map[string]string{}
	tools := map[string]bool{}
	for _, call := range externalCalls(ext) {
		key := fmt.Sprint(call.method, " ", call.params)
		out[key] = rawCall(t, c, call.method, call.params)
		if name, ok := call.params["name"].(string); ok {
			tools[name] = true
		}
	}
	if len(tools) != 21 {
		t.Errorf("covered %d tools, want 21", len(tools))
	}
	return out
}

func TestMCPExternalSymlinks_Setting(t *testing.T) {
	bin := buildArk(t)
	for _, tc := range []struct {
		name  string
		flags []string
		allow bool
	}{
		{"default", nil, false},
		{"off", []string{"--allow-external-symlinks", "off"}, false},
		{"default, masking off", []string{"--mask-secrets", "off"}, false},
		{"on", []string{"--allow-external-symlinks", "on"}, true},
		{"on, masking off", []string{"--allow-external-symlinks", "on", "--mask-secrets", "off"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj, ext := externalFixture(t)
			c := startServer(t, proj, t.TempDir(), bin, append([]string{"mcp-server", "--root", "./", "--no-cache"}, tc.flags...)...)
			c.handshake()
			responses := externalSession(t, c, ext)
			var all strings.Builder
			for key, line := range responses {
				all.WriteString(line)
				if m := markersIn(line, neverMarkers); len(m) > 0 {
					t.Errorf("%s reveals %q:\n%s", key, m, line)
				}
				if !tc.allow {
					if m := markersIn(line, allowedMarkers); len(m) > 0 {
						t.Errorf("%s reveals external content %q with external symlinks off:\n%s", key, m, line)
					}
				}
			}
			if tc.allow {
				// Through the links, directly and through walks and the index.
				for _, want := range []struct{ tool, arg, marker string }{
					{"get_file_content", "path:src/extlink.go", "EXTERNAL_BODY_MARKER"},
					{"get_file_content", "path:src/chain.go", "CHAINED_MARKER"},
					{"get_file_content", "path:shared/pub.txt", "SHARED_PUBLIC_MARKER"},
					{"search_in_files", "path:. query:MARKER", "EXTERNAL_BODY_MARKER"},
					{"get_context", "symbol:ExternalHelper", "EXTERNAL_BODY_MARKER"},
					{"get_context", "symbol:ChainedHelper", "CHAINED_MARKER"},
				} {
					found := false
					for key, line := range responses {
						if strings.Contains(key, "name:"+want.tool+"]") && strings.Contains(key, want.arg+"]") && strings.Contains(line, want.marker) {
							found = true
						}
					}
					if !found {
						t.Errorf("with external symlinks on, %s %s does not return %s", want.tool, want.arg, want.marker)
					}
				}
			}
			// The repository's own files are unaffected.
			if line := rawCall(t, c, "tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "src/visible.go"}}); !strings.Contains(line, "VISIBLE_BODY_MARKER") {
				t.Errorf("visible file missing:\n%s", line)
			}
			stopServer(c)
		})
	}
}

// An invalid value is refused at startup, not taken as off or on.
func TestMCPExternalSymlinks_InvalidValue(t *testing.T) {
	bin := buildArk(t)
	proj, _ := externalFixture(t)
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache", "--allow-external-symlinks", "yes")
	stderr := stopServer(c)
	if !strings.Contains(stderr, "--allow-external-symlinks") {
		t.Errorf("invalid value not reported:\n%s", stderr)
	}
}

// The setting decides what the index holds across restarts sharing one
// persistent cache: content indexed while external symlinks were allowed is
// not served after a restart that disallows them.
func TestMCPExternalSymlinks_CacheFollowsTheSetting(t *testing.T) {
	bin := buildArk(t)
	proj, _ := externalFixture(t)
	home := t.TempDir()
	reachable := func(flags ...string) bool {
		c := startServer(t, proj, home, bin, append([]string{"mcp-server", "--root", "./"}, flags...)...)
		c.handshake()
		ctx := rawCall(t, c, "tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "ExternalHelper"}})
		search := rawCall(t, c, "tools/call", map[string]any{"name": "search_context", "arguments": map[string]any{"query": "ExternalHelper"}})
		stopServer(c)
		in := strings.Contains(ctx, "EXTERNAL_BODY_MARKER")
		if in != strings.Contains(search, "src/extlink.go") {
			t.Errorf("tools disagree: context %v, search_context:\n%s", in, search)
		}
		return in
	}
	if reachable() {
		t.Error("off: external content indexed")
	}
	if !reachable("--allow-external-symlinks", "on") {
		t.Error("on: external content not indexed")
	}
	if _, err := os.Stat(filepath.Join(proj, ".ark", "index")); err != nil {
		t.Fatalf("no persistent cache to test against: %v", err)
	}
	if reachable() {
		t.Error("off after on: external content served from the cache")
	}
}

// The HTTP transport applies the same setting.
func TestMCPExternalSymlinks_HTTP(t *testing.T) {
	bin := buildArk(t)
	for _, allow := range []bool{false, true} {
		proj, _ := externalFixture(t)
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		args := []string{"mcp-server", "--root", "./", "--no-cache", "--type", "http", "--http-port", fmt.Sprint(port)}
		if allow {
			args = append(args, "--allow-external-symlinks", "on")
		}
		c := startServer(t, proj, t.TempDir(), bin, args...)
		post := func(id int, method string, params any) (string, error) {
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
			resp, err := http.Post(fmt.Sprintf("http://localhost:%d/mcp", port), "application/json", bytes.NewReader(body))
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
		for i, call := range []map[string]any{
			{"name": "get_file_content", "arguments": map[string]any{"path": "src/extlink.go"}},
			{"name": "get_file_content", "arguments": map[string]any{"path": "shared/private/key.txt"}},
			{"name": "search_in_files", "arguments": map[string]any{"path": ".", "query": "MARKER"}},
			{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "ExternalHelper"}},
		} {
			s, err := post(i+1, "tools/call", call)
			if err != nil {
				t.Fatal(err)
			}
			out.WriteString(s)
		}
		if got := strings.Contains(out.String(), "EXTERNAL_BODY_MARKER"); got != allow {
			t.Errorf("HTTP, allow %v: external content returned = %v:\n%s", allow, got, out.String())
		}
		if m := markersIn(out.String(), neverMarkers); len(m) > 0 {
			t.Errorf("HTTP, allow %v: reveals %q", allow, m)
		}
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
}
