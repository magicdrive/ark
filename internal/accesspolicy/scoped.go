package accesspolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/libgitignore"
)

// Scoped snapshots.
//
// Build walks the whole tree to capture every rule file: what a walk of the
// repository needs, and tens of milliseconds on a large one. A request that
// names a few paths needs only the rule files that can apply to them — the
// root's and those of the directories above each path, as
// libgitignore.IgnoreReader.For reads them, which decides every path as the
// whole repository's rules do. BuildScoped returns a snapshot that captures
// those on demand, through the snapshot's tree: each directory's rule file
// is read from the directory's own handle in one walk from the root
// (fsroot.ReadChain), read a second time (stable reads, the second settled,
// as Build reads them) to rule out a rewrite in progress,
// and kept with the directory's identity for the rest of the snapshot's
// life — later decisions reuse it, and a resolution that passed another
// object at that path is refused (ErrDirectoryChanged), as with Build.
//
// A decision applies the rule files of the directories above the path asked
// for (its canonical form) and above the file it leads to, and of the path
// itself when it is a directory (its own rule file can match it). A rule file that
// cannot be read refuses the paths it could apply to (ErrRuleUnavailable),
// not the whole tree. RuleSet and Fingerprint, which describe the whole
// tree's rules, are not available.

// ErrScoped: the operation needs the whole tree's rules, which a scoped
// snapshot does not capture.
var ErrScoped = errors.New("accesspolicy: a scoped snapshot has no whole-tree rules")

type scope struct {
	base *libgitignore.IgnoreFiles // the root and the additional rule files only

	mu      sync.Mutex
	dirs    map[string]fsroot.Identity // directories captured
	skipped map[string]fsroot.Identity // .git and .ark directories met
	caps    map[string]capture         // the rule file captured in a directory
	failed  map[string]error           // directories whose rules could not be read
	chains  map[string]bool            // paths whose chains were captured

	compiled map[string]*libgitignore.GitIgnore // rules compiled, by the directories whose rule files they hold

	// dirExcluded memoizes whether a directory, or one above it, is
	// excluded: it depends only on the rule files of that directory and
	// those above it, all captured — for good — before anything below it
	// is decided.
	dirExcluded sync.Map // string → bool
}

// BuildScoped returns a snapshot of tree that captures rule files as
// decisions need them. The additional rule files are read once, here.
func BuildScoped(tree *fsroot.Tree, opts Options) (*Snapshot, error) {
	base, err := libgitignore.IgnoreFilesFromCaptured(tree.Root(), nil, opts.AdditionalRuleFiles)
	if err != nil {
		// The additional rule files cannot be read: nothing but the root
		// is decided.
		return &Snapshot{tree: tree, ruleErr: err, scope: &scope{}}, nil
	}
	return &Snapshot{tree: tree, scope: &scope{
		base:    base,
		dirs:    map[string]fsroot.Identity{},
		skipped: map[string]fsroot.Identity{},
		caps:    map[string]capture{},
		failed:  map[string]error{},
		chains:  map[string]bool{},

		compiled: map[string]*libgitignore.GitIgnore{},
	}}, nil
}

// Scoped reports whether s was made by BuildScoped.
func (s *Snapshot) Scoped() bool { return s.scope != nil }

// ensure captures the rule files of the directories from the root through
// rel (as far as a walk would enter), verifying the directories captured
// before; a tree that keeps changing under it is ErrRuleChanged.
func (s *Snapshot) ensure(rel string) error {
	sc := s.scope
	sc.mu.Lock()
	done := sc.chains[rel]
	sc.mu.Unlock()
	if done {
		return nil // captured; each resolution's directories are verified against it
	}
	var last error
	for i := 0; i < maxAttempts; i++ {
		err := s.capture(rel)
		if !errors.Is(err, errRetry) {
			return err
		}
		last = err
	}
	return fmt.Errorf("%w (%d attempts): %v", ErrRuleChanged, maxAttempts, last)
}

