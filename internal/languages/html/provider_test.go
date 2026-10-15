package html_test

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/html"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	xhtml "golang.org/x/net/html"
)

// The HTML provider states the document, its id elements, the id references
// of its IDREF attributes and fragments, and links to other documents of
// the repository; a reference resolves by identity only, Exact within a
// static document, never to an element a browser does not build.

func extract(t *testing.T, file, src string) language.Extraction {
	t.Helper()
	ex, err := html.NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

// symbols lists "Kind Qualified [start,end)" of a file's symbols.
func symbols(t *testing.T, file, src string) []string {
	t.Helper()
	var out []string
	for _, s := range extract(t, file, src).Symbols {
		out = append(out, fmt.Sprintf("%s %s [%d,%d)", s.Kind, s.Qualified, s.StartByte, s.EndByte))
	}
	return out
}

func TestSymbols(t *testing.T) {
	src := `<p id="a">x</p><svg><g id=icon></g></svg><template><i id="a"></i><template><b id=b></b></template></template>`
	got := symbols(t, "d/p.html", src)
	want := []string{
		fmt.Sprintf("document d/p.html [0,%d)", len(src)),
		"element d/p.html#a [0,10)",
		"element d/p.html#icon [20,31)",
		"element d/p.html#1 a [51,61)",
		"element d/p.html#1 2 b [75,83)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("symbols\n got %q\nwant %q", got, want)
	}
	ex := extract(t, "d/p.html", src)
	if !ex.IdentityOnly {
		t.Error("IdentityOnly = false")
	}
	if l := ex.Symbols[1].Location.Range; l.Start.Line != 1 || l.Start.Column != 1 || l.End.Column != 11 {
		t.Errorf("location of #a = %+v", l)
	}
}

// Ids a browser does not give the element are no symbols.
func TestSymbols_OnlyWhatTheTreeHas(t *testing.T) {
	cases := map[string]string{
		"fake in svg style":     `<svg><style><a title="</style><b id=fake>">x</a></style></svg>`,
		"fake in svg title":     `<svg><title><a title="</title><b id=fake>">x</a></title></svg>`,
		"fake in cdata":         `<svg><![CDATA[ a > <a id="fake"> ]]></svg>`,
		"hidden real and fake":  `<svg><style><b id="fake"></style></svg><p>-</p><svg><title><a title="</title><b id=fake>">x</a></title></svg>`,
		"ignored by frameset":   `<input id=fake type=hidden><frameset>`,
		"later duplicate":       `<p id=real id=fake>`,
		"duplicate up to case":  `<p id=real ID=fake>`,
		"merged into body":      `<body id=real><p>x</p><body id=fake>`,
		"same value merged":     `<body id=real><body id=real class=c>`,
		"marker on other tag":   "<math \nid='real'><template>'<script><a t=\"</script>\"<rect \nid=fake>",
		"nul":                   "<p id=\"a\x00fake\">",
		"whitespace":            `<p id="a fake">`,
		"template syntax":       `<p id="{{ fake }}">`,
		"empty":                 `<p id="">`,
		"cut off by eof":        `<p id=real></p><div id="fake`,
		"noah's ark unverified": `<p><b id=fake><b id=fake><b id=fake><b id=fake><p>x`,
	}
	// The b after an xmp in a select: no element in a tree built with the
	// select insertion mode (the xmp is ignored, the b is in an attribute
	// value), a real one in the standard's tree without it.
	selectXmp := `<select><xmp><a title="</xmp><b id=%s>">`
	if !selectKeepsContent(t) {
		cases["fake in select xmp"] = fmt.Sprintf(selectXmp, "fake")
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			for _, s := range symbols(t, "f.html", src) {
				if strings.Contains(s, "fake") {
					t.Errorf("symbol %s", s)
				}
			}
		})
	}
	// The real ones stay.
	reals := map[string]string{
		`<body id=real><p>x</p><body id=fake>`:                      "f.html#real",
		`<body id=real><body id=real class=c>`:                      "f.html#real",
		`<p id=real></p><div id="fake`:                              "f.html#real",
		`<p id=real id=fake>`:                                       "f.html#real",
		`<svg><foreignObject><b id=real></b></foreignObject></svg>`: "f.html#real",
	}
	if selectKeepsContent(t) {
		reals[fmt.Sprintf(selectXmp, "real")] = "f.html#real"
	}
	for src, want := range reals {
		n := 0
		for _, s := range symbols(t, "f.html", src) {
			if strings.Contains(s, " "+want+" ") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: %d symbols %s, want 1", src, n, want)
		}
	}
}

