// Package declname decides whether a text of a data or markup file may be
// a declaration's name. Names leave the MCP server as identifiers (paths,
// names and kinds are not masked as content is): a multi-line or
// control-character "name" could forge a line of a text result, and a long
// one is data, not a name. Every provider of a data or markup language
// (YAML, TOML, XML, Markdown) declares only what this accepts.
package declname

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/secrets"
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

// HoldsSecret reports whether s, a would-be name, holds a secret by Ark's
// recommended secret policy: a known token format as written (before a
// slug lowers its case), or a value the policy masks after an "=" or a ":"
// ("password = …", "api_key: …"). A name is shown as written (the MCP
// server masks names by a token's form only), so a provider declares or
// references nothing by such a name. A value after a "-" (the policy's
// reading of "password-stdin" in a text) is a word of a name, not a
// secret. A format character (a zero-width space) in s hides nothing: s is
// also checked without them.
func HoldsSecret(s string) bool {
	if holdsSecret(s) {
		return true
	}
	if t := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s); t != s {
		return holdsSecret(t)
	}
	return false
}

func holdsSecret(s string) bool {
	p := secrets.Recommended()
	if len(p.Find(s, secrets.Mode{Tokens: true})) > 0 {
		return true
	}
	for _, f := range p.Find(s, secrets.Mode{}) {
		i := f.Start
		for i > 0 && strings.IndexByte(" \t\"'`", s[i-1]) >= 0 {
			i--
		}
		if i > 0 && (s[i-1] == '=' || s[i-1] == ':') {
			return true
		}
	}
	return false
}
