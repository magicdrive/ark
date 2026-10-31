// Package xml is Ark's XML provider (.xml).
//
// Two layers, kept apart. The document layer (xmldoc) reads the XML with
// encoding/xml and knows elements, attributes, namespaces, their bytes and
// the document's problems; it never reads a DTD, an external entity or any
// other file, and expands no entity (xmldoc's package comment). The meaning
// layer (maven) reads a Maven POM from those elements. This package states,
// for every XML file:
//
//   - the file itself (a document, Qualified its FileID);
//   - for a POM, what maven states (its project, parent, modules and
//     dependencies) — first, so the bounds below never cut them;
//   - the IDs of its elements: an xml:id (XML's own ID attribute) is an
//     anchor "<FileID>#<id>" named "#<id>"; an attribute "id" in no
//     namespace is one too, at most a Candidate target (without a schema
//     the attribute is an ID only by convention);
//   - its elements, by an XPath-like path from the root:
//     "<FileID>#/project/dependencies/dependency[2]" (below);
//   - their attributes, "<FileID>#/project/@id" (a namespace declaration
//     is a namespace, "…/@xmlns:p", its URI the signature);
//   - contains references: the document its root element, an element its
//     child elements and its attributes (Name the child's identity);
//   - id_reference references of a fragment href="#x" (XLink's xlink:href,
//     Exact; an href in no namespace, which a vocabulary may define
//     otherwise, at most Strong) to the anchor of the document's ID x;
//   - resource_link references with path evidence of an XInclude
//     <xi:include href> and an <?xml-stylesheet href?>, when the href is a
//     plain relative path (no scheme, query, fragment, escape or xml:base
//     around it) — to the file's own declaration, of any language;
//   - the problems xmldoc and maven report, as diagnostics.
//
// Element identity. A step of the path is the element's name and, for its
// n-th sibling (n >= 2) of that name, "[n]": "/a/b[2]" is the second b of
// the first a. The name is its namespace's label and its local name
// ("label:local", the label "" written as nothing): elements are the same
// by namespace URI and local name, not by the prefix written. A
// namespace's label is the first prefix a declaration binds it to in the
// document, "" when only a default declaration binds it; when two
// namespaces would share a label, the later one (in document order) is
// "label~2", "label~3", … ("~" is no character of an XML name). Inserting a
// sibling of another name keeps every identity; inserting one of the same
// name before others renumbers those after it, and a new namespace
// declaration before others may relabel them — the identity is a path,
// not a fact about the element.
//
// Bounds and priority. Every name and identity is a one-line name
// (declname); an element whose path is none is not declared, nor anything
// under it. A file states at most maxSymbols declarations — the document
// and the POM's first, then IDs, then elements breadth first (shallow
// first) up to maxElements, then attributes in the same order — and
// maxRefs references; past them xml_truncated. Text is no declaration.
//
// Identity. Every file is IdentityOnly: a reference reaches a declaration
// only by its qualified identity, never by a name match — an element
// "/project/artifactId" is no Java declaration, and no Java name reaches
// an XML element.
package xml

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/languages/xml/maven"
	"github.com/magicdrive/ark/internal/languages/xml/xmldoc"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

const cacheVersion = "xml-1"

const (
	maxSymbols  = 5000
	maxElements = 3000
	maxRefs     = 20000
)

// CodeTruncated reports a file whose declarations or references go past
// the provider's bounds.
const CodeTruncated = "xml_truncated"

// The namespaces of the relations read.
const (
	namespaceXLink     = "http://www.w3.org/1999/xlink"
	namespaceXInclude  = "http://www.w3.org/2001/XInclude"
	namespaceXInclude3 = "http://www.w3.org/2003/XInclude"
)

// Provider implements language.Provider for XML.
type Provider struct{}

// NewProvider returns the XML provider.
func NewProvider() *Provider { return &Provider{} }

func (*Provider) Language() language.Language { return "xml" }
func (*Provider) Extensions() []string        { return []string{".xml"} }
func (*Provider) CacheVersion() string        { return cacheVersion }

