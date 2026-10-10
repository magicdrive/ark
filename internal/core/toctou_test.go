package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/safewalk"
)

// C3-6: the dump decides and reads its files through a pinned tree under a
// snapshot of its rules (attachFileAccess). A file listed by the tree is
// replaced — by a symlink to an ignored file, to a file outside — while the
// dump runs: the other file's content is never written. Every format.
func TestC3_6_DumpEntryReplaced(t *testing.T) {
	if err := os.Symlink("x", filepath.Join(t.TempDir(), "probe")); err != nil {
		t.Skipf("SKIP: symlinks unavailable on %s: %v", runtime.GOOS, err)
	}
	for _, format := range []string{"txt", "md", "xml", "arklite"} {
		for _, event := range []string{"decide", "read"} {
			for _, target := range []string{"ignored", "outside"} {
				ws := t.TempDir()
				repo := filepath.Join(ws, "repo")
				write := func(p, body string) {
					os.MkdirAll(filepath.Dir(p), 0o755)
					if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				write(filepath.Join(repo, ".arkignore"), "secret.txt\n")
				write(filepath.Join(repo, ".gitignore"), "gitsecret.txt\n")
				write(filepath.Join(repo, "secret.txt"), "DUMP_SECRET_ARK")
				write(filepath.Join(repo, "gitsecret.txt"), "DUMP_SECRET_GIT")
				write(filepath.Join(repo, "x.txt"), "PUBLIC_X")
				write(filepath.Join(ws, "outside/o.txt"), "DUMP_SECRET_OUTSIDE")
				out := filepath.Join(ws, "dump.out")
				_, opt, err := commandline.GeneralOptParse([]string{"-S", "-o", out, "-f", map[string]string{"txt": "txt", "md": "md", "xml": "xml", "arklite": "arklite"}[format], repo})
				if err != nil {
					t.Fatal(err)
				}
				for _, link := range []string{"secret.txt", "gitsecret.txt", filepath.Join(ws, "outside/o.txt")} {
					if target == "outside" && !strings.Contains(link, "outside") || target == "ignored" && strings.Contains(link, "outside") {
						continue
					}
					var once sync.Once
					fired := false
					restore := safewalk.SetHook(func(ev, rel string) {
						if ev == event && rel == "x.txt" {
							once.Do(func() {
								fired = true
								tmp := filepath.Join(repo, "x.swap")
								os.Remove(tmp)
								if err := os.Symlink(link, tmp); err != nil {
									t.Fatal(err)
								}
								if err := os.Rename(tmp, filepath.Join(repo, "x.txt")); err != nil {
									t.Fatal(err)
								}
							})
						}
					})
					os.Remove(out)
					err := createDumpFile(opt)
					restore()
					dump, _ := os.ReadFile(out)
					if !fired || !strings.Contains(string(dump), ".arkignore") {
						t.Errorf("%s %s link to %s: vacuous (fired %v, err %v):\n%s", format, event, link, fired, err, dump)
					}
					if strings.Contains(string(dump), "DUMP_SECRET") {
						t.Errorf("%s %s link to %s: the dump contains another file (err %v):\n%s", format, event, link, err, dump)
					}
					os.Remove(filepath.Join(repo, "x.txt")) // the symlink swapped in
					write(filepath.Join(repo, "x.txt"), "PUBLIC_X")
				}
			}
		}
	}
}
