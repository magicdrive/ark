package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/safewalk"
)

// Phase 3-C: walks (listings, searches, trees, statistics, symbol and
// reference search), the index and what index-based tools return. The tree
// is changed where a concurrent writer aims (safewalk.SetHook): after a walk
// listed an entry and before it is decided, after it was reported and
// before it is read — including the reads of an index build.

// onWalk runs fn once, the first time event happens for rel; the result
// reports whether it ran.
func onWalk(t *testing.T, event, rel string, fn func()) *bool {
	t.Helper()
	var once sync.Once
	fired := new(bool)
	restore := safewalk.SetHook(func(ev, r string) {
		if ev == event && r == rel {
			once.Do(func() { *fired = true; fn() })
		}
	})
	t.Cleanup(restore)
	return fired
}

const walkSecret = "package p\n\n// WALK_SECRET_MARKER\nfunc SecretFn() string { return \"WALK_SECRET_MARKER\" }\n"

// walkRepo: a repository with a public file x.go, an excluded secret.go and
// a secret outside.
func walkRepo(t *testing.T) (ws, repo string) {
	t.Helper()
	ws = t.TempDir()
	repo = filepath.Join(ws, "repo")
	put(t, filepath.Join(repo, "go.mod"), "module example.com/p\n\ngo 1.22\n")
	put(t, filepath.Join(repo, ".arkignore"), "secret.go\n")
	put(t, filepath.Join(repo, "secret.go"), walkSecret)
	put(t, filepath.Join(repo, "x.go"), "package p\n\n// Public is public.\nfunc Public() string { return \"PUBLIC\" }\n")
	put(t, filepath.Join(ws, "outside/o.go"), walkSecret)
	return ws, repo
}

var walkCalls = []struct {
	name string
	args map[string]interface{}
}{
	{"search_in_files", map[string]interface{}{"path": ".", "query": "MARKER"}},
	{"search_in_files", map[string]interface{}{"path": ".", "query": "func"}},
	{"list_files", map[string]interface{}{"path": "."}},
	{"get_project_stats", map[string]interface{}{"path": "."}},
	{"find_symbol", map[string]interface{}{"pattern": "."}},
	{"find_references", map[string]interface{}{"path": ".", "name": "SecretFn"}},
	{"get_directory_tree", map[string]interface{}{"path": "."}},
	{"get_files_arklite", map[string]interface{}{"paths": []interface{}{"x.go"}}},
}