// Extract states the file's declarations, references and problems.
func (*Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	if err := ctx.Err(); err != nil {
		return language.Extraction{}, err
	}
	e := &extractor{file: file, d: xmldoc.Parse(src), lines: newLines(src), declared: map[*xmldoc.Node]string{}, qualified: map[string]bool{}}
	e.run()
	return language.Extraction{
		Symbols:      e.symbols,
		References:   e.refs,
		Diagnostics:  e.diags,
		Aliases:      e.aliases,
		IdentityOnly: true,
	}, nil
}

type extractor struct {
	file     source.FileID
	d        *xmldoc.Document
	lines    lines
	symbols  []language.SymbolDraft
	refs     []language.ReferenceDraft
	aliases  []language.AliasDraft
	diags    []language.Diagnostic
	full     bool // a bound was reached and reported
	diagsCut bool

	labels    map[string]string       // namespace URI -> label
	declared  map[*xmldoc.Node]string // element -> its identity
	paths     map[*xmldoc.Node]string // element -> its path
	attrIDs   map[*xmldoc.Attr]string // attribute -> its identity
	qualified map[string]bool         // identities declared
}

func (e *extractor) run() {
	for _, p := range e.d.Problems {
		e.problem(p)
	}
	if e.d.ProblemsTruncated {
		e.diagsFull()
	}
	file := string(e.file)
	e.symbol(path.Base(strings.ReplaceAll(file, "\\", "/")), file, symbol.KindDocument, xmldoc.Span{End: len(e.d.Src)}, "", "")
	if r, ok := maven.Extract(file, e.d); ok {
		for _, s := range r.Symbols {
			e.symbol(s.Name, s.Qualified, s.Kind, s.Span, s.Parent, s.Signature)
		}
		e.ref(language.ReferenceDraft{Name: r.Project, Kind: string(reference.KindContains), Container: file,
			NameQualified: r.Project, IdentityInRepository: true, TargetKinds: string(symbol.KindPackage)}, e.d.Root.Span)
		for _, r := range r.Refs {
			e.ref(language.ReferenceDraft{
				Name: r.Name, Kind: r.Kind, Container: r.Container, NameQualified: r.NameQualified,
				FileIdentity: r.FileIdentity, IdentityInRepository: r.InRepository, Dynamic: r.Dynamic,
				ConfidenceCap: r.Cap, TargetKinds: r.TargetKinds,
			}, r.Span)
		}
		for _, a := range r.Aliases {
			if e.isName(a.Qualified) && e.isName(a.Target) {
				e.aliases = append(e.aliases, language.AliasDraft{Qualified: a.Qualified, Target: a.Target,
					Location: e.loc(a.Span.Start, a.Span.Start), ConfidenceCap: a.Cap, Fallback: a.Fallback})
			}
		}
		for _, p := range r.Problems {
			e.problem(p)
		}
	}
	e.label()
	e.ids()
	e.elements()
	e.attributes()
	e.idReferences()
	e.links()
}

// label gives every namespace the document uses its label (package
// comment).
func (e *extractor) label() {
	want := map[string]string{}
	for _, decl := range e.d.Declarations {
		if _, ok := want[decl.URI]; !ok && decl.Prefix != "" {
			want[decl.URI] = decl.Prefix
		}
	}
	want[xmldoc.NamespaceXML] = "xml"
	e.labels = map[string]string{}
	taken := map[string]bool{}
	use := func(uri string) {
		if _, ok := e.labels[uri]; ok {
			return
		}
		l := want[uri]
		if uri != "" && uri[0] == '?' {
			l = uri[1:] // an undeclared prefix, as written
		}
		lbl := l
		for n := 2; taken[lbl]; n++ {
			lbl = l + "~" + strconv.Itoa(n)
		}
		taken[lbl] = true
		e.labels[uri] = lbl
	}
	e.d.Walk(func(n *xmldoc.Node) bool {
		use(n.Name.URI)
		for _, a := range n.Attrs {
			if !a.Namespace && a.Name.URI != "" {
				use(a.Name.URI)
			}
		}
		return true
	})
}

func (e *extractor) step(n xmldoc.Name) string {
	if l := e.labels[n.URI]; l != "" {
		return l + ":" + n.Local
	}
	return n.Local
}

