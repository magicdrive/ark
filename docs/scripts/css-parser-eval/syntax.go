//go:build ignore

package main

// A CSS tokenizer and parser following CSS Syntax Level 3 (§4 tokenization,
// §5 parsing), over the original bytes: offsets are the source's, the
// input's preprocessing (CR LF / CR / FF as LF, NUL and invalid UTF-8 as
// U+FFFD) is applied as it reads. One candidate of the comparison
// (run.sh); the others are adapted to the same token stream.

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type tokType int

const (
	tEOF tokType = iota
	tIdent
	tFunction
	tAtKeyword
	tHash
	tString
	tBadString
	tURL
	tBadURL
	tDelim
	tNumber
	tPercentage
	tDimension
	tUnicodeRange
	tWhitespace
	tCDO
	tCDC
	tColon
	tSemicolon
	tComma
	tLBracket
	tRBracket
	tLParen
	tRParen
	tLBrace
	tRBrace
	tMatch // ~= |= ^= $= *= (Value), || (Value "||")
	tComment
)

type token struct {
	typ        tokType
	start, end int    // the token's bytes
	value      string // ident / function / at-keyword / hash / string / url name or value; delim; match
	hashID     bool   // a hash that would start an ident
	repr       string // number representation
	num        float64
	integer    bool
	unit       string
	ur0, ur1   int64 // unicode-range
	eofIn      bool  // a string or url the end of the input cut off
}

type tokenizer struct {
	src []byte
	pos int
	// legacy: the tokens css-parsing-tests still has, of the 2014 CR and
	// neither the 2021 CR nor the current draft — unicode-range and the
	// match tokens (~= |= ^= $= *= ||). Only for the test suite; the
	// standard's tokens are delims.
	legacy bool
}

const eof = -1

// at returns the code point at byte offset i (after preprocessing) and its
// width in bytes; eof past the end.
func (z *tokenizer) at(i int) (rune, int) {
	if i >= len(z.src) {
		return eof, 0
	}
	c := z.src[i]
	switch c {
	case '\r':
		if i+1 < len(z.src) && z.src[i+1] == '\n' {
			return '\n', 2
		}
		return '\n', 1
	case '\f':
		return '\n', 1
	case 0:
		return '\uFFFD', 1
	}
	if c < utf8.RuneSelf {
		return rune(c), 1
	}
	return decodeUTF8(z.src[i:])
}

// decodeUTF8 decodes the code point at the start of b as the Encoding
// standard's UTF-8 decoder does: an invalid sequence is one U+FFFD for its
// maximal subpart (the lead byte and the continuation bytes valid after
// it), where Go's decoder gives one per byte.
func decodeUTF8(b []byte) (rune, int) {
	r, w := utf8.DecodeRune(b)
	if r != utf8.RuneError || w > 1 {
		return r, w
	}
	lead := b[0]
	need, lo, hi := 0, byte(0x80), byte(0xBF)
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 1
	case lead >= 0xE0 && lead <= 0xEF:
		need = 2
		if lead == 0xE0 {
			lo = 0xA0
		} else if lead == 0xED {
			hi = 0x9F
		}
	case lead >= 0xF0 && lead <= 0xF4:
		need = 3
		if lead == 0xF0 {
			lo = 0x90
		} else if lead == 0xF4 {
			hi = 0x8F
		}
	default:
		return '\uFFFD', 1
	}
	n := 1
	for k := 0; k < need && n < len(b); k++ {
		c := b[n]
		if c < lo || c > hi {
			break
		}
		lo, hi = 0x80, 0xBF
		n++
	}
	return '\uFFFD', n
}

// peek returns the n-th code point from pos (0-based).
func (z *tokenizer) peek(n int) rune {
	i := z.pos
	for k := 0; ; k++ {
		r, w := z.at(i)
		if k == n || r == eof {
			return r
		}
		i += w
	}
}

