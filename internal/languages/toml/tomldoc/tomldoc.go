// Package tomldoc reads a TOML document into the tree of tables and keys it
// defines, with the bytes each one is written at, and the document's
// problems. It parses nothing itself: github.com/pelletier/go-toml/v2's
// unstable.Parser reads the expressions (keys, key/values, table headers and
// values, with their bytes), and github.com/BurntSushi/toml's decoder checks
// what the parser does not (impossible dates, invalid escapes). What
// tomldoc adds is what neither returns: the tree the expressions define
// (TOML's table rules), where a table header and an array begin and end, and
// a problem per duplicate (the decoder stops at the first one).
//
// Positions are byte offsets into the source. go-toml reads TOML 1.1 (the
// versions differ in \e and \xHH escapes, times without seconds, and inline
// tables over several lines or with a trailing comma), as does BurntSushi/toml,
// and so does tomldoc:
// a TOML 1.0 parser rejects these, tomldoc reports nothing for them.
//
// Bounds. Parse reads at most MaxBytes; a key of more than MaxKeyParts
// dotted parts is reported and its expression left out (with the key/values
// of a header so left out); at most MaxProblems problems are reported.
// Nothing recurses deeper than the parser's own bound on nested values
// (10000), and every step is linear in the source.
package tomldoc

import (
	"errors"
	"fmt"
	"sort"

	bstoml "github.com/BurntSushi/toml"
	"github.com/pelletier/go-toml/v2/unstable"
)

// MaxBytes is the largest document Parse reads. A larger one is reported
// (toml_too_large) and its structure not read.
const MaxBytes = 4 << 20

// MaxKeyParts bounds the dotted parts of one key or table header.
const MaxKeyParts = 128

// MaxProblems bounds the problems a document reports.
const MaxProblems = 1000

// Problem codes.
const (
	CodeSyntax       = "toml_syntax_error"
	CodeInvalid      = "toml_invalid"
	CodeDuplicateKey = "toml_duplicate_key"
	CodeRedefined    = "toml_table_redefined"
	CodeTooLarge     = "toml_too_large"
	CodeKeyTooLong   = "toml_key_too_long"
	CodeNotChecked   = "toml_not_checked"
)

// MaxCheckedNesting and MaxCheckedKeyParts bound the nesting of arrays and
// inline tables, and the dotted parts of a key, of a document the decoder
// checks: it takes time quadratic in both.
const (
	MaxCheckedNesting  = 64
	MaxCheckedKeyParts = 8
)

// Span is a range of bytes [Start, End).
type Span struct{ Start, End int }

// Problem is a problem of the document, located at its bytes.
type Problem struct {
	Code, Message string
	Start, End    int
	Error         bool // the document is no valid TOML
}

// Kind of node.
type Kind int

const (
	// Table is a table: the root, a [header] table, a table a header or a
	// dotted key implies, an inline table, an array-of-tables element.
	Table Kind = iota
	// ArrayOfTables is the array a [[header]] appends its tables to; its
	// Children are the elements, in order.
	ArrayOfTables
	// Value is any other value: a string, number, boolean, date or time, or
	// an array (an array of inline tables too: its tables are static data,
	// no table a header may reach).
	Value
)

// How a table came to be defined.
type How int

const (
	Root     How = iota
	Header       // by a [header]
	Implicit     // by a header's prefix ([a.b] implies a), not (yet) by its own header
	Dotted       // by a dotted key (a.b = 1 defines a)
	Inline       // an inline table { ... }
	Element      // an element of an array of tables ([[a]])
)

// ValueKind is the TOML type of a Value.
type ValueKind string

const (
	String         ValueKind = "string"
	Integer        ValueKind = "integer"
	Float          ValueKind = "float"
	Bool           ValueKind = "boolean"
	OffsetDateTime ValueKind = "offset-date-time"
	LocalDateTime  ValueKind = "local-date-time"
	LocalDate      ValueKind = "local-date"
	LocalTime      ValueKind = "local-time"
	Array          ValueKind = "array"
)

// Str is a string value and the bytes it is written at.
type Str struct {
	Value string
	Span  Span
}

