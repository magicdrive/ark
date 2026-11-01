// Package xmldoc reads an XML document into a tree of elements with the
// bytes of every element and attribute, and the document's problems. It is
// the document layer of Ark's XML provider: it knows XML, not what a
// vocabulary (Maven) means by it.
//
// Parser. encoding/xml's Decoder in strict mode, token by token
// (RawToken): it checks the lexical syntax (names, quoting, characters,
// UTF-8, comments, CDATA, processing instructions, the DOCTYPE
// declaration's brackets) and reports the byte offset after each token, so
// a token spans from the previous offset to its own. What RawToken leaves
// to its caller, this package checks: that end tags match (recovering at the
// nearest open element of the name), namespace prefixes (a prefix no
// declaration binds is a diagnostic, and the element's namespace is a
// pseudo-namespace of its own), duplicate attributes (by namespace and
// local name), one root element and no text outside it.
//
// Safety. Nothing outside the source is read: encoding/xml never opens a
// DTD, an external entity or a URL, and this package never asks it to (no
// CharsetReader: a document in another encoding than UTF-8 or ASCII is
// reported and not read). Entities are never expanded: encoding/xml knows
// no DTD, so an entity a DOCTYPE declares is unknown to it; in a document
// with a DOCTYPE every entity reference the source spells (&name;) is given
// its own spelling as its replacement text — inserted once, never scanned
// again — so a billion-laughs document is as long read as written, and an
// external entity (SYSTEM "file:///etc/passwd") stays the text "&name;".
// Without a DOCTYPE an undeclared entity is a well-formedness error, as XML
// defines it. A file over MaxBytes is not read; nesting deeper than
// MaxDepth and more than MaxNodes elements and attributes end the reading
// with a diagnostic (the elements read so far are kept, their open ends at
// the stopping point); every bound is a diagnostic, never a panic.
package xmldoc

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Bounds of what Parse reads.
const (
	MaxBytes = 4 << 20 // bytes of a file
	MaxDepth = 256     // nesting of elements
	MaxNodes = 200000  // elements and attributes
	MaxText  = 1024    // bytes of an element's own text kept (Node.Text)

	maxProblems = 1000
	maxEntities = 10000 // distinct entity names given their own spelling
)

// The namespaces XML itself binds.
const (
	NamespaceXML   = "http://www.w3.org/XML/1998/namespace"
	NamespaceXMLNS = "http://www.w3.org/2000/xmlns/"
)

// Problem codes.
const (
	CodeSyntax            = "xml_syntax"           // encoding/xml rejects the text
	CodeMismatchedTag     = "xml_mismatched_tag"   // an end tag that closes no open element, or closes others with it
	CodeUnclosed          = "xml_unclosed"         // elements open at the end of the file
	CodeUndefinedPrefix   = "xml_undefined_prefix" // a namespace prefix no declaration binds
	CodeDuplicateAttr     = "xml_duplicate_attribute"
	CodeMultipleRoots     = "xml_multiple_roots" // a second root element, or text outside the root
	CodeTooLarge          = "xml_too_large"      // over MaxBytes
	CodeTooDeep           = "xml_too_deep"       // over MaxDepth
	CodeTooManyNodes      = "xml_too_many_nodes" // over MaxNodes
	CodeEncoding          = "xml_unsupported_encoding"
	CodeEntityNotExpanded = "xml_entity_not_expanded" // entity references a DTD would define, left as written
	CodeExternalEntity    = "xml_external_entity"     // a DOCTYPE declaring an external entity or DTD, not read
	CodeProblemsTruncated = "xml_truncated"
)

// Span is a range of bytes [Start, End) of the source.
type Span struct{ Start, End int }

// Problem is something wrong with the document, at a span of its bytes.
type Problem struct {
	Code       string
	Message    string
	Start, End int
	Error      bool // the document is not well-formed (else a warning)
}

// Name is an element's or attribute's name: its prefix as written, its local
// name and the namespace URI the prefix is bound to ("" for none; for a
// prefix no declaration binds, UndeclaredURI of the prefix).
type Name struct {
	Prefix, Local, URI string
}