func (z *tokenizer) next() rune {
	r, w := z.at(z.pos)
	z.pos += w
	return r
}

func isNewline(r rune) bool    { return r == '\n' }
func isWS(r rune) bool         { return r == '\n' || r == '\t' || r == ' ' }
func isDigitR(r rune) bool     { return r >= '0' && r <= '9' }
func isHexR(r rune) bool       { return isDigitR(r) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' }
func isLetter(r rune) bool     { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
func isIdentStart(r rune) bool { return isLetter(r) || r >= 0x80 || r == '_' }
func isIdentChar(r rune) bool  { return isIdentStart(r) || isDigitR(r) || r == '-' }
func isNonPrintable(r rune) bool {
	return r >= 0 && r <= 8 || r == 0xB || r >= 0xE && r <= 0x1F || r == 0x7F
}

func validEscape(a, b rune) bool { return a == '\\' && !isNewline(b) }

func startsIdent(a, b, c rune) bool {
	switch {
	case a == '-':
		return isIdentStart(b) || b == '-' || validEscape(b, c)
	case isIdentStart(a):
		return true
	case a == '\\':
		return validEscape(a, b)
	}
	return false
}

func startsNumber(a, b, c rune) bool {
	switch {
	case a == '+' || a == '-':
		return isDigitR(b) || b == '.' && isDigitR(c)
	case a == '.':
		return isDigitR(b)
	}
	return isDigitR(a)
}

// nextToken consumes a token (§4.3.1). Comments are tokens here (tComment):
// the parser skips them.
func (z *tokenizer) nextToken() token {
	start := z.pos
	t := z.consume()
	t.start, t.end = start, z.pos
	return t
}

func (z *tokenizer) consume() token {
	if z.peek(0) == '/' && z.peek(1) == '*' {
		z.next()
		z.next()
		for {
			r := z.next()
			if r == eof {
				return token{typ: tComment, eofIn: true}
			}
			if r == '*' && z.peek(0) == '/' {
				z.next()
				break
			}
		}
		return token{typ: tComment}
	}
	p0 := z.pos // reconsume: back to p0
	r := z.next()
	switch {
	case r == eof:
		return token{typ: tEOF}
	case isWS(r):
		for isWS(z.peek(0)) {
			z.next()
		}
		return token{typ: tWhitespace}
	case r == '"' || r == '\'':
		return z.consumeString(r)
	case r == '#':
		if isIdentChar(z.peek(0)) || validEscape(z.peek(0), z.peek(1)) {
			id := startsIdent(z.peek(0), z.peek(1), z.peek(2))
			return token{typ: tHash, value: z.consumeName(), hashID: id}
		}
		return token{typ: tDelim, value: "#"}
	case r == '(':
		return token{typ: tLParen}
	case r == ')':
		return token{typ: tRParen}
	case r == '+' || r == '.':
		if startsNumber(r, z.peek(0), z.peek(1)) {
			z.pos = p0
			return z.consumeNumeric()
		}
		return token{typ: tDelim, value: string(r)}
	case r == ',':
		return token{typ: tComma}
	case r == '-':
		if startsNumber(r, z.peek(0), z.peek(1)) {
			z.pos = p0
			return z.consumeNumeric()
		}
		if z.peek(0) == '-' && z.peek(1) == '>' {
			z.next()
			z.next()
			return token{typ: tCDC}
		}
		if startsIdent(r, z.peek(0), z.peek(1)) {
			z.pos = p0
			return z.consumeIdentLike()
		}
		return token{typ: tDelim, value: "-"}
	case r == ':':
		return token{typ: tColon}
	case r == ';':
		return token{typ: tSemicolon}
	case r == '<':
		if z.peek(0) == '!' && z.peek(1) == '-' && z.peek(2) == '-' {
			z.next()
			z.next()
			z.next()
			return token{typ: tCDO}
		}
		return token{typ: tDelim, value: "<"}
	case r == '@':
		if startsIdent(z.peek(0), z.peek(1), z.peek(2)) {
			return token{typ: tAtKeyword, value: z.consumeName()}
		}
		return token{typ: tDelim, value: "@"}
	case r == '[':
		return token{typ: tLBracket}
	case r == '\\':
		if validEscape(r, z.peek(0)) {
			z.pos = p0
			return z.consumeIdentLike()
		}
		return token{typ: tDelim, value: "\\"}
	case r == ']':
		return token{typ: tRBracket}
	case r == '{':
		return token{typ: tLBrace}
	case r == '}':
		return token{typ: tRBrace}
	case isDigitR(r):
		z.pos = p0
		return z.consumeNumeric()
	case z.legacy && (r == 'u' || r == 'U') && z.peek(0) == '+' && (isHexR(z.peek(1)) || z.peek(1) == '?'):
		z.next()
		return z.consumeUnicodeRange()
	case isIdentStart(r):
		z.pos = p0
		return z.consumeIdentLike()
	case z.legacy && r == '|' && z.peek(0) == '|':
		z.next()
		return token{typ: tMatch, value: "||"}
	case z.legacy && (r == '~' || r == '|' || r == '^' || r == '$' || r == '*') && z.peek(0) == '=':
		z.next()
		return token{typ: tMatch, value: string(r) + "="}
	}
	return token{typ: tDelim, value: string(r)}
}

func (z *tokenizer) consumeUnicodeRange() token {
	var b strings.Builder
	for b.Len() < 6 && isHexR(z.peek(0)) {
		b.WriteRune(z.next())
	}
	q := 0
	for b.Len()+q < 6 && z.peek(0) == '?' {
		z.next()
		q++
	}
	if q > 0 {
		lo, _ := strconv.ParseInt(b.String()+strings.Repeat("0", q), 16, 64)
		hi, _ := strconv.ParseInt(b.String()+strings.Repeat("F", q), 16, 64)
		return token{typ: tUnicodeRange, ur0: lo, ur1: hi}
	}
	lo, _ := strconv.ParseInt(b.String(), 16, 64)
	hi := lo
	if z.peek(0) == '-' && isHexR(z.peek(1)) {
		z.next()
		var e strings.Builder
		for e.Len() < 6 && isHexR(z.peek(0)) {
			e.WriteRune(z.next())
		}
		hi, _ = strconv.ParseInt(e.String(), 16, 64)
	}
	return token{typ: tUnicodeRange, ur0: lo, ur1: hi}
}

// consumeEscape consumes an escaped code point after the '\' (§4.3.7).
func (z *tokenizer) consumeEscape() rune {
	r := z.next()
	switch {
	case r == eof:
		return '\uFFFD'
	case isHexR(r):
		v := int64(hexVal(r))
		for k := 1; k < 6 && isHexR(z.peek(0)); k++ {
			v = v*16 + int64(hexVal(z.next()))
		}
		if isWS(z.peek(0)) {
			z.next()
		}
		if v == 0 || v >= 0xD800 && v <= 0xDFFF || v > 0x10FFFF {
			return '\uFFFD'
		}
		return rune(v)
	}
	return r
}

func hexVal(r rune) int {
	switch {
	case isDigitR(r):
		return int(r - '0')
	case r >= 'a':
		return int(r-'a') + 10
	}
	return int(r-'A') + 10
}

// consumeName consumes an ident sequence (§4.3.11).
func (z *tokenizer) consumeName() string {
	var b strings.Builder
	for {
		r := z.peek(0)
		switch {
		case isIdentChar(r):
			b.WriteRune(z.next())
		case validEscape(r, z.peek(1)):
			z.next()
			b.WriteRune(z.consumeEscape())
		default:
			return b.String()
		}
	}
}

func (z *tokenizer) consumeNumeric() token {
	var repr strings.Builder
	integer := true
	if r := z.peek(0); r == '+' || r == '-' {
		repr.WriteRune(z.next())
	}
	for isDigitR(z.peek(0)) {
		repr.WriteRune(z.next())
	}
	if z.peek(0) == '.' && isDigitR(z.peek(1)) {
		repr.WriteRune(z.next())
		for isDigitR(z.peek(0)) {
			repr.WriteRune(z.next())
		}
		integer = false
	}
	if e := z.peek(0); e == 'e' || e == 'E' {
		if s := z.peek(1); isDigitR(s) || (s == '+' || s == '-') && isDigitR(z.peek(2)) {
			repr.WriteRune(z.next())
			repr.WriteRune(z.next())
			for isDigitR(z.peek(0)) {
				repr.WriteRune(z.next())
			}
			integer = false
		}
	}
	v, _ := strconv.ParseFloat(repr.String(), 64)
	t := token{repr: repr.String(), num: v, integer: integer}
	switch {
	case startsIdent(z.peek(0), z.peek(1), z.peek(2)):
		t.typ, t.unit = tDimension, z.consumeName()
	case z.peek(0) == '%':
		z.next()
		t.typ = tPercentage
	default:
		t.typ = tNumber
	}
	return t
}

func (z *tokenizer) consumeIdentLike() token {
	name := z.consumeName()
	if strings.EqualFold(name, "url") && z.peek(0) == '(' {
		z.next()
		for isWS(z.peek(0)) && isWS(z.peek(1)) {
			z.next()
		}
		if q := z.peek(0); q == '"' || q == '\'' || isWS(q) && (z.peek(1) == '"' || z.peek(1) == '\'') {
			return token{typ: tFunction, value: name}
		}
		return z.consumeURL()
	}
	if z.peek(0) == '(' {
		z.next()
		return token{typ: tFunction, value: name}
	}
	return token{typ: tIdent, value: name}
}

func (z *tokenizer) consumeURL() token {
	var b strings.Builder
	for isWS(z.peek(0)) {
		z.next()
	}
	for {
		r := z.next()
		switch {
		case r == ')':
			return token{typ: tURL, value: b.String()}
		case r == eof:
			return token{typ: tURL, value: b.String(), eofIn: true}
		case isWS(r):
			for isWS(z.peek(0)) {
				z.next()
			}
			if p := z.peek(0); p == ')' || p == eof {
				z.next()
				return token{typ: tURL, value: b.String(), eofIn: p == eof}
			}
			z.badURLRemnants()
			return token{typ: tBadURL}
		case r == '"' || r == '\'' || r == '(' || isNonPrintable(r):
			z.badURLRemnants()
			return token{typ: tBadURL}
		case r == '\\':
			if validEscape(r, z.peek(0)) {
				b.WriteRune(z.consumeEscape())
				continue
			}
			z.badURLRemnants()
			return token{typ: tBadURL}
		default:
			b.WriteRune(r)
		}
	}
}

func (z *tokenizer) badURLRemnants() {
	for {
		r := z.next()
		switch {
		case r == ')' || r == eof:
			return
		case validEscape(r, z.peek(0)):
			z.consumeEscape()
		}
	}
}

func (z *tokenizer) consumeString(q rune) token {
	var b strings.Builder
	for {
		r := z.peek(0)
		switch {
		case r == q:
			z.next()
			return token{typ: tString, value: b.String()}
		case r == eof:
			return token{typ: tString, value: b.String(), eofIn: true}
		case isNewline(r):
			return token{typ: tBadString}
		case r == '\\':
			z.next()
			switch n := z.peek(0); {
			case n == eof:
			case isNewline(n):
				z.next()
			default:
				b.WriteRune(z.consumeEscape())
			}
		default:
			b.WriteRune(z.next())
		}
	}
}

// bomLength is the length of a UTF-8 byte order mark at the start of src
// (the decoder removes it, §3.2), or 0.
func bomLength(src []byte) int {
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		return 3
	}
	return 0
}
