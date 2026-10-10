package libgitignore

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// IgnoreFiles is a snapshot of the ignore files of a root — every .gitignore
// and .arkignore under it (not in .git, .ark or symlinked directories) and the
// additional rule files.
// Its Fingerprint and the rules it Compiles come from the same bytes, so a
// compiled rule is always the rule of the files that fingerprint names, even
// when the files change while it is used.
type IgnoreFiles struct {
	root        string
	dirs        []ignoreDir // in walk order
	extra       []*ignoreFile
	fingerprint string
}

type ignoreDir struct {
	dir      string
	git, ark *ignoreFile // nil: absent
}

type ignoreFile struct {
	path  string
	lines []string
	sum   [sha256.Size]byte
	err   error // the file exists but could not be read or split
}

// IgnoreReader reads each ignore file at most once, so everything built from
// one reader — the whole repository's rules and the rules for single paths —
// sees one version of every file. It is safe for concurrent use.
type IgnoreReader struct {
	root  string
	extra []string

	mu    sync.Mutex
	files map[string]*ignoreFile // by path; nil: absent

	allOnce sync.Once
	all     *IgnoreFiles
	allErr  error

	// collectSkip, when set (CollectEntries), makes All record the entries
	// it walks, except below directories it names; entries holds them.
	collectSkip func(name string) bool
	entries     []Entry
}

// Entry is one entry of the repository walk All makes, in filepath.WalkDir
// order: its path (below the reader's root, the root itself first) and its
// directory entry.
type Entry struct {
	Path string
	D    fs.DirEntry
}

// CollectEntries makes All record the entries of its walk, for a caller that
// would otherwise walk the same directories again (Entries). Entries below a
// directory skip names (other than the root) are not recorded; the directory
// itself is. It must be called before All.
func (r *IgnoreReader) CollectEntries(skip func(name string) bool) {
	r.collectSkip = skip
}

// Entries returns the entries All recorded, and whether they are complete:
// false when collection was not requested, All has not run, or its walk
// failed.
func (r *IgnoreReader) Entries() ([]Entry, bool) {
	if r.collectSkip == nil {
		return nil, false
	}
	if _, err := r.All(); err != nil {
		return nil, false
	}
	return r.entries, true
}

// NewIgnoreReader returns a reader for root and the additional rule files.
func NewIgnoreReader(root string, additionallyFileList []string) *IgnoreReader {
	return &IgnoreReader{root: ToAbsDir(root), extra: additionallyFileList, files: map[string]*ignoreFile{}}
}

// ReadIgnoreFiles reads the ignore files for root (NewIgnoreReader(...).All()).
func ReadIgnoreFiles(root string, additionallyFileList []string) (*IgnoreFiles, error) {
	return NewIgnoreReader(root, additionallyFileList).All()
}

// file returns the ignore file at path: nil when absent. strict reports a
// failure to tell whether it exists as an error instead of as absence.
func (r *IgnoreReader) file(path string, strict bool) (*ignoreFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.files[path]; ok {
		return f, nil
	}
	if _, err := os.Stat(path); err != nil {
		if strict && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return nil, err
		}
		// Outside For, any Stat failure means absence.
		r.files[path] = nil
		return nil, nil
	}
	f := &ignoreFile{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		f.err = err
	} else {
		f.sum = sha256.Sum256(data)
		f.lines, f.err = scanLines(data)
	}
	r.files[path] = f
	return f, nil
}

// dirEntry returns the ignore files of one directory.
func (r *IgnoreReader) dirEntry(dir string, strict bool) (ignoreDir, error) {
	e := ignoreDir{dir: dir}
	var err error
	if e.git, err = r.file(filepath.Join(dir, ".gitignore"), strict); err != nil {
		return e, err
	}
	if e.ark, err = r.file(filepath.Join(dir, ".arkignore"), strict); err != nil {
		return e, err
	}
	return e, nil
}

// extraFiles returns the additional rule files, which must be readable.
func (r *IgnoreReader) extraFiles() ([]*ignoreFile, error) {
	var out []*ignoreFile
	for _, p := range r.extra {
		f, err := r.file(p, false)
		if err != nil {
			return nil, err
		}
		if f == nil {
			f = &ignoreFile{path: p, err: fmt.Errorf("open %s: %w", p, fs.ErrNotExist)}
		}
		out = append(out, f)
	}
	return out, nil
}

