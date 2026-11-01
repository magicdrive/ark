package xmldoc

import (
	"strings"
	"testing"
)

func codes(d *Document) []string {
	var out []string
	for _, p := range d.Problems {
		out = append(out, p.Code)
	}
	return out
}

// TestSpans: an element spans its start tag to its end tag (a self-closing
// one its tag), an attribute its `name="value"` — a '>' and an entity in
// the value included — and the value its text between the quotes.
func TestSpans(t *testing.T) {
	src := `<a x = "1>&amp;2" y='q'><b/>text<c>t</c></a>`
	d := Parse([]byte(src))
	a := d.Root
	if got := src[a.Span.Start:a.Span.End]; got != src {
		t.Errorf("a: %q", got)
	}
	if got := src[a.Attrs[0].Span.Start:a.Attrs[0].Span.End]; got != `x = "1>&amp;2"` {
		t.Errorf("x: %q", got)
	}
	if got := src[a.Attrs[0].ValueSpan.Start:a.Attrs[0].ValueSpan.End]; got != `1>&amp;2` || a.Attrs[0].Value != "1>&2" {
		t.Errorf("x's value: %q %q", got, a.Attrs[0].Value)
	}
	if got := src[a.Attrs[1].Span.Start:a.Attrs[1].Span.End]; got != `y='q'` {
		t.Errorf("y: %q", got)
	}
	b, c := a.Children[0], a.Children[1]
	if got := src[b.Span.Start:b.Span.End]; got != "<b/>" {
		t.Errorf("b: %q", got)
	}
	if got := src[c.Span.Start:c.Span.End]; got != "<c>t</c>" || c.Text != "t" || src[c.TextSpan.Start:c.TextSpan.End] != "t" {
		t.Errorf("c: %q %q", got, c.Text)
	}
	if a.Text != "text" {
		t.Errorf("a's own text %q", a.Text)
	}
}

// TestRecovery: a mismatched end tag closes the open elements up to its
// own; one that matches none is ignored; elements open at the end end
// there.
func TestRecovery(t *testing.T) {
	src := "<a><b><c></b><d/></a></x><e>"
	d := Parse([]byte(src))
	var names []string
	d.Walk(func(n *Node) bool {
		names = append(names, n.Name.Local+"="+src[n.Span.Start:n.Span.End])
		return true
	})
	want := []string{"a=<a><b><c></b><d/></a>", "b=<b><c></b>", "c=<c>", "d=<d/>", "e=<e>"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("got %v\nwant %v", names, want)
	}
	if got := strings.Join(codes(d), ","); got != "xml_mismatched_tag,xml_mismatched_tag,xml_multiple_roots,xml_unclosed" {
		t.Errorf("problems %s", got)
	}
}

// TestText: CDATA is text; text past MaxText is cut and says so; comments
// and processing instructions are none; the XML declaration is no
// processing instruction.
func TestText(t *testing.T) {
	d := Parse([]byte("<?xml version='1.0'?><?pi data?><a>x<![CDATA[<y>]]><!--c-->z</a>"))
	if d.Root.Text != "x<y>z" {
		t.Errorf("text %q", d.Root.Text)
	}
	if len(d.ProcInst) != 1 || d.ProcInst[0].Target != "pi" {
		t.Errorf("processing instructions %+v", d.ProcInst)
	}
	d = Parse([]byte("<a>" + strings.Repeat("x", MaxText+10) + "</a>"))
	if len(d.Root.Text) != MaxText || !d.Root.TextCut {
		t.Errorf("text of %d bytes, cut %v", len(d.Root.Text), d.Root.TextCut)
	}
}

// TestNamespaceResolution: prefixes bind by the innermost declaration;
// xml is predefined; the default namespace applies to elements, not
// attributes; an undeclared prefix is a problem and its own namespace.
func TestNamespaceResolution(t *testing.T) {
	d := Parse([]byte(`<a xmlns="u" xmlns:p="v"><p:b xmlns:p="w" p:x="1" y="2" xml:lang="en"/><q:c/></a>`))
	a := d.Root
	b, c := a.Children[0], a.Children[1]
	if a.Name.URI != "u" || b.Name.URI != "w" || b.Attrs[1].Name.URI != "w" || b.Attrs[2].Name.URI != "" || b.Attrs[3].Name.URI != NamespaceXML {
		t.Errorf("a %+v b %+v", a.Name, b)
	}
	if c.Name.URI != UndeclaredURI("q") || strings.Join(codes(d), ",") != CodeUndefinedPrefix {
		t.Errorf("c %+v, problems %v", c.Name, codes(d))
	}
	if len(d.Declarations) != 3 || d.Declarations[2].Prefix != "p" || d.Declarations[2].URI != "w" {
		t.Errorf("declarations %+v", d.Declarations)
	}
}

