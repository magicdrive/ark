package html_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/languages/css"
	"github.com/magicdrive/ark/internal/languages/html"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/source"
)

// Embedded regions (Phase 8): a document's <style> and <script> texts, read
// by the CSS and JavaScript extractors, located in the document's source.

// at returns the bytes of src a location covers (lines by '\n', columns in
// bytes from 1).
func at(src string, start, end [2]uint32) string {
	off := func(p [2]uint32) int {
		lines := strings.SplitAfter(src, "\n")
		o := 0
		for i := 0; i < int(p[0])-1; i++ {
			o += len(lines[i])
		}
		return o + int(p[1]) - 1
	}
	return src[off(start):off(end)]
}

const embedPage = "<!doctype html><title>t</title>\n" +
	"<style>@import \"x.css\";@keyframes spin {}\r\n.a { animation-name: spin; color: var(--c) }\n:root { --c: red }\n@import \"late.css\";</style>\n" +
	"<style type=\"TEXT/CSS\">@font-face { font-family: Inter; src: url(i.woff) } p { font-family: Inter, serif }</style>\n" +
	"<style type=\"text/less\">@keyframes less {}</style><style type=\" text/css\">@keyframes padded {}</style>\n" +
	"<script>function init() { helper(); new Widget(); later() }\nfunction helper() {}\nclass Widget { run() { this.go(); init() } go() {} }</script>\n" +
	"<script type=module>import { util } from './u.js'\nfunction own() { util(); own(); init() }\nexport function exported() {}</script>\n" +
	"<script>function later() { own() }</script>\n" +
	"<script src=\"a.js\">function ignored() {}</script><script type=\"text/template\">function tmpl() {}</script>\n" +
	"<template><style>@keyframes inert {}</style><script>function inert() {}</script></template>\n" +
	"<svg><style>@keyframes svg {}</style><script>function svg() {}</script></svg>\n" +
	"<p id=\"init\">x</p><textarea><script>function text() {}</script></textarea>\n"

