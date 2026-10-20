package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, content string) func() (string, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ark")
	if err := os.WriteFile(p, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return func() (string, error) { return p, nil }
}

// Only the flag GoReleaser sets for a release makes a release build; the
// executable is then not read at all.
func TestBuildFor_ReleaseFlag(t *testing.T) {
	unreadable := func() (string, error) { return "", errors.New("no executable") }
	b, err := buildFor("true", unreadable, nil)
	if err != nil || b != (Build{Scope: ReleaseScope, Release: true}) {
		t.Fatalf("release build: %+v, %v", b, err)
	}
	exe := writeExecutable(t, "binary")
	for _, v := range []string{"", "false", "TRUE", "1", "yes", "v6.5.1", "(devel)"} {
		b, err := buildFor(v, exe, nil)
		if err != nil || b.Release || !strings.HasPrefix(b.Scope, devScopePrefix) {
			t.Errorf("release=%q: %+v, %v (want a development scope)", v, b, err)
		}
	}
}

// A development build is its executable: the same bytes give the same scope
// (a restart, or a rebuild that reproduces the binary), any other bytes
// another.
func TestBuildFor_DevelopmentScopeIsTheExecutable(t *testing.T) {
	a1, err1 := buildFor("", writeExecutable(t, "build one"), nil)
	a2, err2 := buildFor("", writeExecutable(t, "build one"), nil)
	b, err3 := buildFor("", writeExecutable(t, "build twO"), nil)
	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatal(err1, err2, err3)
	}
	if a1 != a2 {
		t.Errorf("same executable bytes, different scopes: %s, %s", a1.Scope, a2.Scope)
	}
	if a1 == b {
		t.Errorf("different executables share scope %s", a1.Scope)
	}
	if !devScopeName.MatchString(a1.Scope) {
		t.Errorf("scope %q is not a development scope name", a1.Scope)
	}
}

// Without a readable executable there is no build identity, so no
// persistent cache: never a guessed or shared scope.
func TestBuildFor_UnidentifiableBuildHasNoScope(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	for name, exe := range map[string]func() (string, error){
		"no executable path": func() (string, error) { return "", errors.New("unsupported") },
		"missing file":       func() (string, error) { return missing, nil },
	} {
		if b, err := buildFor("", exe, nil); err == nil {
			t.Errorf("%s: scope %q, want an error", name, b.Scope)
		}
	}
}

// An executable replaced while it is read (a rebuild under a starting
// server) names no build: what was hashed need not be what runs.
func TestBuildFor_ExecutableReplacedWhileReadHasNoScope(t *testing.T) {
	exe := writeExecutable(t, "build one")
	path, _ := exe()
	b, err := buildFor("", exe, func() {
		next := path + ".next"
		if err := os.WriteFile(next, []byte("build two"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(next, path); err != nil {
			t.Fatal(err)
		}
	})
	if err == nil {
		t.Fatalf("scope %q for an executable replaced while it was read", b.Scope)
	}
}

// The running test binary is a development build whose scope is the
// SHA-256 of its own executable, the same on every call.
func TestCurrentBuild_IsTheRunningExecutable(t *testing.T) {
	b1, err1 := CurrentBuild()
	b2, err2 := CurrentBuild()
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if b1 != b2 || b1.Release {
		t.Fatalf("CurrentBuild: %+v then %+v", b1, b2)
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if want := devScopePrefix + hex.EncodeToString(sum[:16]); b1.Scope != want {
		t.Fatalf("CurrentBuild scope %s, want %s (the test binary's SHA-256)", b1.Scope, want)
	}
}

// The persistent cache shared by builds of Ark is opened in one place: no
// production code outside this package opens a FileStore of no scope.
func TestOnlyOpenBuildStoreOpensTheSharedCache(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".ark", ".agent-work", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			filepath.Dir(path) == "." {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "cache.NewFileStore(") {
			t.Errorf("%s opens a FileStore without a build scope: use cache.OpenBuildStore", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