// All reads the ignore files of the whole repository, once. An error means
// the repository could not be walked; a single unreadable file is recorded
// and fails only the rules that need it (Compile).
func (r *IgnoreReader) All() (*IgnoreFiles, error) {
	r.allOnce.Do(func() {
		f := &IgnoreFiles{root: r.root}
		// Directories in walk order (each source appends its rules in
		// this order); a directory's ignore files are found
		// among its entries, which the walk lists anyway, instead of by
		// probing every directory.
		var order []string
		found := map[string]*ignoreDir{}
		skipping := "" // the directory whose entries are not being recorded
		// The root may itself be a symlink (a server or dump root given
		// through a link): filepath.WalkDir would not enter it, and no rule
		// would be read while the directory it leads to is served. Walk the
		// directory it resolves to and report every path in the root's own
		// spelling, which the rules are anchored at.
		walkRoot := r.root
		if real, err := filepath.EvalSymlinks(r.root); err == nil {
			walkRoot = real
		}
		err := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
			if walkRoot != r.root {
				path = r.root + strings.TrimPrefix(path, walkRoot)
			}
			if err != nil {
				return err
			}
			if r.collectSkip != nil {
				if skipping != "" && !strings.HasPrefix(path, skipping) {
					skipping = ""
				}
				if skipping == "" {
					r.entries = append(r.entries, Entry{Path: path, D: d})
					if d.IsDir() && path != r.root && r.collectSkip(d.Name()) {
						skipping = path + string(filepath.Separator)
					}
				}
			}
			if path != r.root && (d.Name() == ".gitignore" || d.Name() == ".arkignore") {
				// What os.Stat would find: anything but a dangling symlink.
				file, _ := r.file(path, false)
				if file != nil {
					dir := filepath.Dir(path)
					e := found[dir]
					if e == nil {
						e = &ignoreDir{dir: dir}
						found[dir] = e
					}
					if d.Name() == ".gitignore" {
						e.git = file
					} else {
						e.ark = file
					}
				}
			}
			if !d.IsDir() {
				return nil
			}
			if path != r.root && (d.Name() == ".git" || d.Name() == ".ark") {
				return filepath.SkipDir
			}
			order = append(order, path)
			return nil
		})
		if err != nil {
			r.allErr = err
			return
		}
		for _, dir := range order {
			if e := found[dir]; e != nil {
				f.dirs = append(f.dirs, *e)
			}
		}
		if f.extra, err = r.extraFiles(); err != nil {
			r.allErr = err
			return
		}
		f.fingerprint = f.digest()
		r.all = f
	})
	return r.all, r.allErr
}

// For reads only the ignore files whose rules can apply to rel (a clean,
// "/"-separated path relative to the root): those of the root, of each
// directory above rel and of rel itself — as far as the repository walk
// would reach them (it does not enter .git, .ark or a symlinked directory).
// The rules compiled from them decide rel, and each directory above it,
// exactly as the whole repository's rules do. Unlike All, a directory that
// cannot be examined fails only the paths below it.
func (r *IgnoreReader) For(rel string) (*IgnoreFiles, error) {
	f := &IgnoreFiles{root: r.root}
	add := func(dir string) error {
		e, err := r.dirEntry(dir, true)
		if err != nil {
			return err
		}
		if e.git != nil || e.ark != nil {
			f.dirs = append(f.dirs, e)
		}
		return nil
	}
	if err := add(r.root); err != nil {
		return nil, err
	}
	cur := r.root
	if rel != "." && rel != "" {
		for _, part := range strings.Split(rel, "/") {
			if part == ".git" || part == ".ark" {
				break
			}
			cur = filepath.Join(cur, part)
			fi, err := os.Lstat(cur)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
					break
				}
				return nil, err
			}
			if !fi.IsDir() {
				break // a file, or a symlink the walk would not enter
			}
			if err := add(cur); err != nil {
				return nil, err
			}
		}
	}
	extra, err := r.extraFiles()
	if err != nil {
		return nil, err
	}
	f.extra = extra
	f.fingerprint = f.digest()
	return f, nil
}

