//go:build windows

package fsroot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// A directory junction (a reparse point that is not a symlink) is never
// entered: not by a walk, not on the way to a file.
func TestWindows_JunctionNotFollowed(t *testing.T) {
	f := newFixture(t)
	out, err := exec.Command("cmd", "/c", "mklink", "/J", f.path(`A\jdir`), f.path(`A\sub`)).CombinedOutput()
	if err != nil {
		t.Skipf("SKIP: cannot create a junction: %v %s", err, out)
	}
	tr := pin(t, f.path("root"), Options{})
	if b, err := tr.ReadFile("jdir/f.txt", nil); err == nil {
		t.Errorf("read through a junction: %q", b)
	}
	tr.Walk(".", func(e Entry) error {
		if strings.HasPrefix(e.Rel(), "jdir/") {
			t.Errorf("walk entered a junction: %s", e.Rel())
		}
		return nil
	})
}

// C1-10: Windows names are case-insensitive by default: another case
// spelling resolves to the same object and is named as listed.
func TestWindows_CaseInsensitiveNames(t *testing.T) {
	f := newFixture(t)
	tr := pin(t, f.path("root"), Options{})
	a, err1 := tr.Resolve("sub/f.txt")
	b, err2 := tr.Resolve("SUB/F.TXT")
	if err1 != nil || err2 != nil {
		t.Fatalf("%v %v", err1, err2)
	}
	if !a.Identity().Same(b.Identity()) {
		t.Error("two case spellings are different objects")
	}
	if b.Canonical() != "sub/f.txt" || b.Real() != "sub/f.txt" {
		t.Errorf("SUB/F.TXT: canonical %q real %q, want sub/f.txt", b.Canonical(), b.Real())
	}
	for _, ads := range []string{"sub/f.txt:stream", "sub/f.txt::$DATA", "SUB/F.TXT::$DATA", "excluded.txt:x:$DATA"} {
		if _, err := tr.Resolve(ads); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("alternate data stream %q accepted: %v", ads, err)
		}
	}
}

// C1-10: an 8.3 short name reaches the file under another name entirely: it
// resolves to the long name listed.
func TestWindows_ShortNames(t *testing.T) {
	f := newFixture(t)
	f.write(t, "A/longsecretname.txt", "LONG")
	long := f.path("A/longsecretname.txt")
	lp, _ := windows.UTF16PtrFromString(long)
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(lp, &buf[0], uint32(len(buf)))
	short := filepath.Base(windows.UTF16ToString(buf[:n]))
	if err != nil || n == 0 || strings.EqualFold(short, "longsecretname.txt") {
		t.Skipf("SKIP: 8.3 short names are disabled on this volume (%v)", err)
	}
	tr := pin(t, f.path("root"), Options{})
	r, err := tr.Resolve(short)
	if err != nil || r.Canonical() != "longsecretname.txt" {
		t.Errorf("Resolve(%s) = %q %v, want longsecretname.txt", short, r.Canonical(), err)
	}
}

// C1-10: a directory made case-sensitive (fsutil) holds names that differ
// only in case; each is its own file, resolved as spelled.
func TestWindows_CaseSensitiveDirectory(t *testing.T) {
	f := newFixture(t)
	dir := f.path("A/cs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("fsutil.exe", "file", "setCaseSensitiveInfo", dir, "enable").CombinedOutput(); err != nil {
		t.Skipf("SKIP: cannot make a directory case-sensitive: %v %s", err, out)
	}
	f.write(t, "A/cs/x.txt", "LOWER")
	if err := os.WriteFile(f.path("A/cs/X.TXT"), []byte("UPPER"), 0o644); err != nil {
		t.Skipf("SKIP: the directory did not become case-sensitive: %v", err)
	}
	tr := pin(t, f.path("root"), Options{})
	for rel, want := range map[string]string{"cs/x.txt": "LOWER", "CS/X.TXT": "UPPER"} {
		var canonical string
		b, err := tr.ReadFileChecked(rel, func(r Resolved) error { canonical = r.Canonical(); return nil })
		if err != nil || string(b) != want || !strings.HasSuffix(canonical, rel[3:]) {
			t.Errorf("%s: %q as %q %v; want %q", rel, b, canonical, err, want)
		}
	}
}