func TestDiagnostics(t *testing.T) {
	codes := func(src string) []string {
		var out []string
		for _, d := range extract(t, "f.html", src).Diagnostics {
			out = append(out, string(d.Severity)+" "+d.Code)
		}
		return out
	}
	if got := codes(`<p id=a></p><div id="b`); !reflect.DeepEqual(got, []string{"error " + html.DiagEOFInTag}) {
		t.Errorf("eof in tag: %v", got)
	}
	if got := codes(`<p><b><b><b><b><p>x`); !reflect.DeepEqual(got, []string{"warning " + html.DiagTreeUnverified}) {
		t.Errorf("noah's ark: %v", got)
	}
	for _, src := range []string{"", "<!doctype html><p id=a>x", "plain text", "\x00\xff<p>", `<a =b=c ==d = e>x</a>`, "<p class=\"a\r\nb\" id=x>"} {
		for _, d := range extract(t, "f.html", src).Diagnostics {
			if d.Severity == language.SeverityError || d.Code == html.DiagAttributeMismatch {
				t.Errorf("%q: %s %s", src, d.Severity, d.Code)
			}
		}
	}
}

func TestDeterministic(t *testing.T) {
	src := `<label for=a>x</label><input id=a aria-describedby="b c"><p id=b><p id=c><a href="o.html#a">`
	a, b := extract(t, "f.html", src), extract(t, "f.html", src)
	if !reflect.DeepEqual(a, b) {
		t.Error("two extractions differ")
	}
}

// resolution is how Ark resolved one reference: its confidence and unique
// Strong / Exact target ("" without one).
type resolution struct {
	conf    resolver.Confidence
	target  string
	outside bool
}

// resolveAll indexes files with the HTML provider and resolves every
// reference, by "file:line:name".
func resolveAll(t *testing.T, files map[string]string) map[string][]resolution {
	t.Helper()
	prov := html.NewProvider()
	var fis []resolver.FileIndex
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		fis = append(fis, index.NewFileIndex(prov, source.FileID(name), extract(t, name, files[name])))
	}
	byID := map[string]string{}
	for _, fi := range fis {
		for _, r := range fi.References {
			byID[string(r.ID)] = fmt.Sprintf("%s:%d:%s", fi.FileID, r.Location.Range.Start.Line, r.Name)
		}
	}
	out := map[string][]resolution{}
	for _, res := range resolver.NewInRoot(fis, "r").Resolve() {
		r := resolution{conf: res.Confidence, outside: res.OutsideRepository}
		if res.HasUniqueTarget() {
			r.target = res.Candidates[0].Qualified
		}
		out[byID[string(res.ReferenceID)]] = append(out[byID[string(res.ReferenceID)]], r)
	}
	return out
}

// want is the resolution one reference must have: conf ("exact", "strong",
// "candidate", "unresolved") and, for exact / strong, its target.
type want struct {
	key, conf, target string
}

func check(t *testing.T, got map[string][]resolution, wants []want, absent []string) {
	t.Helper()
	for _, w := range wants {
		rs := got[w.key]
		if len(rs) != 1 {
			t.Errorf("%s: %d references, want 1 (have %v)", w.key, len(rs), keysOf(got))
			continue
		}
		r := rs[0]
		if r.conf.String() != w.conf || r.target != w.target {
			t.Errorf("%s: %v %q, want %s %q", w.key, r.conf, r.target, w.conf, w.target)
		}
		if r.outside {
			t.Errorf("%s: outside the repository", w.key)
		}
	}
	for _, k := range absent {
		if len(got[k]) > 0 {
			t.Errorf("%s: %d references, want none", k, len(got[k]))
		}
	}
}

