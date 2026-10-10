// Package fsroot reads a repository through descriptors anchored at a pinned
// root, so that what a caller checked is what it reads even while the tree
// changes underneath (time-of-check/time-of-use).
//
// A Tree pins the root once (Pin): the directory the root path leads to at
// that moment, held open; retargeting a root symlink or renaming the
// directory later does not change what the Tree reads. Every access below the
// root is a sequence of single-component operations, each relative to a
// directory handle opened without following symlinks:
//
//   - Resolve follows symlinks by hand — reading each link, splicing its
//     target, starting again from the root — and records the identity of
//     every directory passed and of the object reached. Targets outside the
//     root are refused unless Options.AllowExternalSymlinks.
//   - Open walks the resolved, symlink-free path again without following
//     anything and verifies each identity against the record: it opens the
//     object Resolve checked, or fails with ErrChanged. It never reopens a
//     path that could resolve elsewhere.
//   - Walk enumerates directories through their handles; it never enters a
//     symlink, and an entry's file is opened from the directory handle the
//     entry was listed from.
//
// A failed safe operation is an error; the package never falls back to a
// path-based read. Paths given to and returned by a Tree are "/"-separated
// and relative to the root; Logical turns them into the root as the caller
// spelled it, joined with the relative path.
//
// What is not guaranteed: a rename or hard link that puts another file at a
// checked path between two operations (path-based policies cannot tell it
// from an edit); targets reached through allowed external symlinks, whose
// path outside the root is not pinned; filesystems without stable object
// identities (some network filesystems).
package fsroot

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// maxLinks bounds symlink resolution, as the kernel's own limit does.
const maxLinks = 40

var (
	// ErrClosed: the Tree, or the walk an Entry belongs to, is closed.
	ErrClosed = errors.New("fsroot: closed")
	// ErrInvalidPath: the path is empty, absolute, or not a plain path.
	ErrInvalidPath = errors.New("fsroot: invalid path")
	// ErrOutsideRoot: the path, or a symlink on it, leads outside the root.
	ErrOutsideRoot = errors.New("fsroot: path leads outside the root")
	// ErrSymlinkLoop: too many symlinks were followed.
	ErrSymlinkLoop = errors.New("fsroot: too many levels of symbolic links")
	// ErrSymlink: a symlink where none may be followed (a walk start, an
	// entry opened as a file or directory).
	ErrSymlink = errors.New("fsroot: symbolic link not followed")
	// ErrChanged: the object found is not the object checked — the tree
	// changed between check and use.
	ErrChanged = errors.New("fsroot: file system changed during access")
	// ErrNotRegular: not a regular file (directory, device, pipe, socket).
	ErrNotRegular = errors.New("fsroot: not a regular file")
	// ErrNotDir: not a directory.
	ErrNotDir = errors.New("fsroot: not a directory")
	// ErrDenied: the caller's allow function refused the resolved object.
	ErrDenied = errors.New("fsroot: access denied")
)

// Options configure a Tree.
type Options struct {
	// AllowExternalSymlinks lets symlinks below the root lead outside it
	// (mcp-server --allow-external-symlinks on). The path given is still
	// confined to the root, and the part outside is not pinned.
	AllowExternalSymlinks bool
}

// Identity identifies a file system object for the life of a Tree: device
// and inode on Unix, volume and file index on Windows. The zero Identity is
// the identity of nothing.
type Identity struct {
	dev, ino uint64
	fi       fs.FileInfo // Windows: compared with os.SameFile
}

// Same reports whether two identities are of the same object.
func (a Identity) Same(b Identity) bool {
	if a.fi != nil || b.fi != nil {
		return a.fi != nil && b.fi != nil && os.SameFile(a.fi, b.fi)
	}
	return (a.dev != 0 || a.ino != 0) && a.dev == b.dev && a.ino == b.ino
}

// IsZero reports whether a is the identity of nothing.
func (a Identity) IsZero() bool { return a.fi == nil && a.dev == 0 && a.ino == 0 }

// objInfo is what a single-component lstat reports.
type objInfo struct {
	mode fs.FileMode // type bits only matter
	size int64
	id   Identity
}

