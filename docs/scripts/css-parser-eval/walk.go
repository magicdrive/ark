//go:build ignore

package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// The structure of a stylesheet as Ark's CSS provider reads it: rules,
// at-rules and declarations with their byte spans (nested rules included,
// CSS Syntax Level 3 current draft), and the places where written content
// is lost or a block is left open.

type item struct {
	kind       string // "rule", "@name", "decl", or a problem "!…"
	name       string // a declaration's name
	start, end int    // the item's bytes
	depth      int
	text       string // prelude or value (whitespace collapsed, for display)
	block      bool   // a rule or at-rule with a {} block
}

type walker struct {
	src   []byte
	items []item
	deep  bool
	match []int // per token: the index after the block or function it opens (matching)
}

// matching computes, for each opener ({ [ ( or a function), the index just
// past its closing token (or the end), once: a declaration that turns out
// to be none is given up after stepping over its blocks, never reading them.
func matching(ts []token) []int {
	m := make([]int, len(ts))
	type open struct {
		at    int
		close tokType
	}
	var stack []open
	for i, t := range ts {
		switch t.typ {
		case tLBrace:
			stack = append(stack, open{i, tRBrace})
		case tLBracket:
			stack = append(stack, open{i, tRBracket})
		case tLParen, tFunction:
			stack = append(stack, open{i, tRParen})
		case tRBrace, tRBracket, tRParen:
			// a closer ends the innermost block when it is that block's
			// closer; otherwise it is a token of that block (componentValue)
			if n := len(stack); n > 0 && stack[n-1].close == t.typ {
				m[stack[n-1].at] = i + 1
				stack = stack[:n-1]
			}
		}
	}
	for _, o := range stack {
		m[o.at] = len(ts)
	}
	return m
}

func (w *walker) show(a, b int) string {
	s := strings.Join(strings.Fields(string(w.src[a:b])), " ")
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return s
}

func (w *walker) problem(kind string, at, end, depth int) {
	w.items = append(w.items, item{kind: kind, start: at, end: end, depth: depth})
}

// sheet reads a whole stylesheet.
func sheet(src []byte) *walker {
	w := &walker{src: src}
	if len(src) >= 2 && (src[0] == 0xFE && src[1] == 0xFF || src[0] == 0xFF && src[1] == 0xFE) {
		w.problem("!encoding utf-16", 0, 2, 0)
	}
	all := specTokens(src, false)
	// a NUL or an invalid UTF-8 byte (read as U+FFFD), once
	for i := 0; i < len(src); {
		if src[i] == 0 {
			w.problem("!replacement-character", i, i+1, 0)
			break
		}
		if src[i] < utf8.RuneSelf {
			i++
			continue
		}
		if r, n := utf8.DecodeRune(src[i:]); r == utf8.RuneError && n == 1 {
			w.problem("!replacement-character", i, i+1, 0)
			break
		} else {
			i += n
		}
	}
	for _, t := range all {
		switch {
		case t.typ == tComment && t.eofIn:
			w.problem("!eof-in-comment", t.start, t.end, 0)
		case t.typ == tBadString:
			w.problem("!bad-string", t.start, t.end, 0)
		case t.typ == tBadURL:
			w.problem("!bad-url", t.start, t.end, 0)
		case t.eofIn && t.typ == tString:
			w.problem("!eof-in-string", t.start, t.end, 0)
		case t.eofIn && t.typ == tURL:
			w.problem("!eof-in-url", t.start, t.end, 0)
		}
	}
	toks := withoutCommentsP(all)
	if len(toks) > 0 && toks[0].typ == tAtKeyword && toks[0].value == "charset" {
		if len(toks) > 2 && toks[2].typ == tString && !map[string]bool{"unicode-1-1-utf-8": true, "unicode11utf8": true,
			"unicode20utf8": true, "utf-8": true, "utf8": true, "x-unicode20utf8": true}[strings.ToLower(strings.TrimSpace(toks[2].value))] {
			w.problem("!encoding "+toks[2].value, toks[0].start, toks[2].end, 0)
		}
	}
	w.match = matching(toks)
	w.list(&parser{toks: toks, deep: &w.deep}, true, 0)
	if w.deep {
		w.problem("!too-deep", 0, len(src), 0)
	}
	return w
}