// undeclared starts the pseudo-namespace of a prefix no declaration binds.
// It is U+0000, which no XML document can spell — neither as a character
// nor as a character reference (&#0; is not a Char) — so no declared
// namespace URI (an attribute value) is ever one of these.
const undeclared = "\x00"

// UndeclaredURI is the pseudo-namespace of the elements and attributes of
// a prefix no declaration binds: one per prefix, equal to no declared URI.
func UndeclaredURI(prefix string) string { return undeclared + prefix }

// UndeclaredPrefix returns the prefix of a pseudo-namespace UndeclaredURI
// made (false for a declared or empty URI).
func UndeclaredPrefix(uri string) (string, bool) { return strings.CutPrefix(uri, undeclared) }

// Attr is an attribute of an element, at the bytes of `name="value"`.
type Attr struct {
	Name  Name
	Value string // as encoding/xml reads it: entities replaced, line breaks normalized
	Span  Span
	// ValueSpan is the bytes of the value between its quotes.
	ValueSpan Span
	// Namespace marks a namespace declaration (xmlns, xmlns:p).
	Namespace bool
	// Duplicate marks the second attribute of one name in an element.
	Duplicate bool
}

// Node is an element.
type Node struct {
	Name     Name
	Attrs    []Attr
	Children []*Node
	Parent   *Node
	// Span is the element from its start tag's '<' to its end tag's '>'
	// (an element left open ends where the reading ended).
	Span Span
	// TagEnd is the end of the start tag.
	TagEnd int
	// Text is the element's own character data (not its children's),
	// CDATA included, at most MaxText bytes; TextCut reports more.
	Text    string
	TextCut bool
	// TextSpan spans the element's own character data, first to last
	// (Start == End == TagEnd when it has none).
	TextSpan Span
	// Index is the element's ordinal, from 1, among its siblings of its
	// namespace URI and local name.
	Index int
	// Depth is 1 for a root element.
	Depth int

	hasText bool
}

// ProcInst is a processing instruction (not the XML declaration).
type ProcInst struct {
	Target string
	Inst   string
	Span   Span
}

// Document is a parsed XML file.
type Document struct {
	Src      []byte
	Root     *Node   // the first root element (nil when there is none)
	Roots    []*Node // every root element (well-formed: one)
	ProcInst []ProcInst
	// Doctype reports a DOCTYPE declaration; Entities, that entity
	// references were left as written (CodeEntityNotExpanded).
	Doctype  bool
	Entities bool
	// Declarations lists, in document order, every namespace declaration:
	// its prefix ("" for a default declaration) and URI.
	Declarations []Name
	// Nodes counts the elements and attributes read.
	Nodes int
	// Stopped reports that reading ended before the end of the file (a
	// syntax error or a bound).
	Stopped bool

	Problems          []Problem
	ProblemsTruncated bool
}

// Parse reads src. It never fails: what it cannot read is a problem.
func Parse(src []byte) *Document {
	doc := &Document{Src: src}
	if len(src) > MaxBytes {
		doc.problem(CodeTooLarge, 0, 0, false, fmt.Sprintf("the file is larger than %d bytes and is not read", MaxBytes))
		doc.Stopped = true
		return doc
	}
	if enc := otherEncoding(src); enc != "" {
		doc.problem(CodeEncoding, 0, 0, false, fmt.Sprintf("the file is encoded in %s: Ark reads UTF-8 XML only", enc))
		doc.Stopped = true
		return doc
	}
	p := &parser{doc: doc}
	p.run()
	return doc
}

