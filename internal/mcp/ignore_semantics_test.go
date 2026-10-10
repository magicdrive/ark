package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// ignoreFixture is a repository exercising the shared ignore semantics: root
// and nested .arkignore and .gitignore files, both kinds in one directory,
// anchored, directory and negated patterns, and negations across sources.
func ignoreFixture(t *testing.T) (root string, paths []string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "repo")
	files := map[string]string{
		".arkignore":          "ark_root.txt\n/sub/ark_anchored.txt\nsecret.txt\nbuild/\n",
		".gitignore":          "git_root.txt\n*.log\n",
		"sub/.arkignore":      "*.tmp\n!keep.tmp\n",
		"sub/.gitignore":      "!keep.log\n",
		"both/.gitignore":     "g.txt\n",
		"both/.arkignore":     "a.txt\n!g.txt\n",
		"neg/.gitignore":      "!secret.txt\n",
		"deep/x/y/.arkignore": "**/z.txt\n",
		"ark_root.txt":        "", "git_root.txt": "", "keep.txt": "", "secret.txt": "", "a.log": "",
		"sub/ark_anchored.txt": "", "sub/a.tmp": "", "sub/keep.tmp": "", "sub/keep.log": "", "sub/plain.txt": "",
		"both/g.txt": "", "both/a.txt": "", "both/n.txt": "", "neg/secret.txt": "",
		"build/out.txt": "", "x/build/out.txt": "", "deep/x/y/w/z.txt": "", "deep/z.txt": "",
	}
	for p, body := range files {
		write(t, root, p, body)
		if !strings.HasSuffix(p, "ignore") {
			paths = append(paths, p)
		}
	}
	return root, append(paths, "sub", "build", "x/build", "deep/x/y/w")
}

// The CLI dump and the MCP server read the same rules for the same root,
// wherever the command is run from: path by path, the CLI's rule decides as
// the MCP file tools' rule does, and the .arkignore part of both decides as
// the MCP access policy does.
func TestIgnoreSemantics_CLIAndMCPAgree(t *testing.T) {
	root, paths := ignoreFixture(t)
	for _, allow := range []string{"on", "off"} {
		h := NewToolsHandlerWithCache(root, nil, nil)
		mcpRules := h.forRequest().ignoreRule(allow == "on")
		policy := h.forRequest().accessPolicy()
		for _, cwd := range []string{root, filepath.Join(root, "sub"), filepath.Dir(root), t.TempDir()} {
			t.Chdir(cwd)
			target, _ := filepath.Rel(cwd, root)
			for _, arg := range []string{root, target} {
				_, opt, err := commandline.GeneralOptParse([]string{"-a", allow, arg})
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range paths {
					abs := filepath.Join(root, filepath.FromSlash(p))
					cli := opt.GitIgnoreRule.MatchesPath(abs)
					if mcp := mcpRules.MatchesRel(p); cli != mcp {
						t.Errorf("-a %s, cwd %s, target %q: %s ignored by CLI %v, by MCP file tools %v", allow, cwd, arg, p, cli, mcp)
					}
					if ark, pol := opt.GitIgnoreRule.Ark.MatchesPath(abs), policy.rule.MatchesRel(p); ark != pol {
						t.Errorf("-a %s, cwd %s: %s .arkignore rule %v, MCP access policy %v", allow, cwd, p, ark, pol)
					}
				}
			}
		}
	}
}

// The expected decisions of the shared semantics.
func TestIgnoreSemantics_Decisions(t *testing.T) {
	root, _ := ignoreFixture(t)
	h := NewToolsHandlerWithCache(root, nil, nil)
	both, arkOnly := h.forRequest().ignoreRule(true), h.forRequest().ignoreRule(false)
	for _, tc := range []struct {
		path          string
		both, arkOnly bool // ignored with .gitignore handling on / off
		why           string
	}{
		{"keep.txt", false, false, "no rule"},
		{"ark_root.txt", true, true, "root .arkignore, although the root has a .gitignore"},
		{"git_root.txt", true, false, ".gitignore applies only with handling on"},
		{"sub/ark_anchored.txt", true, true, "anchored pattern in the root .arkignore"},
		{"sub/a.tmp", true, true, "nested .arkignore"},
		{"sub/keep.tmp", false, false, "negation within the same source"},
		{"sub/keep.log", false, false, "a .gitignore negation re-includes within .gitignore"},
		{"a.log", true, false, "root .gitignore"},
		{"both/g.txt", true, false, "a .arkignore negation never re-includes what .gitignore excludes"},
		{"both/a.txt", true, true, ".arkignore in a directory that also has a .gitignore"},
		{"neg/secret.txt", true, true, "a .gitignore negation never re-includes what .arkignore excludes"},
		{"build/out.txt", true, true, "directory pattern"},
		{"x/build/out.txt", true, true, "unanchored directory pattern at depth"},
		{"deep/x/y/w/z.txt", true, true, "** in a nested file"},
		{"deep/z.txt", false, false, "a nested file's pattern does not reach above it"},
	} {
		if got := both.MatchesRel(tc.path); got != tc.both {
			t.Errorf("%s with .gitignore on: ignored %v, want %v (%s)", tc.path, got, tc.both, tc.why)
		}
		if got := arkOnly.MatchesRel(tc.path); got != tc.arkOnly {
			t.Errorf("%s with .gitignore off: ignored %v, want %v (%s)", tc.path, got, tc.arkOnly, tc.why)
		}
	}
}

// Rules belong to the target: ignore files above it, or in the working
// directory, never apply.
func TestIgnoreSemantics_RulesBelongToTheTarget(t *testing.T) {
	root, _ := ignoreFixture(t)
	outside := t.TempDir()
	write(t, outside, ".arkignore", "keep.txt\n")
	t.Chdir(outside)
	_, opt, err := commandline.GeneralOptParse([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if opt.GitIgnoreRule.MatchesPath(filepath.Join(root, "keep.txt")) {
		t.Error("the working directory's .arkignore applied to the target")
	}
	_, sub, err := commandline.GeneralOptParse([]string{filepath.Join(root, "sub")})
	if err != nil {
		t.Fatal(err)
	}
	if sub.GitIgnoreRule.MatchesPath(filepath.Join(root, "sub", "ark_anchored.txt")) {
		t.Error("the parent's .arkignore applied to a subdirectory target")
	}
	if !sub.GitIgnoreRule.MatchesPath(filepath.Join(root, "sub", "a.tmp")) {
		t.Error("the target's own .arkignore did not apply")
	}
	// A path outside the target is never decided by its rules, however it is
	// spelled.
	write(t, root, "elsewhere/a.tmp", "")
	for _, p := range []string{filepath.Join(root, "elsewhere", "a.tmp"), filepath.Join(root, "sub", "..", "elsewhere", "a.tmp"), filepath.Join(outside, "a.tmp")} {
		if sub.GitIgnoreRule.MatchesPath(p) {
			t.Errorf("rules of %s/sub matched %s", root, p)
		}
	}
}

// An ignore file the dump cannot read stops it instead of letting what the
// file excludes through.
func TestIgnoreSemantics_UnreadableRulesStopTheDump(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root, _ := ignoreFixture(t)
	p := filepath.Join(root, "sub", ".arkignore")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p, 0o644)
	if _, _, err := commandline.GeneralOptParse([]string{root}); err == nil || !strings.Contains(err.Error(), "ignore rules") {
		t.Errorf("unreadable .arkignore: %v", err)
	}
}
