//go:build ignore

package main

import "strings"

// CSS Syntax Level 3 parsing (§5) over a token stream, producing the
// css-parsing-tests representation (README.rst of SimonSapin/css-parsing-tests).
// Shared by every tokenizer candidate: what differs is the token stream.

type parser struct {
	toks  []token // without comments
	i     int
	depth int   // nesting of blocks and functions being consumed
	deep  *bool // set when maxDepth stopped the nesting (shared by sub-parsers)
}

// maxDepth bounds the nesting of blocks and functions: deeper openers are
// read as tokens (and the stylesheet reported), so recursion never
// exhausts the stack.
const maxDepth = 256

func (p *parser) peek() token {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return token{typ: tEOF}
}

func (p *parser) next() token {
	t := p.peek()
	if p.i < len(p.toks) {
		p.i++
	}
	return t
}

func (p *parser) skipWS() {
	for p.peek().typ == tWhitespace {
		p.i++
	}
}

func num(t token, kind string) []any {
	ty := "number"
	if t.integer {
		ty = "integer"
	}
	v := t.num
	if v == 0 {
		v = 0 // -0 is 0 in the test representation
	}
	out := []any{kind, t.repr, v, ty}
	if kind == "dimension" {
		out = append(out, t.unit)
	}
	return out
}

// preserved represents one token as a component value; a string or url cut
// off by the end of the input is followed by its error.
func preserved(t token) []any {
	var v any
	switch t.typ {
	case tIdent:
		v = []any{"ident", t.value}
	case tAtKeyword:
		v = []any{"at-keyword", t.value}
	case tHash:
		ty := "unrestricted"
		if t.hashID {
			ty = "id"
		}
		v = []any{"hash", t.value, ty}
	case tString:
		v = []any{"string", t.value}
	case tBadString:
		v = []any{"error", "bad-string"}
	case tURL:
		v = []any{"url", t.value}
	case tBadURL:
		v = []any{"error", "bad-url"}
	case tDelim, tMatch:
		v = t.value
	case tNumber:
		v = num(t, "number")
	case tPercentage:
		v = num(t, "percentage")
	case tDimension:
		v = num(t, "dimension")
	case tUnicodeRange:
		v = []any{"unicode-range", t.ur0, t.ur1}
	case tWhitespace:
		v = " "
	case tCDO:
		v = "<!--"
	case tCDC:
		v = "-->"
	case tColon:
		v = ":"
	case tSemicolon:
		v = ";"
	case tComma:
		v = ","
	case tRParen:
		v = []any{"error", ")"}
	case tRBracket:
		v = []any{"error", "]"}
	case tRBrace:
		v = []any{"error", "}"}
	}
	out := []any{v}
	if t.eofIn {
		if t.typ == tString {
			out = append(out, []any{"error", "eof-in-string"})
		} else {
			out = append(out, []any{"error", "eof-in-url"})
		}
	}
	return out
}

// componentValue consumes a component value (§5.4.7), appending its
// representation to out.
func (p *parser) componentValue(out []any) []any {
	t := p.next()
	if p.depth >= maxDepth {
		switch t.typ {
		case tLBrace, tLBracket, tLParen, tFunction:
			if p.deep != nil {
				*p.deep = true
			}
			return append(out, []any{"error", "too-deep"})
		}
	}
	p.depth++
	defer func() { p.depth-- }()
	switch t.typ {
	case tLBrace:
		b, _ := p.block("{}", tRBrace)
		return append(out, b)
	case tLBracket:
		b, _ := p.block("[]", tRBracket)
		return append(out, b)
	case tLParen:
		b, _ := p.block("()", tRParen)
		return append(out, b)
	case tFunction:
		f := []any{"function", t.value}
		for {
			switch p.peek().typ {
			case tEOF:
				return append(out, f)
			case tRParen:
				p.next()
				return append(out, f)
			}
			f = p.componentValue(f)
		}
	}
	return append(out, preserved(t)...)
}

// block consumes a simple block's contents after its opener, and reports
// whether its closing token ended it (not the end of the input).
func (p *parser) block(kind string, end tokType) ([]any, bool) {
	b := []any{kind}
	for {
		switch p.peek().typ {
		case tEOF:
			return b, false
		case end:
			p.next()
			return b, true
		}
		b = p.componentValue(b)
	}
}

