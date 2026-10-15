package html

import (
	"strings"

	xhtml "golang.org/x/net/html"
)

// Attribute spans. x/net's tokenizer reports a tag's attributes without
// their positions, so the provider recomputes them from the tag's raw bytes
// with the standard's attribute states (HTML §13.2.5.32–39) and accepts them
// only when they match the tokenizer's attributes one for one: names up to
// ASCII case, values after x/net's own normalization (attrValue).

// span is a byte range [start, end) of the source; start -1 means none.
type span struct{ start, end int }

// scannedAttr is one attribute of a tag's raw bytes: its name (ASCII
// lower-cased, as the tokenizer reports it), the name's span and the raw
// value's span (start -1 without a value), relative to the tag.
type scannedAttr struct {
	name        string
	nameS, valS span
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// tagNameEnd returns the offset in raw (a start tag) where its name ends.
func tagNameEnd(raw []byte) int {
	i := 1 // after '<'
	for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++
	}
	return i
}

// scanAttrs follows the tokenizer's states from after the tag name (before
// attribute name … after attribute value (quoted)) over a start tag's raw
// bytes.
func scanAttrs(raw []byte) []scannedAttr {
	i := tagNameEnd(raw)
	var out []scannedAttr
	for i < len(raw) {
		// before attribute name
		for i < len(raw) && (isSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) || raw[i] == '>' {
			break
		}
		// attribute name: '=' as its first character is part of it
		ns := i
		if raw[i] == '=' {
			i++
		}
		for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' && raw[i] != '=' {
			i++
		}
		a := scannedAttr{name: asciiLower(string(raw[ns:i])), nameS: span{ns, i}, valS: span{-1, -1}}
		// after attribute name
		j := i
		for j < len(raw) && isSpace(raw[j]) {
			j++
		}
		if j < len(raw) && raw[j] == '=' {
			j++
			for j < len(raw) && isSpace(raw[j]) {
				j++
			}
			switch {
			case j < len(raw) && (raw[j] == '"' || raw[j] == '\''):
				q := raw[j]
				k := strings.IndexByte(string(raw[j+1:]), q)
				if k < 0 {
					a.valS = span{j + 1, len(raw)}
					j = len(raw)
				} else {
					a.valS = span{j + 1, j + 1 + k}
					j += 2 + k
				}
			case j < len(raw) && raw[j] == '>':
				a.valS = span{j, j} // a missing value: empty
			default:
				s := j
				for j < len(raw) && !isSpace(raw[j]) && raw[j] != '>' {
					j++
				}
				a.valS = span{s, j}
			}
			i = j
		}
		out = append(out, a)
	}
	return out
}

// asciiLower lower-cases ASCII letters only, as the standard (and x/net) do
// for tag and attribute names.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// attrValue is the value x/net's tokenizer reports for a raw attribute
// value: newlines normalized, then character references decoded as in an
// attribute (HTML §13.2.5.72–73) — a named reference only when the whole
// alphanumeric run (with its ';', if any) is one, and not when it lacks ';'
// and '=' follows; no longest-prefix match as in text.
func attrValue(raw string) string {
	s := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		n, dec := charRef(s[i:])
		b.WriteString(dec)
		i += n
	}
	return b.String()
}

// charRef decodes the character reference at the start of s (at '&') as
// x/net does in an attribute value, returning the bytes consumed and the
// decoded text.
func charRef(s string) (int, string) {
	if len(s) <= 1 {
		return 1, "&"
	}
	i := 1
	if s[1] == '#' {
		if len(s) <= 3 {
			return 1, "&"
		}
		i = 2
		hex := s[i] == 'x' || s[i] == 'X'
		if hex {
			i++
		}
		digits := i
		for i < len(s) && (isDigit(s[i]) || hex && isHex(s[i])) {
			i++
		}
		if i == digits {
			// x/net (not the standard) reads "&#x;" as a reference to
			// U+0000, replaced by U+FFFD.
			if hex && i < len(s) && s[i] == ';' {
				return i + 1, "�"
			}
			return 1, "&"
		}
		if i < len(s) && s[i] == ';' {
			i++
		}
		// The numeric decoding (ranges, Windows-1252, U+FFFD) is the same
		// in text and in attributes.
		return i, xhtml.UnescapeString(s[:i])
	}
	for i < len(s) && isAlnum(s[i]) {
		i++
	}
	if i < len(s) && s[i] == ';' {
		i++
	}
	name := s[1:i]
	switch {
	case name == "":
		return i, s[:i]
	case name[len(name)-1] != ';' && i < len(s) && s[i] == '=':
		return i, s[:i]
	}
	// UnescapeString decodes the entity the whole name is, or else (in text,
	// not in attributes) the longest prefix of at least two characters that
	// is one, leaving the rest of the name: the decoding is exact unless it
	// is such a prefix's.
	dec := xhtml.UnescapeString(s[:i])
	if dec == s[:i] {
		return i, s[:i]
	}
	for j := len(name) - 1; j > 1; j-- {
		p := "&" + name[:j]
		if d := xhtml.UnescapeString(p); d != p && dec == d+name[j:] {
			return i, s[:i]
		}
	}
	return i, dec
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
func isHex(c byte) bool   { return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }
func isAlnum(c byte) bool { return isDigit(c) || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