// TestEntities: without a DOCTYPE an undeclared entity is an error; with
// one, every entity reference is left as written — inside attributes too —
// and reported once; predefined and character references are replaced.
func TestEntities(t *testing.T) {
	d := Parse([]byte(`<!DOCTYPE a [<!ENTITY e "&f;&f;"><!ENTITY f "boom">]><a v="&e;&#65;">&e;&lt;&#x42;</a>`))
	if d.Root.Text != "&e;<B" || d.Root.Attrs[0].Value != "&e;A" {
		t.Errorf("text %q, attribute %q", d.Root.Text, d.Root.Attrs[0].Value)
	}
	if strings.Join(codes(d), ",") != CodeEntityNotExpanded || !d.Entities || !d.Doctype {
		t.Errorf("problems %v", codes(d))
	}
	d = Parse([]byte(`<a/><!DOCTYPE a>`))
	if strings.Join(codes(d), ",") != CodeSyntax {
		t.Errorf("a DOCTYPE after the root: %v", codes(d))
	}
}

// TestStopped: a syntax error and the bounds stop the reading; what was
// read is kept.
func TestStopped(t *testing.T) {
	d := Parse([]byte("<a><b></b><c x=></c></a>"))
	if !d.Stopped || len(d.Root.Children) != 1 {
		t.Errorf("stopped %v, children %d", d.Stopped, len(d.Root.Children))
	}
	if d.Root.Span.End > len(d.Src) {
		t.Errorf("span %v", d.Root.Span)
	}
}

// TestUndeclaredURIUnspellable: the pseudo-namespace of an undeclared
// prefix starts with U+0000, which no attribute value can hold (neither
// raw nor as &#0;), so no declaration binds a prefix to one; and a
// declared "?x" is not the pseudo-namespace of x.
func TestUndeclaredURIUnspellable(t *testing.T) {
	for _, src := range []string{"<r xmlns:p=\"\x00x\"/>", `<r xmlns:p="&#0;x"/>`, `<r xmlns:p="&#x0;x"/>`} {
		d := Parse([]byte(src))
		if len(d.Declarations) != 0 && d.Root != nil && !d.Stopped {
			t.Errorf("%q: read as a declaration %+v", src, d.Declarations)
		}
	}
	d := Parse([]byte(`<r xmlns:p="?x"><p:e/><x:e/></r>`))
	if len(d.Root.Children) != 2 {
		t.Fatalf("children: %+v", d.Root.Children)
	}
	p, x := d.Root.Children[0], d.Root.Children[1]
	if p.Name.URI != "?x" || x.Name.URI == p.Name.URI || x.Index != 1 {
		t.Errorf("p:e %+v, x:e %+v", p.Name, x.Name)
	}
	if prefix, ok := UndeclaredPrefix(x.Name.URI); !ok || prefix != "x" {
		t.Errorf("UndeclaredPrefix(%q) = %q, %v", x.Name.URI, prefix, ok)
	}
	if _, ok := UndeclaredPrefix(p.Name.URI); ok {
		t.Errorf("a declared URI read as undeclared")
	}
}

// TestRepeatedDoctype: every DOCTYPE after the first is a diagnostic and
// changes nothing of the reading (entity references stay as written).
func TestRepeatedDoctype(t *testing.T) {
	d := Parse([]byte(`<!DOCTYPE a><!DOCTYPE b><r><!DOCTYPE c>&e;</r>`))
	n := 0
	for _, p := range d.Problems {
		if p.Code == CodeSyntax {
			n++
		}
	}
	if n != 2 || !d.Doctype || !d.Entities || d.Root == nil || d.Root.Text != "&e;" || d.Stopped {
		t.Errorf("syntax problems %d, doc %+v, root %+v", n, d, d.Root)
	}
}