// componentValues consumes until stop (or EOF); stop is not consumed.
func (p *parser) componentValues(stop ...tokType) []any {
	out := []any{}
	for {
		t := p.peek().typ
		if t == tEOF {
			return out
		}
		for _, s := range stop {
			if t == s {
				return out
			}
		}
		out = p.componentValue(out)
	}
}

// ruleList consumes a list of rules (§5.4.1, CSS Syntax 2021).
func (p *parser) ruleList(top bool) []any {
	out := []any{}
	for {
		switch t := p.peek(); t.typ {
		case tWhitespace:
			p.next()
		case tEOF:
			return out
		case tCDO, tCDC:
			if top {
				p.next()
				continue
			}
			if r := p.qualifiedRule(); r != nil {
				out = append(out, r)
			} else {
				out = append(out, []any{"error", "invalid"})
			}
		case tAtKeyword:
			out = append(out, p.atRule())
		default:
			if r := p.qualifiedRule(); r != nil {
				out = append(out, r)
			} else {
				out = append(out, []any{"error", "invalid"})
			}
		}
	}
}

func (p *parser) atRule() []any {
	name := p.next().value
	prelude := []any{}
	for {
		switch p.peek().typ {
		case tSemicolon:
			p.next()
			return []any{"at-rule", name, prelude, nil}
		case tEOF:
			return []any{"at-rule", name, prelude, nil}
		case tLBrace:
			p.next()
			b, _ := p.block("{}", tRBrace)
			return []any{"at-rule", name, prelude, b[1:]}
		}
		prelude = p.componentValue(prelude)
	}
}

// qualifiedRule returns nil (nothing) at the end of the input.
func (p *parser) qualifiedRule() []any {
	prelude := []any{}
	for {
		switch p.peek().typ {
		case tEOF:
			return nil
		case tLBrace:
			p.next()
			b, _ := p.block("{}", tRBrace)
			return []any{"qualified rule", prelude, b[1:]}
		}
		prelude = p.componentValue(prelude)
	}
}

// declarationList consumes a list of declarations (§5.4.5, CSS Syntax 2021).
func (p *parser) declarationList() []any {
	out := []any{}
	for {
		switch t := p.peek(); t.typ {
		case tWhitespace, tSemicolon:
			p.next()
		case tEOF:
			return out
		case tAtKeyword:
			out = append(out, p.atRule())
		case tIdent:
			start := p.i
			p.componentValuesTokens(tSemicolon)
			sub := &parser{toks: p.toks[start:p.i]}
			if d := sub.declaration(); d != nil {
				out = append(out, d)
			} else {
				out = append(out, []any{"error", "invalid"})
			}
		default:
			p.componentValuesTokens(tSemicolon)
			out = append(out, []any{"error", "invalid"})
		}
	}
}

// componentValuesTokens skips component values until stop or EOF.
func (p *parser) componentValuesTokens(stop tokType) {
	for t := p.peek().typ; t != tEOF && t != stop; t = p.peek().typ {
		p.componentValue(nil)
	}
}

// declaration consumes a declaration (§5.4.6), nil when it is none.
func (p *parser) declaration() []any {
	name := p.next()
	if name.typ != tIdent {
		return nil
	}
	p.skipWS()
	if p.peek().typ != tColon {
		return nil
	}
	p.next()
	p.skipWS()
	value := p.componentValues(tSemicolon)
	value = trimWS(value)
	important := false
	if n := len(value); n >= 2 {
		if last, ok := value[n-1].([]any); ok && len(last) == 2 && last[0] == "ident" && equalFoldASCII(last[1].(string), "important") {
			k := n - 2
			for k >= 0 && value[k] == " " {
				k--
			}
			if k >= 0 && value[k] == "!" {
				important = true
				value = trimWS(value[:k])
			}
		}
	}
	return []any{"declaration", name.value, value, important}
}