func keysOf(m map[string][]resolution) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestIDReferences_StaticDocument(t *testing.T) {
	files := map[string]string{"p.html": strings.Join([]string{
		`<label for=name>N</label><input id=name list=opts form=f><button popovertarget=pop>`, // 1
		`<datalist id=opts></datalist><form id=f></form><div id=pop popover></div>`,           // 2
		`<table><tr><th id=h1>a<th id=h2>b<tr><td headers="h1 h2">c</table>`,                  // 3
		`<p aria-labelledby="l1 l2" aria-describedby=l1 itemref=l2><span id=l1></span><span id=l2></span>`,
		`<output for="name l1">x</output><button commandfor=pop>b</button>`, // 5
		`<a href="#name">top</a><area href=" #l1 ">`,                        // 6
		`<svg><linearGradient id=g></linearGradient><use href="#g"/><use xlink:href="#g"/><rect fill="url(#g)" stroke="url('#g') red"/></svg>`,
		`<label for=missing>x</label><p aria-owns="dup">`, // 8
		`<i id=dup></i><b id=dup></b>`,                    // 9
	}, "\n")}
	got := resolveAll(t, files)
	exact := func(key, target string) want { return want{key, "exact", "p.html#" + target} }
	check(t, got, []want{
		exact("p.html:1:name", "name"),
		exact("p.html:1:opts", "opts"),
		exact("p.html:1:f", "f"),
		exact("p.html:1:pop", "pop"),
		exact("p.html:3:h1", "h1"),
		exact("p.html:3:h2", "h2"),
		exact("p.html:5:name", "name"),
		exact("p.html:5:pop", "pop"),
		exact("p.html:6:name", "name"),
		exact("p.html:6:l1", "l1"),
		{"p.html:8:missing", "unresolved", ""},
		{"p.html:8:dup", "candidate", ""},
	}, nil)
	for _, k := range []string{"p.html:4:l1", "p.html:4:l2", "p.html:5:l1", "p.html:7:g"} {
		for _, r := range got[k] {
			if r.conf.String() != "exact" {
				t.Errorf("%s: %v", k, r.conf)
			}
		}
	}
	if n := len(got["p.html:4:l1"]); n != 2 { // aria-labelledby and aria-describedby
		t.Errorf("p.html:4:l1: %d references, want 2", n)
	}
	if n := len(got["p.html:7:g"]); n != 4 {
		t.Errorf("p.html:7:g: %d references, want 4", n)
	}
}

func TestIDReferences_Negatives(t *testing.T) {
	files := map[string]string{
		"dyn.html":  "<p id=a><label for=a>x</label>\n<script>document.body.innerHTML=''</script>",
		"on.html":   "<p id=a onclick='x()'><label for=a>x</label>",
		"js.html":   "<p id=a><a href='javascript:void 0'>j</a><label for=a>x</label>",
		"tpl.html":  "<p id=a>{{ title }}<label for=a>x</label>",
		"base.html": "<base href=/other/><p id=a><a href='#a'>x</a><label for=a>y</label>",
		"skip.html": strings.Join([]string{
			`<p id=a><a href="#">x</a><a href="#top">x</a><a href="#a%20b">x</a><a href="#:~:text=a">x</a>`,
			`<label for="{{ a }}">x</label><label for="a b">y</label>`,
		}, "\n"),
		"t.html": strings.Join([]string{
			`<p id=out></p><template><i id=in></i><label for=in>x</label><label for=out>y</label></template>`,
			`<label for=in>z</label>`,
			`<template shadowrootmode=open><i id=s></i><label for=s>x</label></template>`,
		}, "\n"),
	}
	got := resolveAll(t, files)
	check(t, got, []want{
		{"dyn.html:1:a", "strong", "dyn.html#a"},
		{"on.html:1:a", "strong", "on.html#a"},
		{"js.html:1:a", "strong", "js.html#a"},
		{"tpl.html:1:a", "strong", "tpl.html#a"},
		{"base.html:1:a", "exact", "base.html#a"}, // label for is no URL
		{"t.html:1:in", "candidate", ""},          // template content, not a shadow root
		{"t.html:1:out", "unresolved", ""},        // the content is its own tree scope
		{"t.html:2:in", "unresolved", ""},         // the document cannot see into a template
		{"t.html:3:s", "exact", "t.html#2 s"},     // a declarative shadow root
	}, []string{"skip.html:1:top", "skip.html:1:a%20b", "skip.html:2:{{ a }}", "skip.html:2:a b", "skip.html:2:a", "skip.html:2:b"})
	if n := len(got["skip.html:1:"]) + len(got["skip.html:1:a"]); n != 0 {
		t.Errorf("skip.html line 1: %d references, want none", n)
	}
	if n := len(got["base.html:1:a"]); n != 1 {
		t.Errorf("base.html: %d references, want only label for's", n)
	}
}

