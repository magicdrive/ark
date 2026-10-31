package secrets

import (
	"regexp"
	"strings"
)

// The masking pipeline.
//
// A policy masks a text in steps, each of which finds spans of the text as
// the previous steps left it and replaces each with maskText — exactly the
// steps, in exactly the order, of v6.1.0's masking:
//
//  1. whole PEM private-key blocks, across lines (pem-private-key-block);
//  2. line by line (and, in compact text, segment by segment between "␤"):
//     a line of an open heredoc or YAML block scalar body is masked whole;
//     any other line has every pattern rule applied in order, then the
//     assignment rules, and may open such a body.
//
// A step's spans are cuts. Mask applies them; Find records, for each, the
// span of the original text it covers — the text keeps track of where each
// of its bytes came from (ptext) — and so finds exactly what Mask masks:
// both run this one pipeline.

// maskText replaces every masked span.
const maskText = "*****MASKED*****"

// lineBreakSymbol is what Ark's compact formats write for a line break.
const lineBreakSymbol = "␤"

// cut is a span [start, end) of a text to mask, and the rule (an index into
// the policy's rules) that masks it. A cut of no length inserts a mask (an
// empty quoted value).
type cut struct {
	start, end int
	rule       int
}

// ptext is a text being masked. Without track it is the text alone; with
// track it also maps every byte back to the original text: segs, in order,
// cover the text, a literal segment mapping byte for byte, a mask covering
// the whole original span it replaced.
type ptext struct {
	s      string
	track  bool
	origin int    // with no segs: s[i] came from original offset origin+i
	segs   []pseg // nil: the identity above
}

type pseg struct {
	start, end   int // in s
	oStart, oEnd int // in the original
	mask         bool
}

// spans records what a pipeline found: the rule and original span of each
// cut, in the order the pipeline made them.
type spans []found

type found struct {
	rule         int
	oStart, oEnd int
}

// sub returns the part [a, b) of t, keeping its mapping.
func (t ptext) sub(a, b int) ptext {
	if !t.track {
		return ptext{s: t.s[a:b]}
	}
	if t.segs == nil {
		return ptext{s: t.s[a:b], track: true, origin: t.origin + a}
	}
	segs := t.clip(nil, a, b, 0)
	if segs == nil { // an empty part: where it is in the original
		o, _ := t.orig(a, a)
		return ptext{track: true, origin: o}
	}
	return ptext{s: t.s[a:b], track: true, segs: segs}
}

// clip appends to dst the segments of t covering [a, b), moved to start at
// to.
func (t ptext) clip(dst []pseg, a, b, to int) []pseg {
	if a == b {
		return dst
	}
	if t.segs == nil {
		return append(dst, pseg{start: to, end: to + b - a, oStart: t.origin + a, oEnd: t.origin + b})
	}
	for _, g := range t.segs[t.segAt(a):] {
		if g.start >= b {
			break
		}
		x, y := max(g.start, a), min(g.end, b)
		if y <= x {
			continue
		}
		n := pseg{start: to + x - a, end: to + y - a, oStart: g.oStart, oEnd: g.oEnd, mask: g.mask}
		if !g.mask {
			n.oStart, n.oEnd = g.oStart+x-g.start, g.oStart+y-g.start
		}
		dst = append(dst, n)
	}
	return dst
}

