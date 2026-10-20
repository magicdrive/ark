package commandline

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/magicdrive/ark/internal/textenc"
)

// FileEncodingRule names the encoding of the files a pattern matches
// (--file-encoding), for files whose encoding cannot be told from their
// bytes.
type FileEncodingRule struct {
	Pattern  string // slash-separated, relative to the target directory
	Encoding string // canonical name (textenc.Lookup)
}

// parseFileEncodings parses `PATTERN=ENCODING[,PATTERN=ENCODING...]`.
func parseFileEncodings(value string) ([]FileEncodingRule, error) {
	var rules []FileEncodingRule
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		pattern, name, ok := strings.Cut(item, "=")
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if path.IsAbs(pattern) || filepath.IsAbs(pattern) || slices.Contains(strings.Split(pattern, "/"), "..") {
			return nil, fmt.Errorf("--file-encoding %q: the pattern must be relative to the directory, within it", item)
		}
		pattern = strings.TrimSuffix(strings.TrimPrefix(path.Clean("/"+pattern), "/"), "/")
		if !ok || pattern == "" || pattern == "." {
			return nil, fmt.Errorf("--file-encoding %q: want PATTERN=ENCODING", item)
		}
		if _, err := path.Match(strings.TrimSuffix(pattern, "/**"), ""); err != nil {
			return nil, fmt.Errorf("--file-encoding %q: bad pattern: %v", item, err)
		}
		enc, ok := textenc.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("--file-encoding %q: unknown encoding %q (one of %s)", item, strings.TrimSpace(name), strings.Join(textenc.Names(), ", "))
		}
		rules = append(rules, FileEncodingRule{Pattern: pattern, Encoding: enc})
	}
	return rules, nil
}

// FileEncodingFor returns the encoding --file-encoding names for the file at
// p, or "" when no rule matches. p is a path below the target directory (the
// MCP server's root: FileEncodingRoot) or relative to it. The first matching
// rule wins. A pattern is matched with path.Match against the path relative
// to that directory ("*" matches within one path element); a pattern without
// "/" also matches a file name in any directory, and a pattern ending in
// "/**" matches every file below its directory — "**" means nothing else.
func (cr *Option) FileEncodingFor(p string) string {
	if len(cr.FileEncodingRules) == 0 {
		return ""
	}
	rel := p
	base := cr.FileEncodingRoot
	if base == "" {
		base = cr.TargetDirname
	}
	if base != "" && filepath.IsAbs(p) == filepath.IsAbs(base) {
		if r, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	for _, r := range cr.FileEncodingRules {
		if r.Matches(rel) {
			return r.Encoding
		}
	}
	return ""
}

// Matches reports whether the rule's pattern matches rel, a slash-separated
// path relative to the target directory.
func (r FileEncodingRule) Matches(rel string) bool {
	if dir, ok := strings.CutSuffix(r.Pattern, "/**"); ok {
		return rel == dir || strings.HasPrefix(rel, dir+"/")
	}
	if ok, _ := path.Match(r.Pattern, rel); ok {
		return true
	}
	if !strings.Contains(r.Pattern, "/") {
		ok, _ := path.Match(r.Pattern, path.Base(rel))
		return ok
	}
	return false
}

// UnusedFileEncodingRules returns the --file-encoding rules no path of paths
// (below the target directory) matches.
func (cr *Option) UnusedFileEncodingRules(paths []string) []FileEncodingRule {
	used := make([]bool, len(cr.FileEncodingRules))
	probe := *cr
	for _, p := range paths {
		for i, r := range cr.FileEncodingRules {
			probe.FileEncodingRules = []FileEncodingRule{r}
			if !used[i] && probe.FileEncodingFor(p) != "" {
				used[i] = true
			}
		}
	}
	var unused []FileEncodingRule
	for i, r := range cr.FileEncodingRules {
		if !used[i] {
			unused = append(unused, r)
		}
	}
	return unused
}