// otherEncoding names the encoding of a file that is neither UTF-8 nor
// ASCII by its byte order mark or its XML declaration ("" for UTF-8).
func otherEncoding(src []byte) string {
	switch {
	case bytes.HasPrefix(src, []byte{0xFE, 0xFF}), bytes.HasPrefix(src, []byte{0xFF, 0xFE}),
		bytes.HasPrefix(src, []byte{0, '<'}), bytes.HasPrefix(src, []byte{'<', 0}):
		return "UTF-16 or UTF-32"
	}
	s := src
	s = bytes.TrimPrefix(s, []byte{0xEF, 0xBB, 0xBF})
	if !bytes.HasPrefix(s, []byte("<?xml")) {
		return ""
	}
	end := bytes.Index(s, []byte("?>"))
	if end < 0 {
		return ""
	}
	decl := string(s[:end])
	i := strings.Index(decl, "encoding")
	if i < 0 {
		return ""
	}
	rest := strings.TrimLeft(decl[i+len("encoding"):], " \t\r\n")
	rest, ok := strings.CutPrefix(rest, "=")
	if !ok {
		return ""
	}
	rest = strings.TrimLeft(rest, " \t\r\n")
	if rest == "" || rest[0] != '"' && rest[0] != '\'' {
		return ""
	}
	q := rest[0]
	j := strings.IndexByte(rest[1:], q)
	if j < 0 {
		return ""
	}
	enc := rest[1 : 1+j]
	switch strings.ToLower(enc) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return ""
	}
	if !utf8.ValidString(enc) || len(enc) > 40 {
		return "an unsupported encoding"
	}
	return fmt.Sprintf("%q", enc)
}

type parser struct {
	doc      *Document
	dec      *xml.Decoder
	stack    []*Node
	scopes   []map[string]string // namespace bindings per open element
	siblings []map[Name]int      // per open element (and the document), ordinals of children by URI and local name
	rootDone bool
	textOut  bool // text outside the root reported
}

// predefined are the entities XML predefines; encoding/xml replaces them.
var predefined = map[string]bool{"lt": true, "gt": true, "amp": true, "apos": true, "quot": true}

func (p *parser) run() {
	src := p.doc.Src
	p.dec = xml.NewDecoder(bytes.NewReader(src))
	p.dec.Strict = true
	// No CharsetReader: a declared encoding other than UTF-8 is an error
	// (otherEncoding reported it first), never a conversion.
	p.siblings = []map[Name]int{{}}
	for {
		start := int(p.dec.InputOffset())
		tok, err := p.dec.RawToken()
		end := int(p.dec.InputOffset())
		if err != nil {
			if !errors.Is(err, io.EOF) {
				p.doc.Stopped = true
				p.doc.problem(CodeSyntax, end, end, true, syntaxMessage(err))
			}
			break
		}
		if !p.token(tok, start, end) {
			p.doc.Stopped = true
			break
		}
	}
	at := int(p.dec.InputOffset())
	if len(p.stack) > 0 {
		if !p.doc.Stopped {
			p.doc.problem(CodeUnclosed, at, at, true, fmt.Sprintf("the element <%s> is not closed", qname(p.stack[len(p.stack)-1].Name)))
		}
		for len(p.stack) > 0 {
			p.pop(at)
		}
	}
}

// syntaxMessage is encoding/xml's message without its line (the
// diagnostic's location states it).
func syntaxMessage(err error) string {
	var se *xml.SyntaxError
	if errors.As(err, &se) {
		return "XML syntax error: " + se.Msg
	}
	return "XML syntax error: " + err.Error()
}

// token handles one token; false stops the reading.
func (p *parser) token(tok xml.Token, start, end int) bool {
	switch t := tok.(type) {
	case xml.StartElement:
		return p.startElement(t, start, end)
	case xml.EndElement:
		p.endElement(t, start, end)
	case xml.CharData:
		p.charData(t, start, end)
	case xml.ProcInst:
		if t.Target != "xml" {
			p.doc.ProcInst = append(p.doc.ProcInst, ProcInst{Target: t.Target, Inst: string(t.Inst), Span: Span{start, end}})
		}
	case xml.Directive:
		p.directive(t, start, end)
	}
	return true
}

