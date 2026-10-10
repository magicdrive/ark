package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// A symlink in the dumped tree to a file the ignore rules exclude does not
// expose it: with .gitignore handling on, a link to a .gitignore'd file is
// left out too; with it off, that file is dumped (and so is the link).
func TestDumpSymlinkToIgnored(t *testing.T) {
	ws := t.TempDir()
	repo := filepath.Join(ws, "repo")
	files := map[string]string{
		".arkignore": "arksecret.txt\n", ".gitignore": "gitsecret.txt\n",
		"arksecret.txt": "ARK_IGNORED_BODY", "gitsecret.txt": "GIT_IGNORED_BODY", "pub.txt": "PUBLIC_BODY",
	}
	for n, b := range files {
		os.MkdirAll(repo, 0o755)
		if err := os.WriteFile(filepath.Join(repo, n), []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{"to-ark.txt": "arksecret.txt", "to-git.txt": "gitsecret.txt"} {
		if err := os.Symlink(target, filepath.Join(repo, link)); err != nil {
			t.Skipf("SKIP: symlinks unavailable: %v", err)
		}
	}
	for _, allow := range []string{"on", "off"} {
		out := filepath.Join(ws, "dump-"+allow)
		_, opt, err := commandline.GeneralOptParse([]string{"-S", "-a", allow, "-o", out, repo})
		if err != nil {
			t.Fatal(err)
		}
		if err := createDumpFile(opt); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(out)
		dump := string(b)
		if strings.Contains(dump, "ARK_IGNORED_BODY") {
			t.Errorf("-a %s: the dump exposes the .arkignore'd file", allow)
		}
		if gitBody := strings.Contains(dump, "GIT_IGNORED_BODY"); gitBody != (allow == "off") {
			t.Errorf("-a %s: .gitignore'd file in the dump: %v", allow, gitBody)
		}
		if !strings.Contains(dump, "PUBLIC_BODY") {
			t.Errorf("-a %s: the public file is missing", allow)
		}
	}
}
