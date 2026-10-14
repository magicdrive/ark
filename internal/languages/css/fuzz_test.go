package css

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractCSS verifies the CSS provider on arbitrary input: it never
// panics, keeps every symbol, reference and item inside the source, is
// deterministic, and reads nesting no deeper than maxDepth.
func FuzzExtractCSS(f *testing.F) {
	seeds := []string{
		"",
		"@keyframes spin { to { x: y } } .a { animation: 1s spin; font-family: \"Inter\", serif; color: var(--x, var(--y)); }",
		"@font-face { font-family: Open Sans; } @property --a { syntax: '*'; } :root { --a: { b } ; }",
		".root, :global(.x) .a :local(.b) { composes: c d from './e.module.css'; } .c{}",
		"@import url(./a.css) layer(x); @import 'b.css' print; @charset \"iso-8859-1\";",
		".a { b: url(bad url); c: \"x\ny; } .d { e: f\n.g{h:i}}",
		"\xef\xbb\xbf.a{b:c} \xfe\xff \x00\xe2\x82 .\\31 23{} @keyframes ✨x{}",
		strings.Repeat("a{", 300),
		".x{a:" + strings.Repeat("(", 300),
		"color: red; .x{y:z} } } .k { l: m } /* open",
		".a { background: url(i.png), image-set(\"a.webp\" 1x, url(b.png) 2x); src: src(\"f.woff2\") } @font-face { src: url( x.ttf ) }",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		for _, file := range []string{"a/b.css", "a/b.module.css"} {
			ext, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ext, again) {
				t.Fatal("two extractions differ")
			}
			n := uint32(len(src))
			for _, s := range ext.Symbols {
				if s.Name == "" || s.Qualified == "" || s.StartByte > s.EndByte || s.EndByte > n || s.Location.Range.Start.Line == 0 {
					t.Fatalf("bad symbol %+v (len %d)", s, n)
				}
			}
			lastLine := uint32(strings.Count(string(src), "\n") + 1)
			for _, r := range ext.References {
				l := r.Location.Range
				if r.Name == "" || r.NameQualified == "" || !r.IdentityInRepository || l.Start.Line == 0 ||
					l.End.Line > lastLine || l.Start.Line > l.End.Line || l.Start.Line == l.End.Line && l.Start.Column > l.End.Column {
					t.Fatalf("bad reference %+v", r)
				}
			}
		}
		r := read(src)
		eof := map[int]bool{}    // starts of the blocks reported open
		closes := map[int]bool{} // ends of the } tokens
		for _, tk := range r.toks {
			if tk.typ == tRBrace {
				closes[tk.end] = true
			}
		}
		for _, it := range r.items {
			if it.start < 0 || it.start > it.end || it.end > len(src) || it.depth >= maxDepth {
				t.Fatalf("bad item %+v (len %d)", it, len(src))
			}
			if it.kind == itemProblem && it.name == "eof_in_block" {
				eof[it.start] = true
			}
		}
		// Incomplete syntax is never taken for complete: every block either
		// ends at its } or is reported open.
		for _, it := range r.items {
			if (it.kind == itemRule || it.kind == itemAtRule) && it.block && !closes[it.end] && !eof[it.start] {
				t.Fatalf("a block neither closed nor reported open: %+v", it)
			}
		}
	})
}
