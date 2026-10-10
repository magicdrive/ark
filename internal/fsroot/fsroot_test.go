package fsroot

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Without concurrent changes a Tree decides as Ark's path gate does today:
// the gate's 14 cases of the design study, external links both ways, link
// chains, and invalid paths.
func TestReadFile_DecidesAsThePathGate(t *testing.T) {
	f := newFixture(t)
	allow := excludedBy("excluded.txt")
	for _, tc := range []struct {
		rel       string
		off, on   string // outcome with external symlinks off / on
		structure string // what is exercised
	}{
		{"s.txt", "A_PUBLIC", "A_PUBLIC", "regular file"},
		{"sub/f.txt", "A_SUB", "A_SUB", "nested directory"},
		{"rel-internal", "A_PUBLIC", "A_PUBLIC", "relative internal link"},
		{"abs-internal", "A_PUBLIC", "A_PUBLIC", "absolute internal link"},
		{"dirlink/f.txt", "A_SUB", "A_SUB", "path below an internal directory link"},
		{"hop1", "A_PUBLIC", "A_PUBLIC", "three-link chain"},
		{"abs-external", "outside", "OUTSIDE_SECRET", "absolute external link"},
		{"rel-external", "outside", "OUTSIDE_SECRET", "relative external link"},
		{"ext-dir/secret.txt", "outside", "OUTSIDE_SECRET", "path below an external directory link"},
		{"excluded.txt", "denied", "denied", "excluded file"},
		{"alias-excluded", "denied", "denied", "relative link to an excluded file"},
		{"abs-alias-excluded", "denied", "denied", "absolute link to an excluded file"},
		{"dangling", "missing", "missing", "dangling link"},
		{"loop1", "loop", "loop", "link loop"},
		{"sub/../s.txt", "A_PUBLIC", "A_PUBLIC", "lexical .."},
		{"../B/s.txt", "outside", "outside", "../ out of the root"},
		{"sub", "not-regular", "not-regular", "a directory read as a file"},
		{"", "invalid", "invalid", "empty path"},
		{f.path("outside/secret.txt"), "invalid", "invalid", "absolute path"},
		{"/etc/hosts", "invalid", "invalid", "absolute path"},
	} {
		for _, allowExt := range []bool{false, true} {
			want := tc.off
			if allowExt {
				want = tc.on
			}
			tr := pin(t, f.path("root"), Options{AllowExternalSymlinks: allowExt})
			if got := outcome(tr.ReadFile(tc.rel, allow)); got != want {
				t.Errorf("external %v, %s (%s): %s, want %s", allowExt, tc.rel, tc.structure, got, want)
			}
		}
	}
}

// Resolve reports what a policy needs: the path asked for, the real path,
// and the links followed.
func TestResolve_ReportsPathsAndLinks(t *testing.T) {
	f := newFixture(t)
	tr := pin(t, f.path("root"), Options{AllowExternalSymlinks: true})
	r, err := tr.Resolve("hop1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Logical() != "hop1" || r.Real() != "s.txt" || strings.Join(r.Links(), ",") != "hop1,hop2,hop3" || !r.IsRegular() {
		t.Errorf("hop1: logical %q real %q links %v", r.Logical(), r.Real(), r.Links())
	}
	r, err = tr.Resolve("dirlink/f.txt")
	if err != nil || r.Real() != "sub/f.txt" || strings.Join(r.Links(), ",") != "dirlink" {
		t.Errorf("dirlink/f.txt: real %q links %v err %v", r.Real(), r.Links(), err)
	}
	r, err = tr.Resolve("abs-external")
	if err != nil || r.Real() != "" || r.External() != f.path("outside/secret.txt") {
		t.Errorf("abs-external: real %q external %q err %v", r.Real(), r.External(), err)
	}
	if r, err := tr.Resolve("."); err != nil || !r.IsDir() || r.Real() != "." {
		t.Errorf("root: %v %v", r, err)
	}
	if got := tr.Logical("sub/f.txt"); got != filepath.Join(f.path("root"), "sub", "f.txt") {
		t.Errorf("Logical keeps the root as given: %s", got)
	}
}

// A Resolved belongs to the Tree that made it.
func TestOpen_RefusesForeignResolved(t *testing.T) {
	f := newFixture(t)
	a := pin(t, f.path("root"), Options{})
	b := pin(t, f.path("B"), Options{})
	r, err := a.Resolve("s.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(r); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("another Tree opened a Resolved: %v", err)
	}
	if _, err := a.Open(Resolved{}); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("a zero Resolved opened: %v", err)
	}
}

// The root is pinned however it is named, relative included.
func TestPin_RootSpellings(t *testing.T) {
	f := newFixture(t)
	t.Chdir(f.ws)
	for _, root := range []string{f.path("A"), f.path("root"), "A", "root", "./root/"} {
		tr := pin(t, root, Options{})
		if got := outcome(tr.ReadFile("s.txt", nil)); got != "A_PUBLIC" {
			t.Errorf("root %q: %s", root, got)
		}
	}
	for _, root := range []string{"", f.path("missing"), f.path("A/s.txt")} {
		if tr, err := Pin(root, Options{}); err == nil {
			tr.Close()
			t.Errorf("Pin(%q) succeeded", root)
		}
	}
	f.link(t, "nowhere", "broken-root")
	f.link(t, "loop-root2", "loop-root1")
	f.link(t, "loop-root1", "loop-root2")
	for _, root := range []string{"broken-root", "loop-root1"} {
		if tr, err := Pin(root, Options{}); err == nil {
			tr.Close()
			t.Errorf("Pin(%q) succeeded", root)
		}
	}
}

