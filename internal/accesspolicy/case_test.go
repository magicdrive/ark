package accesspolicy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/fsroot"
)

// C1: on a case-insensitive file system (Windows, macOS by default) a path
// spelled in another case reaches the same file. The rules stay
// case-sensitive and are matched against the names the file system lists
// (fsroot canonical names), so no spelling escapes them.

func requireFolding(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "case-probe")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	if _, err := os.Lstat(filepath.Join(dir, "CASE-PROBE")); err != nil {
		if os.Getenv("ARK_REQUIRE_CASE_FOLDING") != "" {
			t.Fatalf("ARK_REQUIRE_CASE_FOLDING: the file system at %s is case-sensitive", dir)
		}
		t.Skipf("SKIP: the file system at %s is case-sensitive: a case-folding bypass is not possible here", dir)
	}
}

// C1-12 (formerly reported as a known gap): another case spelling of an
// excluded path is excluded.
func TestCaseOfExcludedPath(t *testing.T) {
	w := ruleRepo(t)
	requireFolding(t, w.dir)
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	for _, rel := range []string{"root-secret.txt", "ROOT-SECRET.TXT", "Root-Secret.Txt", "D/X.TXT", "p/D/x.txt"} {
		if err := decide(t, s, rel); !errors.Is(err, ErrExcluded) {
			t.Errorf("%s: %v, want ErrExcluded", rel, err)
		}
	}
}

// c1Repo: the C1 report's cases.
//
//	.arkignore      secret.txt  Secrets/  hidden-link  ext-dir/secret.txt
//	secret.txt  public.txt  Secrets/api-key.txt
//	src/.arkignore  private.txt         src/private.txt  src/open.txt
//	alias -> SECRET.TXT          hidden-link -> public.txt
//	dlink -> SECRETS             abs-upper -> <REPO IN UPPER CASE>/SECRET.TXT
//	reenter -> ../REPO/Secret.txt   ext-dir -> <ws>/outside
func c1Repo(t *testing.T) ws {
	t.Helper()
	w := newWS(t)
	requireFolding(t, w.dir)
	w.write(t, "repo/.arkignore", "secret.txt\nSecrets/\nhidden-link\next-dir/secret.txt\n")
	w.write(t, "repo/secret.txt", "C1_SECRET")
	w.write(t, "repo/public.txt", "PUBLIC")
	w.write(t, "repo/Secrets/api-key.txt", "C1_DIR_SECRET")
	w.write(t, "repo/src/.arkignore", "private.txt\n")
	w.write(t, "repo/src/private.txt", "C1_NESTED_SECRET")
	w.write(t, "repo/src/open.txt", "OPEN")
	w.write(t, "outside/secret.txt", "C1_EXT_SECRET")
	w.write(t, "outside/other.txt", "EXT_OTHER")
	w.link(t, "SECRET.TXT", "repo/alias")
	w.link(t, "public.txt", "repo/hidden-link")
	w.link(t, "SECRETS", "repo/dlink")
	w.link(t, strings.ToUpper(w.path("repo"))+string(filepath.Separator)+"SECRET.TXT", "repo/abs-upper")
	w.link(t, "../REPO/Secret.txt", "repo/reenter")
	w.link(t, w.path("outside"), "repo/ext-dir")
	return w
}

// C1-1 … C1-5 through Snapshot.Check and Snapshot.ReadFile, with external
// symlinks refused and allowed.
func TestC1_Snapshot(t *testing.T) {
	w := c1Repo(t)
	for _, ext := range []bool{false, true} {
		s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{AllowExternalSymlinks: ext}))
		for _, rel := range []string{
			"SECRET.TXT", "Secret.txt", // C1-1
			"SECRETS/api-key.txt", "secrets/API-KEY.TXT", // C1-2
			"SRC/PRIVATE.TXT", "src/Private.txt", "Src/private.txt", // C1-3
			"alias", "ALIAS", "HIDDEN-LINK", "dlink/api-key.txt", "DLINK/API-KEY.TXT", "abs-upper", "reenter", // C1-5
		} {
			if err := decide(t, s, rel); !errors.Is(err, ErrExcluded) {
				t.Errorf("external %v: Check(%s) = %v, want ErrExcluded", ext, rel, err)
			}
			if b, err := s.ReadFile(rel); !errors.Is(err, ErrExcluded) {
				t.Errorf("external %v: ReadFile(%s) = %q %v, want ErrExcluded", ext, rel, b, err)
			}
		}
		// Through an external directory link: its names are canonical too.
		if ext {
			if b, err := s.ReadFile("EXT-DIR/SECRET.TXT"); !errors.Is(err, ErrExcluded) {
				t.Errorf("ReadFile(EXT-DIR/SECRET.TXT) = %q %v, want ErrExcluded", b, err)
			}
			if b, err := s.ReadFile("EXT-DIR/OTHER.TXT"); err != nil || string(b) != "EXT_OTHER" {
				t.Errorf("ReadFile(EXT-DIR/OTHER.TXT) = %q %v", b, err)
			}
		}
		// What is not excluded stays readable in any spelling.
		for rel, want := range map[string]string{"PUBLIC.TXT": "PUBLIC", "SRC/OPEN.TXT": "OPEN", "public.txt": "PUBLIC"} {
			if b, err := s.ReadFile(rel); err != nil || string(b) != want {
				t.Errorf("external %v: ReadFile(%s) = %q %v", ext, rel, b, err)
			}
		}
	}
}

