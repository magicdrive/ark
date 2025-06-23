package libgitignore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IgnorePattern represents a single ignore rule, including which .gitignore directory it came from.
type IgnorePattern struct {
	Regexp *regexp.Regexp
	Negate bool
	LineNo int
	Raw    string
	Dir    string
}

// GitIgnore represents a set of ignore patterns.
type GitIgnore struct {
	Root                 string
	patterns             []*IgnorePattern
	registeredPatternMap map[string]bool
}

// NewGitIgnore creates an empty GitIgnore object.
func NewGitIgnore() *GitIgnore {
	return &GitIgnore{}
}

func unescapeGitignore(line string) string {
	line = strings.ReplaceAll(line, `\\`, `\`)
	line = strings.ReplaceAll(line, `\ `, ` `)
	if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
		line = line[1:]
	}
	return line
}

func gitignorePatternToRegex(pattern string) string {
	anchor := strings.HasPrefix(pattern, "/")
	if anchor {
		pattern = pattern[1:]
	}

	pattern = regexp.QuoteMeta(pattern)

	pattern = strings.ReplaceAll(pattern, `/\*\*/`, `(?:/[^/]*)*/`)

	pattern = strings.ReplaceAll(pattern, `\*\*/`, `(?:.*/)?`)
	pattern = strings.ReplaceAll(pattern, `/\*\*`, `(?:/.*)?`)
	pattern = strings.ReplaceAll(pattern, `\*\*`, `.*`)

	pattern = strings.ReplaceAll(pattern, `\*`, `[^/]*`)
	pattern = strings.ReplaceAll(pattern, `\?`, `[^/]`)

	if anchor {
		pattern = "^" + pattern + "$"
	} else {
		pattern = "(^|/)" + pattern + "$"
	}

	return pattern
}

func CompileIgnoreLines(lines []string, dir string, startLine int) (*GitIgnore, error) {
	gi := NewGitIgnore()
	var patterns []*IgnorePattern
	for i, raw := range lines {
		p, err := CompileIgnoreLine(raw, dir, startLine+i)
		if err != nil {
			return nil, err
		}
		if p != nil {
			patterns = append(patterns, p)
		}
	}
	gi.patterns = patterns
	gi.Root = ToAbsDir(dir)
	return gi, nil
}

func ToAbsDir(dir string) string {
	if absDir, err := filepath.Abs(dir); err != nil {
		return dir
	} else {
		return absDir
	}
}

// CompileIgnoreLine parses a single line of .gitignore syntax into an IgnorePattern.
func CompileIgnoreLine(raw string, dir string, lineno int) (*IgnorePattern, error) {
	absDir := ToAbsDir(dir)
	line := strings.TrimRight(raw, "\r")
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil
	}

	escaped := strings.HasPrefix(raw, `\!`) || strings.HasPrefix(raw, `\#`)
	negate := false
	if strings.HasPrefix(line, "!") && !escaped {
		negate = true
		line = line[1:]
	}

	line = unescapeGitignore(line)

	if strings.HasSuffix(line, "/") {
		line += "**"
	}

	reStr := gitignorePatternToRegex(line)
	re, err := regexp.Compile(reStr)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern on line %d: %w", lineno, err)
	}

	return &IgnorePattern{
		Regexp: re,
		Negate: negate,
		LineNo: lineno,
		Raw:    raw,
		Dir:    absDir,
	}, nil
}

// AppendIgnoreLinesWithDir adds .gitignore rules from lines[] as coming from dir directory.
func AppendIgnoreLinesWithDir(gi *GitIgnore, dir string, lines ...string) (*GitIgnore, error) {
	for i, raw := range lines {
		p, err := CompileIgnoreLine(raw, dir, i+1)
		if err != nil {
			return nil, err
		}
		if p != nil {
			gi.patterns = append(gi.patterns, p)
		}
	}
	return gi, nil
}

// AppendIgnoreFileWithDir adds rules from a .gitignore file, associating all rules with its directory.
func AppendIgnoreFileWithDir(gi *GitIgnore, path string, dir string) (*GitIgnore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return AppendIgnoreLinesWithDir(gi, dir, lines...)
}

// AppendIgnoreLines is a legacy wrapper for backward compatibility (Dir未指定).
func AppendIgnoreLines(gi *GitIgnore, lines ...string) (*GitIgnore, error) {
	return AppendIgnoreLinesWithDir(gi, "", lines...)
}

// AppendIgnoreFile is a legacy wrapper for backward compatibility (Dir未指定).
func AppendIgnoreFile(gi *GitIgnore, path string) (*GitIgnore, error) {
	return AppendIgnoreFileWithDir(gi, path, "")
}

// ParseGitIgnore reads a .gitignore file and returns a GitIgnore object with all rules.
func ParseGitIgnore(path string) (*GitIgnore, error) {
	gi := NewGitIgnore()
	absdir := filepath.Dir(path)
	return AppendIgnoreFileWithDir(gi, path, absdir)
}

// MatchesPathHow returns (match, pattern) where pattern is the last matched rule.
// Only patterns whose Dir is the same or parent of the file are considered.
func (gi *GitIgnore) MatchesPathHow(path string) (bool, *IgnorePattern) {
	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath = filepath.Join(gi.Root, path)
	}
	targetRel, err := filepath.Rel(gi.Root, absPath)
	if err != nil {
		targetRel = absPath
	}
	targetRel = filepath.ToSlash(targetRel)

	matched := false
	var last *IgnorePattern
	for _, p := range gi.patterns {
		dirRel, err := filepath.Rel(p.Dir, absPath)
		if err != nil || strings.HasPrefix(dirRel, "..") {
			continue
		}
		if p.Regexp.MatchString(targetRel) {
			matched = !p.Negate
			last = p
		}
	}
	return matched, last
}

// MatchesPath is a simplified version returning just a bool.
func (gi *GitIgnore) MatchesPath(path string) bool {
	ok, _ := gi.MatchesPathHow(path)
	return ok
}

// Patterns returns the list of all ignore patterns.
func (gi *GitIgnore) Patterns() []*IgnorePattern {
	return gi.patterns
}
