package mcp

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/magicdrive/ark/internal/accesspolicy"
	arkctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/source"
)

// Request-scoped file access.
//
// The path gate (resolveToolPath) decides a path when a request names it;
// a file opened by path afterwards may no longer be the file decided — a
// symlink swapped in, a directory replaced, the root link retargeted
// (time-of-check/time-of-use). So the tools that read files a client names
// (get_file_content, get_files_arklite, file://, get_file_info,
// get_symbols, get_symbol) and the source text the index-based tools return
// (get_context and search_context snippets, get_repository_map's generated
// packages) read through the request's access instead: the server root
// pinned once for the request (fsroot.Tree) and a policy snapshot over it
// (accesspolicy.BuildScoped), which decides a path and reads the very object
// it decided, through directory handles, in one operation
// (Snapshot.ReadFile, Snapshot.Stat). A read the snapshot refuses is
// refused, whatever the gate said; nothing falls back to a read by path.
//
// The access is made on first use and released when the request ends
// (beginRequest). Within a request every read uses the same pinned root and
// the same captured rule files; a tree that changes under a read fails the
// read (closed), and the request reports it.

// requestAccess is one request's pinned root and policy snapshot.
type requestAccess struct {
	once sync.Once
	tree *fsroot.Tree
	snap *accesspolicy.Snapshot
	err  error

	mu     sync.Mutex
	closed bool
	// resolved holds the gate's resolutions (by canonical path), so a read
	// opens the object the gate resolved — verified identity by identity
	// (Snapshot.ReadResolved) — instead of resolving the path again.
	resolved map[string]fsroot.Resolved
	// indexed holds the index files read for the request's snippets: read
	// once through the snapshot, decided once; a second snippet of the same
	// file is cut from the same bytes.
	indexed map[string][]byte
}

// beforeRead, when set by a test, runs before the request's snapshot reads
// rel: the window between the gate's decision (or the index's) and the
// read, which a concurrent writer aims at.
var beforeRead func(rel string)

func hookRead(rel string) {
	if beforeRead != nil {
		beforeRead(rel)
	}
}

// afterIndex, when set by a test, runs when an index-based tool has its
// index, before it reads source text from it: the window between the
// index's view of a file and the snippet read.
var afterIndex func()

func hookIndex() {
	if afterIndex != nil {
		afterIndex()
	}
}

// errNoRequest: a file was to be read outside a request (a programming
// error); nothing is read.
var errNoRequest = errors.New("file access outside a request")

// errChangedDuringRead marks a read refused because the tree changed while
// it was decided or read.
var errChangedDuringRead = errors.New("the repository changed while the file was read; nothing was returned")

// beginRequest returns the handler for one request and the function that
// ends it, releasing the request's file access. A handler already serving a
// request is returned as is, with nothing to end.
func (h *ToolsHandler) beginRequest() (*ToolsHandler, func()) {
	if h.req != nil {
		return h, func() {}
	}
	r := h.forRequest()
	return r, r.req.access.close
}

// snapshot returns the request's policy snapshot, made on first use.
func (h *ToolsHandler) snapshot() (*accesspolicy.Snapshot, error) {
	if h.req == nil {
		return nil, errNoRequest
	}
	a := &h.req.access
	a.once.Do(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.closed {
			a.err = accesspolicy.ErrClosed
			return
		}
		tree, err := fsroot.Pin(h.rootDir, fsroot.Options{AllowExternalSymlinks: h.opt != nil && h.opt.AllowExternalSymlinks})
		if err != nil {
			a.err = err
			return
		}
		var extra []string
		if h.opt != nil {
			extra = h.opt.AdditionallyIgnoreRuleFilenameList
		}
		snap, err := accesspolicy.BuildScoped(tree, accesspolicy.Options{AdditionalRuleFiles: extra})
		if err != nil {
			tree.Close()
			a.err = err
			return
		}
		a.tree, a.snap = tree, snap
	})
	return a.snap, a.err
}

