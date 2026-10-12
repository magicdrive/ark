// Package secretscan is `ark secrets-scan`: it reports what Ark's masking
// masks in a repository — where, and by which rule — without showing it.
//
// It is not a second detector: every file is examined by the repository's
// masking policy (internal/secretpolicy) with Policy.Find, the pipeline
// Policy.Mask masks by, in the syntax the dump and the MCP server read the
// file in (secrets.SyntaxForFile). What it reports is what Ark masks; what
// Ark's rules do not know it does not find, so no finding is not a proof
// that a repository holds no secret.
//
// It reads files as the dump does: the target pinned once (internal/fsroot),
// its .arkignore and .gitignore rules applied (internal/accesspolicy,
// internal/safewalk), symlinks leading outside the target never followed.
// With IncludeIgnored it also examines the files those rules exclude — and
// still never leaves the target nor follows a symlink to reach them
// (fsroot.Tree.Walk): ignoring the ignore rules is not ignoring the file
// system boundary.
package secretscan

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/accesspolicy"
	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/safewalk"
	"github.com/magicdrive/ark/internal/secretpolicy"
	"github.com/magicdrive/ark/internal/secrets"
)

// MaxFileSize bounds a file the scan reads; a larger one is skipped (and
// counted), as it is no source a reader writes.
const MaxFileSize = 16 << 20

// Options are what a scan examines.
type Options struct {
	// IncludeIgnored also examines the files .arkignore and .gitignore
	// exclude.
	IncludeIgnored bool
	// AllowGitignore applies .gitignore, as the dump and the MCP server do
	// by default.
	AllowGitignore bool
}

// Finding is one span the policy masks: where it is and why, never its
// text.
type Finding struct {
	Path   string             `json:"path"`   // slash-separated, relative to the root
	Line   int                `json:"line"`   // 1-based
	Column int                `json:"column"` // 1-based, in bytes
	Length int                `json:"length"` // bytes masked (may span lines)
	RuleID string             `json:"ruleId"`
	Source secrets.RuleSource `json:"source"`
	Kind   secrets.RuleKind   `json:"kind"`
	// Excluded marks a file the ignore rules exclude (IncludeIgnored):
	// Ark never returns it.
	Excluded bool `json:"excluded,omitempty"`
}

// FileError is a file the scan could not examine.
type FileError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Report is a scan's result.
type Report struct {
	Root     string
	Config   *secretpolicy.Loaded
	Rules    []secrets.Rule
	Findings []Finding
	Scanned  int // files examined
	Skipped  int // binary, non-UTF-8 or too large
	Errors   []FileError
	// Unfollowed are symlinks the scan did not follow: they lead outside
	// the root or nowhere. The dump follows one leading outside; the MCP
	// server only with --allow-external-symlinks on.
	Unfollowed     []string
	IncludeIgnored bool
	files          map[string]*fileStat // per file, for suggestions
}

// fileStat is what a suggestion needs of one file with findings.
type fileStat struct {
	lines, findingLines int
	excluded            bool
}

// ErrConfig wraps a configuration that cannot be loaded.
var ErrConfig = errors.New("secret rules configuration")

// LoadConfig loads the secret rules configuration of root, as the dump
// applies it (whole).
func LoadConfig(root string) (*secretpolicy.Loaded, error) {
	tree, err := fsroot.Pin(root, fsroot.Options{})
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	l, err := secretpolicy.Load(tree, secretpolicy.Options{AllowWeakening: true})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	return l, nil
}

