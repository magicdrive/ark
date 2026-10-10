package fsroot

import (
	"io/fs"
	"os"
)

// StatChecked resolves rel, lets check decide on what it resolved to (nil
// allows), and describes the object checked: a regular file through the
// descriptor it was opened by in the same pass, a directory through a handle
// reached again without following anything and verified against the
// identity checked. Nothing is described unless check agrees. Other types
// (devices, pipes, sockets) are ErrNotRegular.
func (t *Tree) StatChecked(rel string, check func(Resolved) error) (fs.FileInfo, error) {
	r, f, err := t.resolve(rel, true)
	if err != nil {
		return nil, err
	}
	if f != nil {
		defer f.Close()
	}
	if check != nil {
		if err := check(r); err != nil {
			return nil, &fs.PathError{Op: "stat", Path: r.logical, Err: err}
		}
	}
	if f != nil {
		return f.Stat()
	}
	return t.statResolved(r)
}

// StatResolved describes the object r names (resolved by t) as StatChecked
// does, without resolving it again: a regular file is opened by Open, a
// directory reached through handles, each verified against the identities r
// recorded.
func (t *Tree) StatResolved(r Resolved) (fs.FileInfo, error) {
	if r.tree != t {
		return nil, ErrInvalidPath
	}
	if r.info.mode.IsRegular() {
		f, err := t.Open(r)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return f.Stat()
	}
	return t.statResolved(r)
}

func (t *Tree) statResolved(r Resolved) (fs.FileInfo, error) {
	switch {
	case !r.info.mode.IsDir():
		return nil, &fs.PathError{Op: "stat", Path: r.logical, Err: ErrNotRegular}
	case r.external != "":
		fi, err := os.Stat(r.external)
		if err != nil {
			return nil, err
		}
		id, err := pathIdentity(r.external)
		if err != nil || !id.Same(r.info.id) {
			return nil, &fs.PathError{Op: "stat", Path: r.logical, Err: ErrChanged}
		}
		return fi, nil
	}
	root, release, err := t.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	dir := root
	if len(r.real) > 0 {
		parent, held, err := t.walkDirs(root, r.real[:len(r.real)-1], r.dirIDs)
		if err != nil {
			return nil, err
		}
		defer closeAll(held)
		d, err := parent.openDir(r.real[len(r.real)-1], r.info.id)
		if err != nil {
			return nil, &fs.PathError{Op: "stat", Path: r.logical, Err: err}
		}
		defer d.close()
		dir = d
	}
	id, err := dir.identity()
	if err != nil || !id.Same(r.info.id) {
		return nil, &fs.PathError{Op: "stat", Path: r.logical, Err: ErrChanged}
	}
	return dir.stat()
}
