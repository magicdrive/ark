package fsroot

import (
	"io/fs"
	"strings"
)

// Info describes the entry itself (not followed), from the directory handle
// it was listed through: the object at its name now. The start of a walk is
// described through its own handle.
func (e Entry) Info() (fs.FileInfo, error) {
	var fi fs.FileInfo
	err := e.dir.use(func(h handle) error {
		var err error
		if e.name == "" || e.rel == chainRel(e.dir.chain) {
			fi, err = h.stat()
			if err == nil {
				fi = renamed{fi, e.name}
			}
			return err
		}
		fi, err = h.lstatInfo(e.name)
		return err
	})
	if err != nil {
		return nil, &fs.PathError{Op: "lstat", Path: e.rel, Err: err}
	}
	return fi, nil
}

// renamed is a FileInfo under another name (a walk start keeps the name it
// was listed under, as filepath.Walk reports it).
type renamed struct {
	fs.FileInfo
	name string
}

func (r renamed) Name() string {
	if r.name == "" {
		return r.FileInfo.Name()
	}
	return r.name
}

// Describe is Info and, for a regular file, Resolved, from one lstat of the
// entry: ok reports whether r is set.
func (e Entry) Describe() (fi fs.FileInfo, r Resolved, ok bool, err error) {
	if e.name == "" || e.rel == chainRel(e.dir.chain) || e.IsDir() || e.IsSymlink() {
		fi, err = e.Info()
		return fi, Resolved{}, false, err
	}
	var info objInfo
	err = e.dir.use(func(h handle) error {
		var err error
		fi, info, err = h.lstatBoth(e.name)
		return err
	})
	if err != nil {
		return nil, Resolved{}, false, &fs.PathError{Op: "lstat", Path: e.rel, Err: err}
	}
	if !info.mode.IsRegular() {
		return fi, Resolved{}, false, nil
	}
	r, rerr := e.resolvedFrom(info)
	return fi, r, rerr == nil, nil
}

// Resolved returns the resolution of a regular-file entry as its walk
// reached it: the directories from the root as the walk opened them, and the
// object at its name now, checked to be a regular file. The walk need not be
// running when it is used: Tree.Open (and accesspolicy's ReadResolved)
// reaches the file again through handles, verifying every identity — the
// object read later is the object resolved now, or nothing is.
func (e Entry) Resolved() (Resolved, error) {
	if e.IsDir() {
		return Resolved{}, &fs.PathError{Op: "resolve", Path: e.rel, Err: ErrNotRegular}
	}
	if e.IsSymlink() {
		return Resolved{}, &fs.PathError{Op: "resolve", Path: e.rel, Err: ErrSymlink}
	}
	var info objInfo
	err := e.dir.use(func(h handle) error {
		var err error
		info, err = h.lstat(e.name)
		if err != nil {
			return err
		}
		if info.mode&fs.ModeSymlink != 0 {
			return ErrSymlink
		}
		if !info.mode.IsRegular() {
			return ErrNotRegular
		}
		return nil
	})
	if err != nil {
		return Resolved{}, &fs.PathError{Op: "resolve", Path: e.rel, Err: err}
	}
	return e.resolvedFrom(info)
}

func (e Entry) resolvedFrom(info objInfo) (Resolved, error) {
	comps := strings.Split(e.rel, "/")
	chain := e.dir.chain
	if len(chain) != len(comps) {
		return Resolved{}, &fs.PathError{Op: "resolve", Path: e.rel, Err: ErrChanged}
	}
	ids := make([]Identity, 0, len(chain)-1)
	for _, d := range chain[1:] {
		ids = append(ids, d.ID)
	}
	return Resolved{
		tree:    e.dir.tree,
		logical: e.rel,
		canon:   comps,
		real:    comps,
		dirIDs:  ids,
		info:    info,
	}, nil
}

// Lstat describes rel itself (a symlink is not followed), reached from the
// root without following symlinks on the way.
func (t *Tree) Lstat(rel string) (fs.FileInfo, error) {
	root, release, err := t.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	_, comps, err := splitRel(rel)
	if err != nil {
		return nil, err
	}
	if len(comps) == 0 {
		return root.stat()
	}
	dir, held, names, err := t.strictDirs(root, comps[:len(comps)-1])
	if err != nil {
		return nil, err
	}
	defer closeAll(held)
	_ = names
	return dir.lstatInfo(comps[len(comps)-1])
}

// IsStart reports whether e is the start of its walk (reported through its
// own handle, not listed from a directory the walk holds).
func (e Entry) IsStart() bool {
	return e.dir != nil && (e.name == "" || e.rel == chainRel(e.dir.chain))
}
