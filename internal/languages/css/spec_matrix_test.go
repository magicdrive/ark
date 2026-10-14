package css

import (
	"fmt"
	"strings"
	"testing"
)

// The CSS Syntax Level 3 matrix (hardening Phase D): each input's rules,
// at-rules, declarations and problems, with their nesting depth, as the
// specification's algorithms (Editor's Draft: consume a stylesheet's
// contents, a block's contents, a qualified rule, an at-rule, a
// declaration) produce them: each case's output was checked against those
// algorithms, one by one, before it was fixed here.

func matrixEvents(src string) string {
	r := read([]byte(src))
	var out []string
	for _, it := range r.items {
		switch it.kind {
		case itemDecl:
			out = append(out, fmt.Sprintf("%d decl %s", it.depth, it.name))
		case itemRule:
			out = append(out, fmt.Sprintf("%d rule{", it.depth))
		case itemAtRule:
			m := ";"
			if it.block {
				m = "{"
			}
			out = append(out, fmt.Sprintf("%d @%s%s", it.depth, it.name, m))
		case itemProblem:
			out = append(out, fmt.Sprintf("%d !%s", it.depth, it.name))
		}
	}
	return strings.Join(out, " | ")
}

func TestSpecMatrix(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"@media", "@media (min-width: 30em) { .a { color: red } }", "0 @media{ | 1 rule{ | 2 decl color"},
		{"@container", "@container card (width > 400px) { .b { x: y } }", "0 @container{ | 1 rule{ | 2 decl x"},
		{"range query", "@media (400px <= width <= 700px) { a { b: c } }", "0 @media{ | 1 rule{ | 2 decl b"},
		{"@supports", "@supports (display: grid) and (not (display: inline-grid)) { a { d: e } }", "0 @supports{ | 1 rule{ | 2 decl d"},
		{"@layer statement and block", "@layer base, components; @layer base { a { b: c } }", "0 @layer; | 0 @layer{ | 1 rule{ | 2 decl b"},
		{"@property descriptors", "@property --x { syntax: '<length>'; inherits: false; initial-value: 0px }", "0 @property{ | 1 decl syntax | 1 decl inherits | 1 decl initial-value"},
		{"@keyframes", "@keyframes spin { from { a: b } 50% { c: d } }", "0 @keyframes{ | 1 rule{ | 2 decl a | 1 rule{ | 2 decl c"},
		{"@font-face", `@font-face { font-family: X; src: url(x.woff2) format("woff2") }`, "0 @font-face{ | 1 decl font-family | 1 decl src"},
		{"nesting", ".a { color: red; &:hover { color: blue } .b & { c: d } @media (x) { e: f } }", "0 rule{ | 1 decl color | 1 rule{ | 2 decl color | 1 rule{ | 2 decl c | 1 @media{ | 2 decl e"},
		{"nested ident rule", "a { b { c: d } }", "0 rule{ | 1 rule{ | 2 decl c"},
		{"custom properties", ":root { --x: { a: b }; --y: ; --z: calc(1px + 2px) }", "0 rule{ | 1 decl --x | 1 decl --y | 1 decl --z"},
		{"var and calc", "a { width: calc(100% - var(--g, 10px)) }", "0 rule{ | 1 decl width"},
		{"url forms", `a { b: url(x.png); c: url( "y.png" ) }`, "0 rule{ | 1 decl b | 1 decl c"},
		{"escaped identifiers", `.\31 23 { \66 oo: bar }`, "0 rule{ | 1 decl foo"},
		{"non-ASCII identifiers", ".日本 { 色: 赤 }", "0 rule{ | 1 decl 色"},
		{"string escapes", `a { content: "a\"b" }`, "0 rule{ | 1 decl content"},
		{"comments", "/* x */ a /* y */ { /* z */ b: c /* w */ }", "0 rule{ | 1 decl b"},
		{"importance", "a { b: c !important; d: e ! IMPORTANT }", "0 rule{ | 1 decl b | 1 decl d"},
		// A block's contents: ";" is skipped; "d }" is no declaration and,
		// read as a nested qualified rule, ends at "}" with nothing.
		{"malformed declaration", "a { b: c; ; d }", "0 rule{ | 1 decl b | 1 !invalid"},
		{"EOF in string", `a { b: "c`, "0 !eof_in_string | 0 rule{ | 1 decl b | 0 !eof_in_block"},
		{"EOF in comment", "a { b: c } /* x", "0 !eof_in_comment | 0 rule{ | 1 decl b"},
		{"EOF in block", "a{b:c}@media screen{", "0 rule{ | 1 decl b | 0 @media{ | 0 !eof_in_block"},
		// At the top level a "}" is part of the next qualified rule's prelude.
		{"unexpected }", "a { b: c } } d { e: f }", "0 rule{ | 1 decl b | 0 !stray_in_prelude | 0 rule{ | 1 decl e"},
		{"unexpected }}", "a { b: c; d: e }}} f { g: h }", "0 rule{ | 1 decl b | 1 decl d | 0 !stray_in_prelude | 0 rule{ | 1 decl g"},
		{"@import", "@import url(x.css); a { b: c }", "0 @import; | 0 rule{ | 1 decl b"},
		// CDO and CDC are skipped at the top level of a stylesheet.
		{"CDO / CDC", "a { b: c } <!-- d { e: f } -->", "0 rule{ | 1 decl b | 0 rule{ | 1 decl e"},
	} {
		if got := matrixEvents(c.src); got != c.want {
			t.Errorf("%s: %q\n got %s\nwant %s", c.name, c.src, got, c.want)
		}
	}
	// Deep nesting: the reader stops at its depth limit, reports it, and
	// never exceeds it.
	deep := matrixEvents(strings.Repeat("a{", 300) + strings.Repeat("}", 300))
	if !strings.Contains(deep, "!too_deep") || strings.Contains(deep, "257 rule{") {
		t.Errorf("deep nesting: %s", deep[:200])
	}
}