func TestLinks(t *testing.T) {
	files := map[string]string{
		"index.html": strings.Join([]string{
			`<a href="docs/b.html">b</a><a href="docs/b.html#sec">s</a><a href="docs/c.htm?x=1">c</a>`,     // 1
			`<iframe src="docs/b.html"></iframe><a href="docs/%62.html">e</a>`,                             // 2
			`<a href="missing.html">m</a><a href="docs/b.html#nope">n</a>`,                                 // 3
			`<a href="/docs/b.html">r</a><a href="https://x.org/b.html">h</a><a href="//cdn/b.html">c</a>`, // 4
			`<a href="docs/">d</a><a href="../up.html">u</a><a href="style.css">s</a><a href="mailto:x@y">m</a>`,
			`<a href="docs\b.html">w</a><a href="{{ .URL }}">t</a><a href="?q=1">q</a>`, // 6
		}, "\n"),
		"docs/b.html": `<h2 id=sec>S</h2><a href="../index.html">up</a>`,
		"docs/c.htm":  `<p>c</p>`,
	}
	got := resolveAll(t, files)
	check(t, got, []want{
		{"index.html:1:b.html", "strong", "docs/b.html"},
		{"index.html:1:sec", "strong", "docs/b.html#sec"},
		{"index.html:1:c.htm", "strong", "docs/c.htm"},
		{"index.html:3:missing.html", "unresolved", ""},
		{"index.html:3:nope", "unresolved", ""},
		{"docs/b.html:1:index.html", "strong", "index.html"},
	}, nil)
	if rs := got["index.html:2:b.html"]; len(rs) != 2 || rs[0].target != "docs/b.html" || rs[1].target != "docs/b.html" {
		t.Errorf("index.html:2:b.html: %+v", rs)
	}
	for _, line := range []string{"4", "5", "6"} {
		for k := range got {
			if strings.HasPrefix(k, "index.html:"+line+":") {
				t.Errorf("%s: a reference, want none", k)
			}
		}
	}
}

// A provider whose file name holds '#' states no element (its identity would
// collide with an element's of another file).
func TestHashInFileName(t *testing.T) {
	for _, s := range symbols(t, "a#b.html", `<p id=x>`) {
		if strings.HasPrefix(s, "element") {
			t.Errorf("symbol %s", s)
		}
	}
}

