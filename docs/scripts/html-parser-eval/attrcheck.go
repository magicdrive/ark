//go:build ignore

// attrcheck tokenizes each file with x/net/html, recomputes every start
// tag's attribute spans with an HTML5 attribute scanner over Raw(), and
// checks the spans against the tokenizer's own attributes (lower-cased
// name, unescaped value). It prints per-file counts and every mismatch.
package main

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/net/html"
)

type attr struct {
	name           string
	ns, ne, vs, ve int // byte spans within raw; vs = -1 without a value
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// scanAttrs follows the HTML5 tokenizer's states from after the tag name
// (§13.2.5.32 before attribute name … §13.2.5.39 after attribute value
// (quoted)) over a start tag's raw bytes.
func scanAttrs(raw []byte) []attr {
	i := 1 // after '<'
	for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++ // tag name
	}
	var out []attr
	for i < len(raw) {
		// before attribute name
		for i < len(raw) && (isSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) || raw[i] == '>' {
			break
		}
		// attribute name: '=' as the first character is part of the name
		ns := i
		if raw[i] == '=' {
			i++
		}
		for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' && raw[i] != '=' {
			i++
		}
		a := attr{name: asciiLower(string(raw[ns:i])), ns: ns, ne: i, vs: -1, ve: -1}
		// after attribute name
		j := i
		for j < len(raw) && isSpace(raw[j]) {
			j++
		}
		if j < len(raw) && raw[j] == '=' {
			j++
			for j < len(raw) && isSpace(raw[j]) {
				j++
			}
			switch {
			case j < len(raw) && (raw[j] == '"' || raw[j] == '\''):
				q := raw[j]
				k := bytes.IndexByte(raw[j+1:], q)
				if k < 0 {
					a.vs, a.ve = j+1, len(raw)
					j = len(raw)
				} else {
					a.vs, a.ve = j+1, j+1+k
					j = j + 2 + k
				}
			case j < len(raw) && raw[j] == '>':
				a.vs, a.ve = j, j // missing value: empty
			default:
				s := j
				for j < len(raw) && !isSpace(raw[j]) && raw[j] != '>' {
					j++
				}
				a.vs, a.ve = s, j
			}
			i = j
		}
		out = append(out, a)
	}
	return out
}

var shift = 0

func main() {
	if os.Getenv("SHIFT") != "" {
		shift = 1 // mutation: every value span one byte late
	}
	tags, attrs, mism, files := 0, 0, 0, 0
	for _, f := range os.Args[1:] {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		files++
		z := html.NewTokenizer(bytes.NewReader(src))
		off := 0
		for {
			tt := z.Next()
			raw := append([]byte(nil), z.Raw()...) // Token() unescapes in place
			start := off
			off += len(raw)
			if tt == html.ErrorToken {
				if off != len(src) {
					fmt.Printf("SHORT %s %d/%d\n", f, off, len(src))
				}
				break
			}
			if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
				continue
			}
			tags++
			var want []html.Attribute
			for _, a := range z.Token().Attr {
				want = append(want, a)
			}
			got := scanAttrs(raw)
			// x/net drops duplicate attributes? It keeps them all (the
			// tokenizer); compare in order.
			ok := len(got) == len(want)
			for i := 0; ok && i < len(got); i++ {
				rv := ""
				if got[i].vs >= 0 {
					if got[i].ve+shift <= len(raw) {
						rv = string(raw[got[i].vs+shift : got[i].ve+shift])
					}
				}
				// attrValue: x/net's newline conversion and attribute-mode
				// character references (attrlib.go).
				if got[i].name != want[i].Key || attrValue(rv) != want[i].Val {
					ok = false
				}
			}
			attrs += len(want)
			if !ok {
				mism++
				if mism <= 15 {
					fmt.Printf("MISMATCH %s @%d raw=%q want=%v got=%v\n", f, start, raw, want, got)
				}
			}
		}
	}
	fmt.Printf("files=%d tags=%d attrs=%d mismatched_tags=%d\n", files, tags, attrs, mism)
}
