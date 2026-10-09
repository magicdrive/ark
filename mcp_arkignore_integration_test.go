package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The .arkignore access policy through the real binary: a file .arkignore
// excludes cannot be read, searched, indexed, related or listed by any MCP
// tool or resource — directly, through a symlink, or through an index or
// cache built before the rule — and the repository dump excludes the same
// files.

const arkignoreRules = "src/ignored.go\nconfig/private.yaml\nsecrets/\n"

func arkignoreFixture(t *testing.T, withRules, withLinks bool) string {
	t.Helper()
	proj := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/ai\n\ngo 1.22\n",
		"src/visible.go": `package src

// Visible calls a helper declared in an excluded file.
func Visible() string { return IgnoredHelper() + VisibleHelper() }

// VisibleHelper is visible.
func VisibleHelper() string { return "VISIBLE_BODY_MARKER" }
`,
		"src/ignored.go": `package src

// IgnoredHelper is declared in an excluded file.
func IgnoredHelper() string { return "IGNORED_BODY_MARKER" + VisibleHelper() }

func Broken( {
`,
		"config/public.yaml":      "name: PUBLIC_MARKER\n",
		"config/private.yaml":     "name: PRIVATE_MARKER\n",
		"secrets/credentials.env": "CRED_MARKER=1\n",
	}
	if withRules {
		files[".arkignore"] = arkignoreRules
	}
	for p, c := range files {
		full := filepath.Join(proj, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withLinks {
		// Symlinks to an excluded file and an excluded directory.
		if err := os.Symlink("ignored.go", filepath.Join(proj, "src", "link.go")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("secrets", filepath.Join(proj, "vault")); err != nil {
			t.Fatal(err)
		}
	}
	return proj
}

// contentMarkers appear only in the content of excluded files; nameMarkers
// only in their names (which a refusal of a client's own path may echo).
var (
	contentMarkers = []string{"IGNORED_BODY_MARKER", "PRIVATE_MARKER", "CRED_MARKER"}
	nameMarkers    = []string{"ignored.go", "private.yaml", "credentials", "IgnoredHelper\","}
)

func excludedIn(s string) []string { return markersIn(s, append(contentMarkers, nameMarkers...)) }

func markersIn(s string, markers []string) []string {
	var out []string
	for _, m := range markers {
		if strings.Contains(s, m) {
			out = append(out, m)
		}
	}
	return out
}

// arkignoreCalls reach every tool, directly at excluded paths and indirectly
// through searches, the index and the graph.
var arkignoreCalls = []struct {
	method string
	params map[string]any
	direct bool // names an excluded path: must be refused
}{
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "src/ignored.go"}}, true},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "config/private.yaml", "maskSecrets": false}}, true},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "secrets/credentials.env"}}, true},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "src/link.go"}}, true},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "vault/credentials.env"}}, true},
	{"tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "src/../src/ignored.go"}}, true},
	{"tools/call", map[string]any{"name": "get_files_arklite", "arguments": map[string]any{"paths": []string{"src/ignored.go"}}}, true},
	{"tools/call", map[string]any{"name": "get_file_info", "arguments": map[string]any{"path": "src/ignored.go"}}, true},
	{"tools/call", map[string]any{"name": "get_symbols", "arguments": map[string]any{"path": "src/ignored.go"}}, true},
	{"tools/call", map[string]any{"name": "get_symbol", "arguments": map[string]any{"path": "src/ignored.go", "name": "IgnoredHelper"}}, true},
	{"tools/call", map[string]any{"name": "list_files", "arguments": map[string]any{"path": "secrets"}}, true},
	{"resources/read", map[string]any{"uri": "file://src/ignored.go"}, true},
	{"tools/call", map[string]any{"name": "list_files", "arguments": map[string]any{"path": "."}}, false},
	{"tools/call", map[string]any{"name": "list_files", "arguments": map[string]any{"path": ".", "allowGitignore": false}}, false},
	{"tools/call", map[string]any{"name": "get_directory_tree", "arguments": map[string]any{"path": "."}}, false},
	{"tools/call", map[string]any{"name": "get_directory_tree", "arguments": map[string]any{"path": ".", "maxDepth": 5}}, false},
	{"tools/call", map[string]any{"name": "get_project_stats", "arguments": map[string]any{"path": "."}}, false},
	{"tools/call", map[string]any{"name": "search_in_files", "arguments": map[string]any{"path": ".", "query": "MARKER"}}, false},
	{"tools/call", map[string]any{"name": "find_symbol", "arguments": map[string]any{"pattern": "Helper"}}, false},
	{"tools/call", map[string]any{"name": "find_references", "arguments": map[string]any{"path": ".", "name": "VisibleHelper"}}, false},
	{"tools/call", map[string]any{"name": "get_callers", "arguments": map[string]any{"path": ".", "symbol": "VisibleHelper"}}, false},
	{"tools/call", map[string]any{"name": "get_callees", "arguments": map[string]any{"path": ".", "symbol": "Visible"}}, false},
	{"tools/call", map[string]any{"name": "get_relations", "arguments": map[string]any{"path": ".", "symbol": "VisibleHelper"}}, false},
	{"tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "VisibleHelper"}}, false},
	{"tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "IgnoredHelper"}}, false},
	{"tools/call", map[string]any{"name": "search_context", "arguments": map[string]any{"query": "Helper", "contextLimit": 5}}, false},
	{"tools/call", map[string]any{"name": "analyze_change_impact", "arguments": map[string]any{"path": ".", "symbol": "VisibleHelper", "format": "json"}}, false},
	{"tools/call", map[string]any{"name": "search_code", "arguments": map[string]any{"path": ".", "format": "json"}}, false},
	{"tools/call", map[string]any{"name": "get_repository_map", "arguments": map[string]any{"path": ".", "format": "json", "detail": "verbose"}}, false},
	{"tools/call", map[string]any{"name": "get_diagnostics", "arguments": map[string]any{"path": "."}}, false},
	{"tools/call", map[string]any{"name": "get_language_support", "arguments": map[string]any{}}, false},
	{"resources/read", map[string]any{"uri": "directory://."}, false},
}

func TestMCPArkignore_ExcludedFilesAreUnreachable(t *testing.T) {
	bin := buildArk(t)
	for _, flags := range [][]string{nil, {"--mask-secrets", "off"}} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			proj := arkignoreFixture(t, true, true)
			c := startServer(t, proj, t.TempDir(), bin, append([]string{"mcp-server", "--root", "./", "--no-cache"}, flags...)...)
			c.handshake()
			tools := map[string]bool{}
			for _, call := range arkignoreCalls {
				line := rawCall(t, c, call.method, call.params)
				found := excludedIn(line)
				if call.direct {
					found = markersIn(line, contentMarkers)
				}
				if m := found; len(m) > 0 {
					t.Errorf("%s %v reveals %q:\n%s", call.method, call.params, m, line)
				}
				if call.direct && !strings.Contains(line, "does not exist") {
					t.Errorf("%s %v: not refused as nonexistent:\n%s", call.method, call.params, line)
				}
				if name, ok := call.params["name"].(string); ok {
					tools[name] = true
				}
			}
			if len(tools) != 21 {
				t.Errorf("covered %d tools, want 21", len(tools))
			}
			// The visible files stay fully available.
			for _, call := range []map[string]any{
				{"name": "get_file_content", "arguments": map[string]any{"path": "src/visible.go"}},
				{"name": "get_file_content", "arguments": map[string]any{"path": "config/public.yaml"}},
				{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "Visible"}},
			} {
				if line := rawCall(t, c, "tools/call", call); !strings.Contains(line, "MARKER") && !strings.Contains(line, "VisibleHelper") {
					t.Errorf("visible content missing for %v:\n%s", call, line)
				}
			}
		})
	}
}

