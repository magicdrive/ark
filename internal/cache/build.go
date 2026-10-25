package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
)

// Which build may reuse an entry.
//
// An entry is the extraction of one file by one build of Ark. Its key
// (CacheKey) names the file's content, the cache schema and the provider's
// CacheVersion, which a developer changes with the provider's extraction
// semantics. That contract holds for releases: they are built from reviewed,
// tagged commits, and an entry of one release is reused by the next exactly
// when both extract that content alike. A development build breaks it — its
// extraction changes long before anyone bumps a CacheVersion, and two builds
// of different sources carry the same keys — so the key alone does not tell
// which logic wrote an entry. Nor does the displayed version ("(devel)",
// or the same "v…+dirty" for two different edits of one commit), the VCS
// stamp (the same commit and "modified"), or main.version (make build sets it
// as the release build does).
//
// So the store keeps each build's entries in a scope of its own:
//
//   - "release": a release build, the only one with release set (by
//     .github/release/build.sh, the build of hephaestus.yaml, for a tagged
//     build of a clean tree at its tag; never a snapshot, a modified tree
//     or another commit). Releases share it, under the CacheVersion
//     contract.
//   - "dev-<hash>": any other build — go build, make build, go run, go
//     install, a snapshot, a dirty tree, a build without VCS information —
//     named by the SHA-256 of its executable. The same binary finds its
//     entries again on every start; a binary differing in one byte has a
//     scope of its own and extracts afresh.
//
// The executable is hashed once, when the store is opened (at server start),
// and must be the running one. On Linux it is read through /proc/self/exe,
// the running image even after its file is replaced. Elsewhere it is the
// file at os.Executable, which a rebuild can replace under a running server;
// the hash is taken right at start, which leaves only the moment between
// exec and the hash, and is refused when the file changes while it is read.
// Hashing it later would widen that window: a replaced executable names the
// new binary's scope for the old code. When the executable cannot be read,
// there is no scope and no persistent cache (OpenBuildStore fails; the
// server runs without one).

// release is "true" in a release build only: .github/release/build.sh sets
// it with -X for a release, never for a snapshot or a modified tree.
var release string

// ReleaseScope is the scope of release builds.
const ReleaseScope = "release"

const devScopePrefix = "dev-"

// Build identifies the build of Ark that is running, as far as its cache
// entries are concerned.
type Build struct {
	Scope   string // ReleaseScope or "dev-" + 32 hex digits
	Release bool
}

var (
	currentOnce  sync.Once
	currentBuild Build
	currentErr   error
)

// CurrentBuild returns the running build's Build, computing it on first use.
func CurrentBuild() (Build, error) {
	currentOnce.Do(func() {
		currentBuild, currentErr = buildFor(release, runningExecutable, nil)
	})
	return currentBuild, currentErr
}

// runningExecutable returns the path that reads the running executable.
func runningExecutable() (string, error) {
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/proc/self/exe"); err == nil {
			return "/proc/self/exe", nil
		}
	}
	return os.Executable()
}

// buildFor is CurrentBuild given the release flag and how to find the
// executable; hashed, if not nil, runs once the executable has been read.
func buildFor(release string, executable func() (string, error), hashed func()) (Build, error) {
	if release == "true" {
		return Build{Scope: ReleaseScope, Release: true}, nil
	}
	path, err := executable()
	if err != nil {
		return Build{}, fmt.Errorf("cache: locating the executable: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return Build{}, fmt.Errorf("cache: reading the executable: %w", err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Build{}, fmt.Errorf("cache: reading the executable: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Build{}, fmt.Errorf("cache: reading the executable: %w", err)
	}
	if hashed != nil {
		hashed()
	}
	// The file read is still the one at path, unchanged.
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return Build{}, fmt.Errorf("cache: the executable %s changed while it was read", path)
	}
	return Build{Scope: devScopePrefix + hex.EncodeToString(h.Sum(nil)[:16])}, nil
}
