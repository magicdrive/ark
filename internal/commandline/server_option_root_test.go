package commandline

import (
	"os"
	"path/filepath"
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