// digest is the fingerprint: every file's path and content, in order.
func (f *IgnoreFiles) digest() string {
	h := sha256.New()
	write := func(kind string, file *ignoreFile) {
		if file.err != nil {
			fmt.Fprintf(h, "%s %q unreadable\n", kind, file.path)
			return
		}
		fmt.Fprintf(h, "%s %q %x\n", kind, file.path, file.sum)
	}
	for _, d := range f.dirs {
		for _, file := range []*ignoreFile{d.git, d.ark} {
			if file != nil {
				write("dir", file)
			}
		}
	}
	for _, file := range f.extra {
		write("additional", file)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Fingerprint identifies the files' paths and contents: equal fingerprints
// mean equal rules.
func (f *IgnoreFiles) Fingerprint() string { return f.fingerprint }

// Source is a kind of rule: .arkignore files (with the additional rule
// files), or .gitignore files. The two are independent: each is compiled and
// matched on its own, so a negation in one never re-includes what the other
// excludes.
type Source int

const (
	// ArkSource: every .arkignore file, then the additional rule files.
	ArkSource Source = iota
	// GitSource: every .gitignore file.
	GitSource
)

// CompileSource builds one source's rule from these files: its files'
// patterns in walk order, each anchored at its own directory (additional
// rule files at the root). An unreadable file of that source is an error.
func (f *IgnoreFiles) CompileSource(src Source) (*GitIgnore, error) {
	gi := NewGitIgnore()
	gi.Root = f.root
	add := func(file *ignoreFile, dir string) error {
		if file.err != nil {
			return file.err
		}
		_, err := AppendIgnoreLinesWithDir(gi, dir, file.lines...)
		return err
	}
	for _, d := range f.dirs {
		file := d.ark
		if src == GitSource {
			file = d.git
		}
		if file == nil {
			continue
		}
		if err := add(file, d.dir); err != nil {
			return nil, err
		}
	}
	if src == ArkSource {
		for _, file := range f.extra {
			if err := add(file, f.root); err != nil {
				return nil, err
			}
		}
	}
	return gi, nil
}

// RuleSet is the ignore rule of a file selection: the .arkignore rule and,
// when .gitignore handling is on, the .gitignore rule. A path is ignored when
// either rule matches it.
type RuleSet struct {
	Ark *GitIgnore
	Git *GitIgnore // nil: .gitignore handling off
}

// CompileRuleSet compiles the .arkignore rule and, if allowGitignore, the
// .gitignore rule.
func (f *IgnoreFiles) CompileRuleSet(allowGitignore bool) (*RuleSet, error) {
	ark, err := f.CompileSource(ArkSource)
	if err != nil {
		return nil, err
	}
	rs := &RuleSet{Ark: ark}
	if allowGitignore {
		if rs.Git, err = f.CompileSource(GitSource); err != nil {
			return nil, err
		}
	}
	return rs, nil
}

// GenerateRuleSet reads the ignore files under root (every .arkignore and
// .gitignore below it, never above it, and the additional rule files) and
// compiles them. root, not the working directory, anchors every rule.
func GenerateRuleSet(allowGitignore bool, root string, additionallyFileList []string) (*RuleSet, error) {
	files, err := ReadIgnoreFiles(root, additionallyFileList)
	if err != nil {
		return nil, err
	}
	return files.CompileRuleSet(allowGitignore)
}

// Root is the directory the rules belong to ("" for an empty set).
func (rs *RuleSet) Root() string {
	switch {
	case rs == nil:
		return ""
	case rs.Ark != nil:
		return rs.Ark.Root
	case rs.Git != nil:
		return rs.Git.Root
	}
	return ""
}

// MatchesPath reports whether either rule matches path (absolute, or relative
// to the rules' root).
func (rs *RuleSet) MatchesPath(path string) bool {
	if rs == nil {
		return false
	}
	return (rs.Ark != nil && rs.Ark.MatchesPath(path)) || (rs.Git != nil && rs.Git.MatchesPath(path))
}

// MatchesRel is MatchesPath for a clean "/"-separated path relative to the
// root.
func (rs *RuleSet) MatchesRel(rel string) bool {
	if rs == nil {
		return false
	}
	return (rs.Ark != nil && rs.Ark.MatchesRel(rel)) || (rs.Git != nil && rs.Git.MatchesRel(rel))
}

// scanLines splits a file as AppendIgnoreFileWithDir does.
func scanLines(data []byte) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