func (p *parser) startElement(t xml.StartElement, start, end int) bool {
	if len(p.stack) >= MaxDepth {
		p.doc.problem(CodeTooDeep, start, start, false, fmt.Sprintf("elements nest deeper than %d: the rest of the file is not read", MaxDepth))
		return false
	}
	p.doc.Nodes += 1 + len(t.Attr)
	if p.doc.Nodes > MaxNodes {
		p.doc.problem(CodeTooManyNodes, start, start, false, fmt.Sprintf("the file has more than %d elements and attributes: the rest is not read", MaxNodes))
		return false
	}
	if len(p.stack) == 0 && p.rootDone {
		p.doc.problem(CodeMultipleRoots, start, end, true, "a document has one root element: this is a second one")
	}
	scope := map[string]string{}
	attrSpans := scanAttrs(p.doc.Src, start, end, len(t.Attr))
	n := &Node{Span: Span{start, end}, TagEnd: end, TextSpan: Span{end, end}, Depth: len(p.stack) + 1}
	for i, a := range t.Attr {
		at := Attr{Name: Name{Prefix: a.Name.Space, Local: a.Name.Local}, Value: a.Value, Span: Span{start, end}, ValueSpan: Span{start, end}}
		if attrSpans != nil {
			at.Span, at.ValueSpan = attrSpans[i][0], attrSpans[i][1]
		}
		switch {
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			at.Namespace = true
			scope[""] = a.Value
			p.doc.Declarations = append(p.doc.Declarations, Name{Prefix: "", URI: a.Value})
		case a.Name.Space == "xmlns":
			at.Namespace = true
			scope[a.Name.Local] = a.Value
			p.doc.Declarations = append(p.doc.Declarations, Name{Prefix: a.Name.Local, URI: a.Value})
		}
		n.Attrs = append(n.Attrs, at)
	}
	p.scopes = append(p.scopes, scope)
	n.Name = p.resolve(t.Name.Space, t.Name.Local, true, start, end)
	seen := map[Name]bool{}
	for i := range n.Attrs {
		a := &n.Attrs[i]
		switch {
		case a.Namespace:
			a.Name.URI = NamespaceXMLNS
		case a.Name.Prefix == "":
			// An unprefixed attribute is in no namespace.
		default:
			a.Name = p.resolve(a.Name.Prefix, a.Name.Local, false, a.Span.Start, a.Span.End)
		}
		key := Name{URI: a.Name.URI, Local: a.Name.Local}
		if a.Namespace {
			key = Name{URI: NamespaceXMLNS, Local: a.Name.Prefix + ":" + a.Name.Local}
		}
		if seen[key] {
			a.Duplicate = true
			p.doc.problem(CodeDuplicateAttr, a.Span.Start, a.Span.End, true, fmt.Sprintf("the attribute %s appears twice in the element", qname(a.Name)))
		}
		seen[key] = true
	}
	sib := p.siblings[len(p.siblings)-1]
	key := Name{URI: n.Name.URI, Local: n.Name.Local}
	sib[key]++
	n.Index = sib[key]
	if len(p.stack) > 0 {
		parent := p.stack[len(p.stack)-1]
		n.Parent = parent
		parent.Children = append(parent.Children, n)
	} else {
		p.doc.Roots = append(p.doc.Roots, n)
		if p.doc.Root == nil {
			p.doc.Root = n
		}
	}
	p.stack = append(p.stack, n)
	p.siblings = append(p.siblings, map[Name]int{})
	return true
}

// resolve binds a prefix by the open elements' declarations.
func (p *parser) resolve(prefix, local string, element bool, start, end int) Name {
	nm := Name{Prefix: prefix, Local: local}
	if prefix == "" && !element {
		return nm
	}
	if prefix == "xml" {
		nm.URI = NamespaceXML
		return nm
	}
	for i := len(p.scopes) - 1; i >= 0; i-- {
		if uri, ok := p.scopes[i][prefix]; ok {
			nm.URI = uri
			return nm
		}
	}
	if prefix != "" {
		p.doc.problem(CodeUndefinedPrefix, start, end, true, fmt.Sprintf("the namespace prefix %q is not declared", prefix))
		nm.URI = UndeclaredURI(prefix)
	}
	return nm
}

