package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// rootFixture lays out an isolated workspace:
//
//	<base>/project-a/{go.mod,main.go}   the served root
//	<base>/project-b/main.go            a sibling outside the root
//
// and returns (base, projectA, projectB).
func rootFixture(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	a := filepath.Join(base, "project-a")
	b := filepath.Join(base, "project-b")
	files := map[string]string{
		filepath.Join(a, "go.mod"):  "module example.com/a\n\ngo 1.22\n",
		filepath.Join(a, "main.go"): "package main\n\nfunc main() { helper() }\n\nfunc helper() {}\n",
		filepath.Join(b, "main.go"): "package main\n\nfunc main() {}\n\nfunc secret() {}\n",
	}
	for p, c := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return base, a, b
}

func isContainmentError(res *CallToolResult) bool {
	if res == nil || !res.IsError || len(res.Content) == 0 {
		return false
	}
	return strings.Contains(res.Content[0].Text, "outside the server root")
}

// pathTool describes how to call one path-taking tool: args builds the
// arguments for a file path and a directory path (each as given by the client).
type pathTool struct {
	name string
	args func(file, dir string) map[string]interface{}
}

// pathTools lists every MCP tool that takes a path argument. A new path-taking
// tool must be added here so that it is held to the same root contract.
var pathTools = []pathTool{
	{"get_directory_tree", func(_, d string) map[string]interface{} { return map[string]interface{}{"path": d} }},
	{"get_file_content", func(f, _ string) map[string]interface{} { return map[string]interface{}{"path": f} }},
	{"list_files", func(_, d string) map[string]interface{} { return map[string]interface{}{"path": d} }},
	{"search_in_files", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "query": "helper"}
	}},
	{"get_file_info", func(f, _ string) map[string]interface{} { return map[string]interface{}{"path": f} }},
	{"get_project_stats", func(_, d string) map[string]interface{} { return map[string]interface{}{"path": d} }},
	{"get_files_arklite", func(f, _ string) map[string]interface{} {
		return map[string]interface{}{"paths": []interface{}{f}}
	}},
	{"get_symbols", func(f, _ string) map[string]interface{} { return map[string]interface{}{"path": f} }},
	{"find_symbol", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"pattern": "helper", "path": d}
	}},
	{"get_symbol", func(f, _ string) map[string]interface{} {
		return map[string]interface{}{"path": f, "name": "helper"}
	}},
	{"find_references", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "name": "helper"}
	}},
	{"get_relations", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "symbol": "main"}
	}},
	{"get_callers", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "symbol": "helper"}
	}},
	{"get_callees", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "symbol": "main"}
	}},
	{"get_repository_map", func(_, d string) map[string]interface{} { return map[string]interface{}{"path": d} }},
	{"get_context", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "symbol": "helper"}
	}},
	{"analyze_change_impact", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "symbol": "helper"}
	}},
	{"search_code", func(_, d string) map[string]interface{} {
		return map[string]interface{}{"path": d, "kind": "function"}
	}},
	{"get_diagnostics", func(_, d string) map[string]interface{} { return map[string]interface{}{"path": d} }},
}

// Every tool that is not listed in pathTools must take no path.
func TestPathTools_CoverEveryPathTakingTool(t *testing.T) {
	listed := map[string]bool{}
	for _, pt := range pathTools {
		listed[pt.name] = true
	}
	h := NewToolsHandler(t.TempDir(), createTestOption())
	for _, tool := range h.ListTools() {
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		_, hasPath := props["path"]
		_, hasPaths := props["paths"]
		if (hasPath || hasPaths) != listed[tool.Name] {
			t.Errorf("tool %q: takes a path = %v, listed in pathTools = %v", tool.Name, hasPath || hasPaths, listed[tool.Name])
		}
	}
}

// The MCP root contract, for every path-taking tool and for both a relative
// root (as Claude Code launches it: `--root ./` with the project as CWD) and an
// absolute root:
//
//	in-root relative path   → accepted
//	in-root absolute path   → accepted
//	out-of-root absolute    → rejected
//	../ escape              → rejected
func TestRootContract_AllPathTools(t *testing.T) {
	base, projA, projB := rootFixture(t)
	_ = base
	roots := map[string]string{
		"relative ./ root": "./",
		"relative . root":  ".",
		"absolute root":    projA,
		"trailing slash":   projA + string(filepath.Separator),
	}
	for rootName, root := range roots {
		t.Run(rootName, func(t *testing.T) {
			t.Chdir(projA)
			h := NewToolsHandlerWithCache(root, createTestOption(), nil)
			// CWD changes after construction must not change the root's meaning.
			t.Chdir(base)

			cases := []struct {
				name      string
				file, dir string
				accept    bool
			}{
				{"relative inside", "main.go", ".", true},
				{"absolute inside", filepath.Join(projA, "main.go"), projA, true},
				{"absolute outside", filepath.Join(projB, "main.go"), projB, false},
				{"dotdot escape", "../project-b/main.go", "../project-b", false},
			}
			for _, pt := range pathTools {
				for _, c := range cases {
					res, err := h.CallTool(pt.name, pt.args(c.file, c.dir))
					if err != nil {
						t.Errorf("%s / %s: protocol error: %v", pt.name, c.name, err)
						continue
					}
					if c.accept {
						if res.IsError {
							t.Errorf("%s / %s: rejected: %s", pt.name, c.name, res.Content[0].Text)
						}
					} else if !isContainmentError(res) {
						t.Errorf("%s / %s: not rejected as outside the root: %+v", pt.name, c.name, res)
					}
				}
			}
		})
	}
}