// Node is a table, an array of tables or a value of the document.
type Node struct {
	Kind   Kind
	How    How       // of a Table
	VKind  ValueKind // of a Value
	Key    string    // its key in Parent ("" for the root and for an element)
	Parent *Node

	// Children are a table's entries in the order they are first defined,
	// an array of tables' elements.
	Children []*Node
	index    map[string]*Node

	// Span is where the node is written: a key/value's bytes (key to the
	// end of the value) for a Value and an inline table; for a [header]
	// table and an element, the header to the end of its last key/value,
	// for a table dotted keys define the key/values defining it — each
	// widened to the hull of what the table holds (its subtables'
	// sections: [a] holds [a.b], wherever it is written), as for an
	// implicit table and an array of tables; the root the whole document.
	// A table's span so encloses its children's.
	Span Span
	// KeySpan is the key's bytes: the last part of the key that defines
	// the node (of the header, for a header table), the header for an
	// element; the first header or key that names it for an implicit table
	// and an array of tables.
	KeySpan Span
	// Header is the [header] or [[header]] of a Header table or an Element.
	Header Span

	// Str is a String value; Strs the String elements of an Array value
	// (other elements are left out; Mixed tells there were any).
	Str   Str
	Strs  []Str
	Mixed bool
	// True is a Bool value's value.
	True bool
}

// Get returns the child with key k of a table (nil if none).
func (n *Node) Get(k string) *Node {
	if n == nil || n.Kind != Table {
		return nil
	}
	return n.index[k]
}

// IsTrue reports whether n is the Bool value true.
func (n *Node) IsTrue() bool { return n != nil && n.Kind == Value && n.VKind == Bool && n.True }

// String returns a String value's text and whether n is one.
func (n *Node) String() (string, bool) {
	if n == nil || n.Kind != Value || n.VKind != String {
		return "", false
	}
	return n.Str.Value, true
}

// Doc is a parsed document.
type Doc struct {
	Src      []byte
	Root     *Node
	Problems []Problem
	// ProblemsTruncated: the document has more problems than Problems holds.
	ProblemsTruncated bool
	// Complete: the whole document was read without a syntax error.
	Complete bool
}

// Parse reads src. It never fails: what cannot be read is a problem, and
// the tree holds what the document defines up to it.
func Parse(src []byte) *Doc {
	d := &Doc{Src: src, Root: &Node{Kind: Table, How: Root, Span: Span{0, len(src)}}}
	if len(src) > MaxBytes {
		d.problem(CodeTooLarge, 0, 0, false, fmt.Sprintf("the file is larger than %d bytes: its structure is not read", MaxBytes))
		return d
	}
	b := &builder{d: d, cur: d.Root, src: src}
	var p unstable.Parser
	p.Reset(src)
	for p.NextExpression() {
		b.expression(p.Expression())
	}
	syntaxOK := true
	if err := p.Error(); err != nil {
		syntaxOK = false
		off := len(src)
		var pe *unstable.ParserError
		msg := err.Error()
		if errors.As(err, &pe) {
			off = offsetOf(src, pe.Highlight)
			msg = pe.Message
		}
		end := off
		if end < len(src) {
			end++
		}
		d.problem(CodeSyntax, off, end, true, "invalid TOML: "+msg+": the rest of the file is not read")
	}
	hull(d.Root)
	d.Complete = syntaxOK
	if syntaxOK && !b.conflict && (b.nesting > MaxCheckedNesting || b.parts > MaxCheckedKeyParts) {
		d.problem(CodeNotChecked, 0, 0, false, fmt.Sprintf("values nest more than %d deep or a key has more than %d parts: dates, escapes and code points are not checked",
			MaxCheckedNesting, MaxCheckedKeyParts))
	} else if syntaxOK && !b.conflict {
		// A decoder checks what the parser does not: impossible dates,
		// invalid escapes and code points, table rules the builder lets
		// pass. BurntSushi/toml's (go-toml's own takes time quadratic in
		// the number of tables).
		var m map[string]any
		if _, err := bstoml.Decode(string(src), &m); err != nil {
			off, end := 0, 0
			msg := err.Error()
			var pe bstoml.ParseError
			if errors.As(err, &pe) {
				off = min(max(pe.Position.Start, 0), len(src))
				end = min(off+max(pe.Position.Len, 0), len(src))
				msg = pe.Message
			}
			d.problem(CodeInvalid, off, end, true, "invalid TOML: "+trimPrefix(msg))
		}
	}
	sort.SliceStable(d.Problems, func(i, j int) bool { return d.Problems[i].Start < d.Problems[j].Start })
	return d
}

func trimPrefix(s string) string {
	const p = "toml: "
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):]
	}
	return s
}

