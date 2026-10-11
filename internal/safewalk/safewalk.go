// Package safewalk is how Ark's file walks, listings and reads reach the
// repository when they must not be raced: through a pinned tree (fsroot) and
// a policy snapshot over it (accesspolicy). It offers the shapes Ark's walk
// code already has — filepath.Walk, filepath.WalkDir, os.ReadDir,
// os.ReadFile, os.Stat — so the MCP file tools, the repository index and the
// dump keep their logic and output, with one difference: what they see and
// read is decided and reached the safe way.
//
//   - Walks list directories through their handles (no symlink is entered,
//     names are as the directories list them) and decide every entry before
//     the caller sees it: an entry the policy refuses is not reported (a
//     directory is not entered), so neither its content nor its name gets
//     out. An entry that cannot be decided — the tree changed under the
//     walk, a rule file that cannot be read — is not reported either.
//   - A regular file a walk or a listing reported is read later through the
//     resolution the walk made (accesspolicy.Snapshot.ReadResolved): the
//     directories and the file are reached again through handles and
//     verified identity by identity, so the object read is the object the
//     walk decided, or nothing is read. Any other path is resolved, decided
//     and read in one operation (Snapshot.ReadFile). Nothing is read by
//     path.
//   - Special files (FIFOs, sockets, devices) are never opened for reading:
//     a read refuses them before opening anything that could block.
//
// Paths are given and reported in the caller's spelling: below the root as
// given (or its symlink-resolved form), as filepath.Walk would report them
// from the same start. A Walker is used by one goroutine at a time.
package safewalk

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/magicdrive/ark/internal/accesspolicy"
	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/libgitignore"
)

// hook, when set by a test (SetHook), runs at points where a concurrent
// writer aims: "decide" before an entry a walk listed is decided, "read"
// before a file is read. Nil in production.
var hook func(event, rel string)

// SetHook installs fn as the test hook and returns the function that
// removes it. For tests only.
func SetHook(fn func(event, rel string)) (restore func()) {
	hook = fn
	return func() { hook = nil }
}

func at(event, rel string) {
	if hook != nil {
		hook(event, rel)
	}
}

// ErrOutside: the path is not below the walker's root.
var ErrOutside = errors.New("safewalk: path is not below the root")

// Walker walks and reads one pinned tree under one policy snapshot.
type Walker struct {
	tree  *fsroot.Tree
	snap  *accesspolicy.Snapshot
	roots []string // the root as given, and its symlink-resolved form

	// extra, when set, is a further rule (the dump's .gitignore rule): a
	// path it matches, or below a directory it matches, is refused too.
	extra *libgitignore.GitIgnore

	mu   sync.Mutex
	seen map[string]fsroot.Resolved // regular files reported, by root-relative path
	// dirs are the directories reported, by root-relative path: the chain
	// of directories (with identities) a later listing reaches them by
	// (fsroot.Tree.WalkVerified), with no name looked up again.
	dirs map[string][]fsroot.DirRef

	// cur is the entry a walk is reporting, while its callback runs
	// (under the caller's path for it): a read of it goes through the
	// directory handle it was listed from.
	cur     *fsroot.Entry
	curPath string
	// curID is the identity cur's examination (Describe) found, when it
	// is a regular file: the object a read of cur must reach.
	curID fsroot.Identity
}

// New returns a walker of snap's tree. root is the root as callers spell it
// (absolute, or relative to the working directory as they use it).
func New(snap *accesspolicy.Snapshot, root string) *Walker {
	tree := snap.Tree()
	roots := []string{filepath.Clean(root)}
	if abs := tree.Root(); abs != roots[0] {
		roots = append(roots, abs)
	}
	if real, err := filepath.EvalSymlinks(tree.Root()); err == nil && real != tree.Root() {
		roots = append(roots, real)
	}
	return &Walker{tree: tree, snap: snap, roots: roots, seen: map[string]fsroot.Resolved{}, dirs: map[string][]fsroot.DirRef{}}
}

// WithRule adds rule to the walker's decisions (the dump's .gitignore rule,
// compiled from the snapshot's captured rule files). It returns w.
func (w *Walker) WithRule(rule *libgitignore.GitIgnore) *Walker {
	w.extra = rule
	return w
}

// rel returns path relative to the root, "/"-separated.
func (w *Walker) rel(path string) (string, error) {
	p := filepath.Clean(path)
	for _, root := range w.roots {
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if filepath.IsAbs(root) != filepath.IsAbs(p) {
			continue
		}
		return filepath.ToSlash(rel), nil
	}
	return "", &fs.PathError{Op: "walk", Path: path, Err: ErrOutside}
}

