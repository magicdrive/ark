package safewalk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/accesspolicy"
	"github.com/magicdrive/ark/internal/fsroot"
)

type ws struct{ dir string }

func newWS(t *testing.T) ws {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink("x", filepath.Join(dir, "probe")); err != nil {
		if os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
			t.Fatalf("ARK_REQUIRE_SYMLINKS: %v", err)
		}
		t.Skipf("SKIP: symlinks unavailable on %s: %v", runtime.GOOS, err)
	}
	os.Remove(filepath.Join(dir, "probe"))
	return ws{dir}
}

func (w ws) path(rel string) string { return filepath.Join(w.dir, filepath.FromSlash(rel)) }

func (w ws) write(t *testing.T, rel, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(w.path(rel)), 0o755)
	if err := os.WriteFile(w.path(rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (w ws) link(t *testing.T, target, rel string) {
	t.Helper()
	if err := os.Symlink(target, w.path(rel)); err != nil {
		t.Fatal(err)
	}
}

// swapLink puts a symlink to target at rel, in place of what is there.
func (w ws) swapLink(t *testing.T, target, rel string) {
	t.Helper()
	tmp := w.path(rel) + ".swap"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		t.Fatal(err)
	}
	mutate(t, "swap "+rel, os.Rename(tmp, w.path(rel)))
}

func mutate(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		t.Skipf("SKIP: %s: %v (attack not possible here)", what, err)
	}
	t.Fatalf("%s: %v", what, err)
}

// repo: a repository with an excluded file, an excluded directory, nested
// rules, symlinks to excluded files and outside, and a file outside.
func repo(t *testing.T) ws {
	w := newWS(t)
	w.write(t, "repo/.arkignore", "secret.txt\nvault/\n")
	w.write(t, "repo/secret.txt", "ROOT_SECRET")
	w.write(t, "repo/vault/key.txt", "VAULT_SECRET")
	w.write(t, "repo/pub.txt", "PUBLIC")
	w.write(t, "repo/d/.arkignore", "x.txt\n")
	w.write(t, "repo/d/x.txt", "D_SECRET")
	w.write(t, "repo/d/ok.txt", "D_OK")
	w.write(t, "outside/o.txt", "OUTSIDE_SECRET")
	w.link(t, "secret.txt", "repo/alias")
	w.link(t, w.path("outside/o.txt"), "repo/ext")
	w.link(t, "pub.txt", "repo/publink")
	return w
}

func walker(t *testing.T, root string, external, scoped bool) *Walker {
	t.Helper()
	tr, err := fsroot.Pin(root, fsroot.Options{AllowExternalSymlinks: external})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })
	build := accesspolicy.Build
	if scoped {
		build = accesspolicy.BuildScoped
	}
	s, err := build(tr, accesspolicy.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return New(s, root)
}

