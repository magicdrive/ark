package fsroot

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
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
	tree  *Tree
	chain []DirRef // the directories from the root to this one, as opened
	owned bool     // the walk opened h: no one else lists it

	mu     sync.RWMutex // held for reading by entry operations, for writing by close
	h      handle
	closed bool
}

// use runs f with the directory's handle, or fails with ErrClosed once the
// callback its entry was given to has returned — never on a closed handle.
func (w *walkDir) use(f func(h handle) error) error {
	if w == nil {
		return ErrClosed
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrClosed
	}
	return f(w.h)
}

// shutdown ends the entries' use of the handle (and closes it if owned).
func (w *walkDir) shutdown(closeHandle bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if closeHandle {
		w.h.close()
	}
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

// From reports whether the entry was produced by a walk of t.
func (e Entry) From(t *Tree) bool { return e.dir != nil && e.dir.tree == t }

// Valid reports whether the entry may still be used (its callback runs).
func (e Entry) Valid() bool {
	return e.dir.use(func(handle) error { return nil }) == nil
}

// Dirs returns the directories containing the entry, from the root to its
// parent, each with the identity of the directory the walk opened — the
// handles it was listed through. (For the walk's start: its ancestors.)
func (e Entry) Dirs() []DirRef {
	if e.dir == nil {
		return nil
	}
	chain := e.dir.chain
	if e.name == "" || e.rel == chainRel(chain) {
		// The start entry: listed from itself.
		chain = chain[:len(chain)-1]
	}
	return append([]DirRef(nil), chain...)
}

// EachDir calls fn for each directory Dirs returns, without copying them;
// it stops at fn's first error.
func (e Entry) EachDir(fn func(DirRef) error) error {
	if e.dir == nil {
		return nil
	}
	chain := e.dir.chain
	if e.name == "" || e.rel == chainRel(chain) {
		chain = chain[:len(chain)-1]
	}
	for _, d := range chain {
		if err := fn(d); err != nil {
			return err
		}
	}
	return nil
}

func chainRel(chain []DirRef) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1].Rel
}

// Identity returns the identity of the entry itself (not followed).
func (e Entry) Identity() (Identity, error) {
	if e.rel == "." || e.name == "" {
		return e.dirID, nil
	}
	var id Identity
	err := e.dir.use(func(h handle) error {
		info, err := h.lstat(e.name)
		id = info.id
		return err
	})
	return id, err
}

// Open opens the entry as a regular file, from the directory handle it was
// listed from, without following a symlink: the file currently at that name
// in that directory, or an error.
func (e Entry) Open() (*os.File, error) {
	if e.IsSymlink() {
		return nil, &fs.PathError{Op: "open", Path: e.rel, Err: ErrSymlink}
	}
	var f *os.File
	err := e.dir.use(func(h handle) error {
		var err error
		f, _, err = h.openFile(e.name, Identity{})
		return err
	})
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: e.rel, Err: err}
	}
	return f, nil
}

// ReadChecked reads the entry as a regular file in one operation: the object
// at the entry's name is examined from the directory handle it was listed
// through (lstat, never following), check decides on its identity, and that
// very object is opened (same identity, no symlink followed) and read. If
// check refuses, its error is returned and nothing is opened; if the object
// changes, ErrChanged; a read error returns no bytes.
func (e Entry) ReadChecked(check func(Identity) error) ([]byte, error) {
	if e.IsSymlink() {
		return nil, &fs.PathError{Op: "read", Path: e.rel, Err: ErrSymlink}
	}
	var out []byte
	err := e.dir.use(func(h handle) error {
		info, err := h.lstat(e.name)
		if err != nil {
			return err
		}
		if info.mode&fs.ModeSymlink != 0 {
			return ErrSymlink // listed as a file, a symlink now
		}
		if !info.mode.IsRegular() {
			return ErrNotRegular
		}
		if check != nil {
			if err := check(info.id); err != nil {
				return err
			}
		}
		f, oi, err := h.openFile(e.name, info.id)
		if err != nil {
			return err
		}
		defer f.Close()
		out, err = readAllSized(f, oi.size)
		return err
	})
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: e.rel, Err: err}
	}
	return out, nil
}

// ReadAll reads the entry as a regular file (Open) and returns its content
// and the identity of the object read.
func (e Entry) ReadAll() ([]byte, Identity, error) {
	if e.IsSymlink() {
		return nil, Identity{}, &fs.PathError{Op: "open", Path: e.rel, Err: ErrSymlink}
	}
	var b []byte
	var id Identity
	err := e.dir.use(func(h handle) error {
		f, info, err := h.openFile(e.name, Identity{})
		if err != nil {
			return err
		}
		defer f.Close()
		b, err = readAllSized(f, info.size)
		id = info.id
		return err
	})
	if err != nil {
		return nil, Identity{}, &fs.PathError{Op: "open", Path: e.rel, Err: err}
	}
	return b, id, nil
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
	// Entries are named from the start as its directories list it, so a
	// start spelled in another case yields the names policies match.
	dir, held, comps, err := t.strictDirs(root, comps)
	if err != nil {
		return err
	}
	if len(comps) > 0 {
		clean = strings.Join(comps, "/")
	}
	// The chain of directories from the root to the start, as opened.
	rootID, err := root.identity()
	if err != nil {
		closeAll(held)
		return err
	}
	chain := []DirRef{{Rel: ".", ID: rootID}}
	for i, h := range held {
		id, err := h.identity()
		if err != nil {
			closeAll(held)
			return err
		}
		chain = append(chain, DirRef{Rel: strings.Join(comps[:i+1], "/"), ID: id})
	}
	// The start handle is owned by the walk unless it is the root.
	if len(held) > 0 {
		closeAll(held[:len(held)-1])
	}
	id := chain[len(chain)-1].ID
	name := ""
	if len(comps) > 0 {
		name = comps[len(comps)-1]
	}
	w := &walkDir{tree: t, chain: chain, h: dir, owned: dir != root}
	err = fn(Entry{rel: clean, name: name, typ: fs.ModeDir, dir: w, dirID: id})
	if err == nil {
		err = t.walkDir(w, clean, id, fn)
	}
	w.shutdown(dir != root)
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
		if err != nil {
			sub.close()
			return err
		}
		chain := append(append([]DirRef(nil), w.chain...), DirRef{Rel: child, ID: subID})
		sw := &walkDir{tree: t, chain: chain, h: sub, owned: true}
		err = t.walkDir(sw, child, subID, fn)
		sw.shutdown(true)
		if err != nil {
			return err
		}
	}
	return nil
}
