package accesspolicy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/libgitignore"
)

type ws struct{ dir string }

func newWS(t testing.TB) ws {
	t.Helper()
	requireSymlinks(t)
	return ws{dir: t.TempDir()}
}

func (w ws) path(rel string) string { return filepath.Join(w.dir, filepath.FromSlash(rel)) }

func (w ws) write(t testing.TB, rel, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(w.path(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.path(rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (w ws) link(t testing.TB, target, rel string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(w.path(rel)), 0o755)
	if err := os.Symlink(target, w.path(rel)); err != nil {
		t.Fatal(err)
	}
}

func pinTree(t testing.TB, root string, opts fsroot.Options) *fsroot.Tree {
	t.Helper()
	tr, err := fsroot.Pin(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr
}

func snapshotOf(t testing.TB, tr *fsroot.Tree) *Snapshot {
	t.Helper()
	s, err := Build(tr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// policyRepo is a repository exercising the rule semantics: root and nested
// rule files, negations, anchored and directory patterns, "**", a
// subdirectory listed before its parent's rule file, both rule kinds in one
// directory, a symlinked rule file, and links of every kind.
func policyRepo(t testing.TB) (w ws, root string, paths []string) {
	w = newWS(t)
	files := map[string]string{
		"repo/.arkignore":            "*.log\n!keep.log\nbuild/\n/secret.txt\ndeep/**/z.txt\n",
		"repo/.gitignore":            "*.tmp\n",
		"repo/a/.arkignore":          "x.go\n!y.go\n/local\n",
		"repo/a/-early/.arkignore":   "!e.log\n",
		"repo/a/b/.arkignore":        "*\n!*.go\n",
		"repo/both/.gitignore":       "g.txt\n",
		"repo/both/.arkignore":       "a.txt\n!g.txt\n",
		"repo/rules/shared":          "linked.txt\n",
		"repo/secret.txt":            "S",
		"repo/keep.log":              "K",
		"repo/a.log":                 "L",
		"repo/a/x.go":                "X",
		"repo/a/y.go":                "Y",
		"repo/a/local":               "L",
		"repo/a/-early/e.log":        "E",
		"repo/a/-early/f.log":        "F",
		"repo/a/b/c.txt":             "C",
		"repo/a/b/d.go":              "D",
		"repo/build/out":             "O",
		"repo/deep/x/y/z.txt":        "Z",
		"repo/deep/z.txt":            "Z",
		"repo/both/a.txt":            "A",
		"repo/both/g.txt":            "G",
		"repo/both/n.txt":            "N",
		"repo/linkrules/linked.txt":  "LL",
		"repo/linkrules/other.txt":   "LO",
		"repo/plain.txt":             "P",
		"repo/.git/config":           "GITCONFIG",
		"outside/ext.txt":            "EXT",
		"repo/excludeddir/inner.txt": "I",
	}
	files["repo/.arkignore"] += "excludeddir/\n"
	for p, b := range files {
		w.write(t, p, b)
	}
	w.link(t, "../rules/shared", "repo/linkrules/.arkignore")
	w.link(t, "secret.txt", "repo/alias-secret")
	w.link(t, w.path("repo/secret.txt"), "repo/abs-alias-secret")
	w.link(t, "plain.txt", "repo/alias-plain")
	w.link(t, "a/b", "repo/dirlink")
	w.link(t, "excludeddir", "repo/alias-dir")
	w.link(t, w.path("outside/ext.txt"), "repo/ext-link")
	w.link(t, "alias-secret", "repo/hop")
	root = w.path("repo")
	paths = []string{
		"plain.txt", "secret.txt", "keep.log", "a.log", "a/x.go", "a/y.go", "a/local", "a/-early/e.log",
		"a/-early/f.log", "a/b/c.txt", "a/b/d.go", "build/out", "deep/x/y/z.txt", "deep/z.txt", "both/a.txt",
		"both/g.txt", "both/n.txt", "linkrules/linked.txt", "linkrules/other.txt", "alias-secret",
		"abs-alias-secret", "alias-plain", "dirlink/d.go", "dirlink/c.txt", "alias-dir/inner.txt",
		"excludeddir/inner.txt", "hop", ".git/config", "a", "a/b", "build", "excludeddir",
	}
	return w, root, paths
}

// oldDecision is today's MCP policy: IgnoreReader.All's .arkignore rule,
// applied to the path asked for and to its symlink-resolved form, with
// parents (mcp/access_policy.go excludesRel, tools_syntax.go realRel).
func oldDecision(t *testing.T, root, rel string) bool {
	files, err := libgitignore.ReadIgnoreFiles(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := files.CompileSource(libgitignore.ArkSource)
	if err != nil {
		t.Fatal(err)
	}
	excluded := func(rel string) bool {
		for i := 0; i < len(rel); i++ {
			if rel[i] == '/' && rule.MatchesRel(rel[:i]) {
				return true
			}
		}
		return rule.MatchesRel(rel)
	}
	if excluded(rel) {
		return true
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	if real, err := filepath.EvalSymlinks(filepath.Join(root, rel)); err == nil {
		if r, err := filepath.Rel(realRoot, real); err == nil && !strings.HasPrefix(r, "..") && r != "." {
			return excluded(filepath.ToSlash(r))
		}
	}
	return false
}

// The snapshot decides as today's policy on every path of the fixture, and
// compiles the same rules (fingerprint and both rule sets).
func TestDifferential_DecisionsAndRules(t *testing.T) {
	_, root, paths := policyRepo(t)
	tr := pinTree(t, root, fsroot.Options{AllowExternalSymlinks: true})
	s := snapshotOf(t, tr)
	for _, p := range paths {
		r, err := tr.Resolve(p)
		if err != nil {
			t.Errorf("%s: resolve: %v", p, err)
			continue
		}
		err = s.Check(r)
		if err != nil && !errors.Is(err, ErrExcluded) {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if got, want := errors.Is(err, ErrExcluded), oldDecision(t, root, p); got != want {
			t.Errorf("%s: excluded %v, today %v", p, got, want)
		}
	}
	old, err := libgitignore.ReadIgnoreFiles(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Fingerprint() != old.Fingerprint() {
		t.Error("fingerprint differs from IgnoreReader.All's")
	}
	for _, git := range []bool{false, true} {
		want, err1 := old.CompileRuleSet(git)
		got, err2 := s.RuleSet(git)
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		if !reflect.DeepEqual(patterns(got), patterns(want)) {
			t.Errorf("gitignore %v: rule sets differ:\n got %v\nwant %v", git, patterns(got), patterns(want))
		}
	}
}

func patterns(rs *libgitignore.RuleSet) []string {
	var out []string
	for _, gi := range []*libgitignore.GitIgnore{rs.Ark, rs.Git} {
		if gi == nil {
			out = append(out, "-")
			continue
		}
		for _, p := range gi.Patterns() {
			out = append(out, p.Dir+"|"+p.Raw+"|"+p.Regexp.String())
		}
	}
	return out
}

// The same through a symlinked root: decisions and rules do not depend on
// how the root is named.
func TestDifferential_SymlinkedRoot(t *testing.T) {
	w, root, paths := policyRepo(t)
	w.link(t, root, "root-link")
	a := snapshotOf(t, pinTree(t, root, fsroot.Options{}))
	bt := pinTree(t, w.path("root-link"), fsroot.Options{})
	b := snapshotOf(t, bt)
	at := a.Tree()
	for _, p := range paths {
		ra, errA := at.Resolve(p)
		rb, errB := bt.Resolve(p)
		if (errA == nil) != (errB == nil) {
			t.Errorf("%s: resolve differs: %v / %v", p, errA, errB)
			continue
		}
		if errA != nil {
			continue
		}
		if ea, eb := a.Check(ra), b.Check(rb); errors.Is(ea, ErrExcluded) != errors.Is(eb, ErrExcluded) || (ea == nil) != (eb == nil) {
			t.Errorf("%s: %v through the directory, %v through the link", p, ea, eb)
		}
	}
}

// Paths the gate refuses before any rule — outside the root — are refused
// by the tree as before; .gitignore never changes the access policy.
func TestDifferential_GitignoreDoesNotDecideAccess(t *testing.T) {
	w, root, _ := policyRepo(t)
	w.write(t, "repo/only-git.tmp", "T")
	tr := pinTree(t, root, fsroot.Options{})
	s := snapshotOf(t, tr)
	r, err := tr.Resolve("only-git.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Check(r); err != nil {
		t.Errorf(".gitignore decided access: %v", err)
	}
	if _, err := tr.Resolve("ext-link"); !errors.Is(err, fsroot.ErrOutsideRoot) {
		t.Errorf("external link with external symlinks off: %v", err)
	}
}