// close releases the access: the snapshot, then the tree (which waits for
// reads in progress). Later reads fail.
func (a *requestAccess) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.closed = true
	if a.snap != nil {
		a.snap.Close()
	}
	if a.tree != nil {
		a.tree.Close()
	}
}

// canonicalize is fsCanonicalPath on the request's pinned tree: the gate
// and the reads of one request see one root.
func (h *ToolsHandler) canonicalize(rel string) (canonical, real string, err error) {
	if h.req == nil {
		return fsCanonicalPath(h.rootDir, rel, h.opt != nil && h.opt.AllowExternalSymlinks)
	}
	snap, err := h.snapshot()
	if err != nil {
		return rel, "", nil // no root to pin: nothing is read (as fsCanonicalPath)
	}
	return canonicalBy(h.rootDir, rel, func(rel string) (string, string, error) {
		r, err := snap.Tree().Resolve(rel)
		if err != nil {
			return "", "", err
		}
		a := &h.req.access
		a.mu.Lock()
		if a.resolved == nil {
			a.resolved = map[string]fsroot.Resolved{}
		}
		a.resolved[r.Canonical()] = r
		a.mu.Unlock()
		return r.Canonical(), r.Real(), nil
	})
}

// treeRel turns rel (relative to the server root, as the path gate returns
// it) into the tree's form.
func treeRel(rel string) string {
	if rel == "" {
		return "."
	}
	return filepath.ToSlash(rel)
}

// readToolFile reads the file a tool's path argument names: through the path
// gate, then from the request's pinned tree under its policy snapshot. A
// refusal (excluded, outside the root, changed during the read) is gateErr,
// worded as the gate words it; a file that cannot be read is readErr, worded
// as reading it by path would be (open …: no such file or directory, read …:
// is a directory), so tools report both as before.
func (h *ToolsHandler) readToolFile(arg string) (data []byte, fullPath, rel string, gateErr, readErr error) {
	fullPath, rel, gateErr = h.resolveToolPath(arg)
	if gateErr != nil {
		return nil, "", "", gateErr, nil
	}
	data, gateErr, readErr = h.readGated(arg, fullPath, rel)
	return data, fullPath, rel, gateErr, readErr
}

// readGated reads a path the gate admitted (fullPath, rel as it returned
// them) through the request's snapshot.
func (h *ToolsHandler) readGated(arg, fullPath, rel string) (data []byte, gateErr, readErr error) {
	snap, err := h.snapshot()
	if err != nil {
		return nil, fmt.Errorf("path %q: %v", arg, err), nil
	}
	hookRead(treeRel(rel))
	if r, ok := h.gateResolution(rel); ok && r.IsRegular() {
		data, err = snap.ReadResolved(r)
	} else {
		data, err = snap.ReadFile(treeRel(rel))
	}
	if err != nil {
		gateErr, readErr = h.accessError("open", arg, fullPath, rel, err)
		return nil, gateErr, readErr
	}
	return data, nil, nil
}

// statToolFile is readToolFile for a file's description (get_file_info).
func (h *ToolsHandler) statToolFile(arg string) (info fs.FileInfo, fullPath string, gateErr, readErr error) {
	fullPath, rel, gateErr := h.resolveToolPath(arg)
	if gateErr != nil {
		return nil, "", gateErr, nil
	}
	info, gateErr, readErr = h.statGated(arg, fullPath, rel)
	return info, fullPath, gateErr, readErr
}

// gateResolution returns the resolution the gate made of rel in this
// request, if it made one.
func (h *ToolsHandler) gateResolution(rel string) (fsroot.Resolved, bool) {
	if h.req == nil {
		return fsroot.Resolved{}, false
	}
	a := &h.req.access
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.resolved[treeRel(rel)]
	return r, ok
}

