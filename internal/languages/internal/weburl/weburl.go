// Package weburl resolves the relative URLs of web documents (HTML, CSS) to
// the repository files they name, by the text alone: no server, base URL
// or configuration is consulted, and what the text does not fix as a
// repository file is refused.
package weburl

import (
	"net/url"
	"path"
	"strings"
)

// Trim removes what the URL parser removes before parsing: leading and
// trailing C0 controls and spaces, and every ASCII tab and newline.
func Trim(v string) string {
	v = strings.TrimFunc(v, func(r rune) bool { return r <= 0x20 })
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, v)
}

// HasScheme reports whether v starts with a URL scheme ("http:", "mailto:").
func HasScheme(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == ':':
			return i > 0
		case 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z':
		case i > 0 && ('0' <= c && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return false
}

// Relative resolves a relative URL v (trimmed by Trim) written in the
// repository file file to the repository file it names, which must have one
// of exts (lower case, with the dot), and returns the URL's fragment (raw,
// without the '#'; "" without one). It refuses what the text does not fix
// as a repository file: a scheme, a scheme-relative or root-relative URL, a
// backslash (a path separator to the URL parser), a query alone, a
// directory, an escaped slash, an invalid escape, a target outside the
// repository or of another extension.
func Relative(file, v string, exts ...string) (target, fragment string, ok bool) {
	if v == "" || strings.HasPrefix(v, "/") || strings.ContainsAny(v, "\\\x00") || HasScheme(v) {
		return "", "", false
	}
	if i := strings.IndexByte(v, '#'); i >= 0 {
		v, fragment = v[:i], v[i+1:]
	}
	if i := strings.IndexByte(v, '?'); i >= 0 {
		v = v[:i]
	}
	if v == "" || strings.HasSuffix(v, "/") {
		return "", "", false
	}
	// An escaped slash is no path separator to the URL parser; which file a
	// server maps it to is not fixed.
	if l := strings.ToLower(v); strings.Contains(l, "%2f") || strings.Contains(l, "%5c") {
		return "", "", false
	}
	p, err := url.PathUnescape(v)
	if err != nil || strings.ContainsAny(p, "\\\x00") {
		return "", "", false
	}
	ext := strings.ToLower(path.Ext(p))
	found := false
	for _, e := range exts {
		found = found || e == ext
	}
	if !found {
		return "", "", false
	}
	target = path.Join(path.Dir(file), p)
	if target == ".." || strings.HasPrefix(target, "../") || strings.HasPrefix(target, "/") {
		return "", "", false
	}
	return target, fragment, true
}