// A walk lists in filepath.WalkDir order, reports symlinks without entering
// them, and refuses to start through one.
func TestWalk_OrderAndSymlinks(t *testing.T) {
	f := newFixture(t)
	tr := pin(t, f.path("root"), Options{})
	var got []string
	err := tr.Walk(".", func(e Entry) error {
		if e.Err() != nil {
			t.Errorf("%s: %v", e.Rel(), e.Err())
		}
		got = append(got, e.Rel())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	filepath.WalkDir(f.path("A"), func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(f.path("A"), p)
		want = append(want, filepath.ToSlash(rel))
		return nil
	})
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("walk order:\n got %v\nwant %v", got, want)
	}
	for _, p := range got {
		if strings.HasPrefix(p, "dirlink/") || strings.HasPrefix(p, "ext-dir/") {
			t.Errorf("walk entered a directory symlink: %s", p)
		}
	}
	if err := tr.Walk("dirlink", func(Entry) error { return nil }); !errors.Is(err, ErrSymlink) {
		t.Errorf("walk started through a symlink: %v", err)
	}
	// SkipDir on a directory, and an entry's file read from its directory.
	var read []string
	tr.Walk(".", func(e Entry) error {
		if e.IsDir() && e.Rel() == "sub" {
			return SkipDir
		}
		if e.Rel() == "s.txt" {
			b, id, err := e.ReadAll()
			if err != nil || string(b) != "A_PUBLIC" || id.IsZero() {
				t.Errorf("ReadAll: %q %v", b, err)
			}
		}
		if e.IsSymlink() {
			if _, err := e.Open(); !errors.Is(err, ErrSymlink) {
				t.Errorf("%s: a symlink entry opened as a file: %v", e.Rel(), err)
			}
		}
		read = append(read, e.Rel())
		return nil
	})
	for _, p := range read {
		if strings.HasPrefix(p, "sub/") {
			t.Errorf("SkipDir entered sub: %s", p)
		}
	}
	// An entry is not usable after its callback.
	var kept Entry
	tr.Walk(".", func(e Entry) error {
		if e.Rel() == "s.txt" {
			kept = e
		}
		return nil
	})
	if _, err := kept.Open(); !errors.Is(err, ErrClosed) {
		t.Errorf("an entry was usable after its walk: %v", err)
	}
}

// A walk can start below the root and reports directory identities.
func TestWalk_StartAndDirIdentity(t *testing.T) {
	f := newFixture(t)
	tr := pin(t, f.path("root"), Options{})
	id, err := tr.DirIdentity("sub")
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	tr.Walk("sub", func(e Entry) error {
		rels = append(rels, e.Rel())
		if !e.DirIdentity().Same(id) {
			t.Errorf("%s: directory identity differs", e.Rel())
		}
		return nil
	})
	if strings.Join(rels, " ") != "sub sub/f.txt" {
		t.Errorf("walk of sub: %v", rels)
	}
	if _, err := tr.DirIdentity("dirlink"); !errors.Is(err, ErrSymlink) {
		t.Errorf("DirIdentity through a symlink: %v", err)
	}
}

// After Close every operation fails with ErrClosed.
func TestClose(t *testing.T) {
	f := newFixture(t)
	tr, err := Pin(f.path("root"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := tr.Resolve("s.txt")
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); !errors.Is(err, ErrClosed) {
		t.Errorf("second Close: %v", err)
	}
	if _, err := tr.Resolve("s.txt"); !errors.Is(err, ErrClosed) {
		t.Errorf("Resolve: %v", err)
	}
	if _, err := tr.Open(r); !errors.Is(err, ErrClosed) {
		t.Errorf("Open: %v", err)
	}
	if _, err := tr.ReadFile("s.txt", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("ReadFile: %v", err)
	}
	if err := tr.Walk(".", func(Entry) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Errorf("Walk: %v", err)
	}
	if _, err := tr.DirIdentity("."); !errors.Is(err, ErrClosed) {
		t.Errorf("DirIdentity: %v", err)
	}
}

// Special files are not read.
func TestReadFile_RefusesNonRegular(t *testing.T) {
	f := newFixture(t)
	if err := mkfifo(f.path("A/pipe")); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	tr := pin(t, f.path("root"), Options{})
	if got := outcome(tr.ReadFile("pipe", nil)); got != "not-regular" {
		t.Errorf("FIFO: %s", got)
	}
	f.link(t, "pipe", "A/pipelink")
	if got := outcome(tr.ReadFile("pipelink", nil)); got != "not-regular" {
		t.Errorf("link to a FIFO: %s", got)
	}
	_ = os.Remove
}