func (s *Snapshot) capture(rel string) error {
	sc := s.scope
	sc.mu.Lock()
	// The walk reads only the directories not captured yet.
	known := map[string]bool{}
	for d := range sc.dirs {
		known[d] = true
	}
	sc.mu.Unlock()
	chain, err := s.tree.ReadChain(rel, ".arkignore", func(dir string) bool { return !known[dir] })
	if err != nil {
		if errors.Is(err, fsroot.ErrChanged) {
			return errRetry
		}
		return fmt.Errorf("%w: %v", ErrRuleUnavailable, err)
	}
	var fresh []capture
	for _, d := range chain {
		if d.Skipped || known[d.Rel] {
			continue
		}
		c, err := s.scopedCapture(d)
		if err != nil {
			return err
		}
		if c != nil {
			fresh = append(fresh, *c)
		}
	}
	// A second reading of every rule file read: the same object with the
	// same bytes, or a rewrite was in progress. Files read through the
	// chain are read again through a second chain walk (no resolution);
	// symlinked ones through their resolution.
	settle(stampsOf(fresh))
	hook("verify", rel)
	if err := s.verifyChain(rel, chain, fresh); err != nil {
		return err
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for _, d := range chain {
		table := sc.dirs
		if d.Skipped {
			table = sc.skipped
		}
		if want, ok := table[d.Rel]; ok {
			if !want.Same(d.ID) {
				return fmt.Errorf("%w: %s", ErrDirectoryChanged, d.Rel)
			}
			continue
		}
		table[d.Rel] = d.ID
	}
	for _, c := range fresh {
		sc.caps[c.dir] = c
		if c.file.Err != nil {
			sc.failed[c.dir] = c.file.Err
		}
	}
	sc.chains[rel] = true
	return nil
}

// verifyChain reads the rule files of fresh (captured from chain) a second
// time: the same objects with the same bytes, in the same directories.
func (s *Snapshot) verifyChain(rel string, chain []fsroot.ChainDir, fresh []capture) error {
	byDir := map[string]fsroot.ChainDir{}
	for _, d := range chain {
		byDir[d.Rel] = d
	}
	reread := map[string]bool{}
	deepest := "."
	for _, c := range fresh {
		if c.file.Err != nil {
			continue
		}
		if d := byDir[c.dir]; d.File.Link {
			if err := verifyRule(s.tree, c); err != nil {
				return err
			}
			continue
		}
		reread[c.dir] = true
		if strings.Count(c.dir, "/") > strings.Count(deepest, "/") || deepest == "." {
			deepest = c.dir
		}
	}
	if len(reread) == 0 {
		return nil
	}
	// As deep as the deepest of them: the chain below adds nothing.
	again, err := s.tree.RereadChain(deepest, ".arkignore", func(dir string) bool { return reread[dir] })
	if err != nil {
		return errRetry
	}
	seen := 0
	for _, d := range again {
		if !reread[d.Rel] {
			continue
		}
		first := byDir[d.Rel]
		if !d.ID.Same(first.ID) || d.File.Link || d.File.Err != nil || !d.File.Present ||
			!d.File.ID.Same(first.File.ID) || !confirms(d.File.Stamp, d.File.Data, first.File.Stamp, first.File.Data) {
			return errRetry
		}
		seen++
	}
	if seen != len(reread) {
		return errRetry
	}
	return nil
}

// scopedCapture is captureRule for the rule file of one chain directory.
func (s *Snapshot) scopedCapture(d fsroot.ChainDir) (*capture, error) {
	rel := ".arkignore"
	if d.Rel != "." {
		rel = d.Rel + "/.arkignore"
	}
	c := &capture{rel: rel, dir: d.Rel, file: libgitignore.CapturedFile{Name: ".arkignore"}}
	f := d.File
	switch {
	case !f.Present && f.Err == nil:
		return nil, nil
	case f.Err != nil:
		c.file.Err = f.Err
	case f.NotRegular:
		c.file.Err = fsroot.ErrNotRegular
	case f.Link:
		// Followed inside the root; dangling or looping is absent; outside
		// the root is unreadable unless allowed (as captureRule).
		r, err := s.tree.Resolve(rel)
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, fsroot.ErrSymlinkLoop):
			return nil, nil
		case err != nil:
			c.file.Err = fmt.Errorf("%w: %v", ErrRuleUnavailable, err)
		case !r.IsRegular():
			c.file.Err = fsroot.ErrNotRegular
		default:
			if links := r.Links(); len(links) == 0 || links[0] != rel {
				return nil, errRetry
			}
			b, st, err := s.tree.ReadResolvedStable(r)
			if err != nil {
				return nil, errRetry
			}
			c.file.Data, c.id, c.stamp = b, r.Identity(), st
		}
	default:
		c.file.Data, c.id, c.stamp = f.Data, f.ID, f.Stamp
	}
	return c, nil
}

// ensurePaths captures what deciding r needs: the chains of its canonical
// path and of the file it leads to.
func (s *Snapshot) ensurePaths(r fsroot.Resolved) error {
	for _, p := range []string{r.Canonical(), r.Real()} {
		if p == "" {
			continue
		}
		if err := s.ensure(chainOf(p, r.IsDir())); err != nil {
			return err
		}
	}
	return nil
}

