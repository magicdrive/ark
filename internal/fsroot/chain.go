package fsroot

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
)

// ChainDir is one directory ReadChain passed: the directory as opened, and
// the file it looked for in it.
type ChainDir struct {
	DirRef
	// Skipped: a directory named .git or .ark — recorded, not read, and the
	// chain ends there (no rules apply in such directories).
	Skipped bool
	File    ChainFile
}

// ChainFile is what ReadChain found under its name in one directory.
type ChainFile struct {
	Present    bool     // an entry listed under exactly that name
	Link       bool     // a symlink: not read; the caller resolves it
	NotRegular bool     // a directory, device, pipe or socket: not read
	Data       []byte   // the contents of a regular file
	ID         Identity // the regular file read
	Err        error    // it could not be examined or read
}

// ReadChain walks from the root through the directories of rel without
// following symlinks — the root first, then each component that is a plain
// directory — and in each reads the file name (when read reports true for
// the directory's root-relative path), from the directory's own handle. It
// stops at the first component that is missing, not a directory or a
// symlink (as a walk would not enter it), and after a .git or .ark
// directory.
//
// rel must be canonical — as a resolution (Resolved.Canonical, Real) or a
// walk names it: its components are looked up as given, not renamed, so a
// caller that verifies the directories' identities against a resolution's
// (Resolved.Dirs) knows they are the directories that resolution passed.
// The file must be listed under exactly name: one present only in another
// spelling of it (case-insensitive file systems) is absent.
//
// A file replaced while it is opened is ErrChanged: the chain is not
// returned.
func (t *Tree) ReadChain(rel, name string, read func(dir string) bool) ([]ChainDir, error) {
	return t.readChain(rel, name, read, true)
}

// RereadChain is ReadChain for a second reading of files a ReadChain read:
// the file's name is not looked up in the directory's listing again (the
// caller compares what it reads with the first reading, identity and
// bytes).
func (t *Tree) RereadChain(rel, name string, read func(dir string) bool) ([]ChainDir, error) {
	return t.readChain(rel, name, read, false)
}

func (t *Tree) readChain(rel, name string, read func(dir string) bool, listed bool) ([]ChainDir, error) {
	root, release, err := t.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	_, comps, err := splitRel(rel)
	if err != nil {
		return nil, err
	}
	rootID, err := root.identity()
	if err != nil {
		return nil, err
	}
	var held []handle
	defer func() { closeAll(held) }()
	out := []ChainDir{{DirRef: DirRef{Rel: ".", ID: rootID}}}
	if err := t.chainFile(root, &out[0], name, read, listed); err != nil {
		return nil, err
	}
	cur := root
	var names []string
	for _, c := range comps {
		info, err := cur.lstat(c)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				break
			}
			return nil, err
		}
		if info.mode&fs.ModeSymlink != 0 || !info.mode.IsDir() {
			break // a walk would not enter it
		}
		n := c
		next, err := cur.openDir(n, info.id)
		if err != nil {
			return nil, err
		}
		held = append(held, next)
		id, err := next.identity()
		if err != nil {
			return nil, err
		}
		names = append(names, n)
		d := ChainDir{DirRef: DirRef{Rel: strings.Join(names, "/"), ID: id}}
		if n == ".git" || n == ".ark" {
			d.Skipped = true
			out = append(out, d)
			break
		}
		if err := t.chainFile(next, &d, name, read, listed); err != nil {
			return nil, err
		}
		out = append(out, d)
		cur = next
	}
	return out, nil
}

// chainFile fills d.File with name in dir.
func (t *Tree) chainFile(dir handle, d *ChainDir, name string, read func(string) bool, listed bool) error {
	if read != nil && !read(d.Rel) {
		return nil
	}
	info, err := dir.lstat(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			d.File.Err = err
		}
		return nil
	}
	if listed {
		got, err := canonicalName(dir, name, info.id)
		if err != nil {
			if errors.Is(err, ErrChanged) {
				return err
			}
			d.File.Err = err
			return nil
		}
		if got != name {
			return nil // another file, whose name only folds to name
		}
	}
	d.File.Present = true
	switch {
	case info.mode&fs.ModeSymlink != 0:
		d.File.Link = true
		return nil
	case !info.mode.IsRegular():
		d.File.NotRegular = true
		return nil
	}
	f, oi, err := dir.openFile(name, info.id)
	if err != nil {
		if errors.Is(err, ErrChanged) || errors.Is(err, ErrNotRegular) || errors.Is(err, fs.ErrNotExist) {
			return &fs.PathError{Op: "read", Path: name, Err: ErrChanged}
		}
		d.File.Err = err
		return nil
	}
	defer f.Close()
	b, err := readAllSized(f, oi.size)
	if err != nil {
		d.File.Err = err
		return nil
	}
	d.File.Data, d.File.ID = b, oi.id
	return nil
}