// refused reports why the entry e must not be reported, or nil.
func (w *Walker) refused(e fsroot.Entry) error {
	if err := w.snap.CheckEntry(e); err != nil {
		return err
	}
	if w.extra != nil && e.Rel() != "." && matchesWithParents(w.extra, e.Rel()) {
		return accesspolicy.ErrExcluded
	}
	if w.extra != nil && e.IsSymlink() {
		// Where the link leads, inside the root.
		if r, err := w.tree.Resolve(e.Rel()); err == nil && r.Real() != "" && r.Real() != "." && matchesWithParents(w.extra, r.Real()) {
			return accesspolicy.ErrExcluded
		}
	}
	return nil
}

func matchesWithParents(rule *libgitignore.GitIgnore, rel string) bool {
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' && rule.MatchesRel(rel[:i]) {
			return true
		}
	}
	return rule.MatchesRel(rel)
}

// remember records a regular file reported, for a later read.
func (w *Walker) remember(e fsroot.Entry) {
	if e.IsDir() || e.IsSymlink() || !e.Type().IsRegular() {
		return
	}
	r, err := e.Resolved()
	if err != nil {
		return
	}
	w.mu.Lock()
	w.seen[e.Rel()] = r
	w.mu.Unlock()
}

// rememberDir records a directory entry reported.
func (w *Walker) rememberDir(e fsroot.Entry) {
	if !e.IsDir() || e.IsSymlink() {
		return
	}
	id, err := e.Identity()
	if err != nil {
		return
	}
	chain := append(e.Dirs()[1:], fsroot.DirRef{Rel: e.Rel(), ID: id})
	w.mu.Lock()
	w.dirs[e.Rel()] = chain
	w.mu.Unlock()
}

// visit is one reported entry: its path in the caller's spelling and what
// describes it.
type visit struct {
	path string
	info fs.FileInfo
	e    fsroot.Entry
}

// walk walks start, deciding every entry, and calls fn for each entry
// reported; fn's SkipDir and SkipAll mean what they mean to filepath.Walk.
func (w *Walker) walk(start string, fn func(v visit, err error) error) error {
	rel, err := w.rel(start)
	if err != nil {
		return fn(visit{path: start}, err)
	}
	// The tree path walked: rel, or — when a symlink on the way leads
	// elsewhere, which the path API follows — where it leads.
	base := rel
	report := func(r string) string {
		if r == base {
			return start
		}
		sub := r
		if base != "." {
			sub = strings.TrimPrefix(r, base+"/")
		}
		return filepath.Join(start, filepath.FromSlash(sub))
	}
	if rel != "." {
		// A start that is not a plain directory is reported alone, as
		// filepath.Walk reports it (a symlink is not entered).
		fi, err := w.tree.Lstat(rel)
		if errors.Is(err, fsroot.ErrSymlink) {
			// A symlink on the way: the path API follows it to the start's
			// parent. Inside the root, the walk goes there; outside (an
			// allowed external link), by path, as before — the outside is
			// not pinned, and reads still go through the tree.
			parent, name := parentOf(rel), rel[strings.LastIndexByte(rel, '/')+1:]
			pr, perr := w.tree.Resolve(parent)
			switch {
			case perr != nil:
				err = perr
			case pr.External() != "":
				return filepath.Walk(start, func(p string, info fs.FileInfo, err error) error {
					return fn(visit{path: p, info: info}, err)
				})
			case !pr.IsDir():
				err = fsroot.ErrNotDir
			default:
				base = joinRel(pr.Real(), name)
				fi, err = w.tree.Lstat(base)
			}
		}
		if err != nil {
			return fn(visit{path: start}, err)
		}
		if !fi.IsDir() {
			r, err := w.tree.Resolve(rel)
			switch {
			case unreachable(err):
				// A dangling or looping link: decided by its own name.
				err = w.decideName(rel)
			case err == nil:
				err = w.check(r)
			}
			if err != nil {
				return nil // refused: nothing reported
			}
			err = fn(visit{path: start, info: fi}, nil)
			if errors.Is(err, filepath.SkipDir) || errors.Is(err, filepath.SkipAll) {
				return nil
			}
			return err
		}
	}
	err = w.tree.Walk(base, func(e fsroot.Entry) error {
		if e.Err() != nil {
			return mapSkip(fn(visit{path: report(e.Rel())}, e.Err()))
		}
		at("decide", e.Rel())
		if err := w.refused(e); err != nil {
			if e.IsDir() {
				return fsroot.SkipDir
			}
			return nil
		}
		info, r, ok, err := e.Describe()
		if err != nil || info.Mode().Type() != e.Type().Type() {
			// Gone, or no longer what was listed: not reported.
			if e.IsDir() {
				return fsroot.SkipDir
			}
			return nil
		}
		if ok {
			w.mu.Lock()
			w.seen[e.Rel()] = r
			w.mu.Unlock()
		}
		if e.IsDir() && !e.IsStart() {
			w.rememberDir(e)
		}
		p := report(e.Rel())
		var id fsroot.Identity
		if ok {
			id = r.Identity()
		}
		w.mu.Lock()
		w.cur, w.curPath, w.curID = &e, p, id
		w.mu.Unlock()
		err = fn(visit{path: p, info: info, e: e}, nil)
		w.mu.Lock()
		w.cur, w.curPath, w.curID = nil, "", fsroot.Identity{}
		w.mu.Unlock()
		return mapSkip(err)
	})
	if errors.Is(err, filepath.SkipAll) || errors.Is(err, fsroot.SkipAll) {
		return nil
	}
	return err
}