// C1-4: mixed case on the way and in the rule.
func TestC1_MixedCase(t *testing.T) {
	w := newWS(t)
	requireFolding(t, w.dir)
	w.write(t, "repo/.arkignore", "Src/Secrets/ApiKey.JSON\n")
	w.write(t, "repo/Src/Secrets/ApiKey.JSON", "C1_MIXED")
	w.write(t, "repo/Src/Secrets/Other.json", "OTHER")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	for _, rel := range []string{"src/secrets/APIKEY.json", "SRC/SECRETS/APIKEY.JSON", "Src/Secrets/ApiKey.JSON"} {
		if b, err := s.ReadFile(rel); !errors.Is(err, ErrExcluded) {
			t.Errorf("ReadFile(%s) = %q %v, want ErrExcluded", rel, b, err)
		}
	}
	if b, err := s.ReadFile("src/secrets/other.JSON"); err != nil || string(b) != "OTHER" {
		t.Errorf("ReadFile(other) = %q %v", b, err)
	}
}

// A walk started from another spelling names its entries as listed, so
// CheckEntry and ReadEntry apply the nested rules.
func TestC1_WalkFromAnotherSpelling(t *testing.T) {
	w := c1Repo(t)
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	seen := 0
	s.Tree().Walk("SRC", func(e fsroot.Entry) error {
		if strings.HasSuffix(e.Rel(), "private.txt") {
			seen++
			if e.Rel() != "src/private.txt" {
				t.Errorf("entry named %s", e.Rel())
			}
			if err := s.CheckEntry(e); !errors.Is(err, ErrExcluded) {
				t.Errorf("CheckEntry: %v", err)
			}
			if b, err := s.ReadEntry(e); err == nil {
				t.Errorf("ReadEntry read %q", b)
			}
		}
		return nil
	})
	if seen != 1 {
		t.Errorf("private.txt seen %d times", seen)
	}
}

// C1-9: Unicode names — another normalization (macOS), another case of a
// non-ASCII letter.
func TestC1_UnicodeNames(t *testing.T) {
	w := newWS(t)
	const nfc, nfd = "caf\u00e9.txt", "cafe\u0301.txt"
	w.write(t, "repo/.arkignore", nfc+"\n\u00c4rger.txt\n")
	w.write(t, "repo/"+nfc, "C1_NFC")
	w.write(t, "repo/\u00c4rger.txt", "C1_UMLAUT")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	ran := 0
	for _, rel := range []string{nfd, "\u00e4rger.txt", "\u00c4RGER.TXT"} {
		if _, err := os.Lstat(w.path("repo/" + rel)); err != nil {
			continue // another file here: nothing to bypass
		}
		ran++
		if b, err := s.ReadFile(rel); !errors.Is(err, ErrExcluded) {
			t.Errorf("ReadFile(%q) = %q %v, want ErrExcluded", rel, b, err)
		}
	}
	if ran == 0 {
		t.Skip("SKIP: the file system does not fold these names")
	}
}

// C1-6: on a case-sensitive file system the rules apply to exactly the name
// they spell: secret.txt is excluded, SECRET.TXT (another file) is not.
func TestC1_CaseSensitiveRules(t *testing.T) {
	requireSymlinks(t)
	dir := os.Getenv("ARK_CASE_SENSITIVE_DIR")
	if dir != "" {
		d, err := os.MkdirTemp(dir, "cs")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(d) })
		dir = d
	} else {
		dir = t.TempDir()
		os.WriteFile(filepath.Join(dir, "probe"), nil, 0o644)
		if _, err := os.Lstat(filepath.Join(dir, "PROBE")); err == nil {
			t.Skip("SKIP: no case-sensitive file system (set ARK_CASE_SENSITIVE_DIR)")
		}
	}
	w := ws{dir: dir}
	w.write(t, "repo/.arkignore", "secret.txt\n")
	w.write(t, "repo/secret.txt", "LOWER_SECRET")
	w.write(t, "repo/SECRET.TXT", "UPPER_PUBLIC")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	if b, err := s.ReadFile("secret.txt"); !errors.Is(err, ErrExcluded) {
		t.Errorf("secret.txt: %q %v", b, err)
	}
	if b, err := s.ReadFile("SECRET.TXT"); err != nil || string(b) != "UPPER_PUBLIC" {
		t.Errorf("SECRET.TXT: %q %v", b, err)
	}
	if b, err := s.ReadFile("Secret.txt"); err == nil {
		t.Errorf("Secret.txt read %q", b)
	}
}
