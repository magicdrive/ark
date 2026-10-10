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

// C1: on a case-insensitive file system a path spelled in another case
// reaches the same file; every component a resolution passes is renamed to
// the name its directory lists (canon.go), so policies match real names.

// foldsCase reports whether dir's file system reaches "case-probe" by
// "CASE-PROBE".
func foldsCase(t testing.TB, dir string) bool {
	t.Helper()
	p := filepath.Join(dir, "case-probe")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	_, err := os.Lstat(filepath.Join(dir, "CASE-PROBE"))
	return err == nil
}

func requireFolding(t testing.TB, dir string) {
	t.Helper()
	if !foldsCase(t, dir) {
		if os.Getenv("ARK_REQUIRE_CASE_FOLDING") != "" {
			t.Fatalf("ARK_REQUIRE_CASE_FOLDING: the file system at %s is case-sensitive", dir)
		}
		t.Skipf("SKIP: the file system at %s is case-sensitive: a case-folding bypass is not possible here", dir)
	}
}

// caseSensitiveDir returns a directory on a case-sensitive file system:
// ARK_CASE_SENSITIVE_DIR, or the test's temporary directory if it is one.
func caseSensitiveDir(t testing.TB) string {
	t.Helper()
	if d := os.Getenv("ARK_CASE_SENSITIVE_DIR"); d != "" {
		dir, err := os.MkdirTemp(d, "cs")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		return dir
	}
	dir := t.TempDir()
	if foldsCase(t, dir) {
		t.Skipf("SKIP: no case-sensitive file system (set ARK_CASE_SENSITIVE_DIR)")
	}
	return dir
}

func TestC1_CanonicalNames(t *testing.T) {
	f := newFixture(t)
	requireFolding(t, f.ws)
	f.link(t, "EXCLUDED.TXT", "A/upper-link")
	f.link(t, "SUB", "A/upper-dirlink")
	f.link(t, strings.ToUpper(f.path("A"))+string(filepath.Separator)+"EXCLUDED.TXT", "A/abs-upper")
	f.link(t, "../a/EXCLUDED.TXT", "A/reenter-upper")
	for _, ext := range []bool{false, true} {
		tr := pin(t, f.path("root"), Options{AllowExternalSymlinks: ext})
		for _, c := range []struct{ in, canonical, real, links string }{
			{"s.txt", "s.txt", "s.txt", ""},
			{"S.TXT", "s.txt", "s.txt", ""},
			{"SUB", "sub", "sub", ""},
			{"SUB/F.TXT", "sub/f.txt", "sub/f.txt", ""},
			{"Sub/f.TXT", "sub/f.txt", "sub/f.txt", ""},
			{"DIRLINK/F.TXT", "dirlink/f.txt", "sub/f.txt", "dirlink"},
			{"REL-INTERNAL", "rel-internal", "s.txt", "rel-internal"},
			{"HOP1", "hop1", "s.txt", "hop1 hop2 hop3"},
			{"upper-link", "upper-link", "excluded.txt", "upper-link"},
			{"UPPER-DIRLINK/F.TXT", "upper-dirlink/f.txt", "sub/f.txt", "upper-dirlink"},
			{"abs-upper", "abs-upper", "excluded.txt", "abs-upper"},
			{"reenter-upper", "reenter-upper", "excluded.txt", "reenter-upper"},
		} {
			r, err := tr.Resolve(c.in)
			if err != nil {
				t.Errorf("external %v: Resolve(%s): %v", ext, c.in, err)
				continue
			}
			if r.Logical() != c.in || r.Canonical() != c.canonical || r.Real() != c.real || strings.Join(r.Links(), " ") != c.links {
				t.Errorf("external %v: Resolve(%s) = logical %s canonical %s real %s links %v; want canonical %s real %s links %s",
					ext, c.in, r.Logical(), r.Canonical(), r.Real(), r.Links(), c.canonical, c.real, c.links)
			}
		}
	}
	// A walk started from another spelling names entries as listed.
	tr := pin(t, f.path("root"), Options{})
	var rels []string
	tr.Walk("SUB", func(e Entry) error {
		rels = append(rels, e.Rel())
		return nil
	})
	if strings.Join(rels, " ") != "sub sub/f.txt" {
		t.Errorf("Walk(SUB) entries = %v", rels)
	}
}

// Behaviour that does not depend on case: a relative link that leaves the
// root and comes back ("../A/x" from the root A) resolves inside it, so the
// rules apply to its target; it was refused (and with external symlinks
// allowed, read as an outside file no rule applies to).
func TestReenteringLinkResolvesInside(t *testing.T) {
	f := newFixture(t)
	f.link(t, "../A/excluded.txt", "A/reenter")
	for _, ext := range []bool{false, true} {
		tr := pin(t, f.path("root"), Options{AllowExternalSymlinks: ext})
		r, err := tr.Resolve("reenter")
		if err != nil || r.Real() != "excluded.txt" || r.External() != "" {
			t.Errorf("external %v: real %q external %q err %v", ext, r.Real(), r.External(), err)
		}
	}
}

// C1-11: two entries are the object a misspelled name reaches (hard links)
// and neither is spelled as asked: refused, not guessed.
func TestC1_AmbiguousName(t *testing.T) {
	f := newFixture(t)
	requireFolding(t, f.ws)
	if err := os.Link(f.path("A/s.txt"), f.path("A/hard.txt")); err != nil {
		t.Skipf("SKIP: hard links unavailable: %v", err)
	}
	tr := pin(t, f.path("root"), Options{})
	if _, err := tr.Resolve("S.TXT"); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("Resolve(S.TXT) = %v, want ErrAmbiguous", err)
	}
	if _, err := tr.ReadFile("S.TXT", nil); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("ReadFile(S.TXT) = %v, want ErrAmbiguous", err)
	}
	for _, exact := range []string{"s.txt", "hard.txt"} {
		if r, err := tr.Resolve(exact); err != nil || r.Canonical() != exact {
			t.Errorf("Resolve(%s) = %q %v", exact, r.Canonical(), err)
		}
	}
}

