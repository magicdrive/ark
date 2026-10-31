package markdown

import (
	"context"
	"reflect"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractMarkdown: on any input, extraction never panics or fails;
// every range lies within the source and is ordered, a symbol's bytes are
// its location's, every name is a name (declname.IsName), every parent and
// container is a declaration of the file, every qualified name is unique
// within its kind but for anchors (an id may repeat; the index numbers its
// repeats), and a second extraction is the same.
func FuzzExtractMarkdown(f *testing.F) {
	for _, s := range []string{
		"# A\n\ntext [l](b.md#x) ![i](i.png) <https://x.y/z?q#f>\n\n## B\n```go\nx\n```\n",
		"Setext\n===\n\n> # Quoted\n> [r][d]\n\n[d]: <a b.md> 'title'\n",
		"- # item\n  ```\n  code\n  ```\n\n<div id=\"x\">\n\n[x](#x)\n\n</div>\n",
		"# a\r\n## a\r\n### a\r\n[a](#a-1)\r\n",
		"[x]: /root.md\n[x]\n[^1]\n\n[^1]: n\n| a | b |\n|---|---|\n| [c](c.md) | ~~d~~ |\n",
		"# \xff\x00\n[\xff](\xfe.md)\n``` \xff\n```\n",
		"![a ![b](b.png)](c.png) [a [b](b.md)](c.md) [`](x.md)`](y.md)\n",
		"\xef\xbb\xbf# T\n```go\n```\n```go\n```\n## password = hunter2secretvalue\n[x](/b.md#t)\n",
		"|a|b|c|\n|-|-|-|\nx\n[d]: /u \"t\nt\"\n" + "\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t- a\n",
	} {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<16 {
			return
		}
		for _, file := range []string{"docs/a.md", `docs\a.md`} {
			ex, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				t.Fatal(err)
			}
			checkExtraction(t, string(src), ex)
			l := newLines(src)
			seen := map[string]bool{}
			for _, s := range ex.Symbols {
				if s.Location.Range.Start != l.pos(int(s.StartByte)) || s.Location.Range.End != l.pos(int(s.EndByte)) {
					t.Fatalf("%s: symbol %s location %v is not its bytes' [%d,%d)", file, s.Qualified, s.Location.Range, s.StartByte, s.EndByte)
				}
				key := string(s.Kind) + " " + s.Qualified
				if seen[key] && s.Kind != "anchor" {
					t.Fatalf("%s: %s declared twice", file, key)
				}
				seen[key] = true
			}
			for _, d := range ex.Diagnostics {
				if d.Code == "" {
					t.Fatalf("%s: diagnostic without a code: %s", file, d.Message)
				}
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ex, again) {
				t.Fatalf("%s: two extractions differ", file)
			}
		}
	})
}