func mapSkip(err error) error {
	switch {
	case errors.Is(err, filepath.SkipDir):
		return fsroot.SkipDir
	case errors.Is(err, filepath.SkipAll):
		return fsroot.SkipAll
	}
	return err
}

// Walk is filepath.Walk(start, fn) through the tree: fn receives the entry's
// description (a symlink is described, not followed) or an error.
func (w *Walker) Walk(start string, fn filepath.WalkFunc) error {
	return w.walk(start, func(v visit, err error) error {
		return fn(v.path, v.info, err)
	})
}

// WalkDir is filepath.WalkDir(start, fn) through the tree.
func (w *Walker) WalkDir(start string, fn fs.WalkDirFunc) error {
	return w.walk(start, func(v visit, err error) error {
		var d fs.DirEntry
		if v.info != nil {
			d = fs.FileInfoToDirEntry(v.info)
		}
		return fn(v.path, d, err)
	})
}

func parentOf(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return "."
}

func joinRel(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// unreachable reports a resolution that reaches nothing: dangling,
// looping, through a file.
func unreachable(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fsroot.ErrSymlinkLoop) || errors.Is(err, fsroot.ErrNotDir)
}

// decideName decides rel by name alone (the snapshot and the extra rule).
func (w *Walker) decideName(rel string) error {
	if err := w.snap.Decide(rel); err != nil {
		return err
	}
	if w.extra != nil && rel != "." && matchesWithParents(w.extra, rel) {
		return accesspolicy.ErrExcluded
	}
	return nil
}

// ReadDir is os.ReadDir(path) through the tree: the entries of the directory
// path leads to (symlinks on the way followed, as os.ReadDir follows them,
// each decided), those the policy admits, sorted by name, each described as
// listed (a symlink entry is described, not followed). A directory outside
// the root reached through an allowed external symlink is listed by path
// (the outside is not pinned); its entries no rule of the root decides.
func (w *Walker) ReadDir(path string) ([]fs.DirEntry, error) {
	rel, err := w.rel(path)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	chain, reported := w.dirs[rel]
	w.mu.Unlock()
	if reported {
		// A directory this walker reported (decided then): reached again
		// by the chain it was reported with, identity by identity.
		return w.list(rel, func(fn func(fsroot.Entry) error) error { return w.tree.WalkVerified(chain, fn) }, path)
	}
	r, err := w.tree.Resolve(rel)
	if err == nil {
		err = w.check(r)
		if errors.Is(err, accesspolicy.ErrExcluded) {
			err = fs.ErrNotExist
		}
	}
	if err == nil && !r.IsDir() {
		err = syscall.ENOTDIR
	}
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: errnoOf(err)}
	}
	if r.External() != "" {
		return os.ReadDir(r.External())
	}
	rel = r.Real()
	return w.list(rel, func(fn func(fsroot.Entry) error) error { return w.tree.Walk(rel, fn) }, path)
}