// ids states the anchors of the document's IDs.
func (e *extractor) ids() {
	e.d.Walk(func(n *xmldoc.Node) bool {
		seen := map[string]bool{}
		for i := range n.Attrs {
			a := &n.Attrs[i]
			if a.Namespace || a.Duplicate || a.Name.Local != "id" || a.Name.URI != "" && a.Name.URI != xmldoc.NamespaceXML {
				continue
			}
			v := strings.TrimSpace(a.Value)
			if !ncName(v) || seen[v] {
				continue
			}
			seen[v] = true
			sig := "xml:id"
			cap := ""
			if a.Name.URI == "" {
				sig, cap = "id", "candidate"
			}
			if q := string(e.file) + "#" + v; !e.isName(q) {
				continue
			} else if !e.symbol("#"+v, q, symbol.KindAnchor, a.Span, "", sig) {
				return false
			}
			e.symbols[len(e.symbols)-1].ConfidenceCap = cap
		}
		return true
	})
}

// ncName reports whether s is an XML name without a colon (an ID's
// spelling), so "#" + s never reads as a path ("#/…").
func ncName(s string) bool {
	if !declname.IsName(s) {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= 0xC0 && r != 0xD7 && r != 0xF7:
		case i > 0 && (r == '-' || r == '.' || r >= '0' && r <= '9' || r == 0xB7):
		default:
			return false
		}
	}
	return true
}

// elements states the elements breadth first.
func (e *extractor) elements() {
	e.paths = map[*xmldoc.Node]string{}
	queue := append([]*xmldoc.Node(nil), e.d.Roots...)
	count := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		parent, parentPath := string(e.file), ""
		if n.Parent != nil {
			parent, parentPath = e.declared[n.Parent], e.paths[n.Parent]
		}
		st := e.step(n.Name)
		if n.Index > 1 {
			st += "[" + strconv.Itoa(n.Index) + "]"
		}
		p := parentPath + "/" + st
		q := string(e.file) + "#" + p
		if !declname.IsName(p) || !e.isName(q) {
			continue
		}
		if count >= maxElements {
			e.truncated(n.Span)
			return
		}
		if !e.symbol(p, q, symbol.KindElement, n.Span, parent, "") {
			return
		}
		count++
		e.declared[n], e.paths[n] = q, p
		e.contains(parent, q, symbol.KindElement, n.Span)
		queue = append(queue, n.Children...)
	}
}

// attributes states the attributes of the declared elements, in the order
// the elements were declared.
func (e *extractor) attributes() {
	e.attrIDs = map[*xmldoc.Attr]string{}
	var order []*xmldoc.Node
	for n := range e.declared {
		order = append(order, n)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].Depth != order[j].Depth {
			return order[i].Depth < order[j].Depth
		}
		return order[i].Span.Start < order[j].Span.Start
	})
	for _, n := range order {
		for i := range n.Attrs {
			a := &n.Attrs[i]
			if a.Duplicate {
				continue
			}
			var st, sig string
			kind := symbol.KindAttribute
			switch {
			case a.Namespace:
				st = "@" + xmldoc.QName(a.Name)
				kind = symbol.KindNamespace
				if declname.IsName(a.Value) {
					sig = a.Value
				}
			case a.Name.URI == "":
				st = "@" + a.Name.Local // in no namespace, whatever the elements' default
			default:
				st = "@" + e.step(a.Name)
			}
			p := e.paths[n] + "/" + st
			q := string(e.file) + "#" + p
			if !declname.IsName(p) || !e.isName(q) {
				continue
			}
			if !e.symbol(p, q, kind, a.Span, e.declared[n], sig) {
				return
			}
			e.attrIDs[a] = q
			e.contains(e.declared[n], q, kind, a.Span)
		}
	}
}

func (e *extractor) contains(parent, child string, kind symbol.SymbolKind, sp xmldoc.Span) {
	e.ref(language.ReferenceDraft{Name: child, Kind: string(reference.KindContains), Container: parent,
		NameQualified: child, IdentityInRepository: true, TargetKinds: string(kind)}, sp)
}

