package typescript

import (
	"sort"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
)

// Generic calls the parser read as comparisons.
//
// The parser sometimes derives `f<T>(x).m` as the comparisons
// `(f < T) > ((x).m)` — a valid derivation of the grammar, so no error marks
// it, but not TypeScript's: when `<` follows an expression and what follows
// parses as type arguments closed by `>` and followed by `(`, TypeScript
// parses a call with type arguments, never a comparison (parser.ts,
// parseTypeArgumentsInExpression / canFollowTypeArgumentsInExpression). So a
// `<` operator whose left operand is a callee shape and whose source text
// satisfies that rule is a call, whatever tree the parser built.
//
// The type arguments are checked with a deliberately small type grammar
// (names, generic names, unions, intersections, arrays, tuples, literals,
// type literals, typeof / keyof): every text it accepts TypeScript accepts as
// type arguments too, so no comparison is ever turned into a call. What it
// rejects (function types, conditional types, ...) stays unrecorded, as it
// was. The call and the type names in its type arguments are recorded
// exactly as for a correctly parsed call.

// typeArgRef is a type name found in recovered type arguments.
type typeArgRef struct {
	name, receiver string
	start, end     uint32
}

// misparsedGenericCall records the generic call a `<` binary_expression
// stands for, if it does.
func (e *extractor) misparsedGenericCall(n *ts.Node, sc scope) {
	op := e.field(n, "operator")
	left := e.field(n, "left")
	if op == nil || left == nil || e.typ(op) != "<" || left.EndByte() > op.StartByte() {
		return
	}
	var callee, recvNode *ts.Node
	switch e.typ(left) {
	case "identifier":
		callee = left
	case "member_expression":
		prop := e.field(left, "property")
		obj := e.unwrap(e.field(left, "object"))
		if obj == nil || e.typ(obj) == "super" {
			return
		}
		switch e.typ(prop) {
		case "property_identifier", "private_property_identifier":
		default:
			return
		}
		callee, recvNode = prop, obj
	default:
		return
	}
	p := &typeArgParser{src: e.src, pos: int(op.StartByte())}
	if !p.typeArgs() {
		return
	}
	if p.skipSpace(); p.pos >= len(p.src) || p.src[p.pos] != '(' {
		return
	}
	if recvNode == nil {
		e.addRef(callee, e.text(callee), string(reference.KindCall), sc.container, "", "", true)
	} else {
		e.addRef(callee, e.text(callee), string(reference.KindCall), sc.container,
			e.text(recvNode), e.receiverType(recvNode, callee.StartByte(), sc), true)
	}
	for _, r := range p.refs {
		if r.receiver == "" && sc.tparams[r.name] {
			continue
		}
		e.addRefAt(r.start, r.end, r.name, string(reference.KindTypeUse), sc.container, r.receiver)
	}
}

// addRefAt appends a non-call reference located by byte offsets.
func (e *extractor) addRefAt(start, end uint32, name, kind, container, receiver string) {
	e.refs = append(e.refs, language.ReferenceDraft{
		Name:         name,
		Kind:         kind,
		Container:    container,
		Location:     source.Location{File: e.file, Range: source.Range{Start: e.positionAt(start), End: e.positionAt(end)}},
		ReceiverExpr: receiver,
	})
}

// positionAt is the 1-based line and byte column of offset off.
func (e *extractor) positionAt(off uint32) source.Position {
	if e.lineStarts == nil {
		e.lineStarts = []uint32{0}
		for i, c := range e.src {
			if c == '\n' {
				e.lineStarts = append(e.lineStarts, uint32(i+1))
			}
		}
	}
	line := sort.Search(len(e.lineStarts), func(i int) bool { return e.lineStarts[i] > off })
	return source.Position{Line: uint32(line), Column: off - e.lineStarts[line-1] + 1}
}

// typeKeywords are the type keywords the type grammar accepts as types
// (`bigint` included: the grammar reads it as a type name).
var typeKeywords = map[string]bool{
	"string": true, "number": true, "boolean": true, "any": true, "unknown": true,
	"never": true, "void": true, "undefined": true, "null": true, "object": true,
	"symbol": true, "bigint": true, "this": true, "true": true, "false": true,
}

// typeArgParser checks type arguments over raw source text.
type typeArgParser struct {
	src   []byte
	pos   int
	depth int
	refs  []typeArgRef
}

const maxTypeArgDepth = 32

func (p *typeArgParser) skipSpace() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		case c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '*':
			end := strings.Index(string(p.src[p.pos+2:]), "*/")
			if end < 0 {
				p.pos = len(p.src)
				return
			}
			p.pos += end + 4
		default:
			return
		}
	}
}

func isIdentByte(c byte, first bool) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		c >= 0x80 || (!first && c >= '0' && c <= '9')
}