// list lists directory rel (walked by walk), the entries the policy admits.
func (w *Walker) list(rel string, walk func(func(fsroot.Entry) error) error, path string) ([]fs.DirEntry, error) {
	var out []fs.DirEntry
	err := walk(func(e fsroot.Entry) error {
		if e.Rel() == rel {
			return e.Err()
		}
		if e.Err() != nil {
			return fsroot.SkipDir
		}
		at("decide", e.Rel())
		if w.refused(e) == nil {
			if info, err := e.Info(); err == nil && info.Mode().Type() == e.Type().Type() {
				w.remember(e)
				w.rememberDir(e)
				out = append(out, fs.FileInfoToDirEntry(info))
			}
		}
		if e.IsDir() {
			return fsroot.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: errnoOf(err)}
	}
	return out, nil
}

// errnoOf is the system error behind err, as a path-based call reports it,
// or err.
func errnoOf(err error) error {
	var errno syscall.Errno
	switch {
	case errors.As(err, &errno):
		return errno
	case errors.Is(err, fs.ErrNotExist):
		return syscall.ENOENT
	case errors.Is(err, fsroot.ErrSymlinkLoop):
		return syscall.ELOOP
	case errors.Is(err, fsroot.ErrNotDir):
		return syscall.ENOTDIR
	}
	return err
}

// ReadFile reads the file at path: one a walk or a listing reported, through
// the resolution it made (identity by identity); any other, resolved,
// decided and read in one operation. A special file is refused unopened.
func (w *Walker) ReadFile(path string) ([]byte, error) {
	rel, err := w.rel(path)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	r, ok := w.seen[rel]
	cur, curID := w.cur, w.curID
	if cur != nil && filepath.Clean(path) != w.curPath {
		cur = nil
	}
	w.mu.Unlock()
	at("read", rel)
	var data []byte
	if cur != nil && cur.Type().IsRegular() {
		// The entry being reported: read through the handle it was
		// listed from — the object at its name decided and opened in one
		// operation (no walk from the root again); the object the walk
		// described (curID), or nothing.
		if !curID.IsZero() {
			data, err = w.snap.ReadEntryAs(*cur, curID)
		} else {
			data, err = w.snap.ReadEntry(*cur)
		}
		if err == nil && w.extra != nil && matchesWithParents(w.extra, cur.Rel()) {
			data, err = nil, accesspolicy.ErrExcluded
		}
	} else if ok {
		data, err = w.snap.ReadResolved(r)
	} else {
		data, err = w.tree.ReadFileChecked(rel, w.check)
	}
	if err != nil {
		return nil, readError(path, err)
	}
	return data, nil
}

// ReportedPlain reports whether a walk or a listing of this walker
// reported path (in the caller's spelling) as a regular file or a directory
// — not a symlink: its type as the walk examined it (Describe), with no
// lookup by path.
func (w *Walker) ReportedPlain(path string) bool {
	rel, err := w.rel(path)
	if err != nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.seen[rel]; ok {
		return true
	}
	_, ok := w.dirs[rel]
	return ok
}

// ReadReported reads rel (root-relative, "/"-separated) if a walk or a
// listing of this walker reported it as a regular file: through the
// resolution made then (identity by identity), as ReadFile reads it; ok is
// false, and nothing is read, for a file not reported. Errors are the
// snapshot's, unworded.
func (w *Walker) ReadReported(rel string) (data []byte, ok bool, err error) {
	w.mu.Lock()
	r, ok := w.seen[rel]
	w.mu.Unlock()
	if !ok {
		return nil, false, nil
	}
	at("read", rel)
	data, err = w.snap.ReadResolved(r)
	if err == nil && w.extra != nil && matchesWithParents(w.extra, rel) {
		data, err = nil, accesspolicy.ErrExcluded
	}
	return data, true, err
}

// Stat is os.Stat(path) through the tree: the object path leads to, if the
// policy admits it.
func (w *Walker) Stat(path string) (fs.FileInfo, error) {
	rel, err := w.rel(path)
	if err != nil {
		return nil, err
	}
	fi, err := w.tree.StatChecked(rel, w.check)
	if err != nil {
		return nil, readError(path, err)
	}
	return fi, nil
}

// check decides a resolution: the snapshot, and the extra rule on the path
// asked for and the file it leads to.
func (w *Walker) check(r fsroot.Resolved) error {
	if err := w.snap.Check(r); err != nil {
		return err
	}
	if w.extra != nil {
		for _, p := range []string{r.Canonical(), r.Real()} {
			if p != "" && p != "." && matchesWithParents(w.extra, p) {
				return accesspolicy.ErrExcluded
			}
		}
	}
	return nil
}

// readError words a refused or failed read for path; a refusal by the
// policy reads as a file that does not exist.
func readError(path string, err error) error {
	if errors.Is(err, accesspolicy.ErrExcluded) {
		return &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return fmt.Errorf("%s: %w", path, err)
}
