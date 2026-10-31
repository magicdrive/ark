package mcp

import (
	"slices"
	"strings"
	"unicode/utf8"

	arkctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/impact"
	"github.com/magicdrive/ark/internal/secrets"
	"github.com/magicdrive/ark/internal/source"
)

// Identifiers in a text result.
//
// The text of get_context and analyze_change_impact mixes source with the
// paths and declaration names of what it lists. The sanitizer masks a text
// result whole (sanitize.go), so a path read as an assignment —
// "auth/token-store.go" is "token" "-" "store.go" to the generic rules —
// lost its end. A JSON result never masks a path and masks a name only by
// what is a secret by its own form (secrets.Mode.Tokens); the text masks
// them alike: its identifierSpans say where they are, and the sanitizer
// masks every other byte as before and each identifier by the token rules
// alone (maskAroundIdentifiers). A path or a name is display text there,
// not the value a client passes back (that is the JSON format's), so a
// token in one is still masked.
//
// The spans come from formatting the result twice: once as it is, and once
// with each identifier replaced by a marker of as many runes (so padding is
// alike) that no source can hold ambiguously — runes of the supplementary
// private use planes, which the marked text must not otherwise contain. The
// spans are used only when filling the markers in gives exactly the text;
// anything else (a source with such a rune, a format that escapes or
// changes an identifier) leaves the text with no spans, masked whole as
// before.

// identifierSpan is a byte range [start, end) of a result's text holding one
// path or declaration name.
type identifierSpan struct{ start, end int }

const (
	markerFirst  = rune(0xF0000)  // marker i starts with markerFirst+i
	markerLimit  = 0xFFFD         // markers per text (U+FFFFE/U+FFFFF are noncharacters)
	markerFiller = rune(0x100000) // the rest of a marker
)

// isMarkerRune reports whether r is in the planes markers are drawn from.
func isMarkerRune(r rune) bool { return r >= markerFirst && r <= utf8.MaxRune }

// identifierMarks collects the identifiers of one text being formatted.
type identifierMarks struct{ ids []string }

// mark returns the marker standing for s, or s when it cannot be marked (an
// empty identifier, too many of them): such an identifier is text, masked
// as text.
func (m *identifierMarks) mark(s string) string {
	n := utf8.RuneCountInString(s)
	if n == 0 || len(m.ids) >= markerLimit {
		return s
	}
	var b strings.Builder
	b.WriteRune(markerFirst + rune(len(m.ids)))
	for i := 1; i < n; i++ {
		b.WriteRune(markerFiller)
	}
	m.ids = append(m.ids, s)
	return b.String()
}

// spans fills the markers of marked in and returns where the identifiers
// are, or nil unless the filled text is exactly want.
func (m *identifierMarks) spans(marked, want string) []identifierSpan {
	if len(m.ids) == 0 {
		return nil
	}
	var out strings.Builder
	var spans []identifierSpan
	for i := 0; i < len(marked); {
		r, size := utf8.DecodeRuneInString(marked[i:])
		if !isMarkerRune(r) {
			out.WriteString(marked[i : i+size])
			i += size
			continue
		}
		k := int(r - markerFirst)
		if r < markerFirst || k >= len(m.ids) {
			return nil
		}
		id := m.ids[k]
		i += size
		for j := 1; j < utf8.RuneCountInString(id); j++ {
			f, fs := utf8.DecodeRuneInString(marked[i:])
			if f != markerFiller {
				return nil
			}
			i += fs
		}
		spans = append(spans, identifierSpan{out.Len(), out.Len() + len(id)})
		out.WriteString(id)
	}
	if out.String() != want {
		return nil
	}
	return spans
}

// maskAroundIdentifiers masks text with p in mode, but each span by the
// token rules alone. Every byte outside the spans is masked as the whole
// text would mask it with each identifier replaced by one marker rune: the
// identifiers can neither hide nor start a secret around them. When the
// spans do not fit text, text holds a marker rune, or masking swallows a
// marker, text is masked whole.
func maskAroundIdentifiers(text string, spans []identifierSpan, mode secrets.Mode, p *secrets.Policy) string {
	whole := func() string { return p.Mask(text, mode) }
	if len(spans) == 0 || len(spans) > markerLimit || strings.IndexFunc(text, isMarkerRune) >= 0 {
		return whole()
	}
	var in strings.Builder
	prev := 0
	for k, s := range spans {
		if s.start < prev || s.end <= s.start || s.end > len(text) {
			return whole()
		}
		in.WriteString(text[prev:s.start])
		in.WriteRune(markerFirst + rune(k))
		prev = s.end
	}
	in.WriteString(text[prev:])
	masked := p.Mask(in.String(), mode)
	tokens := mode
	tokens.Tokens = true
	var out strings.Builder
	k := 0
	for i := 0; i < len(masked); {
		r, size := utf8.DecodeRuneInString(masked[i:])
		if !isMarkerRune(r) {
			out.WriteString(masked[i : i+size])
			i += size
			continue
		}
		if k >= len(spans) || r != markerFirst+rune(k) {
			return whole()
		}
		s := spans[k]
		out.WriteString(p.Mask(text[s.start:s.end], tokens))
		k++
		i += size
	}
	if k != len(spans) {
		return whole()
	}
	return out.String()
}

// contextTextSpans returns where get_context's text (arkctx.Format of r)
// holds each item's path and qualified name.
func contextTextSpans(r *arkctx.Result, text string) []identifierSpan {
	if r == nil {
		return nil
	}
	var m identifierMarks
	c := *r
	c.Items = slices.Clone(r.Items)
	for i := range c.Items {
		s := &c.Items[i].Symbol
		s.Location.File = source.FileID(m.mark(string(s.Location.File)))
		s.Qualified = m.mark(s.Qualified)
	}
	return m.spans(arkctx.Format(&c), text)
}

// impactTextSpans returns where analyze_change_impact's text (impact.Format
// of r) holds the target's, every entry's and every affected file's path and
// qualified name, and the path of every unresolved reference — not its name
// or receiver, which are source text.
func impactTextSpans(r *impact.ImpactResult, text string) []identifierSpan {
	if r == nil {
		return nil
	}
	var m identifierMarks
	c := *r
	c.Target.Qualified = m.mark(c.Target.Qualified)
	c.Target.Location.File = source.FileID(m.mark(string(c.Target.Location.File)))
	c.Entries = slices.Clone(r.Entries)
	for i := range c.Entries {
		s := &c.Entries[i].Symbol
		s.Qualified = m.mark(s.Qualified)
		s.Location.File = source.FileID(m.mark(string(s.Location.File)))
	}
	c.AffectedFiles = slices.Clone(r.AffectedFiles)
	for i, f := range c.AffectedFiles {
		c.AffectedFiles[i] = source.FileID(m.mark(string(f)))
	}
	c.UnresolvedCallees.References = slices.Clone(r.UnresolvedCallees.References)
	for i := range c.UnresolvedCallees.References {
		l := &c.UnresolvedCallees.References[i].Location
		l.File = source.FileID(m.mark(string(l.File)))
	}
	return m.spans(impact.Format(&c), text)
}
