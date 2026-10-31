// Package declname decides whether a text of a data or markup file may be
// a declaration's name. Names leave the MCP server as identifiers (paths,
// names and kinds are not masked as content is): a multi-line or
// control-character "name" could forge a line of a text result, and a long
// one is data, not a name. Every provider of a data or markup language
// (YAML, TOML, XML, Markdown) declares only what this accepts.
package declname

import (
	"unicode"
	"unicode/utf8"
)

// MaxBytes bounds the bytes of a text that is a declaration's name.
const MaxBytes = 256

// IsName reports whether s may be a declaration's name: one line of at most
// MaxBytes bytes of valid UTF-8 without a control, line break, zero-width
// or bidirectional formatting character.
func IsName(s string) bool {
	if s == "" || len(s) > MaxBytes || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		// Control (Cc), format (Cf: zero-width, bidirectional and other
		// invisible characters), line and paragraph separators.
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Bidi_Control, r) ||
			r == 0x2028 || r == 0x2029 || r == 0x180e {
			return false
		}
	}
	return true
}