// walkAll returns what a walk reports, as root-relative paths, and the
// content of each regular file or symlink reported, read through the walker.
func walkAll(t *testing.T, w *Walker, root string) (paths []string, contents map[string]string) {
	t.Helper()
	contents = map[string]string{}
	err := w.Walk(root, func(p string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		paths = append(paths, filepath.ToSlash(rel))
		if !info.IsDir() {
			if b, err := w.ReadFile(p); err == nil {
				contents[filepath.ToSlash(rel)] = string(b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths, contents
}

func leaked(contents map[string]string) []string {
	var out []string
	for p, c := range contents {
		if strings.Contains(c, "SECRET") {
			out = append(out, p+"="+c)
		}
	}
	return out
}

// A walk reports only what the policy admits — names included — and reads
// the files it reports; symlinks lead where the policy allows.
func TestWalkReportsOnlyAdmitted(t *testing.T) {
	w := repo(t)
	root := w.path("repo")
	for _, scoped := range []bool{false, true} {
		for _, external := range []bool{false, true} {
			paths, contents := walkAll(t, walker(t, root, external, scoped), root)
			got := strings.Join(paths, " ")
			want := ". .arkignore d d/.arkignore d/ok.txt pub.txt publink"
			if external {
				want = ". .arkignore d d/.arkignore d/ok.txt ext pub.txt publink"
			}
			if got != want {
				t.Errorf("scoped %v external %v: reported %s, want %s", scoped, external, got, want)
			}
			if l := leaked(contents); external && len(l) != 1 || !external && len(l) != 0 {
				// With external symlinks allowed, ext reads the outside
				// file: the operator's choice.
				t.Errorf("scoped %v external %v: read %v", scoped, external, l)
			}
			if contents["publink"] != "PUBLIC" || contents["d/ok.txt"] != "D_OK" {
				t.Errorf("scoped %v external %v: contents %v", scoped, external, contents)
			}
		}
	}
}

// Where nothing is excluded, a walk reports what filepath.Walk reports, in
// its order, with the same descriptions; ReadDir lists what os.ReadDir
// lists.
func TestWalkMatchesFilepathWalk(t *testing.T) {
	w := newWS(t)
	for _, f := range []string{"b.txt", "a/z.go", "a/b/c.txt", "A.txt", "-x/y", ".hidden"} {
		w.write(t, "repo/"+f, f)
	}
	w.link(t, "a", "repo/dirlink")
	w.link(t, "b.txt", "repo/filelink")
	w.link(t, "missing", "repo/dangling")
	w.link(t, "loop2", "repo/loop1")
	w.link(t, "loop1", "repo/loop2")
	root := w.path("repo")
	describe := func(walk func(string, filepath.WalkFunc) error) []string {
		var out []string
		walk(root, func(p string, info fs.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			size := info.Size()
			if info.IsDir() {
				size = 0
			}
			out = append(out, p+" "+info.Mode().Type().String()+" "+info.Name()+" "+string(rune('0'+size%10)))
			return nil
		})
		return out
	}
	sw := walker(t, root, false, true)
	for _, start := range []string{".", "a", "dangling", "loop1", "filelink", "dirlink", "dirlink/z.go", "dirlink/b"} {
		from := filepath.Join(root, start)
		walkFrom := func(walk func(string, filepath.WalkFunc) error) func(string, filepath.WalkFunc) error {
			return func(_ string, fn filepath.WalkFunc) error { return walk(from, fn) }
		}
		if got, want := describe(walkFrom(sw.Walk)), describe(walkFrom(filepath.Walk)); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("walk from %s:\n%s\nfilepath.Walk:\n%s", start, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	for _, dir := range []string{root, filepath.Join(root, "a")} {
		got, err1 := sw.ReadDir(dir)
		want, err2 := os.ReadDir(dir)
		if err1 != nil || err2 != nil || len(got) != len(want) {
			t.Fatalf("ReadDir(%s): %v %v %d %d", dir, err1, err2, len(got), len(want))
		}
		for i := range got {
			if got[i].Name() != want[i].Name() || got[i].Type() != want[i].Type() {
				t.Errorf("ReadDir(%s)[%d] = %s %v, want %s %v", dir, i, got[i].Name(), got[i].Type(), want[i].Name(), want[i].Type())
			}
		}
	}
}

// C3-2: an entry the walk listed as a file is replaced by a symlink (to an
// excluded file, to a file outside) before it is decided, or after it was
// reported and before it is read: the other file is never read.
func TestC3_2_EntryReplaced(t *testing.T) {
	for _, event := range []string{"decide", "read"} {
		for _, target := range []string{"excluded", "outside"} {
			w := repo(t)
			root := w.path("repo")
			link := "secret.txt"
			if target == "outside" {
				link = w.path("outside/o.txt")
			}
			restore := SetHook(func(ev, rel string) {
				if ev == event && rel == "pub.txt" {
					w.swapLink(t, link, "repo/pub.txt")
				}
			})
			_, contents := walkAll(t, walker(t, root, false, true), root)
			restore()
			if l := leaked(contents); len(l) > 0 {
				t.Errorf("%s, link to %s: read %v", event, target, l)
			}
		}
	}
}

// C3-3: the directory whose .arkignore excludes a file is swapped for a
// decoy without rules after the walk opened it, before its rules are
// captured: the walk's directory is not the one the rules were read from;
// the excluded file is neither reported nor read.
func TestC3_3_NestedRulesSwappedDuringWalk(t *testing.T) {
	w := repo(t)
	w.write(t, "decoy/x.txt", "DECOY")
	root := w.path("repo")
	swapped := false
	restore := SetHook(func(ev, rel string) {
		if ev == "decide" && strings.HasPrefix(rel, "d/") && !swapped {
			swapped = true
			mutate(t, "move d out", os.Rename(w.path("repo/d"), w.path("d-real")))
			mutate(t, "move decoy in", os.Rename(w.path("decoy"), w.path("repo/d")))
		}
	})
	defer restore()
	paths, contents := walkAll(t, walker(t, root, false, true), root)
	if !swapped {
		t.Fatal("the swap did not happen")
	}
	for _, p := range paths {
		if p == "d/x.txt" {
			t.Errorf("reported d/x.txt (%q)", contents[p])
		}
	}
	if l := leaked(contents); len(l) > 0 {
		t.Errorf("read %v", l)
	}
}

// C3-9: special files are reported as entries but never opened for reading:
// a read refuses them at once (a FIFO would block an open forever).
func TestC3_9_SpecialFilesNotOpened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SKIP: no FIFOs on Windows")
	}
	w := repo(t)
	if err := mkfifo(w.path("repo/pipe.txt")); err != nil {
		t.Skipf("SKIP: mkfifo: %v", err)
	}
	root := w.path("repo")
	done := make(chan error, 1)
	go func() {
		sw := walker(t, root, false, true)
		var readErr error
		sw.Walk(root, func(p string, info fs.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(p, "pipe.txt") {
				_, readErr = sw.ReadFile(p)
			}
			return nil
		})
		_, err := sw.ReadFile(filepath.Join(root, "pipe.txt"))
		done <- errors.Join(readErr, err)
	}()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, fsroot.ErrNotRegular) {
			t.Errorf("pipe read: %v (want ErrNotRegular)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
}

// Paths in the caller's spelling: relative roots, the start below the root,
// and a start given in another case on a case-insensitive file system report
// names as listed (C1).
func TestWalkSpelling(t *testing.T) {
	w := repo(t)
	root := w.path("repo")
	sw := walker(t, root, false, true)
	var got []string
	sw.Walk(filepath.Join(root, "d"), func(p string, info fs.FileInfo, err error) error {
		got = append(got, p)
		return nil
	})
	sort.Strings(got)
	want := []string{filepath.Join(root, "d"), filepath.Join(root, "d/.arkignore"), filepath.Join(root, "d/ok.txt")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("walk of d: %v", got)
	}
	if _, err := sw.ReadFile(filepath.Join(root, "D/X.TXT")); err == nil {
		t.Error("read d/x.txt spelled in another case")
	}
}

// C3-2 (listings): a directory a listing reported is swapped for another
// before it is listed itself: the listing reaches it by the identities it
// was reported with, and refuses the other directory.
func TestC3_2_ListedDirectorySwapped(t *testing.T) {
	w := repo(t)
	w.write(t, "decoy/DECOY_NAME.txt", "DECOY")
	root := w.path("repo")
	sw := walker(t, root, false, true)
	if _, err := sw.ReadDir(root); err != nil {
		t.Fatal(err)
	}
	mutate(t, "move d out", os.Rename(w.path("repo/d"), w.path("d-real")))
	mutate(t, "move decoy in", os.Rename(w.path("decoy"), w.path("repo/d")))
	entries, err := sw.ReadDir(filepath.Join(root, "d"))
	for _, e := range entries {
		if strings.Contains(e.Name(), "DECOY") {
			t.Errorf("listed the swapped-in directory: %s", e.Name())
		}
	}
	if err == nil {
		t.Errorf("listing a swapped directory succeeded: %v", entries)
	}
}