// handle is a directory opened without following symlinks. All operations
// name one component relative to it and never follow a symlink in it.
type handle interface {
	// lstat reports name itself (a symlink is reported, not followed).
	lstat(name string) (objInfo, error)
	// openDir opens directory name; a symlink or other type fails. If want
	// is not zero, the directory opened must be want.
	openDir(name string, want Identity) (handle, error)
	// openFile opens regular file name for reading and reports what it
	// opened; a symlink or other type fails. If want is not zero, the file
	// opened must be want.
	openFile(name string, want Identity) (*os.File, objInfo, error)
	readlink(name string) (string, error)
	// readDir lists the directory, sorted by name. Types are hints: every
	// open re-checks the object it finds. owned: no other goroutine reads
	// this handle's entries (a walk's own directories), so its descriptor
	// may be used for listing.
	readDir(owned bool) ([]dirent, error)
	// identity of the directory, taken when it was opened.
	identity() (Identity, error)
	close() error
}

type dirent struct {
	name string
	typ  fs.FileMode
}

// Tree is a pinned repository root. Its methods are safe for concurrent use;
// Close waits for operations in progress, and later operations fail with
// ErrClosed.
type Tree struct {
	logical string // the root as given, absolute and clean
	real    string // its resolved path when pinned: maps absolute link targets
	opts    Options

	mu   sync.RWMutex
	root handle // nil once closed
}

// Pin resolves logicalRoot once and opens the directory it leads to.
func Pin(logicalRoot string, opts Options) (*Tree, error) {
	if logicalRoot == "" {
		return nil, ErrInvalidPath
	}
	abs, err := filepath.Abs(logicalRoot)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	h, err := openRootDir(real)
	if err != nil {
		return nil, err
	}
	// Bind the resolved name to the directory opened: if the path was
	// switched between resolving and opening, fail rather than map absolute
	// link targets with the wrong name.
	id, err := h.identity()
	if err == nil {
		var cur Identity
		cur, err = pathIdentity(real)
		if err == nil && !id.Same(cur) {
			err = ErrChanged
		}
	}
	if err != nil {
		h.close()
		return nil, err
	}
	return &Tree{logical: abs, real: real, opts: opts, root: h}, nil
}

// Close releases the root. It waits for operations in progress.
func (t *Tree) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.root == nil {
		return ErrClosed
	}
	err := t.root.close()
	t.root = nil
	return err
}

// acquire returns the root for one operation; release must be called.
func (t *Tree) acquire() (handle, func(), error) {
	t.mu.RLock()
	if t.root == nil {
		t.mu.RUnlock()
		return nil, nil, ErrClosed
	}
	return t.root, t.mu.RUnlock, nil
}

// Logical returns rel (root-relative, "/"-separated) under the root as the
// caller spelled it.
func (t *Tree) Logical(rel string) string {
	if rel == "" || rel == "." {
		return t.logical
	}
	return filepath.Join(t.logical, filepath.FromSlash(rel))
}

// Root returns the root as the caller spelled it (absolute and clean).
func (t *Tree) Root() string { return t.logical }

// splitRel validates a root-relative path and returns its components (none
// for the root itself). The path is cleaned lexically first, as Ark's path
// gate does: "a/../b" is "b", and a path that leaves the root lexically is
// refused.
func splitRel(rel string) (clean string, comps []string, err error) {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", nil, ErrInvalidPath
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.HasPrefix(filepath.ToSlash(rel), "/") {
		return "", nil, ErrInvalidPath
	}
	clean = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", nil, ErrOutsideRoot
	}
	if clean == "." {
		return ".", nil, nil
	}
	comps = strings.Split(clean, "/")
	if runtime.GOOS == "windows" {
		for _, c := range comps {
			// Alternate data streams and drive-relative names are not
			// plain paths.
			if strings.ContainsRune(c, ':') {
				return "", nil, ErrInvalidPath
			}
		}
	}
	return clean, comps, nil
}

// Resolved is a path resolved by Resolve: what it names and the identities
// that tie it to the objects checked. Only the Tree that made it can open it.
type Resolved struct {
	tree     *Tree
	logical  string     // the path as asked for, cleaned
	real     []string   // symlink-free components below the root (nil: external)
	dirIDs   []Identity // identity of each directory of real, root excluded
	external string     // absolute path outside the root, reached through allowed links
	links    []string   // root-relative real paths of the symlinks followed, in order
	linkDirs []DirRef   // the directory each of those symlinks was read from
	info     objInfo    // the object reached
}

// Logical returns the path as asked for (cleaned, root-relative).
func (r Resolved) Logical() string { return r.logical }

// Real returns the symlink-free root-relative path of the object, or "" if
// it lies outside the root.
func (r Resolved) Real() string {
	if r.real == nil {
		return ""
	}
	if len(r.real) == 0 {
		return "."
	}
	return strings.Join(r.real, "/")
}