// scopedVerifyDir is verifyDir for a scoped snapshot: a directory not seen
// yet is captured first.
func (s *Snapshot) scopedVerifyDir(d fsroot.DirRef) error {
	if err := s.knownDir(d); err == nil || !errors.Is(err, errUnknownDir) {
		return err
	}
	if err := s.ensure(d.Rel); err != nil {
		return err
	}
	if err := s.knownDir(d); err != nil {
		if errors.Is(err, errUnknownDir) {
			return fmt.Errorf("%w: %s is not a directory a walk enters", ErrDirectoryChanged, d.Rel)
		}
		return err
	}
	return nil
}

var errUnknownDir = errors.New("accesspolicy: directory not captured")

func (s *Snapshot) knownDir(d fsroot.DirRef) error {
	sc := s.scope
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for _, table := range []map[string]fsroot.Identity{sc.dirs, sc.skipped} {
		if want, ok := table[d.Rel]; ok {
			if !want.Same(d.ID) {
				return fmt.Errorf("%w: %s", ErrDirectoryChanged, d.Rel)
			}
			return nil
		}
	}
	for p := parentOf(d.Rel); p != "."; p = parentOf(p) {
		if _, ok := sc.skipped[p]; ok {
			return nil // below .git or .ark, as Build
		}
	}
	return errUnknownDir
}

// scopedExcluded decides the paths of one resolution (its canonical form and
// the file it leads to, each with the directories above it) by the rule
// files of the directories above them.
func (s *Snapshot) scopedExcluded(isDir bool, paths ...string) (bool, error) {
	sc := s.scope
	dirs := map[string]bool{".": true}
	for _, p := range paths {
		// The directories above the path, and the path itself when it is
		// a directory: its own rule file can match it (as
		// IgnoreReader.For reads it). A file has no rule file of its own.
		top := chainOf(p, isDir)
		if err := s.ensure(top); err != nil {
			return false, err
		}
		for d := top; d != "."; d = parentOf(d) {
			dirs[d] = true
		}
	}
	sc.mu.Lock()
	var caps []capture
	for d := range dirs {
		if err, ok := sc.failed[d]; ok {
			sc.mu.Unlock()
			return false, fmt.Errorf("%w: %v", ErrRuleUnavailable, err)
		}
		if c, ok := sc.caps[d]; ok {
			caps = append(caps, c)
		}
	}
	// Parents before children, siblings by name: the walk's order.
	sort.Slice(caps, func(i, j int) bool { return walkOrderLess(caps[i].dir, caps[j].dir) })
	// The rule of these rule files, compiled once: what is captured never
	// changes for the snapshot's life.
	keyParts := make([]string, len(caps))
	for i, c := range caps {
		keyParts[i] = c.dir
	}
	key := strings.Join(keyParts, "\x00")
	ark, cached := sc.compiled[key]
	sc.mu.Unlock()
	if cached {
		return s.scopedMatch(ark, paths), nil
	}
	files := make([]libgitignore.CapturedFile, 0, len(caps))
	for _, c := range caps {
		f := c.file
		f.Dir = s.tree.Logical(c.dir)
		files = append(files, f)
	}
	set, err := sc.base.WithCaptured(files)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrRuleUnavailable, err)
	}
	ark, err = set.CompileSource(libgitignore.ArkSource)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrRuleUnavailable, err)
	}
	sc.mu.Lock()
	sc.compiled[key] = ark
	sc.mu.Unlock()
	return s.scopedMatch(ark, paths), nil
}

// scopedMatch reports whether ark (the rules of the directories above the
// paths, and of the paths that are directories) excludes a path or a
// directory above it.
func (s *Snapshot) scopedMatch(ark *libgitignore.GitIgnore, paths []string) bool {
	for _, p := range paths {
		if d := parentOf(p); d != "." && s.scopedDirExcluded(ark, d) {
			return true
		}
		if ark.MatchesRel(p) {
			return true
		}
	}
	return false
}

func (s *Snapshot) scopedDirExcluded(ark *libgitignore.GitIgnore, d string) bool {
	sc := s.scope
	if v, ok := sc.dirExcluded.Load(d); ok {
		return v.(bool)
	}
	ex := ark.MatchesRel(d)
	if !ex {
		if p := parentOf(d); p != "." {
			ex = s.scopedDirExcluded(ark, p)
		}
	}
	sc.dirExcluded.Store(d, ex)
	return ex
}

// chainOf is the deepest directory whose rule file can decide p: p itself
// when it is a directory, else the directory it is in.
func chainOf(p string, isDir bool) string {
	if isDir {
		return p
	}
	return parentOf(p)
}

func matchesWithParents(ark *libgitignore.GitIgnore, rel string) bool {
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' && ark.MatchesRel(rel[:i]) {
			return true
		}
	}
	return ark.MatchesRel(rel)
}

