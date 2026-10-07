package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// File-selection correctness of list_files / search_in_files / get_project_stats
// / get_directory_tree: request options take effect, ignore rules belong to the
// served repository whatever the process working directory, and repository
// metadata (.git, Ark's .ark cache) is never treated as content.

const knownText = "showsSsoButton"

// writeTree creates files (path → content) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// newFileToolsRepo builds a small Laravel-shaped repository inside an outer
// directory whose own .gitignore ignores the repository. A server started from
// the outer directory must still see the repository's files.
func newFileToolsRepo(t *testing.T) (outer, root string) {
	t.Helper()
	outer = t.TempDir()
	root = filepath.Join(outer, "repo")
	writeTree(t, outer, map[string]string{
		".gitignore": "repo/\n",
	})
	writeTree(t, root, map[string]string{
		".gitignore":                              "ignored/\n",
		"app/Models/InsuranceFoo.php":             "<?php class InsuranceFoo {}\n",
		"app/Models/InsuranceBar.php":             "<?php class InsuranceBar {}\n",
		"app/Models/Clinic.php":                   "<?php class Clinic {}\n",
		"app/Models/notes.txt":                    "Insurance notes\n",
		"app/Services/Auth/LoginScreenPolicy.php": "<?php\nclass LoginScreenPolicy {\n  public function " + knownText + "() {}\n}\n",
		"routes/web.php":                          "<?php\nRoute::get('/', 'X@" + knownText + "');\n",
		"skip/Skipped.php":                        "<?php // " + knownText + "\n",
		"vendor/acme/V.php":                       "<?php // " + knownText + "\n",
		"ignored/Ignored.php":                     "<?php // " + knownText + "\n",
		".ark/index/0a.json":                      `{"Symbols":"` + knownText + `"}`,
		".git/config":                             knownText + "\n",
	})
	return outer, root
}

// serverHandler builds the handler exactly as `ark mcp-server --root root`
// does, from process working directory cwd.
func serverHandler(t *testing.T, cwd, root string) *ToolsHandler {
	t.Helper()
	t.Chdir(cwd)
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	return NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
}