// The repository dump, run in the repository as documented (`ark .`; the
// dump reads ignore files under its working directory), excludes the same
// files — with and without .gitignore handling.
func TestMCPArkignore_DumpExcludesTheSameFiles(t *testing.T) {
	bin := buildArk(t)
	// No directory symlink: the dump fails on one (a separate, known issue).
	proj := arkignoreFixture(t, true, false)
	for _, flags := range [][]string{nil, {"-a", "off"}} {
		out := filepath.Join(t.TempDir(), "dump.txt")
		cmd := exec.Command(bin, append(append([]string{"-S", "-o", out}, flags...), ".")...)
		cmd.Dir = proj
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("dump %v: %v\n%s", flags, err, b)
		}
		dump, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		// The dump includes .arkignore itself, whose rules name the excluded
		// files; their content and their file sections must be absent.
		if m := markersIn(string(dump), contentMarkers); len(m) > 0 {
			t.Errorf("dump %v contains excluded content %q", flags, m)
		}
		for _, line := range strings.Split(string(dump), "\n") {
			if strings.HasPrefix(line, "=== ") && len(markersIn(line, nameMarkers)) > 0 {
				t.Errorf("dump %v has a section for an excluded file: %s", flags, line)
			}
		}
		if !strings.Contains(string(dump), "VISIBLE_BODY_MARKER") || !strings.Contains(string(dump), "PUBLIC_MARKER") {
			t.Errorf("dump %v lost visible files", flags)
		}
	}
}

// Adding a rule hides a file from the next request — of the same server, whose
// index and extraction cache were built with the file, and of a restarted
// server reading that cache — and removing it brings the file back.
func TestMCPArkignore_FollowsRuleChanges(t *testing.T) {
	bin := buildArk(t)
	proj := arkignoreFixture(t, false, false)
	home := t.TempDir()
	reachable := func(c *rpcClient) bool {
		line := rawCall(t, c, "tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "IgnoredHelper"}})
		search := rawCall(t, c, "tools/call", map[string]any{"name": "search_context", "arguments": map[string]any{"query": "IgnoredHelper"}})
		file := rawCall(t, c, "tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "src/ignored.go"}})
		in := []bool{strings.Contains(line, "IGNORED_BODY_MARKER"), strings.Contains(search, "src/ignored.go"), strings.Contains(file, "IGNORED_BODY_MARKER")}
		if in[0] != in[1] || in[1] != in[2] {
			t.Errorf("tools disagree about the file: context %v, search %v, content %v", in[0], in[1], in[2])
		}
		return in[0]
	}
	c := startServer(t, proj, home, bin, "mcp-server", "--root", "./")
	c.handshake()
	if !reachable(c) {
		t.Fatal("file not reachable before any rule")
	}
	if err := os.WriteFile(filepath.Join(proj, ".arkignore"), []byte(arkignoreRules), 0o644); err != nil {
		t.Fatal(err)
	}
	if reachable(c) {
		t.Error("same server: file still reachable after the rule was added")
	}
	stopServer(c)

	if _, err := os.Stat(filepath.Join(proj, ".ark", "index")); err != nil {
		t.Fatalf("no extraction cache to test against: %v", err)
	}
	c = startServer(t, proj, home, bin, "mcp-server", "--root", "./")
	c.handshake()
	if reachable(c) {
		t.Error("restarted server with the old cache: file reachable")
	}
	if err := os.Remove(filepath.Join(proj, ".arkignore")); err != nil {
		t.Fatal(err)
	}
	if !reachable(c) {
		t.Error("file not reachable again after the rule was removed")
	}
}