// walkOrderLess orders root-relative directories as a sorted depth-first
// walk lists them.
func walkOrderLess(a, b string) bool {
	if a == "." || b == "." {
		return a == "." && b != "."
	}
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

// captureFromEntry captures, from a walk's own handles, the rule files a
// decision on e needs: those of the directory e was listed from and, for a
// directory, its own — each read twice (the same object, the same bytes)
// and recorded with the identity of the directory read, exactly as a chain
// capture records them, without walking from the root again. A directory
// whose ancestors are not captured yet is left to the chain capture
// (ensure), as are .git and .ark directories.
func (s *Snapshot) captureFromEntry(e fsroot.Entry) error {
	sc := s.scope
	dirs := e.Dirs()
	if len(dirs) == 0 || e.IsStart() {
		return nil
	}
	parent := dirs[len(dirs)-1]
	sc.mu.Lock()
	ancestorsKnown := true
	for _, d := range dirs[:len(dirs)-1] {
		if _, ok := sc.dirs[d.Rel]; !ok {
			ancestorsKnown = false
		}
	}
	_, parentKnown := sc.dirs[parent.Rel]
	sc.mu.Unlock()
	if !ancestorsKnown || (parent.Rel != "." && skippedName(parent.Rel)) {
		return nil
	}
	if !parentKnown {
		if err := s.captureListed(
			func() (fsroot.ChainDir, error) { return e.ReadSiblingFile(".arkignore") },
			func() (fsroot.ChainDir, error) { return e.RereadSiblingFile(".arkignore") }); err != nil {
			return err
		}
	}
	if e.IsDir() && !e.IsSymlink() && !skippedName(e.Rel()) {
		sc.mu.Lock()
		_, known := sc.dirs[e.Rel()]
		sc.mu.Unlock()
		if !known {
			return s.captureListed(
				func() (fsroot.ChainDir, error) { return e.ReadDirFile(".arkignore") },
				func() (fsroot.ChainDir, error) { return e.RereadDirFile(".arkignore") })
		}
	}
	return nil
}

func skippedName(rel string) bool {
	name := rel[strings.LastIndexByte(rel, '/')+1:]
	return name == ".git" || name == ".ark"
}

// captureListed captures one directory's rule file, read by read (twice:
// the same object with the same bytes, or a rewrite was in progress).
func (s *Snapshot) captureListed(read, reread func() (fsroot.ChainDir, error)) error {
	var last error
	for i := 0; i < maxAttempts; i++ {
		d, err := read()
		if err != nil {
			if errors.Is(err, fsroot.ErrChanged) {
				last = err
				continue
			}
			return fmt.Errorf("%w: %v", ErrRuleUnavailable, err)
		}
		c, err := s.scopedCapture(d)
		if errors.Is(err, errRetry) {
			last = err
			continue
		}
		if err != nil {
			return err
		}
		if c != nil && c.file.Err == nil {
			settle([]fsroot.Stamp{c.stamp})
		}
		hook("verify", d.Rel)
		if c != nil && c.file.Err == nil && !d.File.Link {
			again, err := reread()
			if err != nil || !again.File.Present || again.File.Link || !again.ID.Same(d.ID) || !again.File.ID.Same(d.File.ID) || !confirms(again.File.Stamp, again.File.Data, d.File.Stamp, d.File.Data) {
				last = errRetry
				continue
			}
		} else if c != nil && c.file.Err == nil {
			if err := verifyRule(s.tree, *c); err != nil {
				last = err
				continue
			}
		}
		sc := s.scope
		sc.mu.Lock()
		if want, ok := sc.dirs[d.Rel]; ok {
			sc.mu.Unlock()
			if !want.Same(d.ID) {
				return fmt.Errorf("%w: %s", ErrDirectoryChanged, d.Rel)
			}
			return nil
		}
		sc.dirs[d.Rel] = d.ID
		if c != nil {
			sc.caps[c.dir] = *c
			if c.file.Err != nil {
				sc.failed[c.dir] = c.file.Err
			}
		}
		sc.chains[d.Rel] = true // its ancestors were captured before it
		sc.mu.Unlock()
		return nil
	}
	return fmt.Errorf("%w (%d attempts): %v", ErrRuleChanged, maxAttempts, last)
}

// stampsOf returns the stamps of the captured rule files read.
func stampsOf(caps []capture) []fsroot.Stamp {
	out := make([]fsroot.Stamp, 0, len(caps))
	for _, c := range caps {
		if c.file.Err == nil {
			out = append(out, c.stamp)
		}
	}
	return out
}