func callOut(t *testing.T, h *ToolsHandler, name string, args map[string]interface{}) string {
	t.Helper()
	res, err := h.CallTool(name, args)
	if err != nil {
		return "error: " + err.Error()
	}
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// walkLeaked reports whether a tool's output shows the secret file: its
// marker (content, snippets), or what a tool derives from it — its symbol in
// a listing of symbols, a reference found in it. A tool that echoes the
// name it was asked for ("SecretFn" not found) shows nothing.
func walkLeaked(tool, out string) bool {
	switch {
	case strings.Contains(out, "WALK_SECRET"):
		return true
	case tool == "find_references":
		return strings.Contains(out, "\"file\"")
	case tool == "find_symbol", tool == "search_code", tool == "get_repository_map", tool == "get_diagnostics":
		return strings.Contains(out, "SecretFn")
	case tool == "get_callers", tool == "get_context", tool == "search_context":
		return strings.Contains(out, "SecretFn") && !strings.Contains(out, "not found") && !strings.Contains(out, "no context") && !strings.Contains(out, "\"results\": []") && !strings.Contains(out, "\"results\":[]")
	}
	return false
}

// C3-1 / C3-2: the file a walk lists is replaced by a symlink — outside the
// root, or to the excluded file — before it is decided, or after it was
// reported and before it is read. No walk tool returns the other file.
func TestC3_1_WalkEntryReplaced(t *testing.T) {
	needSymlinks(t)
	for _, event := range []string{"decide", "read"} {
		for _, target := range []string{"outside", "excluded"} {
			for _, c := range walkCalls {
				ws, repo := walkRepo(t)
				link := filepath.Join(ws, "outside/o.go")
				if target == "excluded" {
					link = "secret.go"
				}
				h := handlerFor(t, repo)
				fired := onWalk(t, event, "x.go", func() { replaceWithLink(t, link, filepath.Join(repo, "x.go")) })
				if out := callOut(t, h, c.name, c.args); walkLeaked(c.name, out) {
					t.Errorf("%s, link to %s: %s %v returned the other file:\n%s", event, target, c.name, c.args, out)
				}
				if !*fired && c.name != "get_files_arklite" && (event == "decide" || c.name == "search_in_files" || c.name == "find_symbol") {
					t.Errorf("%s, link to %s: %s: the swap never happened", event, target, c.name)
				}
			}
		}
	}
}

// C3-4: a source the index build reads is replaced by a symlink to a file
// outside the root, or to the excluded file, while the build reads it: its
// content does not enter the index.
func TestC3_4_IndexIngestion(t *testing.T) {
	needSymlinks(t)
	for _, target := range []string{"outside", "excluded"} {
		ws, repo := walkRepo(t)
		link := filepath.Join(ws, "outside/o.go")
		if target == "excluded" {
			link = "secret.go"
		}
		h := handlerFor(t, repo)
		fired := onWalk(t, "read", "x.go", func() { replaceWithLink(t, link, filepath.Join(repo, "x.go")) })
		defer func() {
			if !*fired {
				t.Errorf("link to %s: the index build never read x.go", target)
			}
		}()
		for _, c := range []struct {
			name string
			args map[string]interface{}
		}{
			{"search_code", map[string]interface{}{"path": ".", "format": "json"}},
			{"get_repository_map", map[string]interface{}{"path": ".", "format": "json", "detail": "verbose"}},
			{"get_callers", map[string]interface{}{"path": ".", "symbol": "SecretFn"}},
			{"get_context", map[string]interface{}{"path": ".", "symbol": "SecretFn"}},
			{"search_context", map[string]interface{}{"query": "SecretFn", "contextLimit": 3}},
			{"get_diagnostics", map[string]interface{}{"path": "."}},
		} {
			if out := callOut(t, h, c.name, c.args); walkLeaked(c.name, out) {
				t.Errorf("link to %s: %s returned the other file's content:\n%.600s", target, c.name, out)
			}
		}
	}
}

// C3-5: an index built while a file was admitted is not served after a rule
// excludes it: the request's freshness check walks under the request's
// snapshot, so the file set differs and the index is rebuilt without it.
func TestC3_5_StaleIndexAfterRuleChange(t *testing.T) {
	_, repo := walkRepo(t)
	put(t, filepath.Join(repo, "late.go"), "package p\n\n// LateFn is excluded later.\nfunc LateFn() string { return \"LATE_MARKER\" }\n")
	h := handlerFor(t, repo)
	if out := callOut(t, h, "search_code", map[string]interface{}{"path": ".", "format": "json", "query": "LateFn"}); !strings.Contains(out, "LateFn") {
		t.Fatalf("before the rule: LateFn not indexed:\n%s", out)
	}
	put(t, filepath.Join(repo, ".arkignore"), "secret.go\nlate.go\n")
	for _, c := range []struct {
		name string
		args map[string]interface{}
	}{
		{"search_code", map[string]interface{}{"path": ".", "format": "json", "query": "LateFn"}},
		{"get_context", map[string]interface{}{"path": ".", "symbol": "LateFn"}},
		{"search_context", map[string]interface{}{"query": "LateFn", "contextLimit": 3}},
		{"get_repository_map", map[string]interface{}{"path": ".", "format": "json", "detail": "verbose"}},
	} {
		if out := callOut(t, h, c.name, c.args); strings.Contains(out, "LATE_MARKER") || strings.Contains(out, "late.go") {
			t.Errorf("after the rule: %s still returns late.go:\n%.600s", c.name, out)
		}
	}
}

// C3-9: a FIFO in the repository (named like a source file) is never opened
// for reading: walks, the index and statistics answer at once.
func TestC3_9_FIFOInWalks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SKIP: no FIFOs on Windows")
	}
	_, repo := walkRepo(t)
	if err := mkfifo(filepath.Join(repo, "pipe.go")); err != nil {
		t.Skipf("SKIP: mkfifo: %v", err)
	}
	h := handlerFor(t, repo)
	for _, c := range append(walkCalls[:], struct {
		name string
		args map[string]interface{}
	}{"search_code", map[string]interface{}{"path": ".", "format": "json"}}) {
		done := make(chan string, 1)
		go func() { done <- callOut(t, h, c.name, c.args) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s blocked on a FIFO", c.name)
		}
	}
}