// External returns the absolute path of an object outside the root reached
// through allowed external symlinks, or "".
func (r Resolved) External() string { return r.external }

// Links returns the root-relative real paths of the symlinks followed.
func (r Resolved) Links() []string { return append([]string(nil), r.links...) }

// IsDir and IsRegular report the object's type when resolved.
func (r Resolved) IsDir() bool     { return r.info.mode.IsDir() }
func (r Resolved) IsRegular() bool { return r.info.mode.IsRegular() }

// Size is the object's size when resolved.
func (r Resolved) Size() int64 { return r.info.size }

// Identity is the identity of the object reached.
func (r Resolved) Identity() Identity { return r.info.id }

// From reports whether r was resolved by t.
func (r Resolved) From(t *Tree) bool { return r.tree != nil && r.tree == t }

// DirRef is a directory below or at the root and its identity.
type DirRef struct {
	Rel string // root-relative, "/"-separated; "." for the root
	ID  Identity
}

// Dirs returns every directory the resolution passed through, with the
// identity it had: the directories containing each symlink followed, the
// directories of the real path (not the root) and, for a directory, the
// directory itself. A caller that read rules from directories can verify
// that these are the directories it read them from.
func (r Resolved) Dirs() []DirRef {
	out := append([]DirRef(nil), r.linkDirs...)
	n := len(r.real)
	if r.real == nil {
		return out
	}
	if !r.info.mode.IsDir() {
		n--
	}
	for i := 0; i < n && i < len(r.dirIDs); i++ {
		out = append(out, DirRef{Rel: strings.Join(r.real[:i+1], "/"), ID: r.dirIDs[i]})
	}
	if r.info.mode.IsDir() && len(r.real) > 0 && len(r.dirIDs) < len(r.real) {
		out = append(out, DirRef{Rel: strings.Join(r.real, "/"), ID: r.info.id})
	}
	return out
}

func relOf(comps []string) string {
	if len(comps) == 0 {
		return "."
	}
	return strings.Join(comps, "/")
}

// Resolve resolves rel, following symlinks below the root by hand.
func (t *Tree) Resolve(rel string) (Resolved, error) {
	r, _, err := t.resolve(rel, false)
	return r, err
}