// statGated is readGated for a description.
func (h *ToolsHandler) statGated(arg, fullPath, rel string) (info fs.FileInfo, gateErr, readErr error) {
	snap, err := h.snapshot()
	if err != nil {
		return nil, fmt.Errorf("path %q: %v", arg, err), nil
	}
	hookRead(treeRel(rel))
	if r, ok := h.gateResolution(rel); ok {
		info, err = snap.StatResolved(r)
	} else {
		info, err = snap.Stat(treeRel(rel))
	}
	if err != nil {
		gateErr, readErr = h.accessError("stat", arg, fullPath, rel, err)
		return nil, gateErr, readErr
	}
	return info, nil, nil
}

// accessError sorts a read's error into a refusal and an ordinary read
// error, worded with op as the path-based call (os.ReadFile: "open",
// os.Stat: "stat") would word it. Anything not known to be an ordinary read
// error is a refusal.
func (h *ToolsHandler) accessError(op, arg, fullPath, rel string, err error) (gateErr, readErr error) {
	var errno syscall.Errno
	switch {
	case errors.Is(err, accesspolicy.ErrExcluded):
		return fmt.Errorf("path %q does not exist", arg), nil
	case errors.Is(err, accesspolicy.ErrRuleUnavailable):
		return fmt.Errorf("path %q: %w: %v", arg, errPolicyUnavailable, err), nil
	case errors.Is(err, fsroot.ErrOutsideRoot):
		return fmt.Errorf("path %q resolves through a symlink outside the server root %q; use a path inside the repository", arg, h.rootDir), nil
	case errors.Is(err, fsroot.ErrNotRegular):
		if snap, serr := h.snapshot(); serr == nil {
			if r, rerr := snap.Tree().Resolve(treeRel(rel)); rerr == nil && r.IsDir() {
				return nil, &fs.PathError{Op: "read", Path: fullPath, Err: syscall.EISDIR}
			}
		}
		return nil, &fs.PathError{Op: op, Path: fullPath, Err: fsroot.ErrNotRegular}
	case errors.Is(err, fsroot.ErrSymlinkLoop):
		return nil, &fs.PathError{Op: op, Path: fullPath, Err: syscall.ELOOP}
	case errors.Is(err, fsroot.ErrNotDir):
		return nil, &fs.PathError{Op: op, Path: fullPath, Err: syscall.ENOTDIR}
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		if errors.As(err, &errno) {
			return nil, &fs.PathError{Op: op, Path: fullPath, Err: errno}
		}
		return nil, &fs.PathError{Op: op, Path: fullPath, Err: err}
	}
	return fmt.Errorf("path %q: %w (%v)", arg, errChangedDuringRead, err), nil
}

// sourceReader returns the reader of index source text for the context
// engine: base is the indexed directory relative to the server root (as the
// path gate returns it); the text is read through the request's snapshot.
func (h *ToolsHandler) sourceReader(base string) func(root string, fileID source.FileID, startLine, endLine uint32) (string, error) {
	return func(_ string, fileID source.FileID, startLine, endLine uint32) (string, error) {
		data, err := h.readIndexed(base, fileID)
		if err != nil {
			return "", err
		}
		return arkctxSourceLines(data, startLine, endLine)
	}
}

// readIndexed reads file fileID of the index of directory base (relative to
// the server root) through the request's snapshot.
func (h *ToolsHandler) readIndexed(base string, fileID source.FileID) ([]byte, error) {
	snap, err := h.snapshot()
	if err != nil {
		return nil, err
	}
	rel := path.Join(treeRel(base), string(fileID))
	a := &h.req.access
	a.mu.Lock()
	data, ok := a.indexed[rel]
	a.mu.Unlock()
	if ok {
		return data, nil
	}
	hookRead(rel)
	data, err = snap.ReadFile(rel)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.indexed == nil {
		a.indexed = map[string][]byte{}
	}
	a.indexed[rel] = data
	a.mu.Unlock()
	return data, nil
}

func arkctxSourceLines(data []byte, startLine, endLine uint32) (string, error) {
	return arkctx.SourceLines(data, startLine, endLine)
}
