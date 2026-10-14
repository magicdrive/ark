package css

import (
	"strings"
	"unicode/utf8"
)

// The structure of a stylesheet, read as CSS Syntax Level 3 (§5, the
// current draft: nested rules, custom properties) has it, in one pass of
// recursive descent over the tokens: rules, at-rules and declarations with
// their spans, and the places where written content is lost or a block is
// left open. Measured in docs/scripts/css-parser-eval (walk.go; decisions
// D24): the reading there and here must stay the same.

// maxDepth bounds the nesting of blocks and functions: deeper ones are
// stepped over unread and reported, so recursion never exhausts the stack.
const maxDepth = 256

type itemKind int

const (
	itemRule itemKind = iota
	itemAtRule
	itemDecl
	itemProblem
)

type item struct {
	kind       itemKind
	name       string // an at-rule's name, a declaration's name, a problem's code
	start, end int    // the item's bytes
	depth      int
	// Token indexes (into reader.toks): a rule's or at-rule's prelude, a
	// declaration's value (whitespace trimmed at both ends).
	from, to int
	block    bool // a rule or at-rule with a {} block
	parent   int  // index of the enclosing rule or at-rule item, -1 at the top
}

type reader struct {
	src   []byte
	toks  []token // without comments
	match []int   // per opener: the index past its closing token
	i     int
	items []item
	deep  bool
}

// read reads a whole stylesheet.
func read(src []byte) *reader {
	// Real stylesheets have about 0.23 tokens per byte (967 files of the
	// Phase 5 corpus, at most 0.44): a quarter of the length avoids most of
	// the token slice's regrowth (a third of the time otherwise) — capped,
	// as a large file of few tokens (a data URI, a comment) would allocate
	// many times its size for nothing.
	r := &reader{src: src, toks: make([]token, 0, min(len(src)/4, 1<<16)+8)}
	if len(src) >= 2 && (src[0] == 0xFE && src[1] == 0xFF || src[0] == 0xFF && src[1] == 0xFE) {
		r.problem("encoding", 0, 2, 0)
	}
	z := &tokenizer{src: src, pos: bomLength(src)}
	// A NUL or an invalid UTF-8 byte is read as U+FFFD: a name holding one
	// is not the text's (a U+FFFD written as such, or escaped, is).
	if at := replacedByte(src); at >= 0 {
		r.problem("replacement_character", at, at+1, 0)
	}
	for {
		t := z.nextToken()
		if t.typ == tEOF {
			break
		}
		switch {
		case t.typ == tComment && t.eofIn:
			r.problem("eof_in_comment", t.start, t.end, 0)
		case t.typ == tBadString:
			r.problem("bad_string", t.start, t.end, 0)
		case t.typ == tBadURL:
			r.problem("bad_url", t.start, t.end, 0)
		case t.eofIn && t.typ == tString:
			r.problem("eof_in_string", t.start, t.end, 0)
		case t.eofIn && t.typ == tURL:
			r.problem("eof_in_url", t.start, t.end, 0)
		}
		if t.typ != tComment {
			r.toks = append(r.toks, t)
		}
	}
	if len(r.toks) > 2 && r.toks[0].typ == tAtKeyword && r.toks[0].value == "charset" &&
		r.toks[2].typ == tString && !utf8Label[strings.ToLower(strings.TrimSpace(r.toks[2].value))] {
		r.problem("charset", r.toks[0].start, r.toks[2].end, 0)
	}
	r.match = matching(r.toks)
	r.list(true, 0, -1)
	if r.deep {
		r.problem("too_deep", 0, len(src), 0)
	}
	return r
}

func (r *reader) problem(code string, start, end, depth int) {
	r.items = append(r.items, item{kind: itemProblem, name: code, start: start, end: end, depth: depth, parent: -1})
}

func (r *reader) peek() token {
	if r.i < len(r.toks) {
		return r.toks[r.i]
	}
	return token{typ: tEOF, start: len(r.src), end: len(r.src)}
}

func (r *reader) next() token {
	t := r.peek()
	if r.i < len(r.toks) {
		r.i++
	}
	return t
}

// skipValue steps over one component value: a block or function through
// the bracket table, never reading it twice.
func (r *reader) skipValue() {
	switch r.peek().typ {
	case tLBrace, tLBracket, tLParen, tFunction:
		r.i = r.match[r.i]
	default:
		r.next()
	}
}

// matching computes, for each opener ({ [ ( or a function), the index just
// past its closing token (or the end): a closer ends the innermost block
// when it is that block's closer, and is otherwise one of its tokens
// (§5.4.8 consume a simple block).
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

func (r *reader) lastEnd(def int) int {
	if r.i > 0 && r.i <= len(r.toks) {
		return r.toks[r.i-1].end
	}
	return def
}

// list reads a list of rules (top) or a block's contents; a block's
// contents end at its "}" (left for the caller) or at the end.
func (r *reader) list(top bool, depth, parent int) {
	for {
		t := r.peek()
		switch {
		case t.typ == tEOF, !top && t.typ == tRBrace:
			return
		case t.typ == tWhitespace || t.typ == tSemicolon || top && (t.typ == tCDO || t.typ == tCDC):
			r.next()
			continue
		case t.typ == tAtKeyword:
			r.atRule(top, depth, parent)
			continue
		}
		if !top && t.typ == tIdent && r.declaration(depth, parent) {
			continue
		}
		r.qualifiedRule(top, depth, parent)
	}
}