// container is the innermost declaration around an attribute of n: the
// attribute's own, else the nearest declared element, else the document.
func (e *extractor) container(n *xmldoc.Node, a *xmldoc.Attr) string {
	if a != nil {
		if q, ok := e.attrIDs[a]; ok {
			return q
		}
	}
	for ; n != nil; n = n.Parent {
		if q, ok := e.declared[n]; ok {
			return q
		}
	}
	return string(e.file)
}

// idReferences states the fragment references (href="#x") to the
// document's IDs.
func (e *extractor) idReferences() {
	e.d.Walk(func(n *xmldoc.Node) bool {
		for i := range n.Attrs {
			a := &n.Attrs[i]
			if a.Namespace || a.Duplicate || a.Name.Local != "href" || a.Name.URI != "" && a.Name.URI != namespaceXLink {
				continue
			}
			v := strings.TrimSpace(a.Value)
			id, ok := strings.CutPrefix(v, "#")
			if !ok || !ncName(id) {
				continue
			}
			cap := ""
			if a.Name.URI == "" {
				cap = "strong"
			}
			e.ref(language.ReferenceDraft{Name: v, Kind: string(reference.KindIDReference), Container: e.container(n, a),
				NameQualified: string(e.file) + "#" + id, IdentityInRepository: true, ConfidenceCap: cap,
				TargetKinds: string(symbol.KindAnchor)}, a.ValueSpan)
		}
		return true
	})
}

// links states the relative file references of XInclude and
// xml-stylesheet.
func (e *extractor) links() {
	dir := path.Dir(strings.ReplaceAll(string(e.file), "\\", "/"))
	if dir == "." {
		dir = ""
	}
	for _, pi := range e.d.ProcInst {
		if pi.Target != "xml-stylesheet" {
			continue
		}
		href, ok := pseudoAttr(pi.Inst, "href")
		if !ok {
			continue
		}
		e.link(dir, href, string(e.file), pi.Span)
	}
	e.d.Walk(func(n *xmldoc.Node) bool {
		if _, ok := n.Attr(xmldoc.NamespaceXML, "base"); ok {
			return false // xml:base changes what a relative path names
		}
		if n.Name.Local != "include" || n.Name.URI != namespaceXInclude && n.Name.URI != namespaceXInclude3 {
			return true
		}
		if a, ok := n.Attr("", "href"); ok {
			e.link(dir, a.Value, e.container(n, a), a.ValueSpan)
		}
		return true
	})
}

func (e *extractor) link(dir, href, container string, sp xmldoc.Span) {
	href = strings.TrimSpace(href)
	p, ok := maven.RelativePath(dir, href)
	if !ok || !declname.IsName(href) {
		return
	}
	in := !(p == ".." || strings.HasPrefix(p, "../"))
	e.ref(language.ReferenceDraft{Name: href, Kind: string(reference.KindResourceLink), Container: container,
		NameQualified: e.filePath(p), FileIdentity: true, IdentityInRepository: in}, sp)
}

// pseudoAttr reads a pseudo-attribute (name="value") of a processing
// instruction's text.
func pseudoAttr(inst, name string) (string, bool) {
	for i := 0; i < len(inst); {
		j := strings.Index(inst[i:], name)
		if j < 0 {
			return "", false
		}
		k := i + j
		i = k + len(name)
		if k > 0 && !strings.ContainsRune(" \t\r\n", rune(inst[k-1])) {
			continue
		}
		rest := strings.TrimLeft(inst[i:], " \t\r\n")
		rest, ok := strings.CutPrefix(rest, "=")
		if !ok {
			continue
		}
		rest = strings.TrimLeft(rest, " \t\r\n")
		if rest == "" || rest[0] != '"' && rest[0] != '\'' {
			return "", false
		}
		end := strings.IndexByte(rest[1:], rest[0])
		if end < 0 {
			return "", false
		}
		return rest[1 : 1+end], true
	}
	return "", false
}

// filePath spells a slash-separated repository path as the index's file
// IDs are spelled (the OS's separator, as the file's own ID shows it).
func (e *extractor) filePath(p string) string {
	f := string(e.file)
	sep := string(filepath.Separator)
	switch {
	case strings.Contains(f, "/"):
		sep = "/"
	case strings.Contains(f, "\\"):
		sep = "\\"
	}
	return strings.ReplaceAll(p, "/", sep)
}

