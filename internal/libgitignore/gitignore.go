package libgitignore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type IgnorePattern struct {
	Regexp      *regexp.Regexp
	Negate      bool
	LineNo      int
	Raw         string
	Dir         string
	AnchorSlash bool
}

type GitIgnore struct {
	Root     string
	patterns []*IgnorePattern
}

func NewGitIgnore() *GitIgnore {
	return &GitIgnore{}
}

func gitignorePatternToRegex(pattern, anchorDir, root string, anchorSlash bool) string {
	rootAbs, _ := filepath.Abs(root)
	anchorAbs, _ := filepath.Abs(anchorDir)
	relDir, err := filepath.Rel(rootAbs, anchorAbs)
	if err != nil {
		relDir = ""
	}
	relDir = filepath.ToSlash(relDir)

	pat := regexp.QuoteMeta(pattern)
	pat = strings.ReplaceAll(pat, `/\*\*/`, `(?:/[^/]*)*/`)
	pat = strings.ReplaceAll(pat, `\*\*/`, `(?:.*/)?`)
	pat = strings.ReplaceAll(pat, `/\*\*`, `(?:/.*)?`)
	pat = strings.ReplaceAll(pat, `\*\*`, `.*`)
	pat = strings.ReplaceAll(pat, `\*`, `[^/]*`)
	pat = strings.ReplaceAll(pat, `\?`, `[^/]`)

	var regex string
	if anchorSlash {
		if relDir != "" && relDir != "." {
			regex = "^" + relDir + "/" + pat + `(/|$)`
		} else {
			regex = "^" + pat + `(/|$)`
		}
	} else {
		regex = `(^|/)` + pat + `(/|$)`
	}

	return regex
}

func CompileIgnoreLine(raw string, dir string, lineno int, root string) (*IgnorePattern, error) {
	absDir := ToAbsDir(dir)
	absRoot := ToAbsDir(root)
	line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
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

	anchorSlash := false
	if strings.HasPrefix(line, "/") {
		anchorSlash = true
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		line += "**"
	}
	reStr := gitignorePatternToRegex(line, absDir, absRoot, anchorSlash)
	re, err := regexp.Compile(reStr)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern on line %d: %w", lineno, err)
	}
	return &IgnorePattern{
		Regexp:      re,
		Negate:      negate,
		LineNo:      lineno,
		Raw:         raw,
		Dir:         absDir,
		AnchorSlash: anchorSlash,
	}, nil
}

func CompileIgnoreLines(lines []string, dir string, startLine int, root string) (*GitIgnore, error) {
	gi := NewGitIgnore()
	gi.Root = ToAbsDir(root)
	var patterns []*IgnorePattern
	for i, raw := range lines {
		p, err := CompileIgnoreLine(raw, dir, startLine+i, gi.Root)
		if err != nil {
			return nil, err
		}
		if p != nil {
			patterns = append(patterns, p)
		}
	}
	gi.patterns = patterns
	return gi, nil
}

func ToAbsDir(dir string) string {
	if absDir, err := filepath.Abs(dir); err != nil {
		return dir
	} else {
		return absDir
	}
}

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

func AppendIgnoreLinesWithDir(gi *GitIgnore, dir string, lines ...string) (*GitIgnore, error) {
	for i, raw := range lines {
		p, err := CompileIgnoreLine(raw, dir, i+1, gi.Root)
		if err != nil {
			return nil, err
		}
		if p != nil {
			gi.patterns = append(gi.patterns, p)
		}
	}
	return gi, nil
}

func AppendIgnoreLines(gi *GitIgnore, lines ...string) (*GitIgnore, error) {
	return AppendIgnoreLinesWithDir(gi, "", lines...)
}
func AppendIgnoreFile(gi *GitIgnore, path string) (*GitIgnore, error) {
	return AppendIgnoreFileWithDir(gi, path, "")
}

func ParseGitIgnore(path string, root string) (*GitIgnore, error) {
	gi := NewGitIgnore()
	gi.Root = ToAbsDir(root)
	absdir := filepath.Dir(path)
	return AppendIgnoreFileWithDir(gi, path, absdir)
}

func (gi *GitIgnore) MatchesPathHow(path string) (bool, *IgnorePattern) {
	absPath := path
	if !filepath.IsAbs(path) {
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
		// fmt.Printf("[DEBUG] path=%q, targetRel=%q, pattern=%q, pat.Dir=%q, dirRel=%q, regex=%q\n",
		// 	path, targetRel, p.Raw, p.Dir, dirRel, p.Regexp.String())
		if err != nil || strings.HasPrefix(dirRel, "..") {
			continue
		}
		if p.Regexp.MatchString(targetRel) {
			if p.Negate {
				if matched {
					matched = false
					last = p
				}
			} else {
				matched = true
				last = p
			}
		}
	}
	return matched, last
}

func (gi *GitIgnore) MatchesPath(path string) bool {
	ok, _ := gi.MatchesPathHow(path)
	return ok
}
func (gi *GitIgnore) Patterns() []*IgnorePattern {
	return gi.patterns
}

func unescapeGitignore(line string) string {
	line = strings.ReplaceAll(line, `\\`, `\`)
	line = strings.ReplaceAll(line, `\ `, ` `)
	if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
		line = line[1:]
	}
	return line
}