// declaration reads a declaration (current draft, nested), leaving the
// reader unmoved when there is none. It gives up as soon as the result is
// known not to be one — an ident not followed by a colon; a top-level {}
// block after other content, or other content after one ("!important"
// aside), unless the name is a custom property's — stepping over blocks
// without reading them.
func (r *reader) declaration(depth, parent int) bool {
	mark := r.i
	name := r.next()
	for r.peek().typ == tWhitespace {
		r.next()
	}
	if r.peek().typ != tColon {
		r.i = mark
		return false
	}
	colon := r.next()
	custom := strings.HasPrefix(name.value, "--")
	content, block := false, false
	from, to := -1, -1
	for {
		t := r.peek()
		if t.typ == tEOF || t.typ == tSemicolon || t.typ == tRBrace {
			break
		}
		if !custom && block && t.typ != tWhitespace {
			if n := r.importantEnd(); n > 0 { // "!important" after a {} value
				r.i, to = n, n
				continue
			}
			r.i = mark
			return false
		}
		if !custom && t.typ == tLBrace && content {
			r.i = mark
			return false
		}
		switch t.typ {
		case tLBrace:
			block = true
		case tWhitespace:
		default:
			content = true
		}
		at := r.i
		r.skipValue()
		if t.typ != tWhitespace {
			if from < 0 {
				from = at
			}
			to = r.i
		}
	}
	end := colon.end // an empty value: the declaration ends at its colon
	if to > 0 {
		end = r.toks[to-1].end
	} else {
		from, to = r.i, r.i
	}
	r.items = append(r.items, item{kind: itemDecl, name: name.value, start: name.start, end: end, depth: depth, from: from, to: to, parent: parent})
	return true
}

// importantEnd returns the index past "!" whitespace "important", or 0.
func (r *reader) importantEnd() int {
	if t := r.peek(); t.typ != tDelim || t.value != "!" {
		return 0
	}
	for k := r.i + 1; k < len(r.toks); k++ {
		switch t := r.toks[k]; {
		case t.typ == tWhitespace:
		case t.typ == tIdent && strings.EqualFold(t.value, "important"):
			return k + 1
		default:
			return 0
		}
	}
	return 0
}

// atRule reads an at-rule: its prelude up to ";" (consumed), the end, a
// nested block's "}" (not consumed) or its {} block.
func (r *reader) atRule(top bool, depth, parent int) {
	at := r.next()
	from := r.i
	for {
		k := r.peek()
		switch {
		case k.typ == tEOF || k.typ == tSemicolon || !top && k.typ == tRBrace:
			to := r.i
			end := r.lastEnd(at.end)
			if k.typ == tSemicolon {
				r.next()
				end = k.end
			}
			r.items = append(r.items, item{kind: itemAtRule, name: at.value, start: at.start, end: end, depth: depth, from: from, to: to, parent: parent})
			return
		case k.typ == tLBrace:
			r.blockItem(item{kind: itemAtRule, name: at.value, start: at.start, depth: depth, from: from, to: r.i, parent: parent})
			return
		}
		r.skipValue()
	}
}

// qualifiedRule reads a qualified rule: its prelude and {} block, or an
// invalid one ended by the end of the input or, nested, ";" or "}".
func (r *reader) qualifiedRule(top bool, depth, parent int) {
	first := r.peek()
	from := r.i
	for {
		k := r.peek()
		switch {
		case k.typ == tEOF || !top && (k.typ == tSemicolon || k.typ == tRBrace):
			if k.typ == tSemicolon {
				r.next()
			}
			r.problem("invalid", first.start, r.lastEnd(first.end), depth)
			return
		case k.typ == tLBrace:
			prelude := r.toks[from:r.i]
			if custom, colon := preludeHead(prelude); custom && colon {
				// "--x: … {": a custom property, never a rule (nested: the
				// bad declaration's remnants; top level: the block too)
				if top {
					r.i = r.match[r.i]
				} else {
					for t := r.peek().typ; t != tEOF && t != tSemicolon && t != tRBrace; t = r.peek().typ {
						r.skipValue()
					}
				}
				r.problem("invalid", first.start, r.lastEnd(first.end), depth)
				return
			} else if colon && !top && declarationLike(prelude) {
				// "a: b c {": a declaration whose ";" is missing, read as a rule
				r.problem("rule_from_declaration", first.start, k.start, depth)
			}
			if top {
				for _, t := range prelude {
					if t.typ == tSemicolon || t.typ == tRBrace {
						r.problem("stray_in_prelude", t.start, t.end, depth)
						break
					}
				}
			}
			r.blockItem(item{kind: itemRule, start: first.start, depth: depth, from: from, to: r.i, parent: parent})
			return
		}
		r.skipValue()
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

// blockItem reads the {} block at the reader (the item's prelude read): its
// contents, once, then its "}" — or the end of the input (not closed). Past
// maxDepth the contents are stepped over unread.
func (r *reader) blockItem(it item) {
	r.next() // {
	it.block = true
	k := len(r.items)
	r.items = append(r.items, it)
	if it.depth+1 >= maxDepth {
		r.deep = true
		for t := r.peek().typ; t != tEOF && t != tRBrace; t = r.peek().typ {
			r.skipValue()
		}
	} else {
		r.list(false, it.depth+1, k)
	}
	if r.peek().typ == tRBrace {
		r.items[k].end = r.next().end
		return
	}
	r.items[k].end = len(r.src)
	r.problem("eof_in_block", it.start, len(r.src), it.depth)
}

// utf8Label are the Encoding standard's labels of UTF-8.
var utf8Label = map[string]bool{
	"unicode-1-1-utf-8": true, "unicode11utf8": true, "unicode20utf8": true,
	"utf-8": true, "utf8": true, "x-unicode20utf8": true,
}

// replacedByte returns the offset of the first NUL or invalid UTF-8 byte of
// src, or -1.
func replacedByte(src []byte) int {
	for i := 0; i < len(src); {
		if src[i] == 0 {
			return i
		}
		if src[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, w := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && w == 1 {
			return i
		}
		i += w
	}
	return -1
}
