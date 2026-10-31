package tomldoc

import (
	"testing"

	bstoml "github.com/BurntSushi/toml"
	gotoml "github.com/pelletier/go-toml/v2"
)

// FuzzParse: on any input Parse never panics, every problem and node lies
// within the source, every node lies within its parent's bytes, and the
// verdict agrees with the decoders: a document Parse reads without an
// error go-toml accepts (BurntSushi/toml accepts some TOML forbids), and
// one both accept Parse reads without an error (what nests too deep for
// the decoder to check aside).
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"a = 1\n[b.c]\nd = [1, {e = 2}]\n[[f]]\ng.h = \"\"\"\nx\"\"\"\n",
		"a.b = 1\n[a]\n", "[a.b.c]\n[a]\nb.d = 1\n", "a = {b = {}, b.c = 1}\n",
		"[[a]]\n[a]\n", "x = {a = 1,\n b = 2,}\n", "d = 1985-06-18 17:04:07+12:60\n",
		"'é' = 1\r\n\"\\u0000\" = 2\n", "[a]\nb.c = 1\n[a.b.d]\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<14 {
			return
		}
		d := Parse(src)
		hasErr, checked := false, true
		for _, p := range d.Problems {
			hasErr = hasErr || p.Error
			checked = checked && p.Code != CodeNotChecked
			if p.Start < 0 || p.Start > p.End || p.End > len(src) {
				t.Fatalf("problem %s at [%d,%d) of %d", p.Code, p.Start, p.End, len(src))
			}
		}
		stack := []*Node{d.Root}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, c := range n.Children {
				if c.Span.Start < 0 || c.Span.Start > c.Span.End || c.Span.End > len(src) {
					t.Fatalf("node %q at %v of %d", c.Key, c.Span, len(src))
				}
				if c.Span.Start < n.Span.Start || c.Span.End > n.Span.End {
					t.Fatalf("node %q at %v outside its parent %q at %v", c.Key, c.Span, n.Key, n.Span)
				}
				stack = append(stack, c)
			}
		}
		var m map[string]any
		goErr := gotoml.Unmarshal(src, &m)
		if !hasErr && checked && goErr != nil {
			t.Fatalf("Parse reads %q without an error, go-toml rejects it: %v", src, goErr)
		}
		var m2 map[string]any
		_, bsErr := bstoml.Decode(string(src), &m2)
		if hasErr && goErr == nil && bsErr == nil {
			t.Fatalf("both decoders accept %q, Parse reports %v", src, d.Problems)
		}
	})
}