func (p *parser) endElement(t xml.EndElement, start, end int) {
	if len(p.stack) == 0 {
		p.doc.problem(CodeMismatchedTag, start, end, true, fmt.Sprintf("the end tag </%s> closes no element", qname(Name{Prefix: t.Name.Space, Local: t.Name.Local})))
		return
	}
	top := p.stack[len(p.stack)-1]
	if top.Name.Prefix == t.Name.Space && top.Name.Local == t.Name.Local {
		p.pop(end)
		return
	}
	// Recover at the nearest open element of the name: the ones inside it
	// end here; with none, the end tag is ignored.
	for i := len(p.stack) - 2; i >= 0; i-- {
		n := p.stack[i]
		if n.Name.Prefix == t.Name.Space && n.Name.Local == t.Name.Local {
			p.doc.problem(CodeMismatchedTag, start, end, true, fmt.Sprintf("the end tag </%s> closes <%s>, which is still open", qname(n.Name), qname(top.Name)))
			for len(p.stack) > i+1 {
				p.pop(start)
			}
			p.pop(end)
			return
		}
	}
	p.doc.problem(CodeMismatchedTag, start, end, true, fmt.Sprintf("the end tag </%s> does not match the open element <%s>", qname(Name{Prefix: t.Name.Space, Local: t.Name.Local}), qname(top.Name)))
}

func (p *parser) pop(end int) {
	n := p.stack[len(p.stack)-1]
	n.Span.End = end
	p.stack = p.stack[:len(p.stack)-1]
	p.scopes = p.scopes[:len(p.scopes)-1]
	p.siblings = p.siblings[:len(p.siblings)-1]
	if len(p.stack) == 0 {
		p.rootDone = true
	}
}

func (p *parser) charData(t xml.CharData, start, end int) {
	if len(p.stack) == 0 {
		if !p.textOut && len(bytes.Trim(t, " \t\r\n\ufeff")) > 0 {
			p.textOut = true
			p.doc.problem(CodeMultipleRoots, start, end, true, "text outside the root element")
		}
		return
	}
	n := p.stack[len(p.stack)-1]
	if !n.hasText {
		n.hasText = true
		n.TextSpan.Start = start
	}
	n.TextSpan.End = end
	if room := MaxText - len(n.Text); room > 0 {
		if len(t) > room {
			n.Text += string(t[:room])
			n.TextCut = true
		} else {
			n.Text += string(t)
		}
	} else if len(t) > 0 {
		n.TextCut = true
	}
}

// directive handles <!DOCTYPE ...> (and any other <!...> declaration). A
// DOCTYPE may declare entities; they are never read or expanded (package
// comment). Every entity reference spelled after it is given its own
// spelling, so encoding/xml leaves it as written.
func (p *parser) directive(t xml.Directive, start, end int) {
	text := string(t)
	if !strings.HasPrefix(text, "DOCTYPE") {
		return
	}
	if p.doc.Doctype {
		// The first DOCTYPE already gave every entity reference of the rest
		// of the file its own spelling: scanning the rest again for each
		// further one would read the file once per DOCTYPE (quadratic).
		p.doc.problem(CodeSyntax, start, end, true, "a DOCTYPE declaration is allowed once, before the root element")
		return
	}
	if len(p.stack) > 0 || p.rootDone {
		p.doc.problem(CodeSyntax, start, end, true, "a DOCTYPE declaration is allowed once, before the root element")
	}
	p.doc.Doctype = true
	if external(text) {
		p.doc.problem(CodeExternalEntity, start, end, false,
			"the DOCTYPE names an external DTD or entity: Ark never reads it (nor any file or URL it names)")
	}
	names := entityNames(p.doc.Src[end:])
	if len(names) == 0 {
		return
	}
	if p.dec.Entity == nil {
		p.dec.Entity = map[string]string{}
	}
	for _, name := range names {
		p.dec.Entity[name] = "&" + name + ";"
	}
	p.doc.Entities = true
	p.doc.problem(CodeEntityNotExpanded, start, end, false,
		fmt.Sprintf("the document refers to %d entities its DTD would define: Ark does not read DTDs and leaves the references as written", len(names)))
}