func embedIndex(t *testing.T, files map[string]string) *index.RepositoryIndex {
	t.Helper()
	root := t.TempDir()
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestEmbedded_SymbolsAndRanges(t *testing.T) {
	x, err := html.NewProvider().Extract(context.Background(), "p.html", []byte(embedPage))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range x.Symbols {
		r := s.Location.Range
		text := at(embedPage, [2]uint32{r.Start.Line, r.Start.Column}, [2]uint32{r.End.Line, r.End.Column})
		if text != embedPage[s.StartByte:s.EndByte] {
			t.Errorf("%s: lines and columns cover %q, bytes %q", s.Qualified, text, embedPage[s.StartByte:s.EndByte])
		}
		if len(text) > 24 {
			text = text[:24]
		}
		got = append(got, fmt.Sprintf("%s (%s) parent=%s %q", s.Qualified, s.Kind, s.Parent, text))
	}
	want := []string{
		"p.html (document) parent= \"<!doctype html><title>t<\"",
		"p.html#init (element) parent=p.html \"<p id=\\\"init\\\">\"",
		"p.html @keyframes spin (keyframes) parent=p.html \"@keyframes spin {}\"",
		"p.html --c (custom_property) parent=p.html \"--c: red\"",
		"p.html @font-face inter (font_face) parent=p.html \"@font-face { font-family\"",
		"p.html init (function) parent=p.html \"function init() { helper\"",
		"p.html helper (function) parent=p.html \"function helper() {}\"",
		"p.html Widget (class) parent=p.html \"class Widget { run() { t\"",
		"p.html Widget.run (method) parent=p.html Widget \"run() { this.go(); init(\"",
		"p.html Widget.go (method) parent=p.html Widget \"go() {}\"",
		"p.html module#1 own (function) parent=p.html \"function own() { util();\"",
		"p.html module#1 exported (function) parent=p.html \"function exported() {}\"",
		"p.html later (function) parent=p.html \"function later() { own()\"",
		// An SVG style and script whose tree holds their text as written
		// (hardening Phase E).
		"p.html @keyframes svg (keyframes) parent=p.html \"@keyframes svg {}\"",
		"p.html svg (function) parent=p.html \"function svg() {}\"",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("symbols\n got %q\nwant %q", got, want)
	}
	for _, s := range x.Symbols {
		if s.Exported && strings.Contains(s.Qualified, " ") && s.Kind == "function" {
			t.Errorf("%s: an embedded script's declaration is exported", s.Qualified)
		}
	}
	// Every reference's location covers its name; none has a receiver
	// (this.go() in Widget.run is not stated).
	for _, r := range x.References {
		if r.ReceiverExpr != "" || r.Name == "go" {
			t.Errorf("a reference with a receiver: %+v", r)
		}
		p := r.Location.Range
		text := at(embedPage, [2]uint32{p.Start.Line, p.Start.Column}, [2]uint32{p.End.Line, p.End.Column})
		if !strings.EqualFold(strings.Trim(text, `"`), r.Name) && r.Kind != "resource_link" {
			t.Errorf("reference %s at %v covers %q", r.Name, p, text)
		}
	}
}

func TestEmbedded_Resolution(t *testing.T) {
	idx := embedIndex(t, map[string]string{
		"p.html":     embedPage,
		"x.css":      "",
		"u.js":       "export function util() {}\n",
		"q.html":     "<script>function init() {} helper()</script>\n",
		"g.js":       "function init() {}\nfunction helper() {}\n",
		"base.html":  "<base href=\"/b/\"><style>@import \"x.css\";</style>\n",
		"noimp.html": "<style>@import url(x.css);</style>\n",
	})
	var got []string
	for _, f := range []source.FileID{"p.html", "q.html", "base.html", "noimp.html"} {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				got = append(got, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
			}
		}
	}
	sort.Strings(got)
	want := []string{
		"noimp.html -links-> x.css strong",
		"p.html -links-> x.css strong",
		"p.html -references-> p.html @font-face inter strong",
		"p.html -references-> p.html @keyframes spin strong",
		"p.html Widget.run -calls-> p.html init strong",
		"p.html init -calls-> p.html Widget strong", // new Widget()
		"p.html init -calls-> p.html helper strong",
		"p.html init -calls-> p.html later strong",
		"p.html module#1 own -calls-> p.html init strong",
		"p.html module#1 own -calls-> p.html module#1 own strong",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("relations\n got %q\nwant %q", got, want)
	}
	// var(--c): a candidate (its value is inherited through the tree).
	if c := idx.CandidateCalleeSample(idx.FindSymbolsByQualified("p.html")[0].ID); c.Total != 1 {
		t.Errorf("candidates of p.html: %+v, want var(--c)", c)
	}
	// No relation to another document's or a JavaScript file's declaration
	// of the same name; a classic script's own() is not the module's.
	for _, q := range []string{"q.html init", "init", "helper", "util"} {
		for _, s := range idx.FindSymbolsByQualified(q) {
			if n := len(idx.GetCallers(s.ID)) + idx.CandidateCallerSample(s.ID).Total; n != 0 && q != "q.html init" {
				t.Errorf("%s (%s) has %d callers", q, s.Location.File, n)
			}
		}
	}
	// Unresolved, never outside: util (an import), own from a classic
	// script, q.html's helper.
	for _, d := range []string{"p.html module#1 own", "p.html later", "q.html"} {
		ss := idx.FindSymbolsByQualified(d)
		if len(ss) != 1 {
			t.Fatalf("%s: %d symbols", d, len(ss))
		}
		u := idx.UnresolvedOutgoing(ss[0].ID)
		if u.Unresolved != 1 || u.OutsideRepository != 0 {
			t.Errorf("%s: %+v, want one unresolved", d, u)
		}
	}
}