// list reads a list of rules (top) or a block's contents from p, once:
// a block's contents end at its "}" (left for the caller) or at the end.
func (w *walker) list(p *parser, top bool, depth int) {
	for {
		t := p.peek()
		switch {
		case t.typ == tEOF, !top && t.typ == tRBrace:
			return
		case t.typ == tWhitespace || t.typ == tSemicolon || top && (t.typ == tCDO || t.typ == tCDC):
			p.next()
			continue
		case t.typ == tAtKeyword:
			w.atRule(p, top, depth)
			continue
		}
		if !top && t.typ == tIdent && w.declaration(p, depth) {
			continue
		}
		w.qualifiedRule(p, top, depth)
	}
}

// declaration reads a declaration (current draft, nested), leaving p
// unmoved when there is none. It stops as soon as the result is known not
// to be one, so a block of rules costs about what reading it once does: an
// ident not followed by a colon; a top-level {} block after other content,
// or other content after one ("!important" aside) — unless the name is a
// custom property's, whose value may hold anything.
func (w *walker) declaration(p *parser, depth int) bool {
	mark := p.i
	name := p.next()
	p.skipWS()
	if p.peek().typ != tColon {
		p.i = mark
		return false
	}
	colon := p.next()
	custom := strings.HasPrefix(name.value, "--")
	content, block := false, false
	last := colon.end
	for {
		t := p.peek()
		if t.typ == tEOF || t.typ == tSemicolon || t.typ == tRBrace {
			break
		}
		if !custom && block && t.typ != tWhitespace {
			if n := importantEnd(p); n > 0 { // "!important" after a {} value
				last = p.toks[n-1].end
				p.i = n
				continue
			}
			p.i = mark
			return false
		}
		if !custom && t.typ == tLBrace && content {
			p.i = mark
			return false
		}
		switch t.typ {
		case tLBrace:
			block = true
		case tWhitespace:
		default:
			content = true
		}
		switch t.typ {
		case tLBrace, tLBracket, tLParen, tFunction:
			p.i = w.match[p.i] // step over the block: read once, by whoever keeps it
		default:
			p.next()
		}
		if t.typ != tWhitespace {
			last = p.toks[p.i-1].end
		}
	}
	w.items = append(w.items, item{kind: "decl", name: name.value, start: name.start, end: last, depth: depth, text: w.show(colon.end, last)})
	return true
}

// importantEnd returns the index past "!" whitespace "important" at p, or
// 0 when p is not there.
func importantEnd(p *parser) int {
	if t := p.peek(); t.typ != tDelim || t.value != "!" {
		return 0
	}
	for k := p.i + 1; k < len(p.toks); k++ {
		switch t := p.toks[k]; {
		case t.typ == tWhitespace:
		case t.typ == tIdent && equalFoldASCII(t.value, "important"):
			return k + 1
		default:
			return 0
		}
	}
	return 0
}

func lastEnd(p *parser, def int) int {
	if p.i > 0 && p.i <= len(p.toks) {
		return p.toks[p.i-1].end
	}
	return def
}

// atRule reads an at-rule: its prelude up to ";" (consumed), the end, a
// nested block's "}" (not consumed) or its {} block.
func (w *walker) atRule(p *parser, top bool, depth int) {
	at := p.next()
	for {
		k := p.peek()
		switch {
		case k.typ == tEOF || k.typ == tSemicolon || !top && k.typ == tRBrace:
			end := lastEnd(p, at.end)
			if k.typ == tSemicolon {
				p.next()
				end = k.end
			}
			w.items = append(w.items, item{kind: "@" + at.value, start: at.start, end: end, depth: depth, text: w.show(at.end, k.start)})
			return
		case k.typ == tLBrace:
			w.blockItem(p, item{kind: "@" + at.value, start: at.start, depth: depth, text: w.show(at.end, k.start)})
			return
		}
		p.componentValue(nil)
	}
}

