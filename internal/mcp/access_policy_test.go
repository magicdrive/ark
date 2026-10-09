package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// policyRepo is a repository with files at the given paths (content: the path).
func policyRepo(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		write(t, root, f, f+"\n")
	}
	return root
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ruleBuilds is how often the handler has parsed and compiled the rules.
func ruleBuilds(h *ToolsHandler) (builds, hits int) {
	s := h.shared()
	s.ignoreMu.Lock()
	defer s.ignoreMu.Unlock()
	return s.ignoreBuilds, s.ignoreHits
}

// excluded reports, as a new request sees it, whether rel is excluded.
func excluded(h *ToolsHandler, rel string) bool {
	return h.forRequest().accessPolicy().excludes(rel)
}

// Unchanged rule files are not parsed again; every change a request can see —
// an edit, a new file (also nested), a deletion, a move, a negation, an
// additional rule file — is seen by the next request and compiled once.
func TestAccessPolicyCache_ReuseAndInvalidation(t *testing.T) {
	root := policyRepo(t, "a/x.go", "a/b/y.go", "c/z.go", "keep.log", "drop.log")
	extra := filepath.Join(t.TempDir(), "extra.ignore")
	write(t, filepath.Dir(extra), filepath.Base(extra), "c/\n")
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root, "--additionally-ignorerule", extra})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	write(t, root, ".arkignore", "*.log\n")

	if !excluded(h, "drop.log") || !excluded(h, "c/z.go") || excluded(h, "a/x.go") {
		t.Fatal("initial rules not applied")
	}
	b0, _ := ruleBuilds(h)
	for i := 0; i < 5; i++ {
		excluded(h, "drop.log")
	}
	if b, hits := ruleBuilds(h); b != b0 || hits < 5 {
		t.Fatalf("unchanged rules rebuilt: builds %d → %d, hits %d", b0, b, hits)
	}

	steps := []struct {
		name   string
		change func()
		path   string
		want   bool
	}{
		{"edit", func() { write(t, root, ".arkignore", "*.log\n!keep.log\n") }, "keep.log", false},
		{"negation removed", func() { write(t, root, ".arkignore", "*.log\n") }, "keep.log", true},
		{"nested file added", func() { write(t, root, "a/b/.arkignore", "y.go\n") }, "a/b/y.go", true},
		{"nested file edited", func() { write(t, root, "a/b/.arkignore", "other.go\n") }, "a/b/y.go", false},
		{"nested file moved", func() {
			if err := os.Rename(filepath.Join(root, "a/b/.arkignore"), filepath.Join(root, "a/.arkignore")); err != nil {
				t.Fatal(err)
			}
			write(t, root, "a/.arkignore", "x.go\n")
		}, "a/x.go", true},
		{"file deleted", func() {
			if err := os.Remove(filepath.Join(root, "a/.arkignore")); err != nil {
				t.Fatal(err)
			}
		}, "a/x.go", false},
		{"additional rule file edited", func() { write(t, filepath.Dir(extra), filepath.Base(extra), "a/b/\n") }, "c/z.go", false},
	}
	for _, s := range steps {
		before, _ := ruleBuilds(h)
		s.change()
		if got := excluded(h, s.path); got != s.want {
			t.Errorf("%s: %s excluded = %v, want %v", s.name, s.path, got, s.want)
		}
		after, _ := ruleBuilds(h)
		if after != before+1 {
			t.Errorf("%s: %d rule builds, want 1", s.name, after-before)
		}
		excluded(h, s.path)
		if again, _ := ruleBuilds(h); again != after {
			t.Errorf("%s: unchanged rules rebuilt", s.name)
		}
	}
	if !excluded(h, "a/b/y.go") {
		t.Error("additional rule file change not applied")
	}
}