// peek returns the next punctuation byte, or 0 for anything else.
func (p *typeArgParser) peek() byte {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *typeArgParser) eat(c byte) bool {
	if p.peek() == c {
		p.pos++
		return true
	}
	return false
}

// ident consumes an identifier and returns it with its start, or "".
func (p *typeArgParser) ident() (string, int) {
	p.skipSpace()
	start := p.pos
	if p.pos >= len(p.src) || !isIdentByte(p.src[p.pos], true) {
		return "", start
	}
	for p.pos < len(p.src) && isIdentByte(p.src[p.pos], p.pos == start) {
		p.pos++
	}
	return string(p.src[start:p.pos]), start
}

// typeArgs parses `<` Type {`,` Type} [`,`] `>`.
func (p *typeArgParser) typeArgs() bool {
	if p.depth++; p.depth > maxTypeArgDepth {
		return false
	}
	defer func() { p.depth-- }()
	if !p.eat('<') || p.peek() == '>' {
		return false
	}
	for {
		if !p.typ() {
			return false
		}
		if !p.eat(',') {
			break
		}
		if p.peek() == '>' {
			break
		}
	}
	return p.eat('>')
}

// typ parses a union / intersection of postfix types.
func (p *typeArgParser) typ() bool {
	if p.depth++; p.depth > maxTypeArgDepth {
		return false
	}
	defer func() { p.depth-- }()
	if c := p.peek(); c == '|' || c == '&' {
		p.pos++
	}
	for {
		if !p.postfix() {
			return false
		}
		c := p.peek()
		if (c != '|' && c != '&') || (p.pos+1 < len(p.src) && p.src[p.pos+1] == c) {
			return true // `||` / `&&` are not type operators
		}
		p.pos++
	}
}

// postfix parses a primary type followed by `[]` / `[K]`.
func (p *typeArgParser) postfix() bool {
	if !p.primary() {
		return false
	}
	for p.eat('[') {
		if p.eat(']') {
			continue
		}
		if !p.typ() || !p.eat(']') {
			return false
		}
	}
	return true
}

func (p *typeArgParser) primary() bool {
	switch c := p.peek(); {
	case c == '"' || c == '\'':
		return p.stringLiteral()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c == '[':
		p.pos++
		if p.eat(']') {
			return true
		}
		for {
			if !p.typ() {
				return false
			}
			if !p.eat(',') {
				break
			}
		}
		return p.eat(']')
	case c == '{':
		return p.typeLiteral()
	}
	name, start := p.ident()
	switch name {
	case "":
		return false
	case "typeof":
		// A type query names a value, not a type: not a type reference.
		if q, _ := p.ident(); q == "" {
			return false
		}
		for p.peek() == '.' {
			p.pos++
			if q, _ := p.ident(); q == "" {
				return false
			}
		}
		return true
	case "keyof", "readonly", "unique":
		return p.postfix()
	}
	if typeKeywords[name] {
		return true
	}
	parts := []string{name}
	end := p.pos
	for p.peek() == '.' {
		p.pos++
		q, _ := p.ident()
		if q == "" {
			return false
		}
		parts = append(parts, q)
		end = p.pos
	}
	p.refs = append(p.refs, typeArgRef{
		name:     parts[len(parts)-1],
		receiver: strings.Join(parts[:len(parts)-1], "."),
		start:    uint32(start),
		end:      uint32(end),
	})
	if p.peek() == '<' {
		return p.typeArgs()
	}
	return true
}

func (p *typeArgParser) stringLiteral() bool {
	q := p.src[p.pos]
	for p.pos++; p.pos < len(p.src); p.pos++ {
		switch p.src[p.pos] {
		case '\\':
			p.pos++
		case '\n':
			return false
		case q:
			p.pos++
			return true
		}
	}
	return false
}

func (p *typeArgParser) number() bool {
	if p.src[p.pos] == '-' {
		p.pos++
	}
	start := p.pos
	for p.pos < len(p.src) && ((p.src[p.pos] >= '0' && p.src[p.pos] <= '9') || p.src[p.pos] == '.' || p.src[p.pos] == '_') {
		p.pos++
	}
	return p.pos > start
}

// typeLiteral parses `{ [readonly] name[?]: Type [;|,] ... }`.
func (p *typeArgParser) typeLiteral() bool {
	p.pos++ // {
	for !p.eat('}') {
		var name string
		if c := p.peek(); c == '"' || c == '\'' {
			if !p.stringLiteral() {
				return false
			}
			name = "string"
		} else {
			name, _ = p.ident()
			if name == "readonly" && p.peek() != ':' && p.peek() != '?' {
				name, _ = p.ident()
			}
		}
		if name == "" {
			return false
		}
		p.eat('?')
		if !p.eat(':') || !p.typ() {
			return false
		}
		if !p.eat(';') {
			p.eat(',')
		}
	}
	return true
}