// A symlink inside the root that points outside it must not give access to
// what it points at, through any path-taking tool.
func TestRootContract_SymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	_, projA, projB := rootFixture(t)
	if err := os.Symlink(filepath.Join(projB, "main.go"), filepath.Join(projA, "leak.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(projB, filepath.Join(projA, "leakdir")); err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(projA, createTestOption())
	for _, pt := range pathTools {
		for _, p := range []struct{ file, dir string }{
			{"leak.go", "leakdir"},
			{filepath.Join(projA, "leak.go"), filepath.Join(projA, "leakdir")},
			{"leakdir/main.go", "leakdir"},
		} {
			res, err := h.CallTool(pt.name, pt.args(p.file, p.dir))
			if err != nil {
				t.Errorf("%s %v: protocol error: %v", pt.name, p, err)
				continue
			}
			if !isContainmentError(res) {
				t.Errorf("%s %v: symlink escape not rejected: %+v", pt.name, p, res)
			}
		}
	}
}

// A symlink that stays inside the root keeps working.
func TestRootContract_InternalSymlinkAccepted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	_, projA, _ := rootFixture(t)
	if err := os.Symlink(filepath.Join(projA, "main.go"), filepath.Join(projA, "alias.go")); err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(projA, createTestOption())
	for _, p := range []string{"alias.go", filepath.Join(projA, "alias.go")} {
		full, rel, err := h.resolveToolPath(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if rel != "alias.go" || full != filepath.Join(projA, "alias.go") {
			t.Errorf("%s: got (%q, %q)", p, full, rel)
		}
	}
}

// A root reached through a symlinked directory accepts absolute paths spelled
// through either the link or its target (e.g. macOS /tmp vs /private/tmp), and
// reports them relative to the root as the server was given it.
func TestRootContract_SymlinkedRootAcceptsBothSpellings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base, projA, _ := rootFixture(t)
	link := filepath.Join(base, "link-a")
	if err := os.Symlink(projA, link); err != nil {
		t.Fatal(err)
	}
	realA, err := filepath.EvalSymlinks(projA)
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(link, createTestOption())
	for _, p := range []string{
		filepath.Join(link, "main.go"),
		filepath.Join(projA, "main.go"),
		filepath.Join(realA, "main.go"),
	} {
		full, rel, err := h.resolveToolPath(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if rel != "main.go" || full != filepath.Join(link, "main.go") {
			t.Errorf("%s: got (%q, %q), want (%q, main.go)", p, full, rel, filepath.Join(link, "main.go"))
		}
	}
	if _, _, err := h.resolveToolPath(filepath.Join(base, "project-b", "main.go")); err == nil {
		t.Error("sibling of the symlinked root accepted")
	}
}

// A handler's root is absolute and clean whatever spelling it was given, so
// error messages and relative results never depend on the process CWD.
func TestNewToolsHandler_RootIsAbsolute(t *testing.T) {
	_, projA, _ := rootFixture(t)
	t.Chdir(projA)
	for _, root := range []string{"./", ".", "./sub/..", projA + "/"} {
		h := NewToolsHandler(root, createTestOption())
		if h.rootDir != projA {
			t.Errorf("root %q: rootDir = %q, want %q", root, h.rootDir, projA)
		}
	}
}

func TestRootMismatchWarning(t *testing.T) {
	base, projA, projB := rootFixture(t)
	link := filepath.Join(base, "link-a")
	if err := os.Symlink(projA, link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		root, env string
		warn      bool
	}{
		{projA, "", false},
		{projA, projA, false},
		{projA, link, false},
		{projA, projB, true},
		{projA, filepath.Join(base, "missing"), true},
	} {
		if got := rootMismatchWarning(c.root, c.env); (got != "") != c.warn {
			t.Errorf("root=%s env=%s: warning %q, want warn=%v", c.root, c.env, got, c.warn)
		}
	}
}