// Reviewer B (Phase 4): facts only the tree shows, declarative shadow roots,
// the URL parser's view of javascript:, frameworks, IDREF types, fragments.
func TestReviewB_NoFalseExact(t *testing.T) {
	cases := []struct {
		name, src, key, conf, target string
	}{
		// 1. an element hidden from the tokenizer duplicates the id
		{"hidden duplicate in svg style", `<svg><style><b id=x>h</b></style></svg><p id=x>v</p><label for=x>L</label>`, "f.html:1:x", "unresolved", ""},
		{"hidden duplicate in svg title", `<svg><title><p id=x>h</p></title></svg><p id=x>v</p><label for=x>L</label>`, "f.html:1:x", "unresolved", ""},
		// 2. a script or handler only the tree shows
		{"svg script in svg style", `<p id=a></p><svg><style><script>x()</script></style></svg><label for=a>L</label>`, "f.html:1:a", "strong", "f.html#a"},
		{"img onerror in svg style", `<p id=a></p><svg><style><img src=x onerror="y()"></style></svg><label for=a>L</label>`, "f.html:1:a", "strong", "f.html#a"},
		// 3. a base only the tree shows
		{"base in svg title", `<svg><title><base href="other/"></title></svg><p id=a></p><label for=a>x</label>`, "f.html:1:a", "exact", "f.html#a"},
		// 4. declarative shadow roots
		{"fragment from a shadow root is the document's", `<div><template shadowrootmode=open><i id=s></i><a href="#s">x</a></template></div>`, "f.html:1:s", "unresolved", ""},
		{"label cannot host a shadow root", `<label><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></label>`, "f.html:1:s", "candidate", ""},
		{"second shadow root of one host", `<div><template shadowrootmode=open></template><template shadowrootmode=open><i id=b></i><label for=b>x</label></template></div>`, "f.html:1:b", "candidate", ""},
		{"shadow root in a template", `<template><div><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></div></template>`, "f.html:1:s", "candidate", ""},
		{"valid shadow root", `<div><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></div>`, "f.html:1:s", "exact", "f.html#1 s"},
		// 5. javascript: as the URL parser reads it
		{"javascript with a tab", "<p id=a></p><a href=\"java&#9;script:x()\">j</a><label for=a>L</label>", "f.html:1:a", "strong", "f.html#a"},
		{"javascript after a control", "<p id=a></p><form action=\"\x01javascript:x()\"></form><label for=a>L</label>", "f.html:1:a", "strong", "f.html#a"},
		// 6. srcdoc
		{"iframe srcdoc", `<p id=a></p><iframe srcdoc="&lt;script&gt;x()&lt;/script&gt;"></iframe><label for=a>L</label>`, "f.html:1:a", "strong", "f.html#a"},
		// 7. frameworks
		{"angular property binding", `<input [id]="dyn" id=q><label for=q>L</label>`, "f.html:1:q", "strong", "f.html#q"},
		{"angular control flow", `@for (x of xs; track x) { <input id=q> }<label for=q>L</label>`, "f.html:1:q", "strong", "f.html#q"},
		{"vue v-for", `<input v-for="x in xs" id=q><label for=q>L</label>`, "f.html:1:q", "strong", "f.html#q"},
		// 8. IDREF types and elements
		{"aria-activedescendant is one id", `<i id=a></i><i id=b></i><p aria-activedescendant="a b">`, "f.html:1:a", "", ""},
		{"form on a div", `<form id=f></form><div form=f>x</div>`, "f.html:1:f", "", ""},
		{"popovertarget on a text input", `<div id=p popover></div><input popovertarget=p>`, "f.html:1:p", "", ""},
		{"list on a checkbox", `<datalist id=d></datalist><input type=checkbox list=d>`, "f.html:1:d", "", ""},
		// 11. fragments
		{"non-ascii fragment", `<p id="é"></p><a href="#é">x</a>`, "f.html:1:é", "", ""},
		{"escape in url()", `<svg><g id="a\62"></g><rect fill="url(#a\62)"/></svg>`, `f.html:1:a\62`, "", ""},
		{"iframe src fragment", `<p id=a></p><iframe src="#a"></iframe>`, "f.html:1:a", "", ""},
		// Re-review R1: an SVG hyperlink's fragment is the document's
		{"svg a fragment from a shadow root", `<div><template shadowrootmode=open><i id=s></i><svg><a href="#s"><text>x</text></a></svg></template></div>`, "f.html:1:s", "unresolved", ""},
		{"svg a fragment to the document", `<p id=s></p><div><template shadowrootmode=open><svg><a href="#s"><text>x</text></a></svg></template></div>`, "f.html:1:s", "exact", "f.html#s"},
		// R2: NUL is U+FFFD to the standard
		{"nul duplicate", "<p id=\"a\x00b\"></p><p id=\"a\uFFFDb\"></p><label for=\"a\uFFFDb\">L</label>", "f.html:1:a\uFFFDb", "unresolved", ""},
		// R3: x/net's tree ends early at a template in an integration point
		{"truncated tree", `<p id=a></p><a href="#a">x</a><svg><desc><template></template></desc><title><base href="o/"></title></svg><p id=c>`, "f.html:1:a", "", ""},
		{"truncated tree, last tag kept", `<p id=a></p><a href="#a">x</a><svg><desc><template></template></desc><title><base href="o/"></title></svg>`, "f.html:1:a", "", ""},
		// R4: an earlier declaration the adoption agency moved
		{"moved earlier shadow root", `<b><div><template shadowrootmode=open></template></b></b><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></div>`, "f.html:1:s", "candidate", ""},
		// R5: a tag the tree builder ignores may be an element in a browser;
		// in the standard's tree (no select insertion mode) it is one, and
		// two elements have the id (selectR5, below)
		{"select keeps a div now", `<select><div id=x></div></select><p id=x></p><label for=x>y</label>`, "f.html:1:x", selectR5(t), ""},
		// R6: no valid custom element name, no host
		{"reserved custom element name", `<font-face><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></font-face>`, "f.html:1:s", "candidate", ""},
		{"invalid custom element name", `<a-b!><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></a-b!>`, "f.html:1:s", "candidate", ""},
		{"valid custom element name", `<my-card><template shadowrootmode=open><i id=s></i><label for=s>x</label></template></my-card>`, "f.html:1:s", "exact", "f.html#1 s"},
		// R7: an enumerated attribute's whitespace is no whitespace to trim
		{"padded input type", `<div id=p popover></div><input type=" submit" popovertarget=p>`, "f.html:1:p", "", ""},
		// R8: documents that may change this one
		{"same-origin frame", `<p id=a></p><iframe src="b.html"></iframe><label for=a>L</label>`, "f.html:1:a", "strong", "f.html#a"},
		{"object data", `<p id=a></p><object data="b.html"></object><label for=a>L</label>`, "f.html:1:a", "strong", "f.html#a"},
		{"server-side include", `<!--#include virtual="nav.html" --><p id=a></p><label for=a>L</label>`, "f.html:1:a", "strong", "f.html#a"},
		{"blade foreach", `@foreach($xs as $x) <input id=q> @endforeach<label for=q>L</label>`, "f.html:1:q", "strong", "f.html#q"},
		{"svelte each", `{#each xs as x}<input id=q>{/each}<label for=q>L</label>`, "f.html:1:q", "strong", "f.html#q"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveAll(t, map[string]string{"f.html": c.src})
			if c.conf == "" {
				check(t, got, nil, []string{c.key})
				return
			}
			check(t, got, []want{{c.key, c.conf, c.target}}, nil)
		})
	}
	// 3. the base also stops fragments and links
	got := resolveAll(t, map[string]string{"f.html": `<svg><title><base href="other/"></title></svg><p id=a></p><a href="#a">x</a><a href="b.html">b</a>`, "b.html": ``})
	check(t, got, nil, []string{"f.html:1:a", "f.html:1:b.html"})
	// 12. an escaped slash is no path separator
	got = resolveAll(t, map[string]string{"d/f.html": `<a href="sub%2Fc.html">c</a><a href="..%2Ftop.html">t</a>`, "d/sub/c.html": ``, "top.html": ``})
	check(t, got, nil, []string{"d/f.html:1:c.html", "d/f.html:1:top.html"})
	// 10. a file with '#' in its path states nothing
	if ex := extract(t, "a.html#x.html", `<p id=y>`); len(ex.Symbols) != 0 || len(ex.References) != 0 {
		t.Errorf("a.html#x.html: %d symbols, %d references", len(ex.Symbols), len(ex.References))
	}
	// 13. an end tag cut off loses no fact: a warning
	for _, d := range extract(t, "f.html", `<p id=a>x</p`).Diagnostics {
		if d.Code == html.DiagEOFInTag && d.Severity != language.SeverityWarning {
			t.Errorf("cut-off end tag: %s", d.Severity)
		}
	}
}