// offsetOf is the offset of b, a subslice of src, in src (len(src) for one
// that is none).
func offsetOf(src, b []byte) int {
	if b == nil || cap(b) > cap(src) {
		return len(src)
	}
	off := cap(src) - cap(b)
	if off < 0 || off > len(src) {
		return len(src)
	}
	return off
}

func (d *Doc) problem(code string, start, end int, isErr bool, msg string) {
	if len(d.Problems) >= MaxProblems {
		d.ProblemsTruncated = true
		return
	}
	d.Problems = append(d.Problems, Problem{Code: code, Message: msg, Start: start, End: end, Error: isErr})
}

// builder applies TOML's table rules to the expressions in order. It is
// lenient where a rule only decides validity (a dotted key may extend a
// table a header defines; the decoder reports that), strict where a
// definition would repeat one (a key defined twice, a table's header
// written twice): the second is a problem and defines nothing — the
// key/values under a repeated header go to a detached table.
type builder struct {
	d        *Doc
	src      []byte
	cur      *Node // the table key/values go to
	sect     *Node // the header table or element whose section is open, nil for the root's
	conflict bool
	nesting  int // the deepest nesting of arrays and inline tables
	parts    int // the most dotted parts of a key
}

// measure records how deep the values of an expression nest, without
// recursion.
func (b *builder) measure(e *unstable.Node) {
	type frame struct {
		n     *unstable.Node
		depth int
	}
	stack := []frame{{e, 0}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.depth > b.nesting {
			b.nesting = f.depth
		}
		it := f.n.Children()
		for it.Next() {
			c := it.Node()
			d := f.depth
			if c.Kind == unstable.Array || c.Kind == unstable.InlineTable {
				d++
			}
			stack = append(stack, frame{c, d})
		}
	}
}

func newTable(how How, key string, parent *Node, keySpan Span) *Node {
	return &Node{Kind: Table, How: how, Key: key, Parent: parent, KeySpan: keySpan, Span: Span{-1, -1}}
}

func (n *Node) add(c *Node) {
	if n.index == nil {
		n.index = map[string]*Node{}
	}
	n.index[c.Key] = c
	n.Children = append(n.Children, c)
}

func rng(r unstable.Range) Span {
	return Span{int(r.Offset), int(r.Offset + r.Length)}
}

type keyPart struct {
	name string
	span Span
}

func keyParts(it unstable.Iterator) []keyPart {
	var parts []keyPart
	for it.Next() {
		k := it.Node()
		parts = append(parts, keyPart{string(k.Data), rng(k.Raw)})
		if len(parts) > MaxKeyParts {
			return parts
		}
	}
	return parts
}

func (b *builder) keyParts(it unstable.Iterator) []keyPart {
	parts := keyParts(it)
	b.parts = max(b.parts, len(parts))
	return parts
}

func (b *builder) expression(e *unstable.Node) {
	switch e.Kind {
	case unstable.Table, unstable.ArrayTable:
		parts := b.keyParts(e.Key())
		if len(parts) == 0 {
			return
		}
		hdr := b.headerSpan(parts[0].span.Start, parts[len(parts)-1].span.End)
		if len(parts) > MaxKeyParts {
			b.tooLong(hdr)
			b.detach()
			return
		}
		b.header(parts, hdr, e.Kind == unstable.ArrayTable)
	case unstable.KeyValue:
		b.measure(e)
		kv := rng(e.Raw)
		parts := b.keyParts(e.Key())
		if len(parts) > MaxKeyParts {
			b.tooLong(kv)
			return
		}
		b.keyValue(b.cur, parts, e.Value(), kv, false)
		if b.sect != nil && kv.End > b.sect.Span.End {
			b.sect.Span.End = kv.End
		}
	}
}

func (b *builder) tooLong(sp Span) {
	b.d.problem(CodeKeyTooLong, sp.Start, sp.End, false, fmt.Sprintf("a key of more than %d dotted parts is not read", MaxKeyParts))
}

// headerSpan widens a header's key to its brackets.
func (b *builder) headerSpan(start, end int) Span {
	s, e := start, end
	for s > 0 && b.src[s-1] != '\n' && b.src[s-1] != '[' {
		s--
	}
	for s > 0 && b.src[s-1] == '[' {
		s--
	}
	for e < len(b.src) && b.src[e] != '\n' && b.src[e] != ']' {
		e++
	}
	for e < len(b.src) && b.src[e] == ']' {
		e++
	}
	return Span{s, e}
}