func trimWS(v []any) []any {
	for len(v) > 0 && v[len(v)-1] == " " {
		v = v[:len(v)-1]
	}
	return v
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// blockContents consumes a block's contents (css-syntax-3, current draft:
// declarations and nested rules).
func (p *parser) blockContents() []any {
	out := []any{}
	for {
		switch t := p.peek(); t.typ {
		case tWhitespace, tSemicolon:
			p.next()
		case tEOF:
			return out
		case tAtKeyword:
			out = append(out, p.atRule())
		default:
			mark := p.i
			if t.typ == tIdent {
				p.componentValuesTokens(tSemicolon)
				sub := &parser{toks: p.toks[mark:p.i]}
				if d := sub.declaration(); d != nil && (strings.HasPrefix(d[1].(string), "--") || !hasBlockAndMore(d[2].([]any))) {
					out = append(out, d)
					continue
				}
				p.i = mark
			}
			// A qualified rule, ended by a ';' as an invalid rule.
			prelude := []any{}
			for {
				switch p.peek().typ {
				case tEOF, tSemicolon:
					out = append(out, []any{"error", "invalid"})
					goto next
				case tLBrace:
					if customPrelude(prelude) {
						// "--x: … {": a custom property, never a rule
						p.componentValuesTokens(tSemicolon)
						out = append(out, []any{"error", "invalid"})
						goto next
					}
					p.next()
					b, _ := p.block("{}", tRBrace)
					out = append(out, []any{"qualified rule", prelude, b[1:]})
					goto next
				}
				prelude = p.componentValue(prelude)
			}
		next:
		}
	}
}

// hasBlockAndMore reports whether a declaration value holds a top-level {}
// block and anything else but whitespace (then it is a nested rule).
func hasBlockAndMore(v []any) bool {
	block, other := false, false
	for _, x := range v {
		if a, ok := x.([]any); ok && len(a) > 0 && a[0] == "{}" {
			block = true
		} else if x != " " {
			other = true
		}
	}
	return block && other
}

func (p *parser) oneRule() any {
	p.skipWS()
	var r []any
	switch p.peek().typ {
	case tEOF:
		return []any{"error", "empty"}
	case tAtKeyword:
		r = p.atRule()
	default:
		r = p.qualifiedRule()
		if r == nil {
			return []any{"error", "invalid"}
		}
	}
	p.skipWS()
	if p.peek().typ != tEOF {
		return []any{"error", "extra-input"}
	}
	return r
}

func (p *parser) oneDeclaration() any {
	p.skipWS()
	switch p.peek().typ {
	case tEOF:
		return []any{"error", "empty"}
	case tIdent:
	default:
		return []any{"error", "invalid"}
	}
	if d := p.wholeDeclaration(); d != nil {
		return d
	}
	return []any{"error", "invalid"}
}

// wholeDeclaration is "parse a declaration" as the suite's one_declaration
// tests have it (an earlier draft): the value is the rest of the input
// after the colon, whitespace kept, "!important" (with the whitespace
// around it) removed.
func (p *parser) wholeDeclaration() []any {
	name := p.next()
	p.skipWS()
	if p.peek().typ != tColon {
		return nil
	}
	p.next()
	value := p.componentValues()
	important := false
	k := len(value) - 1
	for k >= 0 && value[k] == " " {
		k--
	}
	if k >= 1 {
		if last, ok := value[k].([]any); ok && len(last) == 2 && last[0] == "ident" && equalFoldASCII(last[1].(string), "important") {
			j := k - 1
			for j >= 0 && value[j] == " " {
				j--
			}
			if j >= 0 && value[j] == "!" {
				important = true
				value = value[:j]
			}
		}
	}
	return []any{"declaration", name.value, value, important}
}

func (p *parser) oneComponentValue() any {
	p.skipWS()
	if p.peek().typ == tEOF {
		return []any{"error", "empty"}
	}
	v := p.componentValue(nil)
	p.skipWS()
	if p.peek().typ != tEOF {
		return []any{"error", "extra-input"}
	}
	if len(v) != 1 {
		return v
	}
	return v[0]
}

// customPrelude reports whether a prelude starts with an ident beginning
// "--" and a colon: a custom property, never a qualified rule (current
// draft, consume a qualified rule).
func customPrelude(prelude []any) bool {
	var firsts []any
	for _, x := range prelude {
		if x != " " {
			firsts = append(firsts, x)
			if len(firsts) == 2 {
				break
			}
		}
	}
	if len(firsts) < 2 || firsts[1] != ":" {
		return false
	}
	id, ok := firsts[0].([]any)
	return ok && len(id) == 2 && id[0] == "ident" && strings.HasPrefix(id[1].(string), "--")
}