// C3-10: concurrent walk and index requests on two servers with different
// rules: each sees its own root under its own rules, no data race, every
// request's access released.
func TestC3_10_ConcurrentWalksAndIndex(t *testing.T) {
	r1, r2 := t.TempDir(), t.TempDir()
	put(t, filepath.Join(r1, ".arkignore"), "s.go\n")
	put(t, filepath.Join(r1, "s.go"), "package p\n\n// R1_SECRET\nfunc S1() {}\n")
	put(t, filepath.Join(r1, "a.go"), "package p\n\n// R1_PUBLIC\nfunc A1() {}\n")
	put(t, filepath.Join(r2, "s.go"), "package p\n\n// R2_PUBLIC\nfunc S2() {}\n")
	h1, h2 := handlerFor(t, r1), handlerFor(t, r2)
	before := openFDs()
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				o1 := callOut(t, h1, "search_in_files", map[string]interface{}{"path": ".", "query": "R"})
				i1 := callOut(t, h1, "search_code", map[string]interface{}{"path": ".", "format": "json"})
				o2 := callOut(t, h2, "search_in_files", map[string]interface{}{"path": ".", "query": "R"})
				if strings.Contains(o1+i1, "R1_SECRET") || strings.Contains(i1, "S1") || strings.Contains(o1, "R2_") || !strings.Contains(o1, "R1_PUBLIC") {
					t.Errorf("root 1: %q %.200q", o1, i1)
				}
				if !strings.Contains(o2, "R2_PUBLIC") || strings.Contains(o2, "R1_") {
					t.Errorf("root 2: %q", o2)
				}
			}
		}()
	}
	wg.Wait()
	time.Sleep(200 * time.Millisecond) // detached index builds finish
	if after := openFDs(); before >= 0 && after > before+6 {
		t.Errorf("open descriptors: %d before, %d after", before, after)
	}
}

// C3-11: what walks return is masked as the operator set; a request cannot
// turn it off.
func TestC3_11_MaskingOnWalks(t *testing.T) {
	repo := t.TempDir()
	const secret = "hunter2hunter2hunter2"
	put(t, filepath.Join(repo, "cfg.go"), "package p\n\nfunc Conf() string {\n\tpassword := \""+secret+"\"\n\treturn password\n}\n")
	for _, flags := range [][]string{nil, {"--mask-secrets", "off"}} {
		h := handlerFor(t, repo, flags...)
		out := callOut(t, h, "search_in_files", map[string]interface{}{"path": ".", "query": "password", "maskSecrets": false})
		if strings.Contains(out, secret) == (flags == nil) {
			t.Errorf("masking %v: search_in_files:\n%s", flags == nil, out)
		}
	}
}

// C3-12: on one state of the repository, the direct reads, the walks, the
// index and the dump agree on what is readable.
func TestC3_12_ToolsAgree(t *testing.T) {
	needSymlinks(t)
	ws, repo := walkRepo(t)
	put(t, filepath.Join(repo, "d/.arkignore"), "hidden.go\n")
	put(t, filepath.Join(repo, "d/hidden.go"), "package d\n\nfunc Hidden() {}\n")
	put(t, filepath.Join(repo, "d/shown.go"), "package d\n\nfunc Shown() {}\n")
	if err := os.Symlink("secret.go", filepath.Join(repo, "alias.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "outside/o.go"), filepath.Join(repo, "ext.go")); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, repo)
	listed := lines(callOut(t, h, "list_files", map[string]interface{}{"path": "."}))
	for _, f := range listed {
		if res, _ := h.CallTool("get_file_content", map[string]interface{}{"path": f}); res.IsError {
			t.Errorf("list_files lists %s; get_file_content refuses it: %s", f, res.Content[0].Text)
		}
	}
	for _, f := range []string{"secret.go", "alias.go", "ext.go", "d/hidden.go"} {
		for _, l := range listed {
			if l == filepath.FromSlash(f) {
				t.Errorf("list_files lists %s", f)
			}
		}
		if res, _ := h.CallTool("get_file_content", map[string]interface{}{"path": f}); !res.IsError {
			t.Errorf("get_file_content reads %s", f)
		}
	}
	var sc struct {
		Matches []struct {
			File string `json:"file"`
		} `json:"matches"`
	}
	out := callOut(t, h, "search_code", map[string]interface{}{"path": ".", "format": "json"})
	if err := json.Unmarshal([]byte(out), &sc); err != nil || len(sc.Matches) == 0 {
		t.Fatalf("search_code: %v:\n%.300s", err, out)
	}
	indexed := map[string]bool{}
	for _, m := range sc.Matches {
		indexed[m.File] = true
	}
	for f := range indexed {
		found := false
		for _, l := range listed {
			found = found || filepath.ToSlash(l) == f
		}
		if !found {
			t.Errorf("the index has %s, which list_files does not list", f)
		}
	}
	for _, bad := range []string{"WALK_SECRET", "Hidden", "SecretFn"} {
		if strings.Contains(out, bad) {
			t.Errorf("search_code returns %s", bad)
		}
	}
	sort.Strings(listed)
	t.Logf("listed %v; indexed %v", listed, indexed)
}
