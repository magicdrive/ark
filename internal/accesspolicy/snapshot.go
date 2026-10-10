// Package accesspolicy is the .arkignore access policy of one pinned tree:
// a snapshot of the rules, read through the tree, and of the directories
// they were read from.
//
// Build walks the pinned tree (fsroot), captures the bytes of every
// .arkignore and .gitignore it finds, together with the identity of every
// directory it walks, and reads every rule file a second time when the walk
// is done: the bytes and the object must be the same, or the build starts
// over (a rule file being rewritten is never taken half-written; after a few
// attempts it fails with ErrRuleChanged). The rules are then compiled from
// the captured bytes by libgitignore — the same compiler, matcher and
// fingerprint as before — and never read again: changes after Build do not
// affect the snapshot; the next snapshot sees them.
//
// Check decides a path resolved by the same tree. It verifies that every
// directory the resolution passed through is the directory the snapshot read
// rules from (a directory replaced, concealed behind a symlink, or created
// after the walk is refused with ErrDirectoryChanged), then applies the
// .arkignore rules to the path asked for and to the path it resolved to,
// with their parent directories, as Ark's path gate does (ErrExcluded).
//
// What a snapshot guarantees: the rules applied are the bytes of rule files
// as they were during the walk, read from the very directories the decided
// path passes through. What it does not: the rule files are not read at one
// instant — two files changed during the walk may be taken from different
// moments; a rule file added, removed or renamed during the walk is taken as
// found (as if the change happened before or after it, which any writer of
// the repository could also do permanently); renames and hard links that put
// a file at an allowed path (path-based rules cannot tell them from edits).
package accesspolicy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/libgitignore"
)

var (
	// ErrExcluded: the .arkignore rules exclude the path. A refusal, not a
	// failure.
	ErrExcluded = errors.New("accesspolicy: excluded by .arkignore")
	// ErrDirectoryChanged: a directory on the path is not the directory
	// the snapshot read rules from (replaced, concealed, or new).
	ErrDirectoryChanged = errors.New("accesspolicy: directory changed since its rules were read")
	// ErrRuleChanged: the rule files kept changing while being read.
	ErrRuleChanged = errors.New("accesspolicy: ignore rules changed while being read")
	// ErrRuleUnavailable: the rules could not be read; every path but the
	// root is refused.
	ErrRuleUnavailable = errors.New("accesspolicy: ignore rules could not be read")
	// ErrClosed: the snapshot is closed.
	ErrClosed = errors.New("accesspolicy: snapshot closed")
	// ErrTreeMismatch: the path was resolved by another tree.
	ErrTreeMismatch = errors.New("accesspolicy: path resolved by another tree")
)

// errRetry: the tree changed during the build; build again.
var errRetry = errors.New("accesspolicy: tree changed during the build")

// maxAttempts bounds the builds Build tries while the tree keeps changing.
const maxAttempts = 3

// testHook, when set by this package's tests, is called at points of the
// build ("dir" after a directory is recorded, "rule" after a rule file is
// read, "verify" before the second reading). It is not an API.
var testHook func(event, rel string)

func hook(event, rel string) {
	if testHook != nil {
		testHook(event, rel)
	}
}

// Options configure a snapshot.
type Options struct {
	// AdditionalRuleFiles are --additionally-ignorerule files: chosen by the
	// operator, outside the tree, read by path as before.
	AdditionalRuleFiles []string
}

// Snapshot is the access policy of one pinned tree. It is immutable once
// built and safe for concurrent use; Close only marks it closed (the tree
// belongs to the caller).
type Snapshot struct {
	tree    *fsroot.Tree
	files   *libgitignore.IgnoreFiles
	ark     *libgitignore.GitIgnore
	ruleErr error                      // the rules could not be read: refuse all but the root
	dirs    map[string]fsroot.Identity // every directory walked
	skipped map[string]fsroot.Identity // .git and .ark: not walked (no rules apply in them)
	rules   map[string][]byte          // the captured bytes, by rule file (root-relative)
	closed  atomic.Bool
	scope   *scope // non-nil: captures rules on demand (BuildScoped)

	// dirExcluded memoizes whether a directory, or one above it, is
	// excluded: immutable rules, so each directory is decided once per
	// snapshot (at most one entry per directory).
	dirExcluded sync.Map // string → bool
}

// Build captures the access policy of tree.
func Build(tree *fsroot.Tree, opts Options) (*Snapshot, error) {
	var last error
	for i := 0; i < maxAttempts; i++ {
		s, err := build(tree, opts)
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, errRetry) {
			return nil, err
		}
		last = err
	}
	return nil, fmt.Errorf("%w (%d attempts): %v", ErrRuleChanged, maxAttempts, last)
}

