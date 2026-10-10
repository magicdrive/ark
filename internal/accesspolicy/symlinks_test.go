package accesspolicy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// requireSymlinks: see fsroot. Skips (saying why) where symlinks cannot be
// created; fails instead under ARK_REQUIRE_SYMLINKS (CI).
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

// mutate applies an attacker's change; where the OS refuses it (Windows does
// not rename a directory held open), the test is skipped, saying so.
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