// external reports whether a DOCTYPE's text names an external DTD or
// declares an external entity (a SYSTEM or PUBLIC identifier).
func external(text string) bool {
	return strings.Contains(text, "SYSTEM") || strings.Contains(text, "PUBLIC")
}

// entityNames lists, once each in order, the names of the entity
// references (&name;) src spells, predefined ones and character references
// aside, at most maxEntities.
func entityNames(src []byte) []string {
	var names []string
	seen := map[string]bool{}
	for i := 0; i < len(src); i++ {
		if src[i] != '&' {
			continue
		}
		j := i + 1
		for j < len(src) && j-i <= 256 && src[j] != ';' && nameByte(src[j]) {
			j++
		}
		if j >= len(src) || src[j] != ';' || j == i+1 || src[i+1] == '#' {
			continue
		}
		name := string(src[i+1 : j])
		if !predefined[name] && !seen[name] && utf8.ValidString(name) {
			seen[name] = true
			names = append(names, name)
			if len(names) >= maxEntities {
				break
			}
		}
		i = j
	}
	return names
}

func nameByte(c byte) bool {
	return c >= 0x80 || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':'
}

// scanAttrs finds, in the start tag src[start:end], the spans of its n
// attributes (`name = "value"`) and of their values, in order; nil when
// the tag does not read as n attributes.
func scanAttrs(src []byte, start, end, n int) [][2]Span {
	if n == 0 || end > len(src) || start >= end {
		return nil
	}
	i := start + 1
	for i < end && !space(src[i]) && src[i] != '/' && src[i] != '>' {
		i++
	}
	out := make([][2]Span, 0, n)
	for len(out) < n {
		for i < end && space(src[i]) {
			i++
		}
		ns := i
		for i < end && !space(src[i]) && src[i] != '=' && src[i] != '>' && src[i] != '/' {
			i++
		}
		if i == ns {
			return nil
		}
		for i < end && space(src[i]) {
			i++
		}
		if i >= end || src[i] != '=' {
			return nil
		}
		i++
		for i < end && space(src[i]) {
			i++
		}
		if i >= end || src[i] != '"' && src[i] != '\'' {
			return nil
		}
		q := src[i]
		vs := i + 1
		j := bytes.IndexByte(src[vs:end], q)
		if j < 0 {
			return nil
		}
		ve := vs + j
		out = append(out, [2]Span{{ns, ve + 1}, {vs, ve}})
		i = ve + 1
	}
	return out
}

func space(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func qname(n Name) string {
	if n.Prefix == "" {
		return n.Local
	}
	return n.Prefix + ":" + n.Local
}

// QName is a name as written (prefix:local).
func QName(n Name) string { return qname(n) }

func (d *Document) problem(code string, start, end int, isErr bool, msg string) {
	if len(d.Problems) >= maxProblems {
		d.ProblemsTruncated = true
		return
	}
	d.Problems = append(d.Problems, Problem{Code: code, Message: msg, Start: start, End: end, Error: isErr})
}

// Walk visits every element in document order (depth first); f returning
// false skips the element's children.
func (d *Document) Walk(f func(*Node) bool) {
	var walk func(n *Node)
	walk = func(n *Node) {
		if !f(n) {
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range d.Roots {
		walk(r)
	}
}

// Child returns n's first child element of the namespace URI and local
// name (nil for none).
func (n *Node) Child(uri, local string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name.URI == uri && c.Name.Local == local {
			return c
		}
	}
	return nil
}

// ChildrenNamed returns n's child elements of the namespace URI and local
// name, in order.
func (n *Node) ChildrenNamed(uri, local string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, c := range n.Children {
		if c.Name.URI == uri && c.Name.Local == local {
			out = append(out, c)
		}
	}
	return out
}

// Attr returns the value of n's attribute of the namespace URI and local
// name (the first, if it is duplicated).
func (n *Node) Attr(uri, local string) (*Attr, bool) {
	for i := range n.Attrs {
		a := &n.Attrs[i]
		if !a.Namespace && a.Name.URI == uri && a.Name.Local == local {
			return a, true
		}
	}
	return nil, false
}
