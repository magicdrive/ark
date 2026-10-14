package css_test

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/css"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
)

// The CSS provider states a stylesheet's @keyframes, @font-face families,
// @property registrations, custom properties and CSS Modules classes, and
// resolves the references to them within the stylesheet only; @import
// links stylesheets by relative path.

func extract(t *testing.T, file, src string) language.Extraction {
	t.Helper()
	ex, err := css.NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func symbols(t *testing.T, file, src string) []string {
	t.Helper()
	var out []string
	for _, s := range extract(t, file, src).Symbols {
		out = append(out, fmt.Sprintf("%s %q [%d,%d)", s.Kind, s.Qualified, s.StartByte, s.EndByte))
	}
	return out
}

func TestSymbols(t *testing.T) {
	src := "@keyframes spin { to { x: y } }\n" + // 0..32
		"@keyframes \"quoted name\" { }\n" + // 33..62
		"@font-face { font-family: \"Inter Var\"; src: url(i.woff2) }\n" +
		"@property --angle { syntax: '<angle>'; inherits: false }\n" +
		":root { --brand: #0af; --empty:; }\n" +
		".a { color: red }\n" +
		"@keyframes none { } @keyframes inherit { } @keyframes a b { }\n"
	got := symbols(t, "s/a.css", src)
	want := []string{
		fmt.Sprintf(`stylesheet "s/a.css" [0,%d)`, len(src)),
		`keyframes "s/a.css @keyframes spin" [0,31)`,
		`keyframes "s/a.css @keyframes quoted name" [32,60)`,
		`font_face "s/a.css @font-face inter var" [61,119)`,
		`property_registration "s/a.css @property --angle" [120,176)`,
		`custom_property "s/a.css --brand" [185,198)`,
		`custom_property "s/a.css --empty" [200,208)`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("symbols\n got %q\nwant %q", got, want)
	}
	if !extract(t, "a.css", src).IdentityOnly {
		t.Error("IdentityOnly = false")
	}
}

func TestModuleClasses(t *testing.T) {
	src := ".root, .a:hover > .b { x: y }\n" +
		":global(.dark) .root .c { x: y }\n" +
		".d :global .e .f, .g { x: y }\n" +
		":local(.h) :global(.i :local(.j)) { x: y }\n" +
		".k:not(.l) { &.m { x: y } }\n" +
		"@media (min-width: 1px) { .n { x: y } }\n" +
		".\\31 23 { x: y }\n"
	var got []string
	for _, s := range extract(t, "s/a.module.css", src).Symbols {
		if s.Kind == "css_class" {
			got = append(got, s.Name)
		}
	}
	want := []string{"root", "a", "b", "c", "d", "g", "h", "j", "k", "l", "m", "n", "123"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("classes\n got %q\nwant %q", got, want)
	}
	for _, s := range extract(t, "s/a.css", src).Symbols {
		if s.Kind == "css_class" {
			t.Errorf("a class symbol %q outside a CSS Modules file", s.Name)
		}
	}
}

type resolution struct {
	conf    resolver.Confidence
	target  string
	outside bool
}

func resolveAll(t *testing.T, files map[string]string) map[string][]resolution {
	t.Helper()
	prov := css.NewProvider()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var fis []resolver.FileIndex
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

type want struct{ key, conf, target string }

func check(t *testing.T, got map[string][]resolution, wants []want, absent []string) {
	t.Helper()
	for _, w := range wants {
		rs := got[w.key]
		if len(rs) != 1 {
			t.Errorf("%s: %d references, want 1 (have %v)", w.key, len(rs), keysOf(got))
			continue
		}
		if r := rs[0]; r.conf.String() != w.conf || r.target != w.target || r.outside {
			t.Errorf("%s: %v %q outside=%v, want %s %q", w.key, r.conf, r.target, r.outside, w.conf, w.target)
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

func TestReferences(t *testing.T) {
	files := map[string]string{
		"a.css": strings.Join([]string{
			`@keyframes spin {} @keyframes fade {} @keyframes dup {} @keyframes dup {}`, // 1
			`.a { animation-name: spin, none, "fade"; animation: 1s ease-in infinite spin, 2s fade both; }`,
			`.b { animation: 1s dup; animation-name: missing; animation: 1s spin fade; }`, // 3: dup, missing, two names
			`@font-face { font-family: "Inter Var"; src: url(a.woff) } @font-face { font-family: Open Sans; src: url(b.woff) }`,
			`.c { font-family: "inter var", "Open  Sans", Open  Sans, sans-serif, system-ui; }`, // 5
			`:root { --x: 1; } .d { --x: 2; color: var(--x); margin: var(--y, var(--x)); }`,     // 6
			`.e { font-family: var(--f); animation-name: var(--a); }`,                           // 7: not static
			`.f { animation: {{ name }} 1s; font-family: "{{ f }}"; }`,                          // 8: templated
		}, "\n"),
		"b.css": `.g { animation-name: spin; font-family: "Inter Var"; color: var(--x); }`, // names of a.css: never
	}
	got := resolveAll(t, files)
	for _, k := range []string{"a.css:2:spin", "a.css:2:fade"} {
		if rs := got[k]; len(rs) != 2 {
			t.Errorf("%s: %d references, want 2", k, len(rs))
		}
		for _, r := range got[k] {
			if r.conf.String() != "strong" {
				t.Errorf("%s: %v, want strong", k, r.conf)
			}
		}
	}
	check(t, got, []want{
		{"a.css:3:dup", "candidate", ""},
		{"a.css:3:missing", "unresolved", ""},
		{"a.css:5:open sans", "strong", "a.css @font-face open sans"},
		{"a.css:5:inter var", "strong", "a.css @font-face inter var"},
		{"a.css:5:open  sans", "unresolved", ""},
		{"b.css:1:spin", "unresolved", ""},
		{"b.css:1:inter var", "unresolved", ""},
		{"b.css:1:--x", "unresolved", ""},
		{"a.css:7:--f", "unresolved", ""}, // var() is a reference; the font-family and
		{"a.css:7:--a", "unresolved", ""}, // animation-name it computes are not
	}, []string{"a.css:2:none", "a.css:2:ease-in", "a.css:2:infinite", "a.css:2:both", "a.css:5:sans-serif", "a.css:5:system-ui",
		"a.css:8:name", "a.css:8:{{ f }}"})
	if rs := got["a.css:6:--x"]; len(rs) != 2 || rs[0].conf.String() != "candidate" || rs[0].target != "" {
		t.Errorf("a.css:6:--x: %+v, want two candidates", rs)
	}
	if rs := got["a.css:6:--y"]; len(rs) != 1 || rs[0].conf.String() != "unresolved" {
		t.Errorf("a.css:6:--y: %+v", rs)
	}
	// the shorthand with two names states none
	n := 0
	for k := range got {
		if strings.HasPrefix(k, "a.css:3:") && (strings.HasSuffix(k, ":spin") || strings.HasSuffix(k, ":fade")) {
			n++
		}
	}
	if n != 0 {
		t.Errorf("animation with two names: %d references, want none", n)
	}
}

func TestImportsAndComposes(t *testing.T) {
	files := map[string]string{
		"s/main.css": strings.Join([]string{
			`@import "base.css"; @import url(./theme.css) layer(t); @import url("../top.css") print;`,
			`@import "/abs.css"; @import url(https://cdn/x.css); @import "missing.css"; @import "img.png";`,
		}, "\n"),
		"s/base.css":  ``,
		"s/theme.css": ``,
		"top.css":     ``,
		"s/a.module.css": strings.Join([]string{
			`.base { x: y } .title { composes: base; composes: base other; }`,
			`.t { composes: card from "./b.module.css"; composes: g from global; composes: z from "./c.css"; }`,
		}, "\n"),
		"s/b.module.css": `.card { x: y }`,
	}
	got := resolveAll(t, files)
	check(t, got, []want{
		{"s/main.css:1:base.css", "strong", "s/base.css"},
		{"s/main.css:1:theme.css", "strong", "s/theme.css"},
		{"s/main.css:1:top.css", "strong", "top.css"},
		{"s/main.css:2:missing.css", "unresolved", ""},
		{"s/a.module.css:1:other", "unresolved", ""},
		{"s/a.module.css:2:card", "strong", "s/b.module.css .card"},
	}, []string{"s/main.css:2:abs.css", "s/main.css:2:x.css", "s/main.css:2:img.png", "s/a.module.css:2:g", "s/a.module.css:2:z"})
	if rs := got["s/a.module.css:1:base"]; len(rs) != 2 || rs[0].conf.String() != "exact" || rs[0].target != "s/a.module.css .base" {
		t.Errorf("composes base: %+v", rs)
	}
}

func TestDiagnostics(t *testing.T) {
	codes := func(src string) []string {
		var out []string
		for _, d := range extract(t, "a.css", src).Diagnostics {
			out = append(out, string(d.Severity)+" "+d.Code)
		}
		sort.Strings(out)
		return out
	}
	for src, want := range map[string][]string{
		".a { b: url(bad url); }":                    {"error css_bad_url"},
		".a { b: \"x\ny; }":                          {"error css_bad_string"},
		".a { animation-name: spin\n.b{c:d}}":        {"error css_rule_from_declaration"},
		"color: red; .x{y:z}":                        {"error css_stray_in_prelude"},
		".a { *zoom: 1; }":                           {"warning css_invalid"},
		".a { b: c":                                  {"warning css_eof_in_block"},
		".a{b:(}":                                    {"warning css_eof_in_block"},
		"/* open":                                    {"warning css_eof_in_comment"},
		"\xfe\xff\x00.\x00a":                         {"error css_encoding", "warning css_invalid", "warning css_replacement_character"},
		"@charset \"iso-8859-1\"; .a{b:c}":           {"warning css_charset"},
		"@media x{a:hover{c:d}} .a{b:{} !important}": nil,
		"\xef\xbb\xbf@import url(x.css);\n.a{b:c}":   nil,
	} {
		if got := codes(src); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", src, got, want)
		}
	}
	// a UTF-8 BOM is removed: the @import is read, at its own offset
	ex := extract(t, "a.css", "\xef\xbb\xbf@import url(x.css);\n.a{b:c}")
	if len(ex.References) != 1 || ex.References[0].Location.Range.Start.Column != 12 {
		t.Errorf("after a BOM: %+v", ex.References)
	}
}

func TestDeterministic(t *testing.T) {
	src := "@keyframes k{} .a{animation:1s k;--x:1;color:var(--x)} @import 'b.css';"
	if a, b := extract(t, "a.css", src), extract(t, "a.css", src); !reflect.DeepEqual(a, b) {
		t.Error("two extractions differ")
	}
}

// Worst cases (docs/scripts/css-parser-eval/run.sh): linear time, nesting
// bounded, no stack exhaustion.
func TestWorstCases(t *testing.T) {
	cases := map[string]string{
		"media32000":       "@media x{" + strings.Repeat("div{color:red}\n", 32000) + "}",
		"nest4000":         strings.Repeat("a{", 4000),
		"declnest4000":     ".x{" + strings.Repeat("a:b{", 4000),
		"custom4000":       ".x{" + strings.Repeat("--a:{", 4000),
		"paren1m":          ".x{a:" + strings.Repeat("(", 1000000),
		"brace1m":          strings.Repeat("{", 1000000),
		"blockvalue":       ".x{" + strings.Repeat("a:{", 250) + strings.Repeat("b ", 400000) + strings.Repeat("}x", 250) + "}",
		"blockvalue_rules": ".x{" + strings.Repeat("a:{", 200) + strings.Repeat("p{c:d}", 20000) + strings.Repeat("} b", 200) + "}",
	}
	for name, src := range cases {
		start := time.Now()
		ex := extract(t, name+".css", src)
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("%s: %v", name, d)
		}
		if len(ex.Symbols) == 0 {
			t.Errorf("%s: no stylesheet", name)
		}
	}
	// past the bound: reported, contents unread
	found := false
	for _, d := range extract(t, "a.css", strings.Repeat("a{", 300)).Diagnostics {
		found = found || d.Code == "css_too_deep"
	}
	if !found {
		t.Error("nesting past the bound is not reported")
	}
}

// Reviewer B (Phase 6): the :global mode inside pseudo-class arguments,
// !important, the last font-family descriptor and src, @import's place,
// at-rules nested in style rules, U+FFFD written as such, @charset labels.
func TestReviewB_NoFalseStrong(t *testing.T) {
	classes := func(src string) []string {
		var out []string
		for _, s := range extract(t, "a.module.css", src).Symbols {
			if s.Kind == "css_class" {
				out = append(out, s.Name)
			}
		}
		return out
	}
	for src, want := range map[string][]string{
		`:global .x :is(.a, .b) .c { x: y }`:     nil, // all global: :is inherits the mode
		`:global .x :where(.a,.b) .c { x: y }`:   nil,
		`:global .p :not(:local .q) .r { x: y }`: {"q"},      // .r stays global
		`.p :not(:global .q) .r { x: y }`:        {"p", "r"}, // .r stays local
		`.p, :global .q, .r { x: y }`:            {"p", "r"}, // a "," ends :global
		`:is(.a, :global .b) .c { x: y }`:        {"a", "c"},
	} {
		if got := classes(src); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: classes %q, want %q", src, got, want)
		}
	}
	got := resolveAll(t, map[string]string{"a.module.css": `:global .x :is(.a, .b) .c { x: y } .t { composes: b c }`})
	check(t, got, []want{{"a.module.css:1:b", "unresolved", ""}, {"a.module.css:1:c", "unresolved", ""}}, nil)

	got = resolveAll(t, map[string]string{
		"a.css": strings.Join([]string{
			`@keyframes important {} @keyframes spin {}`,                             // 1
			`.a { animation: 1s ease !important; animation: spin 1s ! important; }`,  // 2
			`.b { animation-name: spin !important; font-family: Inter !IMPORTANT; }`, // 3
			`@font-face { font-family: A; font-family: B; src: url(x.woff) }`,        // 4
			`@font-face { font-family: Inter; src: url(i.woff) } @font-face { font-family: NoSrc; }`,
			`.c { font-family: A; font-family: B; font-family: NoSrc; }`, // 6
			`.d { @keyframes nested {} @font-face { font-family: N; src: url(n.woff) } animation-name: nested; font-family: N; }`,
			`@media print { @keyframes inmedia {} } .e { animation-name: inmedia; }`, // 8
		}, "\n"),
	})
	check(t, got, []want{
		{"a.css:2:spin", "strong", "a.css @keyframes spin"},
		{"a.css:3:spin", "strong", "a.css @keyframes spin"},
		{"a.css:3:inter", "strong", "a.css @font-face inter"},
		{"a.css:6:a", "unresolved", ""},
		{"a.css:6:b", "strong", "a.css @font-face b"},
		{"a.css:6:nosrc", "unresolved", ""},
		{"a.css:7:nested", "unresolved", ""},
		{"a.css:7:n", "unresolved", ""},
		{"a.css:8:inmedia", "strong", "a.css @keyframes inmedia"},
	}, []string{"a.css:2:important", "a.css:2:ease"})

	got = resolveAll(t, map[string]string{
		"s/a.css":    "@charset \"utf-8\"; @layer base, x; @import \"ok.css\";\n.a {} @import \"late.css\";\n@media print { @import \"n.css\"; }\n",
		"s/ok.css":   ``,
		"s/late.css": ``,
		"s/n.css":    ``,
	})
	check(t, got, []want{{"s/a.css:1:ok.css", "strong", "s/ok.css"}}, []string{"s/a.css:2:late.css", "s/a.css:3:n.css"})

	// Phase 9 (F9B-3, F9B-4): a @layer statement after an @import is a rule
	// between @imports; an @import with a block is no @import.
	got = resolveAll(t, map[string]string{
		"t/a.css": "@import \"x.css\";\n@layer l;\n@import \"y.css\";\n",
		"t/b.css": "@import \"z.css\" { }\n@import \"x.css\";\n",
		"t/x.css": ``, "t/y.css": ``, "t/z.css": ``,
	})
	check(t, got, []want{{"t/a.css:1:x.css", "strong", "t/x.css"}, {"t/b.css:2:x.css", "strong", "t/x.css"}}, []string{"t/a.css:3:y.css", "t/b.css:1:z.css"})

	for src, want := range map[string]int{
		".a { content: \"\uFFFD\" } .b { content: \"\\FFFD\" }": 0, // U+FFFD written: no replacement
		".a { content: \"x\x00y\" }":                            1,
		"@charset \"utf8\"; .a{}":                               0, // a UTF-8 label
	} {
		n := 0
		for _, d := range extract(t, "a.css", src).Diagnostics {
			if d.Code == "css_replacement_character" || d.Code == "css_charset" {
				n++
			}
		}
		if n != want {
			t.Errorf("%q: %d diagnostics, want %d", src, n, want)
		}
	}
	// a multi-word family is located over all its words
	ex := extract(t, "a.css", ".a { font-family: Open  Sans, serif; }")
	if len(ex.References) != 1 || ex.References[0].Location.Range.Start.Column != 19 || ex.References[0].Location.Range.End.Column != 29 {
		t.Errorf("Open Sans: %+v", ex.References)
	}
}

// Reviewer C (hardening Phase F): image-set() names an image by a string at
// the start of an argument only — not one inside a nested function — and
// one the input's end cuts off still counts.
func TestImageSetURLs(t *testing.T) {
	var got []string
	for _, src := range []string{
		`.a{b:image-set(foo(1, "nested.png") 1x, "ok.png" 2x)}`,
		`.a{b:image-set("cut.png"`,
		`.a{b:-webkit-image-set(url(u.png) 1x, "s.png" type("image/png"))}`,
	} {
		for _, r := range extract(t, "d/s.css", src).References {
			if r.TargetKinds == "asset" {
				got = append(got, r.NameQualified)
			}
		}
	}
	if want := []string{"d/ok.png", "d/cut.png", "d/s.png", "d/u.png"}; !reflect.DeepEqual(got, want) {
		t.Errorf("image-set URLs %q, want %q", got, want)
	}
}
