package fsroot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// requireSymlinks skips a test that needs symlinks where they cannot be
// created (Windows without the privilege or Developer Mode), saying why —
// unless ARK_REQUIRE_SYMLINKS is set (CI), where it fails instead, so the
// security tests cannot pass by being skipped.
func requireSymlinks(t testing.TB) {
	t.Helper()
	dir := t.TempDir()
	err := os.Symlink("target", filepath.Join(dir, "probe"))
	if err == nil {
		return
	}
	if os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
		t.Fatalf("symlinks required (ARK_REQUIRE_SYMLINKS) but unavailable on %s: %v", runtime.GOOS, err)
	}
	t.Skipf("SKIP: symlinks unavailable on %s: %v", runtime.GOOS, err)
}

// mutate applies a change an attacker would make. Where the operating system
// refuses it (Windows does not rename a directory a process holds open), the
// attack is not possible there: the test is skipped, saying so — never passed.
func mutate(t testing.TB, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		t.Skipf("SKIP: %s refused by %s: %v (attack not possible here)", what, runtime.GOOS, err)
	}
	t.Fatalf("%s: %v", what, err)
}

// swapLinkChecked atomically points link at target, reporting failure.
func swapLinkChecked(target, link string) error {
	tmp := link + ".swap"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, link)
}