// resolve resolves rel; with open, a regular file reached is also opened —
// from the directory handle it was checked in, verified against the
// identity checked — and returned.
func (t *Tree) resolve(rel string, open bool) (Resolved, *os.File, error) {
	root, release, err := t.acquire()
	if err != nil {
		return Resolved{}, nil, err
	}
	defer release()
	clean, comps, err := splitRel(rel)
	if err != nil {
		return Resolved{}, nil, err
	}
	res := Resolved{tree: t, logical: clean}
	if len(comps) == 0 {
		id, err := root.identity()
		if err != nil {
			return Resolved{}, nil, err
		}
		res.real = []string{}
		res.info = objInfo{mode: fs.ModeDir, id: id}
		return res, nil, nil
	}
	external := func(abs string) (Resolved, *os.File, error) {
		r, err := t.resolveExternal(res, abs)
		if err != nil || !open || !r.info.mode.IsRegular() {
			return r, nil, err
		}
		f, err := openExternal(r)
		return r, f, err
	}

	var held []handle // directory handles below the root, closed on return
	defer func() { closeAll(held) }()
	cur := root
	var real []string // components of cur below the root
	var ids []Identity
	reanchor := func(to []string) error {
		closeAll(held)
		held, ids, cur, real = nil, nil, root, nil
		for _, c := range to {
			next, err := cur.openDir(c, Identity{})
			if err != nil {
				return err
			}
			id, err := next.identity()
			if err != nil {
				next.close()
				return err
			}
			held = append(held, next)
			ids = append(ids, id)
			cur, real = next, append(real, c)
		}
		return nil
	}

	links := 0
	for i := 0; i < len(comps); {
		c := comps[i]
		switch c {
		case "", ".":
			i++
			continue
		case "..":
			// Only a symlink target brings ".." here; it means the physical
			// parent of the directory reached so far.
			if len(real) == 0 {
				return external(filepath.Join(append([]string{t.real, ".."}, comps[i+1:]...)...))
			}
			if err := reanchor(real[:len(real)-1]); err != nil {
				return Resolved{}, nil, err
			}
			i++
			continue
		}
		if i < len(comps)-1 {
			// A directory on the way: opened without following; only if
			// that fails is it examined (a symlink to follow by hand).
			if next, err := cur.openDir(c, Identity{}); err == nil {
				id, err := next.identity()
				if err != nil {
					next.close()
					return Resolved{}, nil, err
				}
				held = append(held, next)
				ids = append(ids, id)
				cur, real = next, append(real, c)
				i++
				continue
			}
		}
		info, err := cur.lstat(c)
		if err != nil {
			return Resolved{}, nil, err
		}
		if info.mode&fs.ModeSymlink != 0 {
			if links++; links > maxLinks {
				return Resolved{}, nil, ErrSymlinkLoop
			}
			target, err := cur.readlink(c)
			if err != nil {
				return Resolved{}, nil, err
			}
			res.links = append(res.links, strings.Join(append(append([]string{}, real...), c), "/"))
			dirID, err := cur.identity()
			if err != nil {
				return Resolved{}, nil, err
			}
			res.linkDirs = append(res.linkDirs, DirRef{Rel: relOf(real), ID: dirID})
			rest := comps[i+1:]
			var next []string
			if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
				inside, ok := t.rootRelative(target)
				if !ok {
					return external(filepath.Join(append([]string{target}, rest...)...))
				}
				next = inside
			} else {
				next = append(append([]string{}, real...), strings.Split(filepath.ToSlash(target), "/")...)
			}
			comps = append(next, rest...)
			i = 0
			if err := reanchor(nil); err != nil {
				return Resolved{}, nil, err
			}
			continue
		}
		if i < len(comps)-1 {
			if !info.mode.IsDir() {
				return Resolved{}, nil, &fs.PathError{Op: "resolve", Path: clean, Err: ErrNotDir}
			}
			// It is a directory, yet opening it failed: it changed.
			return Resolved{}, nil, &fs.PathError{Op: "resolve", Path: clean, Err: ErrChanged}
		}
		res.real = append(append([]string{}, real...), c)
		res.dirIDs = append([]Identity(nil), ids...)
		res.info = info
		if !open || !info.mode.IsRegular() {
			return res, nil, nil
		}
		f, oi, err := cur.openFile(c, info.id)
		if err != nil {
			return Resolved{}, nil, &fs.PathError{Op: "open", Path: clean, Err: err}
		}
		res.info.size = oi.size
		return res, f, nil
	}
	// The path ended at a directory reached through a symlink or "..".
	id, err := cur.identity()
	if err != nil {
		return Resolved{}, nil, err
	}
	res.real = append([]string{}, real...)
	if len(ids) > 0 {
		res.dirIDs = append([]Identity(nil), ids[:len(ids)-1]...)
	}
	res.info = objInfo{mode: fs.ModeDir, id: id}
	return res, nil, nil
}

// resolveExternal finishes a resolution that left the root through a
// symlink: refused unless allowed; the outside path is not pinned.
func (t *Tree) resolveExternal(res Resolved, abs string) (Resolved, error) {
	if !t.opts.AllowExternalSymlinks {
		return Resolved{}, &fs.PathError{Op: "resolve", Path: res.logical, Err: ErrOutsideRoot}
	}
	abs = filepath.Clean(abs)
	fi, err := os.Stat(abs)
	if err != nil {
		return Resolved{}, err
	}
	id, err := pathIdentity(abs)
	if err != nil {
		return Resolved{}, err
	}
	res.real = nil
	res.external = abs
	res.info = objInfo{mode: fi.Mode() & fs.ModeType, size: fi.Size(), id: id}
	return res, nil
}

// rootRelative maps an absolute symlink target to components below the root.
// It computes a name only: the name is walked again through handles.
func (t *Tree) rootRelative(target string) ([]string, bool) {
	cands := []string{filepath.Clean(target)}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(target)); err == nil {
		cands = append(cands, filepath.Join(dir, filepath.Base(target)))
	}
	for _, c := range cands {
		for _, root := range []string{t.real, t.logical} {
			rel, err := filepath.Rel(root, c)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if rel == "." {
				return []string{}, true
			}
			return strings.Split(filepath.ToSlash(rel), "/"), true
		}
	}
	return nil, false
}

