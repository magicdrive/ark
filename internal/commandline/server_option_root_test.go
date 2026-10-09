package commandline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/libgitignore"
)

// The MCP server's ignore rules are built for the served repository (--root),
// never for the directory the process was started from.
func TestServerOptParse_IgnoreRuleRootedAtServedRepository(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(outer)
	_, opt, err := ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	rule := opt.GeneralOption.GitIgnoreRule
	if rule == nil {
		t.Fatal("no ignore rule built")
	}
	if want := libgitignore.ToAbsDir(root); rule.Root != want {
		t.Errorf("ignore rule rooted at %q, want %q", rule.Root, want)
	}
}

// --root is resolved once, at startup, against the launch CWD: the server
// holds an absolute, clean root whose meaning no later CWD change can alter.
func TestServerOptParse_RootIsNormalizedToAbsolute(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ cwd, arg string }{
		{root, "./"},
		{root, "."},
		{root, ""},
		{filepath.Join(root, "sub"), ".."},
		{filepath.Join(root, "sub"), "../"},
		{outer, "repo"},
		{outer, "./repo/"},
		{outer, root},
		{outer, root + string(filepath.Separator)},
		{outer, filepath.Join(root, "sub", "..")},
	}
	for _, c := range cases {
		t.Chdir(c.cwd)
		args := []string{"--root", c.arg}
		if c.arg == "" {
			args = nil // default: the launch CWD
		}
		_, opt, err := ServerOptParse("test", args)
		if err != nil {
			t.Fatalf("cwd=%s --root %q: %v", c.cwd, c.arg, err)
		}
		if opt.RootDir != root {
			t.Errorf("cwd=%s --root %q: RootDir = %q, want %q", c.cwd, c.arg, opt.RootDir, root)
		}
		if opt.GeneralOption.WorkingDir != root {
			t.Errorf("cwd=%s --root %q: WorkingDir = %q, want %q", c.cwd, c.arg, opt.GeneralOption.WorkingDir, root)
		}
	}
}

// A root that cannot be served is a startup error naming the resolved path,
// never a server that later reports every path as outside its root.
func TestServerOptParse_InvalidRootIsAnError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, c := range []struct{ arg, want string }{
		{"missing", "does not exist"},
		{file, "not a directory"},
		// An unexpanded client placeholder reaches us as a literal relative path.
		{"${CLAUDE_PROJECT_DIR:-.}/", "does not exist"},
	} {
		_, _, err := ServerOptParse("test", []string{"--root", c.arg})
		if err == nil {
			t.Errorf("--root %q: no error", c.arg)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("--root %q: error %q does not mention %q", c.arg, err, c.want)
		}
	}
}