// Rules that cannot be read fail closed, even though a compiled rule for the
// same files exists: a walk (listing, search, index) refuses every path but
// the root; the path gate refuses each path the unreadable rules could apply
// to. Access returns when they can be read again.
func TestAccessPolicyCache_UnreadableRulesFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := policyRepo(t, "src/a.go", "sub/b.go")
	write(t, root, ".arkignore", "*.log\n")
	write(t, root, "sub/.arkignore", "*.log\n")
	h := NewToolsHandlerWithCache(root, nil, nil)
	if excluded(h, "src/a.go") {
		t.Fatal("readable rules exclude a.go")
	}
	for _, tc := range []struct {
		unreadable string
		mode       os.FileMode
		refused    []string // by the path gate
		admitted   []string // by the path gate: the unreadable rules cannot apply
	}{
		{".arkignore", 0o644, []string{"src/a.go", "sub/b.go"}, nil},
		{"sub/.arkignore", 0o644, []string{"sub/b.go"}, []string{"src/a.go"}},
		{"sub", 0o755, []string{"sub/b.go"}, []string{"src/a.go"}},
	} {
		p := filepath.Join(root, tc.unreadable)
		if err := os.Chmod(p, 0o000); err != nil {
			t.Fatal(err)
		}
		pol := h.forRequest().accessPolicy()
		if pol.err == nil || !pol.excludes("src/a.go") || pol.excludes(".") {
			t.Errorf("%s unreadable: walk policy err %v, a.go excluded %v", tc.unreadable, pol.err, pol.excludes("src/a.go"))
		}
		if line, _ := callText(t, h, "list_files", map[string]interface{}{"path": "."}); strings.Contains(line, "a.go") {
			t.Errorf("%s unreadable: list_files lists a.go: %s", tc.unreadable, line)
		}
		for _, rel := range tc.refused {
			if _, _, err := h.forRequest().resolveToolPath(rel); err == nil || !strings.Contains(err.Error(), "could not be read") {
				t.Errorf("%s unreadable: path gate admitted %s: %v", tc.unreadable, rel, err)
			}
		}
		for _, rel := range tc.admitted {
			if _, _, err := h.forRequest().resolveToolPath(rel); err != nil {
				t.Errorf("%s unreadable: path gate refused %s: %v", tc.unreadable, rel, err)
			}
		}
		if err := os.Chmod(p, tc.mode); err != nil {
			t.Fatal(err)
		}
		if excluded(h, "src/a.go") {
			t.Errorf("%s readable again: a.go still excluded", tc.unreadable)
		}
		if _, _, err := h.forRequest().resolveToolPath("sub/b.go"); err != nil {
			t.Errorf("%s readable again: sub/b.go refused: %v", tc.unreadable, err)
		}
	}
}

// One request sees one generation of the rules, however they change while it
// runs; the next request sees the change.
func TestAccessPolicyCache_RequestSnapshot(t *testing.T) {
	root := policyRepo(t, "a.go", "b.go")
	write(t, root, ".arkignore", "a.go\n")
	h := NewToolsHandlerWithCache(root, nil, nil)
	req := h.forRequest()
	if !req.accessPolicy().excludes("a.go") {
		t.Fatal("rule not applied")
	}
	write(t, root, ".arkignore", "b.go\n")
	if !req.accessPolicy().excludes("a.go") || req.accessPolicy().excludes("b.go") {
		t.Error("a request's policy changed while it ran")
	}
	if rule, _ := req.ignoreRuleErr(true); rule.MatchesRel("b.go") {
		t.Error("a request's .gitignore-enabled rule is of a newer generation than its policy")
	}
	if !excluded(h, "b.go") || excluded(h, "a.go") {
		t.Error("the next request does not see the change")
	}

	// The path gate and the walks of one request read the same version,
	// whichever reads first.
	req = h.forRequest()
	if _, _, err := req.resolveToolPath("a.go"); err != nil {
		t.Fatalf("gate refused a.go: %v", err)
	}
	write(t, root, ".arkignore", "a.go\n")
	if p := req.accessPolicy(); p.excludes("a.go") || !p.excludes("b.go") {
		t.Error("a request's walk policy is of a newer generation than its path gate")
	}
	if _, _, err := req.resolveToolPath("a.go"); err != nil {
		t.Error("a request's path gate changed while it ran")
	}
}

// Concurrent requests while the rules change: no data race (go test -race),
// and no request answers by two generations.
func TestAccessPolicyCache_ConcurrentRequests(t *testing.T) {
	root := policyRepo(t, "a.go", "b.go")
	write(t, root, ".arkignore", "a.go\n")
	h := NewToolsHandlerWithCache(root, nil, nil)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				p := h.forRequest().accessPolicy()
				a, b := p.excludes("a.go"), p.excludes("b.go")
				// A rule file read mid-write may be empty; it never
				// excludes both.
				if p.err == nil && a && b {
					t.Errorf("mixed generation: a.go and b.go excluded")
				}
				h.CallTool("list_files", map[string]interface{}{"path": "."})
			}
		}()
	}
	for i := 0; i < 20; i++ {
		write(t, root, ".arkignore", []string{"a.go\n", "b.go\n"}[i%2])
	}
	wg.Wait()
}
