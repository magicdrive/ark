package fsroot

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Canonical names.
//
// A file system may reach one entry by several spellings: case-insensitive
// volumes (macOS by default, Windows, casefolded Linux directories) ignore
// case, APFS and HFS+ ignore Unicode normalization, Windows accepts 8.3 short
// names. A policy that matches names must match the name the entry has, not
// the spelling a caller used, or "SECRET.TXT" escapes a rule on
// "secret.txt". So every component a resolution passes is renamed to the
// name its directory lists for the object the lookup reached: the directory
// is listed through the same handle the lookup used, and the entry chosen is
// the one whose identity is the identity reached — the comparison of names is
// the file system's own, never guessed here. A lookup no listed entry
// accounts for (the tree changed) fails with ErrChanged; one that two listed
// entries account for (hard links whose names both fit) with ErrAmbiguous.
//
// A directory whose lookups are known to be exact (handle.exactNames) is not
// listed: the spelling that resolved is the name.

// ErrAmbiguous: more than one entry of a directory is the object a name
// reached, and none is spelled as asked; the name to apply rules to is not
// known.
var ErrAmbiguous = errors.New("fsroot: name matches more than one directory entry")

// canonHook, when set by a test, runs before a directory is listed to
// canonicalize name: the window between the lookup and the listing.
var canonHook func(name string)

// canonicalName returns the name dir lists for the object id that looking up
// name in dir reached.
func canonicalName(dir handle, name string, id Identity) (string, error) {
	if dir.exactNames(name) {
		return name, nil
	}
	if canonHook != nil {
		canonHook(name)
	}
	// Spelled as listed: the lookup reached this entry. The listing stops
	// there.
	names, listed, err := dir.scanNames(func(n string) bool { return n == name })
	if err != nil {
		return "", err
	}
	if listed {
		return name, nil
	}
	found, n := "", 0
	for _, e := range names {
		info, err := dir.lstat(e)
		if err != nil {
			continue // gone since listed: not the object reached
		}
		if info.id.Same(id) {
			found, n = e, n+1
		}
	}
	switch n {
	case 0:
		return "", &fs.PathError{Op: "canonicalize", Path: name, Err: ErrChanged}
	case 1:
		return found, nil
	}
	return "", &fs.PathError{Op: "canonicalize", Path: name, Err: ErrAmbiguous}
}

// externalName is canonicalName for a directory outside the root, reached
// through an allowed external symlink: by path, as everything outside the
// root is. A name that does not exist is returned as is (opening it fails).
func externalName(dir, name string) (string, error) {
	fi, err := os.Lstat(filepath.Join(dir, name))
	if err != nil {
		return name, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Name() == name {
			return name, nil
		}
	}
	found, n := "", 0
	for _, e := range entries {
		efi, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err == nil && os.SameFile(fi, efi) {
			found, n = e.Name(), n+1
		}
	}
	switch n {
	case 0:
		return "", &fs.PathError{Op: "canonicalize", Path: name, Err: ErrChanged}
	case 1:
		return found, nil
	}
	return "", &fs.PathError{Op: "canonicalize", Path: name, Err: ErrAmbiguous}
}

// scanFile reads the names of directory f in batches until stop reports
// true.
func scanFile(f *os.File, stop func(string) bool) ([]string, bool, error) {
	var seen []string
	for {
		batch, err := f.Readdirnames(256)
		for _, n := range batch {
			if stop(n) {
				return seen, true, nil
			}
			seen = append(seen, n)
		}
		if err == io.EOF || (err == nil && len(batch) == 0) {
			return seen, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}
}

// Canonicalize pins root and resolves rel below it, for a caller that
// decides by name but reads by path (Ark's path gates, until they read
// through a Tree): canonical is rel with every component spelled as its
// directory lists it, real the symlink-free path inside the root it leads
// to ("" when it leads outside). The names hold when returned; nothing keeps
// them true after.
func Canonicalize(root, rel string, opts Options) (canonical, real string, err error) {
	t, err := Pin(root, opts)
	if err != nil {
		return "", "", err
	}
	defer t.Close()
	r, err := t.Resolve(rel)
	if err != nil {
		return "", "", err
	}
	return r.Canonical(), r.Real(), nil
}
