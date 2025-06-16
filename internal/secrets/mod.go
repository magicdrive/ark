package secrets

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SecretMatch struct {
	File    string
	Line    int
	Text    string
	Pattern string
}

var (
	reAKIA   = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	reGHP    = regexp.MustCompile(`ghp_[A-Za-z0-9_]{36,255}`)
	reJWT    = regexp.MustCompile(`eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}`)
	reKeyVal = regexp.MustCompile(`(?i)(secret|password|passwd|pass|pw|token|api[_\-]?key|access[_\-]?key|private[_\-]?key)\s*[:=]\s*['"]?[^'"\s]+['"]?`)
)

var defaultAllowed = []*regexp.Regexp{
	regexp.MustCompile(`AKIAIOSFODNN7EXAMPLE`),
	regexp.MustCompile(`wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`),
}

type RuleSet struct {
	Allowed  []*regexp.Regexp
	Patterns []*regexp.Regexp // AddPattern用
}

func DefaultRuleSet() *RuleSet {
	return &RuleSet{
		Allowed:  defaultAllowed,
		Patterns: []*regexp.Regexp{},
	}
}

func (r *RuleSet) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.Patterns = append(r.Patterns, re)
	return nil
}

func (r *RuleSet) AddAllowed(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.Allowed = append(r.Allowed, re)
	return nil
}

func (r *RuleSet) ScanReader(rd io.Reader, filename string) ([]SecretMatch, error) {
	var matches []SecretMatch
	scanner := bufio.NewScanner(rd)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if r.isAllowed(line) {
			continue
		}

		// 1. AddedPattern
		matched := false
		for _, pat := range r.Patterns {
			if pat.MatchString(line) {
				matches = append(matches, SecretMatch{filename, lineNum, line, pat.String()})
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		// 2. key=val: check value section AKIA, ghp_, JWT
		if m := regexp.MustCompile(`(?i)[\w-]+\s*[:=]\s*['"]?([^\s'"]+)['"]?`).FindStringSubmatch(line); m != nil {
			val := m[1]
			switch {
			case reAKIA.MatchString(val):
				matches = append(matches, SecretMatch{filename, lineNum, line, reAKIA.String()})
				continue
			case reGHP.MatchString(val):
				matches = append(matches, SecretMatch{filename, lineNum, line, reGHP.String()})
				continue
			case reJWT.MatchString(val):
				matches = append(matches, SecretMatch{filename, lineNum, line, reJWT.String()})
				continue
			}
		}

		// 3. AKIA
		if reAKIA.MatchString(line) {
			matches = append(matches, SecretMatch{filename, lineNum, line, reAKIA.String()})
			continue
		}
		// 4. ghp_
		if reGHP.MatchString(line) {
			matches = append(matches, SecretMatch{filename, lineNum, line, reGHP.String()})
			continue
		}
		// 5. JWT
		if reJWT.MatchString(line) {
			matches = append(matches, SecretMatch{filename, lineNum, line, reJWT.String()})
			continue
		}
		// 6. key=val
		if reKeyVal.MatchString(line) {
			matches = append(matches, SecretMatch{filename, lineNum, line, reKeyVal.String()})
			continue
		}
	}
	return matches, scanner.Err()
}

func (r *RuleSet) isAllowed(s string) bool {
	for _, a := range r.Allowed {
		if a.MatchString(s) {
			return true
		}
	}
	return false
}

func MaskLine(line string) string {
	trim := strings.TrimSpace(line)
	if reAKIA.MatchString(trim) && trim == reAKIA.FindString(trim) {
		return "*****MASKED*****"
	}
	out := reJWT.ReplaceAllStringFunc(line, func(_ string) string {
		return "*****MASKED*****"
	})
	out = regexp.MustCompile(`(?i)(secret|password|passwd|pass|pw|token|api[_\-]?key|access[_\-]?key|private[_\-]?key)\s*[:=]\s*['"]?([^\s'"]+)['"]?`).ReplaceAllStringFunc(out, func(s string) string {
		m := regexp.MustCompile(`(?i)(secret|password|passwd|pass|pw|token|api[_\-]?key|access[_\-]?key|private[_\-]?key)\s*[:=]\s*['"]?([^\s'"]+)['"]?`).FindStringSubmatch(s)
		if len(m) > 2 {
			val := m[2]
			if strings.HasPrefix(val, "ghp_") {
				return s
			}
			return strings.Replace(s, val, "*****MASKED*****", 1)
		}
		return s
	})
	return out
}

func (r *RuleSet) ScanFile(path string) ([]SecretMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return r.ScanReader(f, path)
}

func (r *RuleSet) ScanDir(root string, recursive bool) ([]SecretMatch, error) {
	var results []SecretMatch
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !IsTextFile(path) {
			return nil
		}
		m, err := r.ScanFile(path)
		if err == nil && len(m) > 0 {
			results = append(results, m...)
		}
		return nil
	})
	return results, err
}

func IsTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 800)
	n, _ := f.Read(buf)
	for i := range n {
		b := buf[i]
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			return false
		}
	}
	return true
}