func callText(t *testing.T, h *ToolsHandler, tool string, args map[string]interface{}) (string, bool) {
	t.Helper()
	res, err := h.CallTool(tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res.Content[0].Text, res.IsError
}

func lines(s string) []string {
	if s == "" {
		return []string{}
	}
	out := strings.Split(s, "\n")
	sort.Strings(out)
	return out
}

// searchFiles returns the distinct files of search_in_files output lines.
func searchFiles(s string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range lines(s) {
		f := l[:strings.Index(l, ":")]
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func TestFileTools_ListFiles(t *testing.T) {
	outer, root := newFileToolsRepo(t)
	cases := []struct {
		name    string
		args    map[string]interface{}
		want    []string
		wantErr bool
	}{
		{name: "routes", args: map[string]interface{}{"path": "routes"}, want: []string{"web.php"}},
		{name: "includeExt without dot", args: map[string]interface{}{"path": "routes", "includeExt": "php"}, want: []string{"web.php"}},
		{name: "includeExt with dot", args: map[string]interface{}{"path": "routes", "includeExt": ".php"}, want: []string{"web.php"}},
		{name: "includeExt other", args: map[string]interface{}{"path": "app/Models", "includeExt": "txt"}, want: []string{"notes.txt"}},
		{name: "patternRegex", args: map[string]interface{}{"path": "app/Models", "patternRegex": "Insurance"},
			want: []string{"InsuranceBar.php", "InsuranceFoo.php"}},
		{name: "includeExt + patternRegex", args: map[string]interface{}{"path": "app/Models", "patternRegex": "Insurance", "includeExt": "php"},
			want: []string{"InsuranceBar.php", "InsuranceFoo.php"}},
		{name: "invalid regex", args: map[string]interface{}{"path": "app/Models", "patternRegex": "Insurance("}, wantErr: true},
		{name: "excludeDir", args: map[string]interface{}{"path": "app", "excludeDir": "Models"},
			want: []string{"Services/Auth/LoginScreenPolicy.php"}},
		{name: "root skips .git, .ark and gitignored, keeps vendor", args: map[string]interface{}{"path": ".", "includeExt": "php,json"},
			want: []string{
				"app/Models/Clinic.php", "app/Models/InsuranceBar.php", "app/Models/InsuranceFoo.php",
				"app/Services/Auth/LoginScreenPolicy.php", "routes/web.php", "skip/Skipped.php", "vendor/acme/V.php",
			}},
		{name: "allowGitignore false", args: map[string]interface{}{"path": "ignored", "allowGitignore": false}, want: []string{"Ignored.php"}},
		{name: "explicit .ark path is honoured", args: map[string]interface{}{"path": ".ark/index"}, want: []string{"0a.json"}},
	}
	for _, cwd := range []struct{ name, dir string }{{"cwd==root", root}, {"cwd!=root", outer}} {
		h := serverHandler(t, cwd.dir, root)
		for _, tc := range cases {
			t.Run(cwd.name+"/"+tc.name, func(t *testing.T) {
				text, isErr := callText(t, h, "list_files", tc.args)
				if isErr != tc.wantErr {
					t.Fatalf("isError = %v, want %v (%q)", isErr, tc.wantErr, text)
				}
				if tc.wantErr {
					return
				}
				if got := lines(text); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			})
		}
	}
}

func TestFileTools_SearchInFiles(t *testing.T) {
	outer, root := newFileToolsRepo(t)
	cases := []struct {
		name    string
		args    map[string]interface{}
		want    []string
		wantErr bool
	}{
		{name: "known hit", args: map[string]interface{}{"path": "app/Services/Auth", "query": knownText},
			want: []string{"LoginScreenPolicy.php"}},
		{name: "known hit includeExt", args: map[string]interface{}{"path": "app", "query": knownText, "includeExt": "php"},
			want: []string{"Services/Auth/LoginScreenPolicy.php"}},
		{name: "no hit", args: map[string]interface{}{"path": "app", "query": "noSuchTextAnywhere"}, want: []string{}},
		{name: "root excludes .ark, .git and gitignored", args: map[string]interface{}{"path": ".", "query": knownText},
			want: []string{"app/Services/Auth/LoginScreenPolicy.php", "routes/web.php", "skip/Skipped.php", "vendor/acme/V.php"}},
		{name: "excludeDir", args: map[string]interface{}{"path": ".", "query": knownText, "excludeDir": "skip,vendor"},
			want: []string{"app/Services/Auth/LoginScreenPolicy.php", "routes/web.php"}},
		{name: "regex", args: map[string]interface{}{"path": "routes", "query": "Route::(get|post)", "isRegex": true},
			want: []string{"web.php"}},
		{name: "invalid regex query", args: map[string]interface{}{"path": "routes", "query": "(", "isRegex": true}, wantErr: true},
		{name: "invalid option regex", args: map[string]interface{}{"path": "routes", "query": "x", "excludeDirRegex": "["}, wantErr: true},
	}
	for _, cwd := range []struct{ name, dir string }{{"cwd==root", root}, {"cwd!=root", outer}} {
		h := serverHandler(t, cwd.dir, root)
		for _, tc := range cases {
			t.Run(cwd.name+"/"+tc.name, func(t *testing.T) {
				text, isErr := callText(t, h, "search_in_files", tc.args)
				if isErr != tc.wantErr {
					t.Fatalf("isError = %v, want %v (%q)", isErr, tc.wantErr, text)
				}
				if tc.wantErr {
					return
				}
				if got := searchFiles(text); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			})
		}
	}
}

// The same repository yields the same file discovery whatever directory the
// server process was started from.
func TestFileTools_DiscoveryIndependentOfCwd(t *testing.T) {
	outer, root := newFileToolsRepo(t)
	calls := []struct {
		tool string
		args map[string]interface{}
	}{
		{"list_files", map[string]interface{}{"path": "."}},
		{"search_in_files", map[string]interface{}{"path": ".", "query": knownText}},
		{"get_project_stats", map[string]interface{}{"path": "."}},
		{"get_directory_tree", map[string]interface{}{"path": "."}},
	}
	for _, c := range calls {
		inRoot, _ := callText(t, serverHandler(t, root, root), c.tool, c.args)
		fromOuter, _ := callText(t, serverHandler(t, outer, root), c.tool, c.args)
		if inRoot != fromOuter {
			t.Errorf("%s differs by cwd:\n cwd==root: %s\n cwd!=root: %s", c.tool, inRoot, fromOuter)
		}
		if inRoot == "" {
			t.Errorf("%s: empty result", c.tool)
		}
	}
}

func TestFileTools_TreeAndStatsSkipMetadata(t *testing.T) {
	_, root := newFileToolsRepo(t)
	h := serverHandler(t, root, root)

	tree, _ := callText(t, h, "get_directory_tree", map[string]interface{}{"path": "."})
	for _, name := range []string{`".ark"`, `".git"`, `"ignored"`} {
		if strings.Contains(tree, name) {
			t.Errorf("directory tree contains %s: %s", name, tree)
		}
	}
	if !strings.Contains(tree, `"vendor"`) {
		t.Errorf("directory tree lost vendor: %s", tree)
	}

	text, _ := callText(t, h, "get_project_stats", map[string]interface{}{"path": "."})
	var stats struct {
		ExtensionStats map[string]int `json:"extensionStats"`
	}
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatal(err)
	}
	if n := stats.ExtensionStats[".json"]; n != 0 {
		t.Errorf("stats counted %d .json file(s) from .ark", n)
	}
	if n := stats.ExtensionStats[".php"]; n != 7 {
		t.Errorf("stats counted %d .php files, want 7", n)
	}
}

// A handler given an option whose ignore rule was built for another directory
// (here: the process working directory) builds its own rule for the served
// repository instead of using it.
func TestFileTools_HandlerRebuildsForeignIgnoreRule(t *testing.T) {
	outer, root := newFileToolsRepo(t)
	t.Chdir(outer)
	_, opt, err := commandline.GeneralOptParse([]string{"."})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(root, opt)
	for _, allow := range []bool{true, false} {
		text, isErr := callText(t, h, "list_files", map[string]interface{}{"path": "routes", "allowGitignore": allow})
		if isErr || text != "web.php" {
			t.Errorf("allowGitignore=%v: got %q (isError=%v), want web.php", allow, text, isErr)
		}
	}
}