// capture is one rule file captured by the walk.
type capture struct {
	rel  string // root-relative path of the rule file
	dir  string // root-relative directory
	file libgitignore.CapturedFile
	id   fsroot.Identity
}

func isRuleName(name string) bool { return name == ".arkignore" || name == ".gitignore" }

func parentOf(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return "."
}

func build(tree *fsroot.Tree, opts Options) (*Snapshot, error) {
	s := &Snapshot{tree: tree, dirs: map[string]fsroot.Identity{}, skipped: map[string]fsroot.Identity{}}
	order := map[string]int{} // directory → walk order (rules apply in it)
	var caps []capture
	var walkErr error
	err := tree.Walk(".", func(e fsroot.Entry) error {
		rel := e.Rel()
		if e.Err() != nil {
			// A directory that cannot be entered: its rules are unknown.
			walkErr = e.Err()
			return nil
		}
		if rel == "." {
			s.dirs["."] = e.DirIdentity()
			order["."] = 0
			return nil
		}
		// Every entry must come from the directory recorded for its
		// parent: if the walk entered another object than the one
		// recorded, the tree changed under it.
		if want, ok := s.dirs[parentOf(rel)]; !ok || !want.Same(e.DirIdentity()) {
			return errRetry
		}
		if e.IsDir() {
			id, err := e.Identity()
			if err != nil {
				return errRetry
			}
			if e.Name() == ".git" || e.Name() == ".ark" {
				s.skipped[rel] = id
				return fsroot.SkipDir
			}
			s.dirs[rel] = id
			order[rel] = len(order)
			if isRuleName(e.Name()) {
				// A directory named .arkignore: present, unreadable.
				caps = append(caps, capture{rel: rel, dir: parentOf(rel), file: libgitignore.CapturedFile{Name: e.Name(), Err: fsroot.ErrNotRegular}})
			}
			hook("dir", rel)
			return nil
		}
		if !isRuleName(e.Name()) {
			return nil
		}
		c, err := captureRule(tree, e)
		if err != nil {
			return err
		}
		if c != nil {
			caps = append(caps, *c)
		}
		hook("rule", rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if walkErr == nil {
		hook("verify", "")
		for _, c := range caps {
			if c.file.Err != nil {
				continue
			}
			if err := verifyRule(tree, c); err != nil {
				return nil, err
			}
		}
	}
	// Rules apply in the order their directories were walked (as
	// IgnoreReader.All orders them): an entry listed before a directory's
	// own rule file (a subdirectory "-a" before ".arkignore") must not put
	// its rules first.
	sort.SliceStable(caps, func(i, j int) bool { return order[caps[i].dir] < order[caps[j].dir] })
	files := make([]libgitignore.CapturedFile, 0, len(caps))
	s.rules = map[string][]byte{}
	for _, c := range caps {
		if c.file.Err == nil {
			s.rules[c.rel] = c.file.Data
		}
		f := c.file
		f.Dir = tree.Logical(c.dir)
		files = append(files, f)
	}
	s.files, err = libgitignore.IgnoreFilesFromCaptured(tree.Root(), files, opts.AdditionalRuleFiles)
	switch {
	case walkErr != nil:
		s.ruleErr = walkErr
	case err != nil:
		s.ruleErr = err
	default:
		s.ark, s.ruleErr = s.files.CompileSource(libgitignore.ArkSource)
	}
	return s, nil
}

// captureRule reads one rule file the walk found. A file that disappears or
// changes type between listing and reading means the tree changed. A
// symlinked rule file is followed inside the root; a dangling or looping one
// is absent (as for os.Stat); one leading outside the root counts as
// unreadable unless external symlinks are allowed — its rules are not known.
func captureRule(tree *fsroot.Tree, e fsroot.Entry) (*capture, error) {
	c := &capture{rel: e.Rel(), dir: parentOf(e.Rel()), file: libgitignore.CapturedFile{Name: e.Name()}}
	switch {
	case e.IsSymlink():
		r, err := tree.Resolve(e.Rel())
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, fsroot.ErrSymlinkLoop):
			return nil, nil
		case err != nil:
			c.file.Err = fmt.Errorf("%w: %v", ErrRuleUnavailable, err)
			return c, nil
		case !r.IsRegular():
			c.file.Err = fsroot.ErrNotRegular
			return c, nil
		}
		b, err := readResolved(tree, r)
		if err != nil {
			return nil, errRetry
		}
		c.file.Data, c.id = b, r.Identity()
	case e.Type().IsRegular():
		b, id, err := e.ReadAll()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fsroot.ErrNotRegular) || errors.Is(err, fsroot.ErrChanged) || isLoop(err) {
				return nil, errRetry
			}
			c.file.Err = err
			return c, nil
		}
		c.file.Data, c.id = b, id
	default:
		// A FIFO, socket or device named like a rule file: never read.
		c.file.Err = fsroot.ErrNotRegular
	}
	return c, nil
}

