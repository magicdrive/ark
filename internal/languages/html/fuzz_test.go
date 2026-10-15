package html

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// FuzzExtractHTML verifies the HTML provider on arbitrary input: it never
// panics, keeps every range inside the source and on a start tag, is
// deterministic, and, with the standard's tree as the oracle, states an id
// only as often as the tree gives it elements (no fake, no hidden
// duplicate) and caps every same-document reference of a document whose
// tree holds a script or an event handler.
func FuzzExtractHTML(f *testing.F) {
	seeds := []string{
		"",
		`<!doctype html><p id=a><label for=a>x</label><a href="#a">a</a>`,
		`<svg><style><a title="</style><b id=fake>">x</a></style></svg>`,
		`<svg><![CDATA[ a > <a id="fake"> ]]></svg><p id=z>`,
		`<select><xmp><a title="</xmp><b id=fake>">`,
		`<template><i id=a></i><template shadowrootmode=open><b id=a></b></template></template>`,
		`<body id=a><body id=b><html id=c><html id=d>`,
		`<p><b id=x><b id=x><b id=x><b id=x><p>y`,
		"<math \nid='v1'><template>'<script><a t=\"</script>\"<rect \nid=v2>",
		`<a href="docs/b.html#x">b</a><iframe src="../c.htm"></iframe><base href=/>`,
		`<p id="a&amp;b" ID=c aria-labelledby="a&amp;b c" class="x` + "\r\n" + `y">`,
		`<a =b=c ==d = e id=q>x</a><p id=" data-ark-tok-0=1 ">`,
		"<p id=\"a\x00b\"><div id=\"cut",
		`<table><tr><td id=c headers="h"><th id=h></table><frameset><input id=f type=hidden>`,
		`<svg><style><b id=x>h</b><script>x()</script></style></svg><p id=x>v</p><label for=x>L</label>`,
		`<div><template shadowrootmode=open><i id=s></i><a href="#s">x</a><label for=s>y</label></template></div>`,
		`<input [id]="d" id=q><label for=q>L</label><a href="java&#9;script:x()">j</a>`,
		`<b><div><template shadowrootmode=open></template></b></b><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></div>`,
		`<select><div id=x></div></select><p id=x></p><label for=x>y</label>`,
		`<p id=a></p><svg><foreignObject><template></template></foreignObject></svg><p id=b></p><label for=b>L</label>`,
		"<p id=\"a\x00b\"></p><p id=\"a\uFFFDb\"></p><label for=\"a\uFFFDb\">L</label>",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		ext, err := p.Extract(context.Background(), source.FileID("d/f.html"), src)
		if err != nil {
			t.Fatal(err)
		}
		again, _ := p.Extract(context.Background(), source.FileID("d/f.html"), src)
		if !reflect.DeepEqual(ext, again) {
			t.Fatal("two extractions differ")
		}
		n := uint32(len(src))
		ids := map[string]int{}
		for _, s := range ext.Symbols {
			if s.Name == "" || s.Qualified == "" || s.StartByte > s.EndByte || s.EndByte > n || s.Location.Range.Start.Line == 0 {
				t.Fatalf("bad symbol %+v (len %d)", s, n)
			}
			if s.Kind == symbol.KindElement {
				if src[s.StartByte] != '<' {
					t.Fatalf("element %q does not start a tag", s.Qualified)
				}
				ids[s.Name]++
			}
		}
		for _, r := range ext.References {
			if r.Name == "" || r.NameQualified == "" || !r.IdentityInRepository {
				t.Fatalf("bad reference %+v", r)
			}
		}
		doc, err := xhtml.Parse(bytes.NewReader(src))
		if err != nil {
			return
		}
		inTree := map[string]int{}
		scripted := false
		var walk func(n *xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode {
				scripted = scripted || strings.EqualFold(n.Data, "script")
				for _, a := range n.Attr {
					if a.Namespace == "" && a.Key == "id" {
						inTree[strings.ReplaceAll(a.Val, "\x00", "\uFFFD")]++
						break
					}
				}
				for _, a := range n.Attr {
					k := strings.ToLower(a.Key)
					v := strings.ToLower(strings.Map(func(r rune) rune {
						if r == '\t' || r == '\n' || r == '\r' {
							return -1
						}
						return r
					}, strings.TrimLeft(a.Val, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\v\f\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f ")))
					if strings.HasPrefix(k, "on") && len(k) > 2 || strings.HasPrefix(v, "javascript:") || k == "srcdoc" ||
						k != "" && strings.ContainsRune("[(*@", rune(k[0])) {
						scripted = true
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
		for id, k := range ids {
			if inTree[id] != k {
				t.Fatalf("%d symbols %q, the tree has %d elements with the id\n%s", k, id, inTree[id], strings.ToValidUTF8(string(src), "?"))
			}
		}
		if scripted {
			for _, r := range ext.References {
				if strings.HasPrefix(r.NameQualified, "d/f.html#") && r.ConfidenceCap == "" {
					t.Fatalf("an uncapped reference %q in a document with a script\n%s", r.NameQualified, strings.ToValidUTF8(string(src), "?"))
				}
			}
		}
	})
}