// isName reports whether an identity is a one-line name (declname) but
// for the file's own path it starts with: the path is the index's, not
// the file's text, and a long one leaves the file's declarations
// declared (the document itself is "<FileID>").
func (e *extractor) isName(q string) bool {
	if q == string(e.file) {
		return true
	}
	if rest, ok := strings.CutPrefix(q, string(e.file)); ok {
		return declname.IsName(rest)
	}
	return declname.IsName(q)
}

// oneLine reports whether s, of any length, is one line of names
// (declname, piece by piece): an identity of another file's path.
func oneLine(s string) bool {
	for len(s) > declname.MaxBytes {
		i := declname.MaxBytes
		for i > 0 && !utf8.RuneStart(s[i]) {
			i--
		}
		if !declname.IsName(s[:i]) {
			return false
		}
		s = s[i:]
	}
	return s == "" || declname.IsName(s)
}

func (e *extractor) symbol(name, qualified string, kind symbol.SymbolKind, sp xmldoc.Span, parent, signature string) bool {
	if !e.isName(name) || !e.isName(qualified) {
		return false // no name: nothing declared
	}
	if !declname.IsName(signature) {
		signature = ""
	}
	if len(e.symbols) >= maxSymbols {
		e.truncated(sp)
		return false
	}
	e.qualified[qualified] = true
	e.symbols = append(e.symbols, language.SymbolDraft{
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Location:  e.loc(sp.Start, sp.End),
		StartByte: uint32(sp.Start),
		EndByte:   uint32(sp.End),
		Parent:    parent,
		Signature: signature,
		Exported:  true,
	})
	return true
}

func (e *extractor) ref(r language.ReferenceDraft, sp xmldoc.Span) {
	// A reference whose container is not declared (a name past a bound,
	// or no name) is left out with it.
	if !e.isName(r.Name) || r.NameQualified != "" && !oneLine(r.NameQualified) || !e.qualified[r.Container] {
		return
	}
	if len(e.refs) >= maxRefs {
		e.truncated(sp)
		return
	}
	r.Location = e.loc(sp.Start, sp.End)
	e.refs = append(e.refs, r)
}

func (e *extractor) truncated(sp xmldoc.Span) {
	if e.full {
		return
	}
	e.full = true
	e.diags = append(e.diags, language.Diagnostic{
		Severity: language.SeverityWarning,
		Code:     CodeTruncated,
		Message:  fmt.Sprintf("the file states more than %d declarations (%d elements) or %d references: the rest are left out", maxSymbols, maxElements, maxRefs),
		Location: e.loc(sp.Start, sp.Start),
	})
}

// maxDiagnostics bounds the diagnostics a file reports.
const maxDiagnostics = 1000

func (e *extractor) diagsFull() {
	if e.diagsCut {
		return
	}
	e.diagsCut = true
	e.diags = append(e.diags, language.Diagnostic{
		Severity: language.SeverityWarning,
		Code:     CodeTruncated,
		Message:  fmt.Sprintf("the file has more than %d problems: the rest are not reported", maxDiagnostics),
		Location: e.loc(0, 0),
	})
}

func (e *extractor) problem(p xmldoc.Problem) {
	if len(e.diags) >= maxDiagnostics {
		e.diagsFull()
		return
	}
	sev := language.SeverityWarning
	if p.Error {
		sev = language.SeverityError
	}
	e.diags = append(e.diags, language.Diagnostic{Severity: sev, Code: p.Code, Message: p.Message, Location: e.loc(p.Start, p.End)})
}

// lines maps byte offsets to 1-based line and byte column positions (lines
// end at '\n', as the other providers' positions do).
type lines []int

func newLines(src []byte) lines {
	l := lines{0}
	for i, c := range src {
		if c == '\n' {
			l = append(l, i+1)
		}
	}
	return l
}

func (l lines) pos(off int) source.Position {
	i := sort.Search(len(l), func(i int) bool { return l[i] > off }) - 1
	return source.Position{Line: uint32(i + 1), Column: uint32(off - l[i] + 1)}
}

func (e *extractor) loc(start, end int) source.Location {
	return source.Location{File: e.file, Range: source.Range{Start: e.lines.pos(start), End: e.lines.pos(end)}}
}
