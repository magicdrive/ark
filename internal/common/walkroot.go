package common

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Repository roots given through a symlink.
//
// filepath.Walk and filepath.WalkDir do not enter a root that is itself a
// symlink, so a repository served or dumped through a link (`--root
// ~/link-to-repo`) would look empty. WalkRoot and WalkDirRoot walk the
// directory such a root leads to and report every path in the root's own
// spelling — the logical path, which ignore rules and the access policy are
// anchored at. Only the root is followed: symlinks below it, directory links
// included, are reported and not entered, exactly as by filepath.WalkDir. A
// directory link named as a walk's start is not a root and is not entered
// either: its entries' paths would not be the paths the rules name.

// SymlinkedRoot returns the directory root leads to when root itself is a
// symlink to a directory.
func SymlinkedRoot(root string) (string, bool) {
	fi, err := os.Lstat(root)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	if st, err := os.Stat(real); err != nil || !st.IsDir() {
		return "", false
	}
	return real, true
}

// respell maps a path below real to the same path below root.
func respell(root, real, p string) string {
	return root + strings.TrimPrefix(p, real)
}

// WalkDirRoot is filepath.WalkDir(root, fn) that also enters root when root
// is a symlink to a directory, reporting paths below root's spelling.
func WalkDirRoot(root string, fn fs.WalkDirFunc) error {
	real, ok := SymlinkedRoot(root)
	if !ok {
		return filepath.WalkDir(root, fn)
	}
	return filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
		return fn(respell(root, real, p), d, err)
	})
}

// WalkRoot is filepath.Walk(root, fn) that also enters root when root is a
// symlink to a directory, reporting paths below root's spelling.
func WalkRoot(root string, fn filepath.WalkFunc) error {
	real, ok := SymlinkedRoot(root)
	if !ok {
		return filepath.Walk(root, fn)
	}
	return filepath.Walk(real, func(p string, info os.FileInfo, err error) error {
		return fn(respell(root, real, p), info, err)
	})
}

// SamePath reports whether a and b name the same path once made absolute and
// clean (without resolving symlinks).
func SamePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}