// detach sends the key/values that follow to a table outside the tree.
func (b *builder) detach() {
	b.cur = newTable(Header, "", nil, Span{})
	b.sect = nil
}

func (b *builder) dup(sp Span, code, what string, prev *Node) {
	b.conflict = true
	line := 1 + countLines(b.src[:prev.KeySpan.Start])
	b.d.problem(code, sp.Start, sp.End, true, fmt.Sprintf("%s is already defined at line %d", what, line))
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// descend walks the prefix of a header's key from the root: an array of
// tables leads to its last element; a missing table is created implicit.
func (b *builder) descend(parts []keyPart) (*Node, bool) {
	t := b.d.Root
	for _, p := range parts {
		c := t.index[p.name]
		switch {
		case c == nil:
			c = newTable(Implicit, p.name, t, p.span)
			t.add(c)
		case c.Kind == ArrayOfTables:
			if len(c.Children) == 0 {
				return nil, false
			}
			c = c.Children[len(c.Children)-1]
		case c.Kind == Value || c.How == Inline:
			b.conflict = true
			b.d.problem(CodeRedefined, p.span.Start, p.span.End, true,
				fmt.Sprintf("%q is a value defined at line %d: no table can be defined in it", p.name, 1+countLines(b.src[:c.KeySpan.Start])))
			return nil, false
		}
		t = c
	}
	return t, true
}

func (b *builder) header(parts []keyPart, hdr Span, array bool) {
	last := parts[len(parts)-1]
	t, ok := b.descend(parts[:len(parts)-1])
	if !ok {
		b.detach()
		return
	}
	c := t.index[last.name]
	if array {
		switch {
		case c == nil:
			c = &Node{Kind: ArrayOfTables, Key: last.name, Parent: t, KeySpan: last.span, Span: Span{-1, -1}}
			t.add(c)
		case c.Kind != ArrayOfTables:
			b.dup(last.span, CodeRedefined, fmt.Sprintf("%q", last.name), c)
			b.detach()
			return
		}
		el := newTable(Element, "", c, hdr)
		el.Header, el.Span = hdr, hdr
		c.Children = append(c.Children, el)
		b.cur, b.sect = el, el
		return
	}
	switch {
	case c == nil:
		c = newTable(Header, last.name, t, last.span)
		t.add(c)
	case c.Kind == Table && c.How == Implicit:
		c.How, c.KeySpan = Header, last.span
	default:
		b.dup(last.span, CodeRedefined, fmt.Sprintf("the table %q", last.name), c)
		b.detach()
		return
	}
	c.Header, c.Span = hdr, hdr
	b.cur, b.sect = c, c
}

// keyValue defines parts = v in t; inline is true within an inline table.
func (b *builder) keyValue(t *Node, parts []keyPart, v *unstable.Node, kv Span, inline bool) {
	if len(parts) == 0 {
		return
	}
	for _, p := range parts[:len(parts)-1] {
		c := t.index[p.name]
		switch {
		case c == nil:
			how := Dotted
			if inline {
				how = Inline // within an inline table every table is sealed with it
			}
			c = newTable(how, p.name, t, p.span)
			t.add(c)
		case c.Kind == Table && (c.How == Dotted || inline && c.How == Inline && c.Span.Start < 0):
			// Dotted keys extend only the tables dotted keys of the
			// same table define: not one a header defines or implies
			// (toml-lang/toml#846), nor a sealed inline table.
		default:
			b.dup(p.span, CodeDuplicateKey, fmt.Sprintf("the key %q", p.name), c)
			return
		}
		grow(c, kv)
		t = c
	}
	last := parts[len(parts)-1]
	if prev := t.index[last.name]; prev != nil {
		b.dup(last.span, CodeDuplicateKey, fmt.Sprintf("the key %q", last.name), prev)
		return
	}
	n := b.value(v, last.name, t, last.span, kv)
	t.add(n)
}

// grow widens the span of a table dotted keys define to a key/value
// defining it.
func grow(c *Node, kv Span) {
	if c.How != Dotted {
		return
	}
	if c.Span.Start < 0 || kv.Start < c.Span.Start {
		c.Span.Start = kv.Start
	}
	if kv.End > c.Span.End {
		c.Span.End = kv.End
	}
}

func (b *builder) value(v *unstable.Node, key string, parent *Node, keySpan, kv Span) *Node {
	switch v.Kind {
	case unstable.InlineTable:
		t := &Node{Kind: Table, How: Inline, Key: key, Parent: parent, KeySpan: keySpan, Span: kv}
		it := v.Children()
		for it.Next() {
			c := it.Node()
			if c.Kind != unstable.KeyValue {
				continue
			}
			parts := b.keyParts(c.Key())
			if len(parts) > MaxKeyParts {
				b.tooLong(rng(c.Raw))
				continue
			}
			b.keyValue(t, parts, c.Value(), rng(c.Raw), true)
		}
		sealInline(t, kv)
		return t
	case unstable.Array:
		n := &Node{Kind: Value, VKind: Array, Key: key, Parent: parent, KeySpan: keySpan, Span: kv}
		it := v.Children()
		for it.Next() {
			c := it.Node()
			switch c.Kind {
			case unstable.Comment:
			case unstable.String:
				n.Strs = append(n.Strs, Str{string(c.Data), rng(c.Raw)})
			default:
				n.Mixed = true
			}
		}
		return n
	}
	n := &Node{Kind: Value, Key: key, Parent: parent, KeySpan: keySpan, Span: kv}
	switch v.Kind {
	case unstable.String:
		n.VKind, n.Str = String, Str{string(v.Data), rng(v.Raw)}
	case unstable.Integer:
		n.VKind = Integer
	case unstable.Float:
		n.VKind = Float
	case unstable.Bool:
		n.VKind, n.True = Bool, string(v.Data) == "true"
	case unstable.DateTime:
		n.VKind = OffsetDateTime
		if !validOffset(v.Data) {
			b.conflict = true
			sp := rng(v.Raw)
			b.d.problem(CodeInvalid, sp.Start, sp.End, true, fmt.Sprintf("invalid TOML: %q has no valid time offset", v.Data))
		}
	case unstable.LocalDateTime:
		n.VKind = LocalDateTime
	case unstable.LocalDate:
		n.VKind = LocalDate
	case unstable.LocalTime:
		n.VKind = LocalTime
	}
	return n
}

// validOffset reports whether a date-time's offset (Z, or +HH:MM / -HH:MM)
// is a time zone offset: hours 00-23, minutes 00-59 (the decoder does not
// check the minutes).
func validOffset(dt []byte) bool {
	if len(dt) == 0 || dt[len(dt)-1] == 'Z' || dt[len(dt)-1] == 'z' {
		return true
	}
	if len(dt) < 6 || dt[len(dt)-3] != ':' {
		return true // the parser and the decoder judge any other form
	}
	h, m := dt[len(dt)-5:len(dt)-3], dt[len(dt)-2:]
	return h[0] >= '0' && h[0] <= '9' && h[1] >= '0' && h[1] <= '9' && (h[0]-'0')*10+h[1]-'0' <= 23 &&
		m[0] >= '0' && m[0] <= '5' && m[1] >= '0' && m[1] <= '9'
}

// sealInline gives the tables dotted keys define within an inline table
// the inline table's span (they are written within it).
func sealInline(t *Node, kv Span) {
	stack := []*Node{t}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == Table && n.Span.Start < 0 {
			n.Span = kv
		}
		for _, c := range n.Children {
			if c.Kind == Table && c.How == Inline && c.Span.Start < 0 {
				stack = append(stack, c)
			}
		}
	}
}

// hull widens every table and array of tables to the hull of what they
// hold (one with nothing of its own and nothing in it takes its key's
// bytes), bottom-up without recursion.
func hull(root *Node) {
	type frame struct {
		n    *Node
		next int
	}
	stack := []frame{{root, 0}}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		if f.next < len(f.n.Children) {
			c := f.n.Children[f.next]
			f.next++
			if c.Kind != Value {
				stack = append(stack, frame{c, 0})
			}
			continue
		}
		n := f.n
		stack = stack[:len(stack)-1]
		if n.How == Root && n.Parent == nil {
			continue
		}
		{
			for _, c := range n.Children {
				if c.Span.Start < 0 {
					continue
				}
				if n.Span.Start < 0 || c.Span.Start < n.Span.Start {
					n.Span.Start = c.Span.Start
				}
				if c.Span.End > n.Span.End {
					n.Span.End = c.Span.End
				}
			}
			if n.Span.Start < 0 {
				n.Span = n.KeySpan
			}
		}
	}
}