// segAt returns the index of the segment holding byte p (or the last).
func (t ptext) segAt(p int) int {
	lo, hi := 0, len(t.segs)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.segs[mid].start <= p {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// orig returns the original span of [a, b): a mask's whole span where a or
// b falls inside one.
func (t ptext) orig(a, b int) (int, int) {
	if t.segs == nil {
		return t.origin + a, t.origin + b
	}
	var oa, ob int
	if g := t.segs[t.segAt(a)]; a >= g.end { // a is the end of the text
		oa = g.oEnd
	} else if g.mask {
		oa = g.oStart
	} else {
		oa = g.oStart + a - g.start
	}
	if a == b {
		return oa, oa
	}
	if g := t.segs[t.segAt(b-1)]; g.mask {
		ob = g.oEnd
	} else {
		ob = g.oStart + b - g.start
	}
	return oa, ob
}

// apply replaces each of cuts — sorted, not overlapping — by maskText,
// recording each in rec when t is tracked.
func (t *ptext) apply(cuts []cut, rec *spans) {
	if len(cuts) == 0 {
		return
	}
	var b strings.Builder
	b.Grow(len(t.s) + len(cuts)*len(maskText))
	var segs []pseg
	prev := 0
	for _, c := range cuts {
		b.WriteString(t.s[prev:c.start])
		if t.track {
			segs = t.clip(segs, prev, c.start, b.Len()-(c.start-prev))
			oa, ob := t.orig(c.start, c.end)
			*rec = append(*rec, found{c.rule, oa, ob})
			segs = append(segs, pseg{start: b.Len(), end: b.Len() + len(maskText), oStart: oa, oEnd: ob, mask: true})
		}
		b.WriteString(maskText)
		prev = c.end
	}
	b.WriteString(t.s[prev:])
	if t.track {
		segs = t.clip(segs, prev, len(t.s), b.Len()-(len(t.s)-prev))
		t.segs = segs
	}
	t.s = b.String()
}

// compiledPattern is one regular expression of a pattern rule: it masks
// each match, or each match's group when group > 0.
type compiledPattern struct {
	re    *regexp.Regexp
	group int
	rule  int
	// assignment marks a pattern that finds a value by the name assigned to
	// it (JWT_SECRET=…), not by the value's own form: it does not apply to
	// an identifier (Mode.Tokens).
	assignment bool
}

// cuts returns the spans of s the pattern masks. A match of no length masks
// nothing.
func (p compiledPattern) cuts(s string, dst []cut) []cut {
	if p.group == 0 {
		for _, m := range p.re.FindAllStringIndex(s, -1) {
			if m[1] > m[0] {
				dst = append(dst, cut{m[0], m[1], p.rule})
			}
		}
		return dst
	}
	for _, m := range p.re.FindAllStringSubmatchIndex(s, -1) {
		if a, b := m[2*p.group], m[2*p.group+1]; a >= 0 && b > a {
			dst = append(dst, cut{a, b, p.rule})
		}
	}
	return dst
}

// keyBlockPattern matches a whole PEM private-key block, across lines.
var keyBlockPattern = regexp.MustCompile(`(?ms)-----BEGIN (?:RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----.*?-----END (?:RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----`)

// run masks content in mode, recording what it masks in rec when rec is not
// nil, and returns the masked text.
func (p *Policy) run(content string, mode Mode, rec *spans) string {
	doc := ptext{s: content, track: rec != nil}
	if p.pemRule >= 0 {
		var cs []cut
		for _, m := range keyBlockPattern.FindAllStringIndex(content, -1) {
			cs = append(cs, cut{m[0], m[1], p.pemRule})
		}
		doc.apply(cs, rec)
	}
	var out strings.Builder
	out.Grow(len(doc.s))
	var block valueBlock
	var buf []cut
	start := 0
	for start <= len(doc.s) {
		end := strings.IndexByte(doc.s[start:], '\n')
		if end < 0 {
			end = len(doc.s)
		} else {
			end += start
		}
		if start > 0 {
			out.WriteByte('\n')
		}
		line := doc.sub(start, end)
		if !mode.Compact {
			out.WriteString(p.maskLine(&line, &block, mode, rec, &buf))
		} else {
			// "␤" stands for a line break in Ark's compact formats.
			seg := 0
			for {
				i := strings.Index(line.s[seg:], lineBreakSymbol)
				if i < 0 {
					break
				}
				part := line.sub(seg, seg+i)
				out.WriteString(p.maskLine(&part, &block, mode, rec, &buf))
				out.WriteString(lineBreakSymbol)
				seg += i + len(lineBreakSymbol)
			}
			part := line.sub(seg, len(line.s))
			out.WriteString(p.maskLine(&part, &block, mode, rec, &buf))
		}
		start = end + 1
	}
	return out.String()
}

// maskLine masks one line (without its "\n"): a line of an open body whole,
// any other by the pattern rules and then the assignment rules, noting a
// body its last secret-like value opens. In mode.Tokens only the pattern
// rules apply.
func (p *Policy) maskLine(line *ptext, b *valueBlock, mode Mode, rec *spans, buf *[]cut) string {
	syntax := mode.Syntax
	body, _ := strings.CutSuffix(line.s, "\r")
	if b.open {
		trimmed := strings.TrimLeft(body, " \t")
		indent := len(body) - len(trimmed)
		switch {
		case b.heredoc != "":
			if endsHeredoc(trimmed, b.heredoc) {
				b.open = false
				return line.s
			}
			if trimmed == "" {
				return line.s
			}
			line.apply([]cut{{indent, len(body), b.rule}}, rec)
			return line.s
		case trimmed == "":
			return line.s
		case indent > b.indent:
			line.apply([]cut{{indent, len(body), b.rule}}, rec)
			return line.s
		}
		b.open = false
	}
	if p.assign != nil && !mode.Tokens {
		b.opens(p.assign, body)
	}
	for _, pat := range p.patterns {
		if mode.Tokens && pat.assignment {
			continue
		}
		*buf = pat.cuts(line.s, (*buf)[:0])
		line.apply(*buf, rec)
	}
	if p.assign != nil && !mode.Tokens {
		*buf = p.assign.cuts(line.s, syntax, (*buf)[:0])
		line.apply(*buf, rec)
	}
	return line.s
}

// A valueBlock is the value of a secret-like key that continues on the lines
// after the key's: a heredoc ("<<EOT", "<<-EOT", "<<~EOT", "<<<'EOT'"), whose body runs to its
// terminator line, or a YAML block scalar ("|", ">-", ...), whose body is
// every following line indented deeper than the key, and blank lines. Every
// line of a body is masked whole, its indentation kept; masking per line
// alone would mask only the line of the key.
type valueBlock struct {
	heredoc string // the terminator of an open heredoc
	indent  int    // the key's column of an open block scalar, or -1
	open    bool
	rule    int // the rule of the key that opened it
}

var (
	heredocStart     = regexp.MustCompile(`^<<[-~<]?[ \t]*(["']?)([A-Za-z_][A-Za-z0-9_]*)(["']?)[ \t]*$`)
	blockScalarStart = regexp.MustCompile(`^[|>][1-9+-]{0,2}[ \t]*(?:#.*)?$`)
)

// endsHeredoc reports whether a line, its indentation trimmed, is the
// terminator of a heredoc: the word alone, or followed by ";", "," or ")"
// (PHP).
func endsHeredoc(trimmed, word string) bool {
	rest, ok := strings.CutPrefix(trimmed, word)
	return ok && (rest == "" || strings.ContainsAny(rest[:1], ";,)"))
}

// opens notes the body the last secret-like value of line opens, if any.
// Such a value starts with "<<", "|" or ">": a line with none of them opens
// nothing, and is not searched for keys (most lines; the search for every
// key of a line is the costliest step of masking).
func (b *valueBlock) opens(a *assignment, line string) {
	if !strings.ContainsAny(line, "<|>") {
		return
	}
	locs := a.key.FindAllStringSubmatchIndex(line, -1)
	if len(locs) == 0 {
		return
	}
	loc := locs[len(locs)-1]
	if loc[2*a.quote+1] > loc[2*a.quote] { // a quoted value ends on its line
		return
	}
	val := line[loc[1]:]
	if m := heredocStart.FindStringSubmatch(val); m != nil && m[1] == m[3] {
		*b = valueBlock{heredoc: m[2], indent: -1, open: true, rule: a.ruleOf(line, loc)}
		return
	}
	if blockScalarStart.MatchString(val) {
		*b = valueBlock{indent: loc[2*a.keyGroup], open: true, rule: a.ruleOf(line, loc)}
	}
}