// selectKeepsContent reports whether x/net builds the tree of the HTML
// standard as it now is for select: no "in select" insertion mode (removed
// with customizable select; x/net v0.60.0), so a div or an xmp in a select
// is an element of the tree, as in a browser (Chrome 155). Older x/net
// builds the earlier standard's tree, which drops them. Either tree is the
// standard's at its version; the tests below pin both.
func selectKeepsContent(t *testing.T) bool {
	t.Helper()
	doc, err := xhtml.Parse(strings.NewReader("<select><div></div></select>"))
	if err != nil {
		t.Fatal(err)
	}
	var sel *xhtml.Node
	var find func(n *xhtml.Node)
	find = func(n *xhtml.Node) {
		for ; n != nil && sel == nil; n = n.NextSibling {
			if n.Type == xhtml.ElementNode && n.Data == "select" {
				sel = n
			}
			find(n.FirstChild)
		}
	}
	find(doc)
	return sel != nil && sel.FirstChild != nil
}

// selectR5 is the confidence of the label in <select><div id=x></div>
// </select><p id=x>: Unresolved where the tree drops the div (a browser may
// keep it: no claim on p#x), a Candidate where the tree keeps it (two
// elements have the id). Never Strong or Exact.
func selectR5(t *testing.T) string {
	if selectKeepsContent(t) {
		return "candidate"
	}
	return "unresolved"
}
