package search

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/testfiles"
)

// This file implements identifier search for search_context: given a partial
// identifier, rank the index's symbols by how closely their names match it.
//
// A rank is search relevance only. It says how the query text relates to a
// symbol's name; it is never evidence that the symbol is what any code refers
// to, and it must not feed resolution, confidence or target lookup (which
// refuses to choose among candidates, ARCHITECTURE.md §3.1). Every match is an
// independent candidate.

// MaxQueryBytes bounds a search query. Identifiers and qualified names are far
// shorter; anything longer is not an identifier query.
const MaxQueryBytes = 256

// MatchType says why a symbol matched, from the strongest relation between the
// query and the symbol's name to the weakest. It is the rank's first key.
type MatchType string

const (
	// MatchExact: Name or Qualified equals the query.
	MatchExact MatchType = "exact"
	// MatchExactCaseInsensitive: Name or Qualified equals the query ignoring case.
	MatchExactCaseInsensitive MatchType = "exact_case_insensitive"
	// MatchPrefix: Name starts with the query, ignoring case.
	MatchPrefix MatchType = "prefix"
	// MatchWordBoundary: the query's words are consecutive words of Name,
	// the last one possibly a prefix ("user_auth" ~ "getUserAuthToken",
	// "auth" ~ "UserAuthenticator"). Words split at camelCase humps, acronym
	// ends and any non-alphanumeric character.
	MatchWordBoundary MatchType = "word_boundary"
	// MatchQualified: a query of several segments ("Service.get") names the
	// trailing segments of Qualified, the last possibly a prefix.
	MatchQualified MatchType = "qualified"
	// MatchSubstring: Name (or, for a segmented query, Qualified) contains the
	// query, ignoring case.
	MatchSubstring MatchType = "substring"
)

var tierOf = map[MatchType]int{
	MatchExact:                0,
	MatchExactCaseInsensitive: 1,
	MatchPrefix:               2,
	MatchWordBoundary:         3,
	MatchQualified:            4,
	MatchSubstring:            5,
}

// SymbolMatch is one ranked candidate.
type SymbolMatch struct {
	Symbol symbol.Symbol
	Type   MatchType
	// extra is how many characters the matched name has beyond the query: of
	// two prefix matches, the one closer to the query ranks first.
	extra  int
	isTest bool
}

// NormalizeQuery trims q and checks that it is an identifier query: not
// empty, at most MaxQueryBytes, valid UTF-8, no control characters.
func NormalizeQuery(q string) (string, error) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return "", fmt.Errorf("query is empty")
	case len(q) > MaxQueryBytes:
		return "", fmt.Errorf("query is %d bytes; the limit is %d (search takes an identifier or qualified name, not text)", len(q), MaxQueryBytes)
	case !utf8.ValidString(q):
		return "", fmt.Errorf("query is not valid UTF-8")
	case strings.IndexFunc(q, unicode.IsControl) >= 0:
		return "", fmt.Errorf("query contains control characters")
	}
	return q, nil
}

// MatchSymbols returns the symbols that match query (already normalized),
// ranked. The order is a function of the query and the set of symbols only —
// never of their input order — so a rebuilt index gives the same ranking:
//
//  1. match type (MatchExact first, MatchSubstring last);
//  2. fewer characters beyond the query in the matched name;
//  3. non-test files before test files (internal/testfiles);
//  4. file path, Qualified, Kind, start line, start column, SymbolID.
//
// A declaration appears at most once, with its strongest match type. A
// declaration is its SymbolID and its location: two declarations sharing an
// ID (e.g. two Go init functions in one file) are both returned, never folded
// into one.
func MatchSymbols(query string, syms []symbol.Symbol) []SymbolMatch {
	q := newMatcher(query)
	type declKey struct {
		id   symbol.SymbolID
		file string
		pos  source.Position
	}
	seen := make(map[declKey]bool, len(syms))
	var out []SymbolMatch
	for _, s := range syms {
		key := declKey{s.ID, string(s.Location.File), s.Location.Range.Start}
		if seen[key] {
			continue
		}
		t, extra, ok := q.match(s)
		if !ok {
			continue
		}
		seen[key] = true
		out = append(out, SymbolMatch{Symbol: s, Type: t, extra: extra, isTest: testfiles.IsTestFile(string(s.Location.File))})
	}
	sort.Slice(out, func(i, j int) bool { return lessMatch(out[i], out[j]) })
	return out
}