// Open opens the regular file r names: the object Resolve checked, reached
// again without following anything, or ErrChanged. Directories are opened
// with OpenDir.
func (t *Tree) Open(r Resolved) (*os.File, error) {
	if r.tree != t {
		return nil, ErrInvalidPath
	}
	if !r.info.mode.IsRegular() {
		return nil, &fs.PathError{Op: "open", Path: r.logical, Err: ErrNotRegular}
	}
	root, release, err := t.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	if r.external != "" {
		return openExternal(r)
	}
	dir, held, err := t.walkDirs(root, r.real[:len(r.real)-1], r.dirIDs)
	if err != nil {
		return nil, err
	}
	defer closeAll(held)
	f, _, err := dir.openFile(r.real[len(r.real)-1], r.info.id)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: r.logical, Err: err}
	}
	return f, nil
}

// walkDirs opens the directories comps below root without following
// anything, each verified against ids when given.
func (t *Tree) walkDirs(root handle, comps []string, ids []Identity) (handle, []handle, error) {
	cur := root
	var held []handle
	for i, c := range comps {
		var want Identity
		if i < len(ids) {
			want = ids[i]
		}
		next, err := cur.openDir(c, want)
		if err != nil {
			closeAll(held)
			return nil, nil, err
		}
		held = append(held, next)
		cur = next
	}
	return cur, held, nil
}

// openExternal opens a file outside the root reached through allowed
// external symlinks, verified against the identity seen when resolved.
func openExternal(r Resolved) (*os.File, error) {
	f, err := openNoBlock(r.external)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, &fs.PathError{Op: "open", Path: r.logical, Err: ErrNotRegular}
	}
	id, err := fileIdentity(f, fi)
	if err != nil || !id.Same(r.info.id) {
		f.Close()
		return nil, &fs.PathError{Op: "open", Path: r.logical, Err: ErrChanged}
	}
	return f, nil
}

// ReadFile resolves rel, lets allow decide on what it resolved to (nil
// allows everything), and reads the object checked.
//
// The file is opened in the same pass that checked it, from the directory
// handle it was found in, before allow runs; nothing is read unless allow
// agrees.
func (t *Tree) ReadFile(rel string, allow func(Resolved) bool) ([]byte, error) {
	var check func(Resolved) error
	if allow != nil {
		check = func(r Resolved) error {
			if !allow(r) {
				return ErrDenied
			}
			return nil
		}
	}
	return t.ReadFileChecked(rel, check)
}

// ReadFileChecked is ReadFile with a check that can tell why it refuses: its
// error is returned (wrapped) and nothing is read. A nil check allows.
func (t *Tree) ReadFileChecked(rel string, check func(Resolved) error) ([]byte, error) {
	r, f, err := t.resolve(rel, true)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, &fs.PathError{Op: "read", Path: r.logical, Err: ErrNotRegular}
	}
	defer f.Close()
	if check != nil {
		if err := check(r); err != nil {
			return nil, &fs.PathError{Op: "read", Path: r.logical, Err: err}
		}
	}
	return readAllSized(f, r.info.size)
}

// readAllSized reads f to the end, sized for an expected length.
func readAllSized(f *os.File, size int64) ([]byte, error) {
	if size <= 0 || size > 1<<30 {
		return io.ReadAll(f)
	}
	buf := make([]byte, 0, size+1)
	for {
		n, err := f.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return buf, err
		}
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
	}
}

// DirIdentity returns the identity of directory rel, reached without
// following any symlink (a symlink on the way is ErrSymlink).
func (t *Tree) DirIdentity(rel string) (Identity, error) {
	root, release, err := t.acquire()
	if err != nil {
		return Identity{}, err
	}
	defer release()
	_, comps, err := splitRel(rel)
	if err != nil {
		return Identity{}, err
	}
	dir, held, err := t.strictDirs(root, comps)
	if err != nil {
		return Identity{}, err
	}
	defer closeAll(held)
	return dir.identity()
}

// strictDirs opens comps below root as directories; a symlink is ErrSymlink.
func (t *Tree) strictDirs(root handle, comps []string) (handle, []handle, error) {
	cur := root
	var held []handle
	for _, c := range comps {
		info, err := cur.lstat(c)
		if err == nil && info.mode&fs.ModeSymlink != 0 {
			err = ErrSymlink
		} else if err == nil && !info.mode.IsDir() {
			err = ErrNotDir
		}
		var next handle
		if err == nil {
			next, err = cur.openDir(c, info.id)
		}
		if err != nil {
			closeAll(held)
			return nil, nil, &fs.PathError{Op: "open", Path: c, Err: err}
		}
		held = append(held, next)
		cur = next
	}
	return cur, held, nil
}

func closeAll(hs []handle) {
	for i := len(hs) - 1; i >= 0; i-- {
		hs[i].close()
	}
}