// verifyRule reads a captured rule file again: the same object with the same
// bytes, or the tree changed during the build.
func verifyRule(tree *fsroot.Tree, c capture) error {
	b, err := tree.ReadFileChecked(c.rel, func(r fsroot.Resolved) error {
		if !r.Identity().Same(c.id) {
			return errRetry
		}
		return nil
	})
	if err != nil || !bytes.Equal(b, c.file.Data) {
		return errRetry
	}
	return nil
}

func readResolved(tree *fsroot.Tree, r fsroot.Resolved) ([]byte, error) {
	f, err := tree.Open(r)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// isLoop reports a symlink where a regular file was listed (ELOOP).
func isLoop(err error) bool {
	return errors.Is(err, syscall.ELOOP) || errors.Is(err, fsroot.ErrSymlink)
}

// Tree returns the tree the snapshot was built from.
func (s *Snapshot) Tree() *fsroot.Tree { return s.tree }

// Fingerprint identifies the rule files' paths and captured contents
// (libgitignore.IgnoreFiles.Fingerprint).
// A scoped snapshot has none: "".
func (s *Snapshot) Fingerprint() string {
	if s.scope != nil {
		return ""
	}
	return s.files.Fingerprint()
}

// Err reports why the rules could not be read (nil if they were).
func (s *Snapshot) Err() error { return s.ruleErr }

// RuleSet compiles the file tools' rule set (.arkignore, and .gitignore if
// allowGitignore) from the captured bytes.
func (s *Snapshot) RuleSet(allowGitignore bool) (*libgitignore.RuleSet, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}
	if s.scope != nil {
		return nil, ErrScoped
	}
	return s.files.CompileRuleSet(allowGitignore)
}

// Close marks the snapshot closed; later checks fail with ErrClosed.
func (s *Snapshot) Close() error {
	if s.closed.Swap(true) {
		return ErrClosed
	}
	return nil
}

// Check decides r: nil if the policy admits it, ErrExcluded if the rules
// exclude it, or why it cannot be decided (ErrDirectoryChanged,
// ErrRuleUnavailable, ErrTreeMismatch, ErrClosed).
func (s *Snapshot) Check(r fsroot.Resolved) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if !r.From(s.tree) {
		return ErrTreeMismatch
	}
	if s.scope != nil {
		// One walk per path the decision needs captures the rules of every
		// directory above it; the directories are verified below.
		if err := s.ensurePaths(r); err != nil {
			return err
		}
	}
	for _, d := range r.Dirs() {
		if err := s.verifyDir(d); err != nil {
			return err
		}
	}
	// Names as the directories list them, not as asked for: on a
	// case-insensitive file system "SECRET.TXT" is the file "secret.txt".
	canonical := r.Canonical()
	if canonical == "." {
		return nil
	}
	paths := []string{canonical}
	if real := r.Real(); real != "" && real != "." && real != canonical {
		paths = append(paths, real)
	}
	return s.decide(paths...)
}

// decide applies the rules to paths (each with the directories above it):
// ErrExcluded if they exclude one, ErrRuleUnavailable if they could not be
// read.
func (s *Snapshot) decide(paths ...string) error {
	if s.ruleErr != nil {
		return fmt.Errorf("%w: %v", ErrRuleUnavailable, s.ruleErr)
	}
	if s.scope != nil {
		ex, err := s.scopedExcluded(paths...)
		if err != nil {
			return err
		}
		if ex {
			return ErrExcluded
		}
		return nil
	}
	for _, p := range paths {
		if s.excluded(p) {
			return ErrExcluded
		}
	}
	return nil
}

// CheckEntry decides a walk entry of the snapshot's tree as Check decides
// the same path resolved: nil, ErrExcluded, or why it cannot be decided. It
// reuses what the walk holds — the identities of the directories the entry
// was listed through, verified against the snapshot — instead of resolving
// the path from the root. A directory entry's own identity is verified when
// the walk enters it (its entries list it among their directories).
//
// A symlink entry is decided by where it leads (resolved through the tree),
// as Check decides it; the symlink must still be the entry listed.
func (s *Snapshot) CheckEntry(e fsroot.Entry) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if !e.From(s.tree) {
		return ErrTreeMismatch
	}
	if !e.Valid() {
		return fsroot.ErrClosed
	}
	if err := e.EachDir(s.verifyDir); err != nil {
		return err
	}
	if e.IsSymlink() {
		r, err := s.tree.Resolve(e.Rel())
		if err != nil {
			return err
		}
		if links := r.Links(); len(links) == 0 || links[0] != e.Rel() {
			return fsroot.ErrChanged // no longer the symlink listed
		}
		return s.Check(r)
	}
	if e.Rel() == "." {
		return nil
	}
	return s.decide(e.Rel())
}

