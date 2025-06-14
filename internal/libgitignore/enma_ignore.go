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
	Regexp *regexp.Regexp
	Negate bool
	LineNo int
	Raw    string
}

type GitIgnore struct {
	registeredPatternMap map[string]bool
	registeredStringMap  map[string]string
	patterns             []*IgnorePattern
}

func NewPlainIgnoreRule() *GitIgnore {
	return &GitIgnore{
		registeredPatternMap: map[string]bool{},
		registeredStringMap:  map[string]string{},
		patterns:             []*IgnorePattern{},
	}
}

func CompileIgnoreText(text string) (*GitIgnore, error) {
	return AppendIgnoreText(NewPlainIgnoreRule(), text)
}

func AppendIgnoreText(gi *GitIgnore, text string) (*GitIgnore, error) {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return AppendIgnoreLines(gi, lines...)
}

func CompileIgnoreLines(lines ...string) (*GitIgnore, error) {
	gi := NewPlainIgnoreRule()
	return AppendIgnoreLines(gi, lines...)
}

func AppendIgnoreFile(gi *GitIgnore, path string) (*GitIgnore, error) {
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
	return AppendIgnoreLines(gi, lines...)
}

func AppendIgnoreLines(gi *GitIgnore, lines ...string) (*GitIgnore, error) {
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if gi.registeredPatternMap[line] {
			continue
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
			return nil, fmt.Errorf("invalid pattern on line %d: %w", i+1, err)
		}

		gi.patterns = append(gi.patterns, &IgnorePattern{
			Regexp: re,
			Negate: negate,
			LineNo: i + 1,
			Raw:    raw,
		})
		gi.registeredPatternMap[line] = true
		gi.registeredStringMap[line] = reStr
	}
	return gi, nil
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

func CompileIgnoreFile(path string) (*GitIgnore, error) {
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
	return CompileIgnoreLines(lines...)
}

func (gi *GitIgnore) Matches(path string) bool {
	match, _ := gi.MatchesPathHow(path)
	return match
}

func (gi *GitIgnore) MatchesPathHow(path string) (bool, *IgnorePattern) {
	norm := filepath.ToSlash(path)
	matched := false
	var last *IgnorePattern
	for _, p := range gi.patterns {
		if p.Regexp.MatchString(norm) {
			matched = !p.Negate
			last = p
		}
	}
	return matched, last
}
