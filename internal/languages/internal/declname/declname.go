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
// slug lowers its case), or a value the policy masks in a text ("password
// = …", "api_key: …", "password => …"). A name is shown as written (the
// MCP server masks names by a token's form only), so a provider declares
// or references nothing by such a name. A word glued to a word by a "-"
// (the policy's reading of "password-stdin" in a text) is a part of a
// name, not a secret. A format character (a zero-width space) in s hides
// nothing: s is also checked without them.
func HoldsSecret(s string) bool { return holdsSecretGlued(s, "-") }

// HoldsSecretQName is HoldsSecret for an XML path, in which a ":" glued
// between words separates a prefix from a local name ("secret:key").
func HoldsSecretQName(s string) bool { return holdsSecretGlued(s, "-:") }

func holdsSecretGlued(s, glue string) bool {
	if holdsSecret(s, glue) {
		return true
	}
	if t := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s); t != s {
		return holdsSecret(t, glue)
	}
	return false
}

func holdsSecret(s, glue string) bool {
	p := secrets.Recommended()
	if len(p.Find(s, secrets.Mode{Tokens: true})) > 0 {
		return true
	}
	for _, f := range p.Find(s, secrets.Mode{}) {
		// A value right after a glue byte that follows a word character,
		// itself no assignment: one name ("password-stdin"), not an
		// assignment ("password-x=y" is one).
		if i := f.Start; i >= 2 && f.End > i && strings.IndexByte(glue, s[i-1]) >= 0 && wordByte(s[i-2]) && wordByte(s[i]) &&
			!strings.ContainsAny(s[i:f.End], "=:") {
			continue
		}
		return true
	}
	return false
}

func wordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= 0x80
}