func lessMatch(a, b SymbolMatch) bool {
	if ta, tb := tierOf[a.Type], tierOf[b.Type]; ta != tb {
		return ta < tb
	}
	if a.extra != b.extra {
		return a.extra < b.extra
	}
	if a.isTest != b.isTest {
		return !a.isTest
	}
	sa, sb := a.Symbol, b.Symbol
	if pa, pb := filepath.ToSlash(string(sa.Location.File)), filepath.ToSlash(string(sb.Location.File)); pa != pb {
		return pa < pb
	}
	if sa.Qualified != sb.Qualified {
		return sa.Qualified < sb.Qualified
	}
	if sa.Kind != sb.Kind {
		return sa.Kind < sb.Kind
	}
	if la, lb := sa.Location.Range.Start.Line, sb.Location.Range.Start.Line; la != lb {
		return la < lb
	}
	if ca, cb := sa.Location.Range.Start.Column, sb.Location.Range.Start.Column; ca != cb {
		return ca < cb
	}
	return sa.ID < sb.ID
}

type matcher struct {
	raw      string
	lower    string
	runes    int
	words    []string // lower-cased words of the query
	segments []string // lower-cased identity segments; len > 1 for a qualified query
}

func newMatcher(query string) matcher {
	lower := strings.ToLower(query)
	return matcher{
		raw:      query,
		lower:    lower,
		runes:    utf8.RuneCountInString(query),
		words:    SplitWords(query),
		segments: segments(lower),
	}
}

// match classifies s, returning its match type and how many characters the
// matched name has beyond the query.
func (m matcher) match(s symbol.Symbol) (MatchType, int, bool) {
	name, qual := s.Name, s.Qualified
	nameExtra := utf8.RuneCountInString(name) - m.runes
	qualExtra := utf8.RuneCountInString(qual) - m.runes
	switch {
	case name == m.raw:
		return MatchExact, 0, true
	case qual == m.raw:
		return MatchExact, 0, true
	case strings.EqualFold(name, m.raw):
		return MatchExactCaseInsensitive, 0, true
	case strings.EqualFold(qual, m.raw):
		return MatchExactCaseInsensitive, 0, true
	}
	lname := strings.ToLower(name)
	if strings.HasPrefix(lname, m.lower) {
		return MatchPrefix, nameExtra, true
	}
	// A word match needs every query word inside the name; testing the first
	// one spares splitting names that cannot match.
	if len(m.words) > 0 && strings.Contains(lname, m.words[0]) && wordMatch(m.words, SplitWords(name)) {
		return MatchWordBoundary, nameExtra, true
	}
	if len(m.segments) > 1 && segmentSuffixMatch(m.segments, segments(strings.ToLower(qual))) {
		return MatchQualified, qualExtra, true
	}
	if strings.Contains(lname, m.lower) {
		return MatchSubstring, nameExtra, true
	}
	if len(m.segments) > 1 && strings.Contains(strings.ToLower(qual), m.lower) {
		return MatchSubstring, qualExtra, true
	}
	return "", 0, false
}

// wordMatch reports whether q is a run of consecutive words of name, the last
// query word possibly a prefix of its name word.
func wordMatch(q, name []string) bool {
	if len(q) == 0 || len(q) > len(name) {
		return false
	}
	last := len(q) - 1
outer:
	for i := 0; i+last < len(name); i++ {
		for j := range last {
			if name[i+j] != q[j] {
				continue outer
			}
		}
		if strings.HasPrefix(name[i+last], q[last]) {
			return true
		}
	}
	return false
}

// segmentSuffixMatch reports whether q names the trailing segments of qual:
// q's segments equal qual's last len(q) segments, except that q's last
// segment may be a prefix of qual's last one.
func segmentSuffixMatch(q, qual []string) bool {
	if len(q) > len(qual) {
		return false
	}
	off := len(qual) - len(q)
	last := len(q) - 1
	for j := range last {
		if qual[off+j] != q[j] {
			return false
		}
	}
	return strings.HasPrefix(qual[off+last], q[last])
}

// segments splits a (lower-cased) qualified name at identity segment
// boundaries: every ASCII character that cannot be part of an identifier
// ("." "::" "\" "/" "->" ...). Multi-byte characters are identifier
// characters, as in target lookup (mcp/target_lookup.go).
func segments(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r < utf8.RuneSelf && !(r == '_' || r == '$' ||
			('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z'))
	})
}

// SplitWords splits an identifier into lower-cased words: at every character
// that is neither a letter nor a digit, at a lower-to-upper case change
// ("getUser" → get, user), and before the last capital of a capital run
// followed by a lower-case letter ("HTTPServer" → http, server). Digits stay
// with the word they follow. Letters without case (e.g. CJK) never split.
func SplitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if unicode.IsUpper(r) && len(cur) > 0 {
			prev := cur[len(cur)-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}
