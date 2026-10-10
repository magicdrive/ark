package fsroot

import (
	"errors"
	"io/fs"
	"os"
	"sync/atomic"
)

// SkipDir and SkipAll steer Walk as they steer filepath.WalkDir.
var (
	SkipDir = fs.SkipDir
	SkipAll = fs.SkipAll
)

// Entry is one entry of a Walk: valid only while the callback that received
// it runs (its directory handle is closed afterwards; later use fails with
// ErrClosed).
type Entry struct {
	rel   string // root-relative, "/"-separated; "." for the root
	name  string
	typ   fs.FileMode
	err   error // the directory could not be entered
	dir   *walkDir
	dirID Identity // the directory listed from (for the start: itself)
}

type walkDir struct {
	h      handle
	owned  bool // the walk opened h: no one else lists it
	closed atomic.Bool
}

// Rel returns the entry's root-relative path ("." for the root).
func (e Entry) Rel() string { return e.rel }

// Name returns the entry's name.
func (e Entry) Name() string { return e.name }

// Type returns the entry's type as listed (a hint: opens re-check it).
func (e Entry) Type() fs.FileMode { return e.typ }

// IsDir reports whether the entry was listed as a directory (a symlink to a
// directory is not one: walks never enter symlinks).
func (e Entry) IsDir() bool { return e.typ.IsDir() }

// IsSymlink reports whether the entry was listed as a symlink. Following it
// is the caller's decision: Resolve(e.Rel()).
func (e Entry) IsSymlink() bool { return e.typ&fs.ModeSymlink != 0 }

// Err reports why a directory entry could not be entered (it is not walked).
func (e Entry) Err() error { return e.err }

// DirIdentity returns the identity of the directory the entry was listed
// from (for the walk's start, of the start itself).
func (e Entry) DirIdentity() Identity { return e.dirID }

func (e Entry) handle() (handle, error) {
	if e.dir == nil || e.dir.closed.Load() {
		return nil, ErrClosed
	}
	return e.dir.h, nil
}

// Identity returns the identity of the entry itself (not followed).
func (e Entry) Identity() (Identity, error) {
	if e.rel == "." || e.name == "" {
		return e.dirID, nil
	}
	h, err := e.handle()
	if err != nil {
		return Identity{}, err
	}
	info, err := h.lstat(e.name)
	return info.id, err
}

// Open opens the entry as a regular file, from the directory handle it was
// listed from, without following a symlink: the file currently at that name
// in that directory, or an error.
func (e Entry) Open() (*os.File, error) {
	h, err := e.handle()
	if err != nil {
		return nil, err
	}
	if e.IsSymlink() {
		return nil, &fs.PathError{Op: "open", Path: e.rel, Err: ErrSymlink}
	}
	f, _, err := h.openFile(e.name, Identity{})
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: e.rel, Err: err}
	}
	return f, nil
}

// ReadAll reads the entry as a regular file (Open) and returns its content
// and the identity of the object read.
func (e Entry) ReadAll() ([]byte, Identity, error) {
	h, err := e.handle()
	if err != nil {
		return nil, Identity{}, err
	}
	if e.IsSymlink() {
		return nil, Identity{}, &fs.PathError{Op: "open", Path: e.rel, Err: ErrSymlink}
	}
	f, info, err := h.openFile(e.name, Identity{})
	if err != nil {
		return nil, Identity{}, &fs.PathError{Op: "open", Path: e.rel, Err: err}
	}
	defer f.Close()
	b, err := readAllSized(f, info.size)
	return b, info.id, err
}

// Walk walks the directory start (root-relative; "." for the root) in
// filepath.WalkDir order: start first, then each directory's entries sorted
// by name, depth first. start must be reached without symlinks
// (ErrSymlink otherwise); symlinks inside are reported and never entered.
// A directory that cannot be entered is reported once more with Err set; if
// fn returns nil for it, the walk goes on. fn may return SkipDir (on a
// directory: do not enter it; on another entry: skip the rest of its
// directory) or SkipAll.
func (t *Tree) Walk(start string, fn func(Entry) error) error {
	root, release, err := t.acquire()
	if err != nil {
		return err
	}
	defer release()
	clean, comps, err := splitRel(start)
	if err != nil {
		return err
	}
	dir, held, err := t.strictDirs(root, comps)
	if err != nil {
		return err
	}
	// The start handle is owned by the walk unless it is the root.
	if len(held) > 0 {
		held = held[:len(held)-1]
	}
	closeAll(held)
	id, err := dir.identity()
	if err != nil {
		if dir != root {
			dir.close()
		}
		return err
	}
	name := ""
	if len(comps) > 0 {
		name = comps[len(comps)-1]
	}
	w := &walkDir{h: dir, owned: dir != root}
	err = fn(Entry{rel: clean, name: name, typ: fs.ModeDir, dir: w, dirID: id})
	if err == nil {
		err = t.walkDir(w, clean, id, fn)
	}
	w.closed.Store(true)
	if dir != root {
		dir.close()
	}
	if errors.Is(err, SkipDir) || errors.Is(err, SkipAll) {
		return nil
	}
	return err
}

// walkDir lists w (at rel, identity id) and walks its entries.
func (t *Tree) walkDir(w *walkDir, rel string, id Identity, fn func(Entry) error) error {
	entries, err := w.h.readDir(w.owned)
	if err != nil {
		return fn(Entry{rel: rel, typ: fs.ModeDir, err: err, dirID: id})
	}
	for _, d := range entries {
		child := d.name
		if rel != "." {
			child = rel + "/" + d.name
		}
		e := Entry{rel: child, name: d.name, typ: d.typ, dir: w, dirID: id}
		err := fn(e)
		if err != nil {
			if errors.Is(err, SkipDir) {
				if d.typ.IsDir() {
					continue
				}
				return nil // the rest of this directory
			}
			return err
		}
		if !d.typ.IsDir() {
			continue
		}
		sub, err := w.h.openDir(d.name, Identity{})
		if err != nil {
			e.err = err
			if err := fn(e); err != nil && !errors.Is(err, SkipDir) {
				return err
			}
			continue
		}
		subID, err := sub.identity()
		if err == nil {
			sw := &walkDir{h: sub, owned: true}
			err = t.walkDir(sw, child, subID, fn)
			sw.closed.Store(true)
		}
		sub.close()
		if err != nil {
			return err
		}
	}
	return nil
}
