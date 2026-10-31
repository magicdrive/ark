package xml

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/languages/xml/xmldoc"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func extract(t testing.TB, file, src string) language.Extraction {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

// symbolLines renders the symbols as "kind qualified".
func symbolLines(ex language.Extraction, kinds ...symbol.SymbolKind) []string {
	var out []string
	for _, s := range ex.Symbols {
		if len(kinds) > 0 && !containsKind(kinds, s.Kind) {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s", s.Kind, s.Qualified))
	}
	return out
}

func containsKind(kinds []symbol.SymbolKind, k symbol.SymbolKind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

func refLines(ex language.Extraction, kinds ...string) []string {
	var out []string
	for _, r := range ex.References {
		if len(kinds) > 0 && !containsString(kinds, r.Kind) {
			continue
		}
		l := fmt.Sprintf("%s %s -> %s", r.Container, r.Kind, r.NameQualified)
		if r.FileIdentity {
			l += " file"
		}
		if r.IdentityInRepository {
			l += " in"
		}
		if r.ConfidenceCap != "" {
			l += " cap=" + r.ConfidenceCap
		}
		if r.Dynamic {
			l += " dynamic(" + r.Name + ")"
		}
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func diagCodes(ex language.Extraction) []string {
	var out []string
	for _, d := range ex.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

func findSymbol(ex language.Extraction, qualified string) (language.SymbolDraft, bool) {
	for _, s := range ex.Symbols {
		if s.Qualified == qualified {
			return s, true
		}
	}
	return language.SymbolDraft{}, false
}

// TestElementIdentities: an element is its path from the root; a second
// sibling of a name is "[2]"; the document, the elements and the
// attributes are declared; every file is IdentityOnly.
func TestElementIdentities(t *testing.T) {
	ex := extract(t, "a.xml", `<r id="x"><a/><b k="v"/><a><c/></a></r>`)
	if !ex.IdentityOnly {
		t.Error("an XML file is IdentityOnly")
	}
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document a.xml",
		"anchor a.xml#x",
		"element a.xml#/r",
		"element a.xml#/r/a",
		"element a.xml#/r/b",
		"element a.xml#/r/a[2]",
		"element a.xml#/r/a[2]/c",
		"attribute a.xml#/r/@id",
		"attribute a.xml#/r/b/@k",
	})
	s, _ := findSymbol(ex, "a.xml#/r/a[2]/c")
	if s.Name != "/r/a[2]/c" || s.Parent != "a.xml#/r/a[2]" || s.Kind != symbol.KindElement {
		t.Errorf("element c: %+v", s)
	}
	equalLines(t, "contains", refLines(ex, "contains"), []string{
		"a.xml contains -> a.xml#/r in",
		"a.xml#/r contains -> a.xml#/r/@id in",
		"a.xml#/r contains -> a.xml#/r/a in",
		"a.xml#/r contains -> a.xml#/r/a[2] in",
		"a.xml#/r contains -> a.xml#/r/b in",
		"a.xml#/r/a[2] contains -> a.xml#/r/a[2]/c in",
		"a.xml#/r/b contains -> a.xml#/r/b/@k in",
	})
}

// TestIdentityStability: inserting an element of another name, an
// attribute, a comment or text keeps every identity; inserting one of the
// same name before others renumbers those after it (the documented limit).
func TestIdentityStability(t *testing.T) {
	base := symbolLines(extract(t, "a.xml", `<r><a/><b><c/></b><a/></r>`), symbol.KindElement)
	other := symbolLines(extract(t, "a.xml", `<r><x/><!-- c --><a/>text<b q="1"><c/></b><a/></r>`), symbol.KindElement)
	var kept []string
	for _, l := range other {
		if !strings.Contains(l, "/x") {
			kept = append(kept, l)
		}
	}
	sort.Strings(base)
	sort.Strings(kept)
	equalLines(t, "kept identities", kept, base)
	same := symbolLines(extract(t, "a.xml", `<r><b/><a/><b><c/></b><a/></r>`), symbol.KindElement)
	if !containsString(same, "element a.xml#/r/b[2]/c") {
		t.Errorf("a b inserted before b renumbers it: %v", same)
	}
}

// TestNamespaces: elements are the same by namespace URI and local name,
// whatever prefix is written; a namespace is labelled by the first prefix
// bound to it; a default-only namespace is unlabelled; two namespaces that
// would share a label are told apart by "~n"; an unprefixed attribute is
// in no namespace; namespace declarations are namespaces with their URI.
func TestNamespaces(t *testing.T) {
	ex := extract(t, "n.xml", `<root xmlns="urn:d" xmlns:p="urn:p">
  <p:item/>
  <q:item xmlns:q="urn:p"/>
  <item/>
  <item xmlns="urn:p"/>
  <x:e xmlns:x="urn:x1"><x:e xmlns:x="urn:x2" x:a="1" a="2"/></x:e>
  <plain xmlns=""/>
</root>`)
	equalLines(t, "elements", symbolLines(ex, symbol.KindElement), []string{
		"element n.xml#/root",
		"element n.xml#/root/p:item",
		"element n.xml#/root/p:item[2]",
		"element n.xml#/root/item",
		"element n.xml#/root/p:item[3]",
		"element n.xml#/root/x:e",
		"element n.xml#/root/~2:plain",
		"element n.xml#/root/x:e/x~2:e",
	})
	equalLines(t, "attributes", symbolLines(ex, symbol.KindAttribute, symbol.KindNamespace), []string{
		"namespace n.xml#/root/@xmlns",
		"namespace n.xml#/root/@xmlns:p",
		"namespace n.xml#/root/p:item[2]/@xmlns:q",
		"namespace n.xml#/root/p:item[3]/@xmlns",
		"namespace n.xml#/root/x:e/@xmlns:x",
		"namespace n.xml#/root/~2:plain/@xmlns",
		"namespace n.xml#/root/x:e/x~2:e/@xmlns:x",
		"attribute n.xml#/root/x:e/x~2:e/@x~2:a",
		"attribute n.xml#/root/x:e/x~2:e/@a",
	})
	if s, _ := findSymbol(ex, "n.xml#/root/@xmlns:p"); s.Signature != "urn:p" {
		t.Errorf("a namespace declaration's signature is its URI: %+v", s)
	}
	if len(ex.Diagnostics) != 0 {
		t.Errorf("diagnostics: %v", ex.Diagnostics)
	}
}

// TestPositions: locations are 1-based lines and byte columns of the
// element's and attribute's own bytes, with multi-byte text and CRLF line
// breaks; a symbol's bytes are its location's.
func TestPositions(t *testing.T) {
	src := "<?xml version=\"1.0\"?>\r\n<résumé nom=\"é\">\r\n  <ö  a = 'x'/>\r\n</résumé>\r\n"
	ex := extract(t, "p.xml", src)
	want := map[string][4]uint32{
		"p.xml#/résumé":      {2, 1, 4, 12},
		"p.xml#/résumé/@nom": {2, 11, 2, 19},
		"p.xml#/résumé/ö":    {3, 3, 3, 17},
		"p.xml#/résumé/ö/@a": {3, 8, 3, 15},
	}
	for q, w := range want {
		s, ok := findSymbol(ex, q)
		if !ok {
			t.Errorf("%s not declared: %v", q, symbolLines(ex))
			continue
		}
		r := s.Location.Range
		if got := [4]uint32{r.Start.Line, r.Start.Column, r.End.Line, r.End.Column}; got != w {
			t.Errorf("%s at %v, want %v", q, got, w)
		}
		if text := src[s.StartByte:s.EndByte]; q == "p.xml#/résumé/ö/@a" && text != "a = 'x'" {
			t.Errorf("%s bytes %q", q, text)
		}
	}
}

// TestDiagnostics: what is not well-formed is reported — never a panic —
// and what was read before it is kept.
func TestDiagnostics(t *testing.T) {
	for _, c := range []struct {
		name, src string
		codes     []string
		elements  int
	}{
		{"mismatched", "<a><b></a>", []string{xmldoc.CodeMismatchedTag}, 2},
		{"stray end", "<a></b></a>", []string{xmldoc.CodeMismatchedTag}, 1},
		{"unclosed", "<a><b>", []string{xmldoc.CodeUnclosed}, 2},
		{"undefined prefix", "<p:a/>", []string{xmldoc.CodeUndefinedPrefix}, 1},
		{"duplicate attribute", `<a x="1" x="2"/>`, []string{xmldoc.CodeDuplicateAttr}, 1},
		{"duplicate by namespace", `<a xmlns:p="u" xmlns:q="u" p:x="1" q:x="2"/>`, []string{xmldoc.CodeDuplicateAttr}, 1},
		{"two roots", "<a/><b/>", []string{xmldoc.CodeMultipleRoots}, 2},
		{"text outside", "<a/>x", []string{xmldoc.CodeMultipleRoots}, 1},
		{"syntax", "<a><b x=1/></a>", []string{xmldoc.CodeSyntax}, 1},
		{"undeclared entity", "<a>&nope;</a>", []string{xmldoc.CodeSyntax}, 1},
		{"control character", "<a>\x01</a>", []string{xmldoc.CodeSyntax}, 1},
		{"invalid UTF-8", "<a>\xff</a>", []string{xmldoc.CodeSyntax}, 1},
		{"latin-1", "<?xml version='1.0' encoding='ISO-8859-1'?><a>\xe9</a>", []string{xmldoc.CodeEncoding}, 0},
		{"utf-16", "\xff\xfe<\x00a\x00/\x00>\x00", []string{xmldoc.CodeEncoding}, 0},
		{"empty", "", nil, 0},
		{"BOM", "\xef\xbb\xbf<a/>", nil, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			ex := extract(t, "d.xml", c.src)
			if got := diagCodes(ex); !reflect.DeepEqual(got, c.codes) && !(len(got) == 0 && len(c.codes) == 0) {
				t.Errorf("diagnostics %v, want %v (%v)", got, c.codes, ex.Diagnostics)
			}
			if n := len(symbolLines(ex, symbol.KindElement)); n != c.elements {
				t.Errorf("%d elements, want %d: %v", n, c.elements, symbolLines(ex))
			}
			for _, s := range ex.Symbols {
				if int(s.EndByte) > len(c.src) || s.StartByte > s.EndByte {
					t.Errorf("%s at [%d,%d)", s.Qualified, s.StartByte, s.EndByte)
				}
			}
		})
	}
}

// TestTextIsNoDeclaration: text, CDATA, comments and processing
// instructions declare nothing, and no text reaches a name or signature.
func TestTextIsNoDeclaration(t *testing.T) {
	ex := extract(t, "t.xml", "<a>secret-token<![CDATA[<b>cdata</b>]]><!-- <c/> --><?pi <d/>?><e>password=hunter2</e></a>")
	equalLines(t, "symbols", symbolLines(ex), []string{"document t.xml", "element t.xml#/a", "element t.xml#/a/e"})
	for _, s := range ex.Symbols {
		for _, text := range []string{"secret", "cdata", "hunter2"} {
			if strings.Contains(s.Name+s.Qualified+s.Signature, text) {
				t.Errorf("%s carries text %q", s.Qualified, text)
			}
		}
	}
}

// TestNamesAreOneLine: no name, identity or signature is anything but a
// one-line name; an element whose path is none is not declared, nor what
// it holds, and no reference is left without its container.
func TestNamesAreOneLine(t *testing.T) {
	long := strings.Repeat("n", 300)
	ex := extract(t, "l.xml", `<r xmlns:p="line1&#10;line2" a="`+strings.Repeat("v", 1000)+`"><`+long+`><x/></`+long+`><b xml:id="`+long+`"/></r>`)
	qualified := map[string]bool{}
	for _, s := range ex.Symbols {
		qualified[s.Qualified] = true
		if !declname.IsName(s.Name) || !declname.IsName(s.Qualified) || s.Signature != "" && !declname.IsName(s.Signature) {
			t.Errorf("not a name: %q %q %q", s.Name, s.Qualified, s.Signature)
		}
		if strings.Contains(s.Qualified, long) {
			t.Errorf("declared %s", s.Qualified)
		}
	}
	// A POM whose project name is too long for a name declares no
	// project, and none of the project's references is left without it.
	file := strings.Repeat("d/", 150) + "pom.xml"
	pom := extract(t, file, `<project><groupId>`+strings.Repeat("g", 100)+`</groupId><artifactId>`+strings.Repeat("a", 199)+`</artifactId><modules><module>m</module></modules>
<dependencies><dependency><groupId>g</groupId><artifactId>b</artifactId></dependency></dependencies></project>`)
	declared := map[string]bool{}
	for _, s := range pom.Symbols {
		declared[s.Qualified] = true
		if s.Kind == symbol.KindPackage {
			t.Errorf("declared %s", s.Qualified)
		}
	}
	for _, r := range pom.References {
		if !declared[r.Container] {
			t.Errorf("reference %s in undeclared %s", r.Name, r.Container)
		}
	}
	// A long path is the index's, not the file's text: its document and
	// elements are declared.
	if _, ok := findSymbol(pom, file+"#/project/modules/module"); !ok || !declared[file] {
		t.Errorf("a file of a long path declares nothing: %v", symbolLines(pom))
	}
	if got, want := len(refLines(pom, "contains")), len(symbolLines(pom, symbol.KindElement)); got != want {
		t.Errorf("%d contains references for %d elements", got, want)
	}
	if s, _ := findSymbol(ex, "l.xml#/r/@xmlns:p"); s.Signature != "" {
		t.Errorf("a multi-line URI is no signature: %q", s.Signature)
	}
	for _, r := range ex.References {
		if !qualified[r.Container] {
			t.Errorf("reference %s in undeclared %s", r.Name, r.Container)
		}
	}
}

// TestBounds: past maxElements and maxSymbols the rest is left out with
// xml_truncated, shallow elements first; the reading bounds are
// diagnostics.
func TestBounds(t *testing.T) {
	var b strings.Builder
	b.WriteString("<r>")
	for i := 0; i < maxElements+10; i++ {
		b.WriteString(`<e a="1"/>`)
	}
	b.WriteString("<deep><x/></deep></r>")
	ex := extract(t, "b.xml", b.String())
	if !containsString(diagCodes(ex), CodeTruncated) {
		t.Errorf("no %s: %v", CodeTruncated, diagCodes(ex))
	}
	if n := len(symbolLines(ex, symbol.KindElement)); n != maxElements {
		t.Errorf("%d elements, want %d", n, maxElements)
	}
	if len(ex.Symbols) > maxSymbols {
		t.Errorf("%d symbols", len(ex.Symbols))
	}

	// Shallow elements first: every child of the root before any
	// grandchild.
	b.Reset()
	b.WriteString("<r>")
	for i := 0; i < maxElements; i++ {
		b.WriteString("<c><g/></c>")
	}
	b.WriteString("</r>")
	ex = extract(t, "w.xml", b.String())
	for _, s := range ex.Symbols {
		if strings.HasSuffix(s.Qualified, "/g") {
			t.Fatalf("a grandchild before the children: %s", s.Qualified)
		}
	}
	if _, ok := findSymbol(ex, "w.xml#/r/c["+strconv.Itoa(maxElements-1)+"]"); !ok {
		t.Errorf("the children are not all declared")
	}

	deep := strings.Repeat("<a>", xmldoc.MaxDepth+5) + strings.Repeat("</a>", xmldoc.MaxDepth+5)
	ex = extract(t, "deep.xml", deep)
	if !containsString(diagCodes(ex), xmldoc.CodeTooDeep) {
		t.Errorf("no %s: %v", xmldoc.CodeTooDeep, diagCodes(ex))
	}
	big := "<a>" + strings.Repeat(" ", xmldoc.MaxBytes) + "</a>"
	ex = extract(t, "big.xml", big)
	if got := diagCodes(ex); !reflect.DeepEqual(got, []string{xmldoc.CodeTooLarge}) || len(ex.Symbols) != 1 {
		t.Errorf("a file over the size bound: %v %v", got, symbolLines(ex))
	}
	var many strings.Builder
	many.WriteString("<r>")
	for i := 0; i < xmldoc.MaxNodes; i++ {
		many.WriteString("<e/>")
	}
	many.WriteString("</r>")
	ex = extract(t, "many.xml", many.String())
	if !containsString(diagCodes(ex), xmldoc.CodeTooManyNodes) {
		t.Errorf("no %s: %v", xmldoc.CodeTooManyNodes, diagCodes(ex))
	}
}

// TestDeterministic: two extractions are the same.
func TestDeterministic(t *testing.T) {
	src := `<r xmlns:a="u1" xmlns:b="u2"><a:x b:y="1" z="2" id="i"/><b:x/><link href="#i"/></r>`
	first := extract(t, "d.xml", src)
	for i := 0; i < 20; i++ {
		if again := extract(t, "d.xml", src); !reflect.DeepEqual(first, again) {
			t.Fatal("two extractions differ")
		}
	}
}

// TestIDs: xml:id is an anchor; an id in no namespace is one at most a
// Candidate target; a fragment href references the ID (xlink Exact, a
// plain href Strong at most); an ID that is no NCName is no anchor.
func TestIDs(t *testing.T) {
	ex := extract(t, "i.xml", `<r xmlns:xlink="http://www.w3.org/1999/xlink"><a xml:id="x"/><b id="y"/><c id="has space"/><d id="/r"/>
<l xlink:href="#x"/><m href="#y"/><n href="other.xml#x"/><o href="#"/></r>`)
	equalLines(t, "anchors", symbolLines(ex, symbol.KindAnchor), []string{"anchor i.xml#x", "anchor i.xml#y"})
	if s, _ := findSymbol(ex, "i.xml#y"); s.ConfidenceCap != "candidate" || s.Signature != "id" {
		t.Errorf("a plain id: %+v", s)
	}
	if s, _ := findSymbol(ex, "i.xml#x"); s.ConfidenceCap != "" || s.Signature != "xml:id" {
		t.Errorf("an xml:id: %+v", s)
	}
	equalLines(t, "references", refLines(ex, "id_reference"), []string{
		"i.xml#/r/l/@xlink:href id_reference -> i.xml#x in",
		"i.xml#/r/m/@href id_reference -> i.xml#y in cap=strong",
	})
}

// TestLinks: XInclude and xml-stylesheet hrefs that are plain relative
// paths are path evidence (in the repository or, leaving it, outside);
// a URL, an absolute path, a query, an escape or an xml:base is none.
func TestLinks(t *testing.T) {
	ex := extract(t, "d/a.xml", `<?xml-stylesheet type="text/xsl" href="s.xsl"?>
<r xmlns:xi="http://www.w3.org/2001/XInclude">
  <xi:include href="b.xml"/>
  <xi:include href="../../up.xml"/>
  <xi:include href="https://example.com/x.xml"/>
  <xi:include href="/etc/passwd"/>
  <xi:include href="c.xml?x"/>
  <xi:include href="%2e%2e/x.xml"/>
  <s xml:base="other/"><xi:include href="e.xml"/></s>
</r>`)
	equalLines(t, "links", refLines(ex, "resource_link"), []string{
		"d/a.xml resource_link -> d/s.xsl file in",
		"d/a.xml#/r/xi:include/@href resource_link -> d/b.xml file in",
		"d/a.xml#/r/xi:include[2]/@href resource_link -> ../up.xml file",
	})
}

// TestSecurity: hostile documents are read safely — no entity is
// expanded, no file or URL is read, nothing is fetched, the bounds hold —
// and reported.
func TestSecurity(t *testing.T) {
	billion := `<?xml version="1.0"?>
<!DOCTYPE lolz [
 <!ENTITY lol "lol">
 <!ENTITY lol1 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">
 <!ENTITY lol2 "&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;">
 <!ENTITY lol9 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">
]>
<lolz a="&lol9;">&lol9;<x>&lol9;</x></lolz>`
	xxe := `<?xml version="1.0"?>
<!DOCTYPE r [<!ENTITY xxe SYSTEM "file:///etc/passwd"><!ENTITY % p SYSTEM "http://127.0.0.1:1/evil.dtd"> %p;]>
<r><groupId>&xxe;</groupId></r>`
	externalDTD := `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">
<html xmlns="http://www.w3.org/1999/xhtml"><body>&nbsp;</body></html>`
	xxePOM := `<!DOCTYPE project [<!ENTITY g SYSTEM "file:///etc/hostname">]>
<project xmlns="http://maven.apache.org/POM/4.0.0"><groupId>&g;</groupId><artifactId>a</artifactId>
<dependencies><dependency><groupId>&g;</groupId><artifactId>b</artifactId></dependency></dependencies></project>`
	for _, c := range []struct {
		name, src string
		codes     []string
	}{
		{"billion laughs", billion, []string{xmldoc.CodeEntityNotExpanded}},
		{"external entity", xxe, []string{xmldoc.CodeExternalEntity, xmldoc.CodeEntityNotExpanded}},
		{"external DTD", externalDTD, []string{xmldoc.CodeExternalEntity, xmldoc.CodeEntityNotExpanded}},
		{"external entity in a POM", xxePOM, []string{xmldoc.CodeExternalEntity, xmldoc.CodeEntityNotExpanded}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := xmldoc.Parse([]byte(c.src))
			var codes []string
			for _, p := range d.Problems {
				codes = append(codes, p.Code)
			}
			if !reflect.DeepEqual(codes, c.codes) {
				t.Errorf("problems %v, want %v", codes, c.codes)
			}
			total := 0
			d.Walk(func(n *xmldoc.Node) bool {
				total += len(n.Text)
				for _, a := range n.Attrs {
					total += len(a.Value)
					if strings.Contains(a.Value, "lollol") {
						t.Errorf("attribute expanded: %q", a.Value)
					}
				}
				if strings.Contains(n.Text, "lollol") || strings.Contains(n.Text, "root") {
					t.Errorf("text expanded: %q", n.Text)
				}
				return true
			})
			if total > len(c.src) {
				t.Errorf("%d bytes of text from %d of source", total, len(c.src))
			}
			// What names carry is the source's own text: an entity
			// reference at most, never what it would expand to.
			ex := extract(t, "pom.xml", c.src)
			for _, s := range ex.Symbols[1:] { // after the document
				for _, part := range strings.FieldsFunc(s.Name, func(r rune) bool { return r == ':' || r == '/' || r == '#' || r == '@' || r == '[' || r == ']' }) {
					if !strings.Contains(c.src, part) {
						t.Errorf("a name carries text the source does not: %q", s.Name)
					}
				}
			}
		})
	}
	// An entity left as written is no coordinate: the POM's project has
	// none, and its dependency is a dynamic name, never one fetched.
	ex := extract(t, "pom.xml", xxePOM)
	for _, r := range ex.References {
		if r.Kind == "depends_on" && r.NameQualified == "" && !r.Dynamic {
			t.Errorf("%+v", r)
		}
	}
	if _, ok := findSymbol(ex, "pom.xml::project"); !ok {
		t.Errorf("project: %v", symbolLines(ex))
	}
}

// TestSecurity_Hostile: deep nesting, a huge attribute, many attributes,
// invalid UTF-8 and control characters end in diagnostics within linear
// time; nothing panics.
func TestSecurity_Hostile(t *testing.T) {
	for _, src := range []string{
		strings.Repeat("<a>", 100000),
		`<a b="` + strings.Repeat("x", 3<<20) + `"/>`,
		"<a " + strings.Repeat(`x="1" `, 1) + strings.Repeat("y", 10) + `="` + strings.Repeat("&amp;", 100000) + `"/>`,
		"<a>\x00</a>",
		"<a\xc0\x80/>",
		"<!DOCTYPE a [" + strings.Repeat("<!ENTITY e \"x\">", 50000) + "]><a>" + strings.Repeat("&e;", 50000) + "</a>",
		"<a>" + strings.Repeat("&e;", 1000) + "</a>",
	} {
		ex := extract(t, "h.xml", src)
		if len(ex.Symbols) == 0 {
			t.Errorf("no document symbol")
		}
	}
}

// TestPOM covers the Maven scenarios: the artifactId (the project's
// name), the dependencies, the parent and the modules.
func TestPOM(t *testing.T) {
	ex := extract(t, "svc/pom.xml", `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <parent><groupId>org.acme</groupId><artifactId>acme-parent</artifactId><version>2</version><relativePath>../build</relativePath></parent>
  <artifactId>svc</artifactId>
  <packaging>jar</packaging>
  <properties><lib.group>org.lib</lib.group></properties>
  <modules><module>api</module><module>impl/custom.xml</module><module>${m}</module><module>../../outside</module></modules>
  <subprojects><subproject>next</subproject></subprojects>
  <dependencies>
    <dependency><groupId>${lib.group}</groupId><artifactId>lib</artifactId><version>${project.version}</version><scope>test</scope></dependency>
    <dependency><groupId>org.lib</groupId><artifactId>lib</artifactId><classifier>tests</classifier></dependency>
    <dependency><groupId>org.lib</groupId><artifactId>lib</artifactId><classifier>tests</classifier></dependency>
    <dependency><groupId>only-group</groupId></dependency>
  </dependencies>
  <build><plugins><plugin><artifactId>maven-surefire-plugin</artifactId></plugin></plugins>
    <extensions><extension><groupId>kr.motd.maven</groupId><artifactId>os-maven-plugin</artifactId></extension></extensions></build>
</project>`)
	equalLines(t, "maven symbols", symbolLines(ex, symbol.KindPackage, symbol.KindDependency), []string{
		"package svc/pom.xml::org.acme:svc",
		"dependency svc/pom.xml::dependencies::org.lib:lib",
		"dependency svc/pom.xml::dependencies::org.lib:lib#2",
		"dependency svc/pom.xml::dependencies::org.lib:lib#3",
		"dependency svc/pom.xml::plugins::org.apache.maven.plugins:maven-surefire-plugin",
		"dependency svc/pom.xml::extensions::kr.motd.maven:os-maven-plugin",
	})
	p, _ := findSymbol(ex, "svc/pom.xml::org.acme:svc")
	if p.Name != "org.acme:svc" || p.Signature != "project org.acme:svc:2 (jar)" {
		t.Errorf("project: %+v", p)
	}
	if d, _ := findSymbol(ex, "svc/pom.xml::dependencies::org.lib:lib"); d.Signature != "dependency org.lib:lib:2 test" {
		t.Errorf("dependency signature %q", d.Signature)
	}
	equalLines(t, "references", refLines(ex, "inheritance", "resource_link", "depends_on"), []string{
		"svc/pom.xml::dependencies::org.lib:lib depends_on -> maven:org.lib:lib cap=strong",
		"svc/pom.xml::dependencies::org.lib:lib#2 depends_on -> maven:org.lib:lib cap=strong",
		"svc/pom.xml::dependencies::org.lib:lib#3 depends_on -> maven:org.lib:lib cap=strong",
		"svc/pom.xml::extensions::kr.motd.maven:os-maven-plugin depends_on -> maven:kr.motd.maven:os-maven-plugin cap=strong",
		"svc/pom.xml::org.acme:svc depends_on -> svc/pom.xml::dependencies::org.lib:lib in",
		"svc/pom.xml::org.acme:svc depends_on -> svc/pom.xml::dependencies::org.lib:lib#2 in",
		"svc/pom.xml::org.acme:svc depends_on -> svc/pom.xml::dependencies::org.lib:lib#3 in",
		"svc/pom.xml::org.acme:svc depends_on -> svc/pom.xml::extensions::kr.motd.maven:os-maven-plugin in",
		"svc/pom.xml::org.acme:svc depends_on -> svc/pom.xml::plugins::org.apache.maven.plugins:maven-surefire-plugin in",
		"svc/pom.xml::org.acme:svc inheritance -> build/pom.xml::org.acme:acme-parent cap=strong",
		"svc/pom.xml::org.acme:svc resource_link ->  file dynamic(${m})",
		"svc/pom.xml::org.acme:svc resource_link -> ../outside/pom.xml file",
		"svc/pom.xml::org.acme:svc resource_link -> svc/api/pom.xml file in",
		"svc/pom.xml::org.acme:svc resource_link -> svc/impl/custom.xml file in",
		"svc/pom.xml::org.acme:svc resource_link -> svc/next/pom.xml file in",
		"svc/pom.xml::plugins::org.apache.maven.plugins:maven-surefire-plugin depends_on -> maven:org.apache.maven.plugins:maven-surefire-plugin cap=strong",
	})
	var aliases []string
	for _, a := range ex.Aliases {
		aliases = append(aliases, fmt.Sprintf("%s -> %s %s %v", a.Qualified, a.Target, a.ConfidenceCap, a.Fallback))
	}
	equalLines(t, "aliases", aliases, []string{
		"maven:org.acme:svc -> svc/pom.xml::org.acme:svc strong false",
		"build/pom.xml::org.acme:acme-parent -> maven:org.acme:acme-parent strong true",
	})
	codes := diagCodes(ex)
	if !containsString(codes, "maven_duplicate_dependency") || !containsString(codes, "maven_missing_coordinate") {
		t.Errorf("diagnostics %v", codes)
	}
	// The scenario "the artifactId": the element and the project's name.
	if s, ok := findSymbol(ex, "svc/pom.xml#/project/artifactId"); !ok || s.Name != "/project/artifactId" {
		t.Errorf("artifactId element: %+v", s)
	}
}

// TestPOM_Parent: an empty relativePath and a path leaving the repository
// name the coordinates only; a project without a groupId inherits its
// parent's.
func TestPOM_Parent(t *testing.T) {
	for _, c := range []struct{ file, rel, want string }{
		{"pom.xml", "<relativePath/>", "pom.xml::g:child inheritance -> maven:g:p cap=strong"},
		{"pom.xml", "", "pom.xml::g:child inheritance -> maven:g:p cap=strong"}, // ../pom.xml leaves the repository
		{"a/pom.xml", "", "a/pom.xml::g:child inheritance -> pom.xml::g:p cap=strong"},
		{"a/b/pom.xml", "<relativePath>../../parent/pom.xml</relativePath>", "a/b/pom.xml::g:child inheritance -> parent/pom.xml::g:p cap=strong"},
	} {
		ex := extract(t, c.file, `<project><parent><groupId>g</groupId><artifactId>p</artifactId>`+c.rel+`</parent><artifactId>child</artifactId></project>`)
		equalLines(t, c.file+" "+c.rel, refLines(ex, "inheritance"), []string{c.want})
	}
}

// TestPOM_Recognition: a POM is a project in the POM namespace, or in no
// namespace named pom.xml; any other XML file is generic only.
func TestPOM_Recognition(t *testing.T) {
	for _, c := range []struct {
		file, src string
		pom       bool
	}{
		{"x/pom.xml", "<project><artifactId>a</artifactId></project>", true},
		{"x/other.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0"><artifactId>a</artifactId></project>`, true},
		{"x/other.xml", `<project xmlns="http://maven.apache.org/POM/4.1.0"><artifactId>a</artifactId></project>`, true},
		{"x/other.xml", "<project><artifactId>a</artifactId></project>", false},
		{"x/pom.xml", `<project xmlns="urn:other"><artifactId>a</artifactId></project>`, false},
		{"x/pom.xml", "<build/>", false},
	} {
		ex := extract(t, c.file, c.src)
		if got := len(symbolLines(ex, symbol.KindPackage)) == 1; got != c.pom {
			t.Errorf("%s %s: POM %v", c.file, c.src, got)
		}
	}
}

// TestWindowsPaths: identities of paths use the separator of the index's
// file IDs.
func TestWindowsPaths(t *testing.T) {
	ex := extract(t, `a\pom.xml`, `<project><parent><groupId>g</groupId><artifactId>p</artifactId></parent><artifactId>c</artifactId><modules><module>m/n</module></modules></project>`)
	equalLines(t, "references", refLines(ex, "inheritance", "resource_link"), []string{
		`a\pom.xml::g:c inheritance -> pom.xml::g:p cap=strong`,
		`a\pom.xml::g:c resource_link -> a\m\n\pom.xml file in`,
	})
}
