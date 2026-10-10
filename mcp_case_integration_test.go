package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// C1 through the real binary: on a case-insensitive file system (macOS by
// default, Windows) a path spelled in another case reaches the same file,
// and the .arkignore rules are case-sensitive. No spelling — of the file, of
// a directory above it, of a symlink's target, of the root in an absolute
// link — reaches an excluded file through an MCP tool, a resource or the
// repository dump.

const caseRules = "secret.go\nSecrets/\nhidden-link\n"

var caseMarkers = []string{"C1_ROOT_MARKER", "C1_DIR_MARKER", "C1_NESTED_MARKER"}

func caseFixture(t *testing.T) string {
	t.Helper()
	proj := t.TempDir()
	probe := filepath.Join(proj, "case-probe")
	os.WriteFile(probe, nil, 0o644)
	_, err := os.Lstat(filepath.Join(proj, "CASE-PROBE"))
	os.Remove(probe)
	if err != nil {
		if os.Getenv("ARK_REQUIRE_CASE_FOLDING") != "" {
			t.Fatalf("ARK_REQUIRE_CASE_FOLDING: the file system at %s is case-sensitive", proj)
		}
		t.Skipf("SKIP: the file system at %s is case-sensitive: a case-folding bypass is not possible here", proj)
	}
	files := map[string]string{
		"go.mod":         "module example.com/c1\n\ngo 1.22\n",
		".arkignore":     caseRules,
		"secret.go":      "package c1\n\nfunc SecretFn() string { return \"C1_ROOT_MARKER\" }\n",
		"public.go":      "package c1\n\nfunc Public() string { return \"PUBLIC_MARKER\" }\n",
		"Secrets/key.go": "package secrets\n\nfunc Key() string { return \"C1_DIR_MARKER\" }\n",
		"src/.arkignore": "private.go\n",
		"src/private.go": "package src\n\nfunc Private() string { return \"C1_NESTED_MARKER\" }\n",
		"src/open.go":    "package src\n\nfunc Open() string { return \"OPEN_MARKER\" }\n",
	}
	for p, c := range files {
		full := filepath.Join(proj, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		"alias.go":     "SECRET.GO",
		"dlink":        "SECRETS",
		"hidden-link":  "public.go",
		"abs-upper.go": strings.ToUpper(proj) + string(filepath.Separator) + "SECRET.GO",
	} {
		if err := os.Symlink(target, filepath.Join(proj, link)); err != nil {
			t.Skipf("SKIP: symlinks unavailable: %v", err)
		}
	}
	return proj
}

func TestMCPCase_ExcludedFilesUnreachable(t *testing.T) {
	bin := buildArk(t)
	tool := func(name string, args map[string]any) (string, map[string]any) {
		return "tools/call", map[string]any{"name": name, "arguments": args}
	}
	type call struct {
		method string
		params map[string]any
		direct bool // names an excluded path: must be refused
	}
	var calls []call
	add := func(direct bool, method string, params map[string]any) {
		calls = append(calls, call{method, params, direct})
	}
	for _, p := range []string{"SECRET.GO", "Secret.go", "SECRETS/key.go", "secrets/KEY.GO", "SRC/PRIVATE.GO", "src/Private.go",
		"alias.go", "ALIAS.GO", "abs-upper.go", "DLINK/KEY.GO", "HIDDEN-LINK"} {
		m, a := tool("get_file_content", map[string]any{"path": p, "maskSecrets": false})
		add(true, m, a)
	}
	for _, p := range []string{"SECRET.GO", "SRC/PRIVATE.GO"} {
		m, a := tool("get_files_arklite", map[string]any{"paths": []string{p}})
		add(true, m, a)
		m, a = tool("get_file_info", map[string]any{"path": p})
		add(true, m, a)
		m, a = tool("get_symbols", map[string]any{"path": p})
		add(true, m, a)
		add(true, "resources/read", map[string]any{"uri": "file://" + p})
	}
	m, a := tool("get_symbol", map[string]any{"path": "SECRET.GO", "name": "SecretFn"})
	add(true, m, a)
	m, a = tool("list_files", map[string]any{"path": "SECRETS"})
	add(true, m, a)
	// Walks from another spelling of a directory, and from the root through
	// links whose targets are spelled in another case.
	for _, dir := range []string{"SRC", "."} {
		for name, args := range map[string]map[string]any{
			"search_in_files":    {"path": dir, "query": "MARKER"},
			"list_files":         {"path": dir},
			"get_directory_tree": {"path": dir},
			"get_files_arklite":  {"paths": []string{dir}},
			"find_symbol":        {"path": dir, "pattern": "."},
			"search_code":        {"path": dir, "format": "json"},
			"get_repository_map": {"path": dir, "format": "json", "detail": "verbose"},
			"get_project_stats":  {"path": dir},
		} {
			m, a := tool(name, args)
			add(false, m, a)
		}
		add(false, "resources/read", map[string]any{"uri": "directory://" + dir})
	}
	for _, flags := range [][]string{nil, {"--allow-external-symlinks", "on"}} {
		proj := caseFixture(t)
		c := startServer(t, proj, t.TempDir(), bin, append([]string{"mcp-server", "--root", "./", "--no-cache"}, flags...)...)
		c.handshake()
		for _, call := range calls {
			line := rawCall(t, c, call.method, call.params)
			if found := markersIn(line, caseMarkers); len(found) > 0 {
				t.Errorf("%v: %s %v reveals %q:\n%s", flags, call.method, call.params, found, line)
			}
			if call.direct && !strings.Contains(line, "does not exist") {
				t.Errorf("%v: %s %v: not refused as nonexistent:\n%s", flags, call.method, call.params, line)
			}
			if !call.direct {
				for _, name := range []string{"private.go", "key.go", "SecretFn", "\"Private\"", "\"Key\""} {
					if strings.Contains(line, name) {
						t.Errorf("%v: %s %v lists the excluded %s:\n%s", flags, call.method, call.params, name, line)
					}
				}
			}
		}
		// Files the rules do not name stay readable in any spelling.
		for p, want := range map[string]string{"PUBLIC.GO": "PUBLIC_MARKER", "src/OPEN.GO": "OPEN_MARKER", "public.go": "PUBLIC_MARKER"} {
			if line := rawCall(t, c, "tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": p}}); !strings.Contains(line, want) {
				t.Errorf("%v: %s not readable:\n%s", flags, p, line)
			}
		}
	}
}

// The dump of the repository excludes the same files, through the same
// links, with and without .gitignore handling.
func TestCLICase_DumpExcludes(t *testing.T) {
	bin := buildArk(t)
	proj := caseFixture(t)
	for _, flags := range [][]string{nil, {"-a", "off"}} {
		out := filepath.Join(t.TempDir(), "dump.txt")
		cmd := exec.Command(bin, append(append([]string{"-S", "-o", out}, flags...), proj)...)
		cmd.Dir = t.TempDir()
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("dump %v: %v\n%s", flags, err, b)
		}
		dump, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if m := markersIn(string(dump), caseMarkers); len(m) > 0 {
			t.Errorf("dump %v contains excluded content %q", flags, m)
		}
		if !strings.Contains(string(dump), "PUBLIC_MARKER") || !strings.Contains(string(dump), "OPEN_MARKER") {
			t.Errorf("dump %v lost visible files", flags)
		}
	}
}