// A region the tree does not hold as the tokenizer read it, or that holds a
// NUL, is not read.
func TestEmbedded_Refused(t *testing.T) {
	for _, src := range []string{
		"<style>@keyframes a {}\x00</style>",
		"<script>function a() {}\x00</script>",
		"<style>\xef\xbb\xbf@keyframes a {}</style>",
		"<table><style>@keyframes a {}</style></table>", // fostered? style stays in table: still read
	} {
		x, err := html.NewProvider().Extract(context.Background(), "r.html", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, s := range x.Symbols {
			if strings.HasPrefix(s.Qualified, "r.html ") {
				n++
			}
		}
		want := 0
		if strings.HasPrefix(src, "<table>") {
			want = 1
		}
		if n != want {
			t.Errorf("%q: %d embedded symbols, want %d", src, n, want)
		}
	}
}

// Cold and restored indexes agree on what embedded regions and CSS Modules
// bindings state.
func TestEmbedded_CacheColdWarm(t *testing.T) {
	files := map[string]string{
		"p.html":           embedPage,
		"x.css":            "",
		"u.js":             "export function util() {}\n",
		"a.module.css":     ".btn {}\n",
		"v.tsx":            "import s from './a.module.css'\nexport const V = () => <i className={s.btn} />\n",
		"w.js":             "import { btn } from './a.module.css'\nexport function w() { return btn }\n",
		"sub/q.html":       "<style>@keyframes k {} a { animation: 1s k }</style><script>function f() { f() }</script>\n",
		"sub/r.htm":        "<script type=module>function g() { g() }</script>\n",
		"sub/empty.html":   "<script></script><style></style>\n",
		"sub/crlf.html":    "<script>\r\nfunction h() {\r\n  h()\r\n}\r\n</script>\r\n",
		"sub/one-line.htm": "<p>x</p><script>let a = 1; function i() { return a }</script>",
	}
	root := t.TempDir()
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	providers := languages.Registry().Providers()
	snapshot := func(idx *index.RepositoryIndex) []string {
		out := relations(idx)
		for _, f := range idx.Files() {
			for _, s := range idx.SymbolsByFile(f) {
				r := s.Location.Range
				out = append(out, fmt.Sprintf("%s %s %v-%v", s.Qualified, s.Kind, r.Start, r.End))
				c := idx.CandidateCalleeSample(s.ID)
				u := idx.UnresolvedOutgoing(s.ID)
				out = append(out, fmt.Sprintf("%s candidates=%d unresolved=%d outside=%d", s.Qualified, c.Total, u.Unresolved, u.OutsideRepository))
			}
		}
		return out
	}
	plain, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	want := snapshot(plain)
	for _, w := range []string{"sub/q.html f", "sub/r.htm module#1 g", "sub/crlf.html h", "sub/one-line.htm i", "V -references-> a.module.css .btn (css_class) strong", "w -references-> a.module.css .btn (css_class) strong"} {
		if !strings.Contains(strings.Join(want, "\n"), w) {
			t.Errorf("missing %q", w)
		}
	}
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"cold", "warm"} {
		idx, err := index.NewWithCache(context.Background(), root, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := snapshot(idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s differs from the plain index", run)
		}
	}
}

// Lines and columns cover the bytes a symbol spans, after CR LF, CR, a
// region that starts mid-line, and multi-byte text.
func TestEmbedded_RangesMap(t *testing.T) {
	for _, src := range []string{
		"<script>\r\nfunction h() {\r\n  h()\r\n}\r\n</script>\r\n",
		"<script>\rfunction h() {\r  h()\r}\r</script>",
		"<p>é</p><script>let a = 'ü'; function i() { return a }</script>",
		"<style>\n\n  @keyframes x { from {} }\r\n  @property --p { syntax: '*'; inherits: false }</style>",
	} {
		x, err := html.NewProvider().Extract(context.Background(), "m.html", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, s := range x.Symbols {
			if !strings.HasPrefix(s.Qualified, "m.html ") {
				continue
			}
			n++
			r := s.Location.Range
			// CR alone ends no line for Ark (as for every provider): its
			// columns count across it.
			text := at(src, [2]uint32{r.Start.Line, r.Start.Column}, [2]uint32{r.End.Line, r.End.Column})
			if text != src[s.StartByte:s.EndByte] {
				t.Errorf("%q: %s covers %q, bytes %q", src, s.Qualified, text, src[s.StartByte:s.EndByte])
			}
		}
		if n == 0 {
			t.Errorf("%q: no embedded symbol", src)
		}
	}
}

// The cache version follows the CSS and JavaScript extractors the inline
// regions are read with.
func TestEmbedded_CacheVersion(t *testing.T) {
	v := html.NewProvider().CacheVersion()
	if !strings.HasPrefix(v, "html-") || !strings.Contains(v, "+"+css.NewProvider().CacheVersion()+"+") ||
		!strings.HasSuffix(v, "+"+javascript.NewProvider().CacheVersion()) {
		t.Errorf("CacheVersion %q", v)
	}
}

// Reviewer B (Phase 8): what a browser does not run is not read; what the
// readings state stays within their scopes.
func TestEmbedded_ReviewB(t *testing.T) {
	idx := embedIndex(t, map[string]string{
		"eof.html":     "<script>function run() {}</script><script>run(); function late() {}",
		"nm.html":      "<script nomodule>function legacy() {}</script><script>function u() { legacy() }</script><script type=module nomodule>function mod() {}</script>",
		"ev.html":      "<script for=\"button\" event=\"onclick\">function ev() {}</script><script for=\" Window \" event=\"ONLOAD()\">function ok() {}</script><script for=window>function half() {}</script>",
		"sh.html":      "<script>function helper() {}\nfunction a(helper) { helper() }</script>",
		"imp.html":     "<script>function util() {}\nfunction helper() {}</script><script type=module>import { util } from './u.js'\nconst { helper } = globalThis\nfunction k() { util(); helper() }\nclass W { m() {} }\nimport s from './a.module.css'\nfunction q() { return s.btn }</script>",
		"u.js":         "export function util() {}\n",
		"a.module.css": ".btn {}\n",
	})
	syms := map[string]string{}
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			parent := ""
			if p, ok := idx.GetSymbol(s.Parent); ok {
				parent = p.Qualified
			}
			syms[s.Qualified] = parent
		}
	}
	for _, q := range []string{"eof.html late", "nm.html legacy", "ev.html ev"} {
		if _, ok := syms[q]; ok {
			t.Errorf("%s: a script that does not run is read", q)
		}
	}
	for _, q := range []string{"eof.html run", "nm.html u", "nm.html module#1 mod", "ev.html ok", "ev.html half"} {
		if _, ok := syms[q]; !ok {
			t.Errorf("%s: missing", q)
		}
	}
	if p := syms["imp.html module#1 W.m"]; p != "imp.html module#1 W" {
		t.Errorf("parent of imp.html module#1 W.m: %q", p)
	}
	got := relations(idx)
	for _, r := range got {
		for _, bad := range []string{"-> eof.html run", "-> imp.html util", "-> imp.html helper", "-> sh.html helper", "a.module.css"} {
			if strings.Contains(r, bad) {
				t.Errorf("relation %s", r)
			}
		}
	}
	// a(helper) calls its parameter: a candidate, never an edge.
	a := idx.FindSymbolsByQualified("sh.html a")
	if len(a) != 1 || idx.CandidateCalleeSample(a[0].ID).Total != 1 {
		t.Errorf("sh.html a: %+v", a)
	}
	// k: util (an import) and helper (a module binding) are unresolved,
	// never the classic script's.
	k := idx.FindSymbolsByQualified("imp.html module#1 k")
	if len(k) != 1 {
		t.Fatalf("imp.html module#1 k: %d", len(k))
	}
	if u := idx.UnresolvedOutgoing(k[0].ID); u.Unresolved != 2 || u.OutsideRepository != 0 || idx.CandidateCalleeSample(k[0].ID).Total != 0 {
		t.Errorf("imp.html module#1 k: %+v", u)
	}
	for _, r := range idx.ReferencesByFile("imp.html") {
		if r.FileIdentity {
			t.Errorf("a CSS Modules reference from an inline script: %+v", r)
		}
	}
}