// C1-6: on a case-sensitive file system names that differ in case are
// different files; each resolves to itself, spelled as asked, and nothing
// is listed to find out.
func TestC1_CaseSensitive(t *testing.T) {
	requireSymlinks(t)
	dir := caseSensitiveDir(t)
	for rel, body := range map[string]string{"secret.txt": "LOWER", "SECRET.TXT": "UPPER", "Dir/x.txt": "DIR_UPPER", "dir/x.txt": "DIR_LOWER"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	listed := 0
	canonHook = func(string) { listed++ }
	defer func() { canonHook = nil }()
	tr := pin(t, dir, Options{})
	for rel, want := range map[string]string{"secret.txt": "LOWER", "SECRET.TXT": "UPPER", "Dir/x.txt": "DIR_UPPER", "dir/x.txt": "DIR_LOWER"} {
		var canonical string
		b, err := tr.ReadFileChecked(rel, func(r Resolved) error { canonical = r.Canonical(); return nil })
		if err != nil || string(b) != want || canonical != rel {
			t.Errorf("%s: %q canonical %q %v; want %q", rel, b, canonical, err, want)
		}
	}
	if _, err := tr.Resolve("Secret.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Secret.txt: %v, want not found", err)
	}
	if runtime.GOOS == "darwin" && listed != 0 {
		t.Errorf("listed %d directories on a case-sensitive volume for ASCII names", listed)
	}
	t.Logf("directories listed: %d", listed)
}

// C1-9: Unicode — a name the file system takes in another normalization or
// another case of a non-ASCII letter resolves to the name listed.
func TestC1_Unicode(t *testing.T) {
	requireSymlinks(t)
	dirs := []string{t.TempDir()}
	if d := os.Getenv("ARK_CASE_SENSITIVE_DIR"); d != "" {
		dirs = append(dirs, caseSensitiveDir(t))
	}
	const nfc, nfd = "caf\u00e9.txt", "cafe\u0301.txt"
	ran := 0
	for _, dir := range dirs {
		os.WriteFile(filepath.Join(dir, nfc), []byte("NFC"), 0o644)
		os.WriteFile(filepath.Join(dir, "\u00c4rger.txt"), []byte("UMLAUT"), 0o644)
		tr := pin(t, dir, Options{})
		for in, want := range map[string]string{nfd: nfc, "\u00e4rger.txt": "\u00c4rger.txt", "\u00c4RGER.TXT": "\u00c4rger.txt"} {
			if _, err := os.Lstat(filepath.Join(dir, in)); err != nil {
				t.Logf("%s: %q is another file here (not folded): %v", dir, in, err)
				continue
			}
			ran++
			r, err := tr.Resolve(in)
			if err != nil || r.Canonical() != want {
				t.Errorf("%s: Resolve(%q) = %q %v; want %q", dir, in, r.Canonical(), err, want)
			}
		}
	}
	if ran == 0 {
		t.Skip("SKIP: no file system here folds Unicode names")
	}
}

// C1-7: the tree changes between the lookup and the listing that names
// what it reached. Whatever the outcome, the object read is the object the
// lookup reached, named as listed — never the object now at the name.
func TestC1_ChangeDuringCanonicalization(t *testing.T) {
	f := newFixture(t)
	requireFolding(t, f.ws)
	once := func(at string, fn func()) {
		done := false
		canonHook = func(name string) {
			if name == at && !done {
				done = true
				fn()
			}
		}
	}
	defer func() { canonHook = nil }()
	tr := pin(t, f.path("root"), Options{})
	read := func(rel string) (string, string, error) {
		var canonical string
		b, err := tr.ReadFileChecked(rel, func(r Resolved) error { canonical = r.Canonical(); return nil })
		return string(b), canonical, err
	}

	// The file is renamed away and another put at its name.
	once("S.TXT", func() {
		mutate(t, "rename the file", os.Rename(f.path("A/s.txt"), f.path("A/moved.txt")))
		mutate(t, "rename another file in", os.Rename(f.path("A/excluded.txt"), f.path("A/s.txt")))
	})
	b, canonical, err := read("S.TXT")
	if b == "A_EXCLUDED" || (err == nil && (b != "A_PUBLIC" || canonical != "moved.txt")) {
		t.Errorf("file swapped: %q as %q, %v", b, canonical, err)
	}

	// The file disappears: nothing names it.
	once("MOVED.TXT", func() { os.Remove(f.path("A/moved.txt")) })
	if b, _, err := read("MOVED.TXT"); err == nil {
		t.Errorf("removed file: read %q", b)
	}

	// The directory is replaced while its name is looked up.
	once("SUB", func() {
		mutate(t, "move the directory", os.Rename(f.path("A/sub"), f.path("A/sub-old")))
		f.write(t, "A/sub/f.txt", "DECOY")
	})
	b, canonical, err = read("SUB/F.TXT")
	if b == "DECOY" || (err == nil && (b != "A_SUB" || canonical != "sub-old/f.txt")) {
		t.Errorf("directory swapped: %q as %q, %v", b, canonical, err)
	}
}