// Scan examines the files below root.
func Scan(root string, opt Options) (*Report, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	tree, err := fsroot.Pin(abs, fsroot.Options{})
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	loaded, err := secretpolicy.Load(tree, secretpolicy.Options{AllowWeakening: true})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	r := &Report{Root: abs, Config: loaded, Rules: loaded.Policy.Rules(), IncludeIgnored: opt.IncludeIgnored, files: map[string]*fileStat{}}
	meta := map[string]secrets.Rule{}
	for _, rule := range r.Rules {
		meta[rule.ID] = rule
	}
	examine := func(rel string, data []byte, syntax secrets.Syntax, excluded bool) {
		if len(data) > MaxFileSize || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			r.Skipped++
			return
		}
		r.Scanned++
		content := string(data)
		lines := map[int]bool{}
		for _, f := range loaded.Policy.Find(content, secrets.Mode{Syntax: syntax}) {
			if f.End == f.Start {
				continue // a mask inserted at an empty value: nothing hidden
			}
			rule := meta[f.RuleID]
			r.Findings = append(r.Findings, Finding{Path: rel, Line: f.Line, Column: f.Column, Length: f.End - f.Start,
				RuleID: f.RuleID, Source: rule.Source, Kind: rule.Kind, Excluded: excluded})
			lines[f.Line] = true
		}
		if len(lines) > 0 {
			r.files[rel] = &fileStat{lines: nonBlankLines(content), findingLines: len(lines), excluded: excluded}
		}
	}

	// The files the dump reads: the access policy's, through its walker.
	snap, err := accesspolicy.Build(tree, accesspolicy.Options{})
	if err != nil {
		return nil, fmt.Errorf("ignore rules: %w", err)
	}
	defer snap.Close()
	if err := snap.Err(); err != nil {
		return nil, fmt.Errorf("ignore rules: %w", err)
	}
	rules, err := snap.RuleSet(opt.AllowGitignore)
	if err != nil {
		return nil, fmt.Errorf("ignore rules: %w", err)
	}
	w := safewalk.New(snap, abs)
	if rules.Git != nil {
		w.WithRule(rules.Git)
	}
	seen := map[string]bool{}
	walkErr := w.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		rel := relPath(abs, path)
		if err != nil {
			r.Errors = append(r.Errors, FileError{Path: rel, Message: err.Error()})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); rel != "." && (n == ".git" || n == ".ark") {
				return fs.SkipDir // Ark's metadata and git's, as the dump skips them
			}
			return nil
		}
		data, err := w.ReadFile(path)
		if err != nil {
			r.Errors = append(r.Errors, FileError{Path: rel, Message: err.Error()})
			return nil
		}
		seen[rel] = true
		syntax := secrets.SyntaxUnknown
		if d.Type()&fs.ModeSymlink == 0 {
			syntax = secrets.SyntaxForFile(d.Name())
		}
		examine(rel, data, syntax, false)
		return nil
	})
	if walkErr != nil {
		r.Errors = append(r.Errors, FileError{Path: ".", Message: walkErr.Error()})
	}

	// Every other entry below the root, reached without following a
	// symlink: with IncludeIgnored, the regular files the ignore rules
	// exclude; always, the symlinks the walk above did not follow (outside
	// the root, or leading nowhere), which are listed, not read.
	excluded := func(rel string) bool {
		if errors.Is(snap.Decide(rel), accesspolicy.ErrExcluded) {
			return true
		}
		if rules.Git == nil {
			return false
		}
		for i := 0; i < len(rel); i++ {
			if rel[i] == '/' && rules.Git.MatchesRel(rel[:i]) {
				return true
			}
		}
		return rules.Git.MatchesRel(rel)
	}
	err = tree.Walk(".", func(e fsroot.Entry) error {
		rel := e.Rel()
		switch {
		case rel == ".":
			return nil
		case e.Err() != nil:
			if !seen[rel] && (opt.IncludeIgnored || !excluded(rel)) {
				r.Errors = append(r.Errors, FileError{Path: rel, Message: e.Err().Error()})
			}
			return nil
		case e.IsDir():
			if n := e.Name(); n == ".git" || n == ".ark" {
				return fs.SkipDir // Ark's metadata and git's, never sources
			}
			if !opt.IncludeIgnored && excluded(rel) {
				return fs.SkipDir
			}
			return nil
		case seen[rel]:
			return nil
		case e.IsSymlink():
			if !excluded(rel) {
				r.Unfollowed = append(r.Unfollowed, rel)
			}
			return nil
		case !opt.IncludeIgnored || !e.Type().IsRegular():
			return nil
		}
		data, _, err := e.ReadAll()
		if err != nil {
			r.Errors = append(r.Errors, FileError{Path: rel, Message: err.Error()})
			return nil
		}
		examine(rel, data, secrets.SyntaxForFile(e.Name()), true)
		return nil
	})
	if err != nil {
		r.Errors = append(r.Errors, FileError{Path: ".", Message: err.Error()})
	}

	sort.Slice(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.RuleID < b.RuleID
	})
	sort.Slice(r.Errors, func(i, j int) bool { return r.Errors[i].Path < r.Errors[j].Path })
	sort.Strings(r.Unfollowed)
	return r, nil
}

// relPath is path relative to root, slash-separated.
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func nonBlankLines(s string) int {
	n := 0
	for _, line := range bytes.Split([]byte(s), []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			n++
		}
	}
	return n
}

// FilesWithFindings counts the files with at least one finding.
func (r *Report) FilesWithFindings() int { return len(r.files) }

// ByRule counts the findings of each rule.
func (r *Report) ByRule() map[string]int {
	out := map[string]int{}
	for _, f := range r.Findings {
		out[f.RuleID]++
	}
	return out
}
