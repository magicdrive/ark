package fsroot

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fixture is a workspace:
//
//	A/                 the repository (root, through the symlink "root")
//	  s.txt  sub/f.txt  excluded.txt  .arkignore ("excluded.txt")
//	  rel-internal -> s.txt          abs-internal -> <ws>/A/s.txt
//	  alias-excluded -> excluded.txt abs-alias-excluded -> <ws>/A/excluded.txt
//	  rel-external -> ../outside/secret.txt   abs-external -> <ws>/outside/secret.txt
//	  ext-dir -> <ws>/outside        dirlink -> sub
//	  hop1 -> hop2 -> hop3 -> s.txt  dangling -> missing   loop1 <-> loop2
//	B/s.txt                          another repository
//	outside/secret.txt               outside every root
//	root -> A
type fixture struct{ ws string }

func newFixture(t testing.TB) fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture: needs symlink privileges; Windows not covered here")
	}
	ws := t.TempDir()
	f := fixture{ws: ws}
	f.write(t, "A/s.txt", "A_PUBLIC")
	f.write(t, "A/sub/f.txt", "A_SUB")
	f.write(t, "A/excluded.txt", "A_EXCLUDED")
	f.write(t, "A/.arkignore", "excluded.txt\n")
	f.write(t, "B/s.txt", "B_SECRET")
	f.write(t, "outside/secret.txt", "OUTSIDE_SECRET")
	for link, target := range map[string]string{
		"A/rel-internal":       "s.txt",
		"A/abs-internal":       filepath.Join(ws, "A/s.txt"),
		"A/alias-excluded":     "excluded.txt",
		"A/abs-alias-excluded": filepath.Join(ws, "A/excluded.txt"),
		"A/rel-external":       "../outside/secret.txt",
		"A/abs-external":       filepath.Join(ws, "outside/secret.txt"),
		"A/ext-dir":            filepath.Join(ws, "outside"),
		"A/dirlink":            "sub",
		"A/hop1":               "hop2",
		"A/hop2":               "hop3",
		"A/hop3":               "s.txt",
		"A/dangling":           "missing",
		"A/loop1":              "loop2",
		"A/loop2":              "loop1",
		"root":                 "A",
	} {
		f.link(t, target, link)
	}
	return f
}

func (f fixture) path(rel string) string { return filepath.Join(f.ws, filepath.FromSlash(rel)) }

func (f fixture) write(t testing.TB, rel, body string) {
	t.Helper()
	p := f.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) link(t testing.TB, target, rel string) {
	t.Helper()
	if err := os.Symlink(target, f.path(rel)); err != nil {
		t.Fatal(err)
	}
}

// swapLink atomically points link at target.
func swapLink(target, link string) {
	tmp := link + ".swap"
	os.Remove(tmp)
	os.Symlink(target, tmp)
	os.Rename(tmp, link)
}

// excludedBy returns the allow function of a rule set of exact paths and
// directory prefixes ("dir/"), applied — as Ark's path gate applies
// .arkignore — to the path asked for and the path it resolves to, with
// their parent directories.
func excludedBy(patterns ...string) func(Resolved) bool {
	match := func(rel string) bool {
		parts := strings.Split(rel, "/")
		for i := range parts {
			p := strings.Join(parts[:i+1], "/")
			for _, pat := range patterns {
				if p == strings.TrimSuffix(pat, "/") {
					return true
				}
			}
		}
		return false
	}
	return func(r Resolved) bool {
		return !match(r.Logical()) && (r.Real() == "" || !match(r.Real()))
	}
}

func pin(t testing.TB, root string, opts Options) *Tree {
	t.Helper()
	tr, err := Pin(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr
}

// outcome classifies a read for tables: the content, or the error kind.
func outcome(b []byte, err error) string {
	switch {
	case err == nil:
		return string(b)
	case errors.Is(err, ErrDenied):
		return "denied"
	case errors.Is(err, ErrOutsideRoot):
		return "outside"
	case errors.Is(err, ErrSymlinkLoop):
		return "loop"
	case errors.Is(err, ErrInvalidPath):
		return "invalid"
	case errors.Is(err, fs.ErrNotExist):
		return "missing"
	case errors.Is(err, ErrNotRegular):
		return "not-regular"
	case errors.Is(err, ErrChanged):
		return "changed"
	}
	return "error: " + err.Error()
}
