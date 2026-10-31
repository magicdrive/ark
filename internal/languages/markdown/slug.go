package markdown

import (
	"strconv"
	"strings"
	"unicode"
)

// GitHub's heading anchors. GitHub gives each heading of a rendered
// Markdown file the id its anchor links name ("#install"), by the rule of
// github-slugger (github.com/Flet/github-slugger, used by GitHub and by the
// tools that check its links):
//
//   - the heading's text content is lower-cased (JavaScript toLowerCase:
//     full case mapping — "İ" is "i̇", a final "Σ" is "ς");
//   - every character but a letter, a mark, a decimal digit, a letter
//     number, a connector punctuation ("_"), a hyphen or a space (U+0020)
//     is removed — punctuation, symbols and emoji, other spaces, controls;
//   - each space becomes "-" (spaces are not collapsed: "a  b" is "a--b");
//   - the second heading of an anchor is "<anchor>-1", the third
//     "<anchor>-2", skipping any anchor already taken.
//
// The character classes are Go's unicode tables, of the Unicode version Go
// ships; github-slugger's are Unicode 13: a character assigned since may
// differ.

// slugger hands out the anchors of one document's headings in order.
type slugger struct{ seen map[string]int }

func newSlugger() *slugger { return &slugger{seen: map[string]int{}} }

// next returns the anchor of the next heading whose text content is text.
func (s *slugger) next(text string) string {
	base := slug(text)
	out := base
	if _, dup := s.seen[base]; dup {
		for {
			s.seen[base]++
			out = base + "-" + strconv.Itoa(s.seen[base])
			if _, taken := s.seen[out]; !taken {
				break
			}
		}
	}
	s.seen[out] = 0
	return out
}

// slug is github-slugger's slug of text, without the duplicate rule.
func slug(text string) string {
	var b strings.Builder
	for _, r := range jsLower(text) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || unicode.IsLetter(r) || unicode.IsMark(r) ||
			unicode.Is(unicode.Nd, r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Pc, r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// jsLower lower-cases s as JavaScript's String.prototype.toLowerCase does
// (Unicode full case mapping without locale): Go's simple mapping, but
// U+0130 (İ) is "i" and a combining dot above, and a capital sigma that
// ends a word is the final sigma.
func jsLower(s string) string {
	if !strings.ContainsAny(s, "İΣ") {
		return strings.ToLower(s)
	}
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		switch r {
		case 'İ':
			b.WriteString("i̇")
		case 'Σ':
			if finalSigma(rs, i) {
				b.WriteRune('ς')
			} else {
				b.WriteRune('σ')
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// finalSigma is the Unicode Final_Sigma condition at rs[i]: a cased letter
// before it and none after it, case-ignorable characters skipped.
func finalSigma(rs []rune, i int) bool {
	cased := func(r rune) bool { return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) }
	ignorable := func(r rune) bool {
		return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) ||
			unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r) || r == '\'' || r == '.' || r == ':' || r == '·' || r == '’'
	}
	before := false
	for j := i - 1; j >= 0; j-- {
		if ignorable(rs[j]) {
			continue
		}
		before = cased(rs[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(rs); j++ {
		if ignorable(rs[j]) {
			continue
		}
		return !cased(rs[j])
	}
	return true
}