// ReadEntry reads a regular-file entry if the policy admits it — in one
// operation: the object at the entry's name is examined through the handle
// it was listed from, decided (CheckEntry), and that object is read
// (fsroot.Entry.ReadChecked). It never resolves the path again. A symlink
// entry is not followed here (walks never follow symlinks): read it by path
// with ReadFile, which resolves and checks it.
func (s *Snapshot) ReadEntry(e fsroot.Entry) ([]byte, error) {
	if e.IsSymlink() {
		return nil, fsroot.ErrSymlink
	}
	return e.ReadChecked(func(fsroot.Identity) error { return s.CheckEntry(e) })
}

// verifyDir checks that d is the directory the snapshot walked (or, below
// .git or .ark, that the skipped directory above it is).
func (s *Snapshot) verifyDir(d fsroot.DirRef) error {
	if s.scope != nil {
		return s.scopedVerifyDir(d)
	}
	if want, ok := s.dirs[d.Rel]; ok {
		if !want.Same(d.ID) {
			return fmt.Errorf("%w: %s", ErrDirectoryChanged, d.Rel)
		}
		return nil
	}
	if want, ok := s.skipped[d.Rel]; ok {
		if !want.Same(d.ID) {
			return fmt.Errorf("%w: %s", ErrDirectoryChanged, d.Rel)
		}
		return nil
	}
	for p := parentOf(d.Rel); p != "."; p = parentOf(p) {
		if _, ok := s.skipped[p]; ok {
			return nil // verified as d's ancestor among the same resolution's directories
		}
	}
	return fmt.Errorf("%w: %s was not walked", ErrDirectoryChanged, d.Rel)
}

// excluded applies the .arkignore rules to rel and its parent directories.
func (s *Snapshot) excluded(rel string) bool {
	if p := parentOf(rel); p != "." && s.dirExcludedMemo(p) {
		return true
	}
	return s.ark.MatchesRel(rel)
}

// dirExcludedMemo reports whether directory rel or one above it is excluded.
func (s *Snapshot) dirExcludedMemo(rel string) bool {
	if v, ok := s.dirExcluded.Load(rel); ok {
		return v.(bool)
	}
	ex := s.ark.MatchesRel(rel)
	if !ex {
		if p := parentOf(rel); p != "." {
			ex = s.dirExcludedMemo(p)
		}
	}
	s.dirExcluded.Store(rel, ex)
	return ex
}

// ReadResolved reads the regular file r (resolved by the snapshot's tree) if
// the policy admits it: the decision is Check's, and the file is opened by
// fsroot.Tree.Open — every directory and the file reached again through
// handles and verified against the identities r recorded — so the object
// read is the object decided, or nothing is (ErrChanged). For a caller that
// already resolved the path (to decide by its canonical form) and must not
// resolve it twice.
func (s *Snapshot) ReadResolved(r fsroot.Resolved) ([]byte, error) {
	if err := s.Check(r); err != nil {
		return nil, &fs.PathError{Op: "read", Path: r.Logical(), Err: err}
	}
	if !r.IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: r.Logical(), Err: fsroot.ErrNotRegular}
	}
	f, err := s.tree.Open(r)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return fsroot.ReadAll(f, r.Size())
}

// ReadFile reads rel through the snapshot's tree if the policy admits it.
func (s *Snapshot) ReadFile(rel string) ([]byte, error) {
	return s.tree.ReadFileChecked(rel, s.Check)
}

// StatResolved describes r (resolved by the snapshot's tree) if the policy
// admits it, without resolving it again (fsroot.Tree.StatResolved).
func (s *Snapshot) StatResolved(r fsroot.Resolved) (fs.FileInfo, error) {
	if err := s.Check(r); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: r.Logical(), Err: err}
	}
	return s.tree.StatResolved(r)
}

// Stat describes rel through the snapshot's tree if the policy admits it
// (fsroot.Tree.StatChecked): the object described is the object decided.
func (s *Snapshot) Stat(rel string) (fs.FileInfo, error) {
	return s.tree.StatChecked(rel, s.Check)
}