// qualifiedRule reads a qualified rule: its prelude and {} block, or an
// invalid one ended by the end of the input or, nested, ";" or "}".
func (w *walker) qualifiedRule(p *parser, top bool, depth int) {
	first := p.peek()
	start := p.i
	for {
		k := p.peek()
		switch {
		case k.typ == tEOF || !top && (k.typ == tSemicolon || k.typ == tRBrace):
			if k.typ == tSemicolon {
				p.next()
			}
			w.problem("!invalid", first.start, lastEnd(p, first.end), depth)
			return
		case k.typ == tLBrace:
			prelude := p.toks[start:p.i]
			if custom, colon := preludeHead(prelude); custom && colon {
				// "--x: … {": a custom property, never a rule (nested: the
				// bad declaration's remnants; top level: the block too)
				if top {
					p.next()
					p.block("{}", tRBrace)
				} else {
					for t := p.peek().typ; t != tEOF && t != tSemicolon && t != tRBrace; t = p.peek().typ {
						p.componentValue(nil)
					}
				}
				w.problem("!invalid", first.start, lastEnd(p, first.end), depth)
				return
			} else if colon && !top && declarationLike(prelude) {
				// "a: b c {": a declaration whose ";" is missing, read as a rule
				w.problem("!rule-from-declaration", first.start, k.start, depth)
			}
			if top {
				for _, t := range prelude {
					if t.typ == tSemicolon || t.typ == tRBrace {
						w.problem("!stray-in-prelude", t.start, t.end, depth)
						break
					}
				}
			}
			w.blockItem(p, item{kind: "rule", start: first.start, depth: depth, text: w.show(first.start, k.start)})
			return
		}
		p.componentValue(nil)
	}
}

// preludeHead reports whether a prelude starts with an ident beginning
// "--" (custom) and whether its first two non-whitespace tokens are an
// ident and a colon.
func preludeHead(prelude []token) (custom, colon bool) {
	var firsts []token
	for _, t := range prelude {
		if t.typ != tWhitespace {
			firsts = append(firsts, t)
			if len(firsts) == 2 {
				break
			}
		}
	}
	if len(firsts) < 2 || firsts[0].typ != tIdent || firsts[1].typ != tColon {
		return false, false
	}
	return strings.HasPrefix(firsts[0].value, "--"), true
}

// blockItem reads the {} block at p (the item's prelude read): its
// contents, once, then its "}" — or the end of the input (not closed).
// Past maxDepth the contents are skipped as component values, unread.
func (w *walker) blockItem(p *parser, it item) {
	p.next() // {
	it.block = true
	k := len(w.items)
	w.items = append(w.items, it)
	if it.depth+1 >= maxDepth {
		w.deep = true
		for t := p.peek().typ; t != tEOF && t != tRBrace; t = p.peek().typ {
			p.componentValue(nil)
		}
	} else {
		w.list(p, false, it.depth+1)
	}
	if p.peek().typ == tRBrace {
		w.items[k].end = p.next().end
		return
	}
	w.items[k].end = len(w.src)
	w.items = append(w.items, item{kind: "!eof-in-block", start: it.start, end: len(w.src), depth: it.depth})
}

// outline lists the items, one line each, indented by nesting.
func outline(src []byte) []string {
	var out []string
	for _, it := range sheet(src).items {
		ind := strings.Repeat("  ", it.depth)
		span := " [" + strconv.Itoa(it.start) + "," + strconv.Itoa(it.end) + ")"
		switch {
		case it.kind == "decl":
			out = append(out, ind+"decl "+it.name+span+" "+it.text)
		case strings.HasPrefix(it.kind, "!"):
			out = append(out, ind+it.kind+span)
		default:
			out = append(out, ind+it.kind+span+" "+it.text)
		}
	}
	return out
}

// structure counts the rules (blocks) and declarations of a stylesheet.
func structure(src []byte) (rules, decls int) {
	for _, it := range sheet(src).items {
		switch {
		case it.kind == "decl":
			decls++
		case it.block:
			rules++
		}
	}
	return rules, decls
}

func withoutCommentsP(ts []token) []token {
	var out []token
	for _, t := range ts {
		if t.typ != tComment {
			out = append(out, t)
		}
	}
	return out
}

// declarationLike reports whether a nested rule's prelude "ident : …" reads
// as a declaration that lost its ";", not as a selector: what follows the
// colon is whitespace or no ident, function or colon ("a:hover",
// "li:first-child", "a::marker", "a:not(.x)" are selectors).
func declarationLike(prelude []token) bool {
	k := 0
	for prelude[k].typ == tWhitespace {
		k++
	}
	k++ // the ident
	for prelude[k].typ == tWhitespace {
		k++
	}
	k++ // the colon
	if k >= len(prelude) {
		return true
	}
	switch prelude[k].typ {
	case tIdent, tFunction, tColon:
		return false
	}
	return true
}
