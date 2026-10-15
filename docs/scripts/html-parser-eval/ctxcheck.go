//go:build ignore

// ctxcheck compares the attributes the x/net tokenizer reports with those of
// html.Parse's tree, as (element, attribute, value) multisets (later
// duplicate attributes dropped on both sides, names ASCII-lower-cased).
//
//	ctxcheck plain|sticky|verified|ids FILE...
//
// plain: the bare tokenizer. sticky: AllowCDATA(true) from the first <svg> /
// <math> start tag on. verified: the bare tokenizer, keeping only the
// attributes attrlib.go's verify finds on an element their own tag
// produced; it also checks that each kept (element, attribute, value) is in
// the tree as often as kept. With FAKE set, it counts the kept attributes
// whose value contains "fake" (hand-written counterexamples mark so the
// attributes a browser does not see).
package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type triple struct{ element, attr, value string }

type tokens struct {
	attrs map[triple]int
	tags  []tag
	names []string // element of each tag
}

func tokenize(src []byte, mode string) tokens {
	out := tokens{attrs: map[triple]int{}}
	z := html.NewTokenizer(bytes.NewReader(src))
	foreign := false
	off := 0
	for {
		tt := z.Next()
		raw := append([]byte(nil), z.Raw()...)
		start := off
		off += len(raw)
		if tt == html.ErrorToken {
			return out
		}
		t := z.Token()
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			if t.Data == "svg" || t.Data == "math" {
				foreign = true
			}
			names := make([]string, len(t.Attr))
			for i, a := range t.Attr {
				names[i] = a.Key
			}
			keep := firstOnly(names)
			var attrs []html.Attribute
			for i, a := range t.Attr {
				if keep[i] {
					attrs = append(attrs, a)
					out.attrs[triple{t.Data, asciiLower(a.Key), a.Val}]++
				}
			}
			nameEnd := 1
			for nameEnd < len(raw) && !isSpace(raw[nameEnd]) && raw[nameEnd] != '/' && raw[nameEnd] != '>' {
				nameEnd++
			}
			out.tags = append(out.tags, tag{start: start, nameEnd: start + nameEnd, name: t.Data, attrs: attrs})
			out.names = append(out.names, t.Data)
		}
		if mode == "sticky" && foreign {
			z.AllowCDATA(true)
		}
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func parse(src []byte) map[triple]int {
	out := map[triple]int{}
	doc, err := html.Parse(bytes.NewReader(src))
	if err != nil {
		return out
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			names := make([]string, len(n.Attr))
			for i, a := range n.Attr {
				names[i] = a.Key
				if a.Namespace != "" {
					names[i] = a.Namespace + ":" + a.Key
				}
			}
			keep := firstOnly(names)
			for i, a := range n.Attr {
				if keep[i] {
					out[triple{asciiLower(n.Data), asciiLower(names[i]), a.Val}]++
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func main() {
	mode := os.Args[1]
	if mode == "ids" {
		// The verified id attributes, for the HTML provider's differential
		// test (internal/languages/html, TestHarnessAgreement):
		// "<file>\t<tag offset>\t<id>\t<elements of the tree with it>"
		// per line.
		for _, f := range os.Args[2:] {
			src, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			tok := tokenize(src, "plain")
			trusted, ok := verify(src, tok.tags)
			if !ok {
				continue
			}
			// The elements of the tree carrying each id value (an
			// element the tokenizer did not show counts too).
			inTree := map[string]int{}
			if doc, err := html.Parse(bytes.NewReader(src)); err == nil {
				var walk func(n *html.Node)
				walk = func(n *html.Node) {
					if n.Type == html.ElementNode {
						for _, a := range n.Attr {
							if a.Namespace == "" && asciiLower(a.Key) == "id" {
								inTree[strings.ReplaceAll(a.Val, "\x00", "\uFFFD")]++
								break
							}
						}
					}
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c)
					}
				}
				walk(doc)
			}
			// An ignored tag's id may be an element's in a browser
			// (x/net parses select by the former standard): it counts.
			for i, t := range tok.tags {
				if !lastUnseen[i] {
					continue
				}
				for _, a := range t.attrs {
					if asciiLower(a.Key) == "id" {
						inTree[strings.ReplaceAll(a.Val, "\x00", "\uFFFD")]++
					}
				}
			}
			for i, t := range tok.tags {
				for j, a := range t.attrs {
					if trusted[i][j] && asciiLower(a.Key) == "id" {
						fmt.Printf("%s\t%d\t%s\t%d\n", f, t.start, strconv.Quote(a.Val), inTree[a.Val])
					}
				}
			}
		}
		return
	}
	files, onlyTok, onlyTree := 0, 0, 0
	unverifiedFiles, kept, dropped, fakes := 0, 0, 0, 0
	for _, f := range os.Args[2:] {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		tok, tr := tokenize(src, mode), parse(src)
		var d []string
		if mode == "verified" {
			trusted, ok := verify(src, tok.tags)
			if !ok {
				unverifiedFiles++
				d = append(d, "markers changed the tree")
			}
			got := map[triple]int{}
			for i, t := range tok.tags {
				for j, a := range t.attrs {
					k := triple{tok.names[i], asciiLower(a.Key), a.Val}
					if !trusted[i][j] {
						dropped++
						if os.Getenv("WHY") != "" {
							d = append(d, fmt.Sprintf("-drop %q", k))
						}
						continue
					}
					kept++
					got[k]++
					if os.Getenv("FAKE") != "" && strings.Contains(a.Val, "fake") {
						fakes++
						d = append(d, fmt.Sprintf("FAKE %q", k))
					}
				}
			}
			for k, n := range got {
				// the tree's element names are adjusted (linearGradient, image → img)
				el := asciiLower(k.element)
				if el == "image" {
					el = "img"
				}
				if r := tr[triple{el, k.attr, k.value}]; r < n {
					onlyTok += n - r
					d = append(d, fmt.Sprintf("+tok %q×%d", k, n-r))
				}
			}
		} else {
			for k, n := range tok.attrs {
				if tr[k] < n {
					onlyTok += n - tr[k]
					d = append(d, fmt.Sprintf("+tok %q×%d", k, n-tr[k]))
				}
			}
			for k, n := range tr {
				if tok.attrs[k] < n {
					onlyTree += n - tok.attrs[k]
					d = append(d, fmt.Sprintf("+tree %q×%d", k, n-tok.attrs[k]))
				}
			}
		}
		if len(d) > 0 {
			files++
			sort.Strings(d)
			fmt.Printf("DIFF %s %s\n", f, strings.Join(d, " "))
		}
	}
	if mode == "verified" {
		fmt.Printf("mode=verified files_reported=%d unverified_files=%d kept_attrs=%d dropped_attrs=%d kept_not_in_tree=%d kept_fake=%d\n",
			files, unverifiedFiles, kept, dropped, onlyTok, fakes)
		return
	}
	fmt.Printf("mode=%s files_differing=%d only_tokenizer=%d only_tree=%d\n", mode, files, onlyTok, onlyTree)
}