// A syntax error of an inline script is reported where it is in the
// document: where the same text, a JavaScript file of its own, has it,
// moved to the script's position (line 2, column 9).
func TestEmbedded_ScriptDiagnosticLocation(t *testing.T) {
	for _, content := range []string{"let = =\r\n", "\r\nlet a = 1\r\nlet = =\r\n"} {
		scriptDiagnosticAt(t, content)
	}
}

func scriptDiagnosticAt(t *testing.T, content string) {
	t.Helper()
	src := "<p>x</p>\n<script>" + content + "</script>\r\n"
	x, err := html.NewProvider().Extract(context.Background(), "d.html", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	alone, err := typescript.ExtractJavaScript(context.Background(), "d.js", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if len(alone.Diagnostics) == 0 || len(x.Diagnostics) != len(alone.Diagnostics) {
		t.Fatalf("diagnostics %+v, alone %+v", x.Diagnostics, alone.Diagnostics)
	}
	move := func(p source.Position) source.Position {
		if p.Line == 1 {
			return source.Position{Line: 2, Column: p.Column + 8}
		}
		return source.Position{Line: p.Line + 1, Column: p.Column}
	}
	for i, d := range x.Diagnostics {
		r, a := d.Location.Range, alone.Diagnostics[i].Location.Range
		if r.Start != move(a.Start) || r.End != move(a.End) || d.Location.File != "d.html" {
			t.Errorf("%s at %v, alone at %v", d.Code, r, a)
		}
	}
}

// Many style elements cost time linear in the document (each region's
// locations are its own and mapped by the document's one line table): four
// times the regions take well under the sixteen times a quadratic cost would
// (the best of three runs, a ratio, so a slow machine or the race detector
// does not matter).
func TestEmbedded_ManyRegions(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	p := html.NewProvider()
	best := func(src []byte) time.Duration {
		d := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			t0 := time.Now()
			if _, err := p.Extract(context.Background(), "m.html", src); err != nil {
				t.Fatal(err)
			}
			if e := time.Since(t0); e < d {
				d = e
			}
		}
		return d
	}
	for _, c := range []struct {
		el string
		n  int
	}{{"<style>a{animation:k 1s}</style>\n", 2000}, {"<script>f()</script>\n", 500}} {
		small, large := best([]byte(strings.Repeat(c.el, c.n))), best([]byte(strings.Repeat(c.el, 4*c.n)))
		if large > 8*small {
			t.Errorf("%q: %d regions %v, %d regions %v (more than linear)", c.el, c.n, small, 4*c.n, large)
		}
	}
}

// A CSS diagnostic of an inline style is reported where it is in the
// document (Reviewer B, R8-1): the stray } of a style that starts mid-line,
// a rule without a block up to the end of the text.
func TestEmbedded_StyleDiagnosticLocation(t *testing.T) {
	src := "<p>x</p>\n<p>y</p><style>a{}}\n</style>"
	x, err := html.NewProvider().Extract(context.Background(), "s.html", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range x.Diagnostics {
		if d.Code == "css_invalid" {
			found = true
			r := d.Location.Range
			if got := at(src, [2]uint32{r.Start.Line, r.Start.Column}, [2]uint32{r.End.Line, r.End.Column}); got != "}\n" || r.Start.Line != 2 || r.Start.Column != 19 {
				t.Errorf("css_invalid at %v covers %q", r, got)
			}
		}
	}
	if !found {
		t.Errorf("no css_invalid in %+v", x.Diagnostics)
	}
}

// Phase 9 audit (Reviewer B): a script language attribute is not stripped
// (F9B-5); a frameset document's tree is unverified, and says so (F9B-6);
// references in a declarative shadow root cost linear time (F9B-2).
func TestAudit9_HTML(t *testing.T) {
	syms := func(src string) []string {
		x, err := html.NewProvider().Extract(context.Background(), "a.html", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range x.Symbols {
			out = append(out, s.Qualified)
		}
		return out
	}
	if got := syms(`<script language="javascript ">function lj() {}</script>`); len(got) != 1 {
		t.Errorf("a padded language attribute's script is read: %q", got)
	}
	if got := syms(`<script language="JavaScript1.5">function lj() {}</script>`); len(got) != 2 {
		t.Errorf("a JavaScript language attribute's script is not read: %q", got)
	}
	x, err := html.NewProvider().Extract(context.Background(), "a.html", []byte("<frameset></frameset><p id=a></p><label for=a>L</label>"))
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Diagnostics) != 1 || x.Diagnostics[0].Code != html.DiagTreeUnverified {
		t.Errorf("a frameset document: %+v", x.Diagnostics)
	}
	if testing.Short() {
		return
	}
	page := func(n int) []byte {
		return []byte("<div>" + strings.Repeat("<p></p>", n) + "<template shadowrootmode=open>" +
			strings.Repeat("<label for=a></label>", n) + "<i id=a></i></template></div>")
	}
	best := func(src []byte) time.Duration {
		d := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			t0 := time.Now()
			if _, err := html.NewProvider().Extract(context.Background(), "s.html", src); err != nil {
				t.Fatal(err)
			}
			if e := time.Since(t0); e < d {
				d = e
			}
		}
		return d
	}
	if small, large := best(page(1500)), best(page(6000)); large > 8*small {
		t.Errorf("shadow root references: 1500 %v, 6000 %v (more than linear)", small, large)
	}
}

// Hardening Phase E: an SVG <style> and classic <script> are read when the
// tree holds their text as written; one with a character reference, a CDATA
// section or an href is not.
func TestEmbedded_SVG(t *testing.T) {
	src := "<svg><style>@keyframes svgk {}</style><script>function svgf() {}</script>" +
		"<style>@keyframes amp { } .x::after { content: \"&amp;\" }</style>" +
		"<script><![CDATA[function cdata() {}]]></script>" +
		"<script href=\"x.js\">function external() {}</script><script type=module>function svgmod() {}</script>" +
		"<script xlink:href=\"y.js\">function xexternal() {}</script><style>@keyframes cmt { } <!-- c --></style></svg>"
	x, err := html.NewProvider().Extract(context.Background(), "s.html", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range x.Symbols {
		if strings.HasPrefix(s.Qualified, "s.html ") {
			got = append(got, s.Qualified)
			if text := src[s.StartByte:s.EndByte]; !strings.Contains(text, s.Name) {
				t.Errorf("%s covers %q", s.Qualified, text)
			}
		}
	}
	if want := []string{"s.html @keyframes svgk", "s.html svgf"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SVG regions %q, want %q", got, want)
	}
}
