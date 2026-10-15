//go:build ignore

package main

// Shared by attrcheck and ctxcheck (run.sh copies it next to each): the
// rules an HTML provider built on the x/net tokenizer needs to state
// attributes as the HTML standard does.

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// asciiLower lower-cases ASCII letters only, as x/net and the standard do
// for tag and attribute names.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// convertNewlines normalizes CR LF and CR to LF (x/net's TagAttr does).
func convertNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// attrValue is the value x/net's TagAttr reports for the raw value text:
// newlines normalized, then character references decoded in attribute
// mode (HTML §13.2.5.72–73): a named reference decodes only when the whole
// alphanumeric run (with its ';', if any) is one; one without ';' followed
// by '=' stays raw; no longest-prefix match as in text.
func attrValue(raw string) string {
	s := convertNewlines(raw)
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		n, dec := reference(s[i:])
		b.WriteString(dec)
		i += n
	}
	return b.String()
}

// reference decodes the character reference at the start of s ('&'),
// returning the bytes consumed and the decoded text.
func reference(s string) (int, string) {
	if len(s) <= 1 {
		return 1, "&"
	}
	i := 1
	if s[1] == '#' {
		if len(s) <= 3 {
			return 1, "&"
		}
		i = 2
		hex := s[i] == 'x' || s[i] == 'X'
		if hex {
			i++
		}
		digits := i
		for i < len(s) && (isDigit(s[i]) || hex && isHex(s[i])) {
			i++
		}
		if i == digits {
			// x/net (not the standard) reads "&#x;" as a reference to
			// U+0000, replaced by U+FFFD.
			if hex && i < len(s) && s[i] == ';' {
				return i + 1, "\uFFFD"
			}
			return 1, "&"
		}
		if i < len(s) && s[i] == ';' {
			i++
		}
		// The numeric decoding (ranges, Windows-1252, U+FFFD) is the same
		// in text and attributes.
		return i, html.UnescapeString(s[:i])
	}
	for i < len(s) && isAlnum(s[i]) {
		i++
	}
	if i < len(s) && s[i] == ';' {
		i++
	}
	name := s[1:i]
	switch {
	case name == "":
		return i, s[:i]
	case name[len(name)-1] != ';' && i < len(s) && s[i] == '=':
		return i, s[:i]
	}
	// html.UnescapeString decodes the entity the whole name is, or else (in
	// text, not in attributes) the longest prefix of at least two
	// characters that is one, leaving the rest of the name: the decoding is
	// exact unless it is such a prefix's.
	dec := html.UnescapeString(s[:i])
	if dec == s[:i] {
		return i, s[:i]
	}
	for j := len(name) - 1; j > 1; j-- {
		p := "&" + name[:j]
		if d := html.UnescapeString(p); d != p && dec == d+name[j:] {
			return i, s[:i]
		}
	}
	return i, dec
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
func isHex(c byte) bool   { return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }
func isAlnum(c byte) bool { return isDigit(c) || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// firstOnly drops an attribute whose ASCII-lower-cased name an earlier one
// of the tag already has (§13.2.5.33: the later one is removed). x/net's
// tokenizer and parser both keep it.
func firstOnly(names []string) []bool {
	keep := make([]bool, len(names))
	seen := map[string]bool{}
	for i, n := range names {
		k := asciiLower(n)
		keep[i] = !seen[k]
		seen[k] = true
	}
	return keep
}

// tag is a start tag the tokenizer reported: its byte offset, the end of
// its name (where a marker attribute is inserted), its name and its
// attributes after firstOnly.
type tag struct {
	start, nameEnd int
	name           string
	attrs          []html.Attribute
}

// markerPrefix returns the name prefix of verify's marker attributes: one
// the source does not hold, so a marker is never confused with the
// document's own text or attributes.
func markerPrefix(src []byte) string {
	for i := 0; ; i++ {
		p := "data-ark-tok-"
		if i > 0 {
			p = "data-ark-tok" + strconv.Itoa(i) + "-"
		}
		if !bytes.Contains(bytes.ToLower(src), []byte(p)) {
			return p
		}
	}
}

// lastUnseen, set by verify, tells per tag whether its marker is nowhere in
// the marked tree: a tag the tree builder ignored.
var lastUnseen []bool

// marker is the attribute verify inserts into tag n, after its name. Its
// name is none the tree builder reads (type, color, face, size, encoding);
// its value and the space after it end it, so what follows reads as before
// (after a bare name, " =b" would be its value; an unquoted value would
// take a following "/"). It holds no quote: a fake tag may lie inside a
// quoted attribute value of the tree builder's.
func marker(prefix string, n int) string { return " " + prefix + strconv.Itoa(n) + "=1 " }

// verify reports, for each attribute of each tag, whether html.Parse's tree
// has it on an element the tag itself produced. The tokenizer decides raw
// text by the start tag alone, the tree builder by its insertion mode, so
// the tokenizer may report a tag the tree builder reads as text, an
// attribute value or a comment (a fake), or one it ignores, or whose
// attributes it merges into another element (html, body).
//
// It parses the source again with a marker attribute inserted into every
// reported tag. An attribute of tag n is used only when
//   - an element carrying marker n (one tag n produced, or its clone) is
//     named as tag n (a marker can land in another tag the tokenizer read
//     as raw text: `<script><a t="</script>"<rect id=x>` gives the svg-less
//     <a> the attributes `<rect` and id), and
//   - that element's first attribute of the name has the value, and no
//     earlier tag whose marker the element carries has an attribute of the
//     name (html and body take a later tag's attribute only when they have
//     none of its name), and
//   - neither its name nor its value holds NUL (the standard replaces it,
//     x/net does not).
//
// A marker in text, a comment or an attribute value is a fake. The markers
// must not change how the tree is built: the marked tree, markers removed,
// must equal the tree of the source; otherwise nothing is verified (ok
// false). The tree builder reads a few attributes (Noah's Ark compares
// formatting elements' attributes), so this is checked, not assumed.
func verify(src []byte, tags []tag) (trusted [][]bool, ok bool) {
	trusted = make([][]bool, len(tags))
	for i, t := range tags {
		trusted[i] = make([]bool, len(t.attrs))
	}
	prefix := markerPrefix(src)
	var b bytes.Buffer
	last := 0
	for i, t := range tags {
		b.Write(src[last:t.nameEnd])
		b.WriteString(marker(prefix, i))
		last = t.nameEnd
	}
	b.Write(src[last:])
	orig, err1 := html.Parse(bytes.NewReader(src))
	marked, err2 := html.Parse(bytes.NewReader(b.Bytes()))
	if err1 != nil || err2 != nil {
		return trusted, false
	}
	markerRE := regexp.MustCompile(" " + regexp.QuoteMeta(prefix) + "[0-9]+=1 ")
	produced := map[int][]*html.Node{}
	markers := map[*html.Node][]int{} // the markers an element carries, ascending
	var strip func(n *html.Node)
	strip = func(n *html.Node) {
		switch n.Type {
		case html.ElementNode:
			kept := n.Attr[:0]
			for _, a := range n.Attr {
				if a.Namespace == "" && strings.HasPrefix(a.Key, prefix) {
					if k, err := strconv.Atoi(strings.TrimPrefix(a.Key, prefix)); err == nil && a.Val == "1" {
						produced[k] = append(produced[k], n)
						markers[n] = append(markers[n], k)
						continue
					}
				}
				a.Val = markerRE.ReplaceAllString(a.Val, "")
				kept = append(kept, a)
			}
			n.Attr = kept
			sort.Ints(markers[n])
		case html.TextNode, html.CommentNode, html.DoctypeNode:
			n.Data = markerRE.ReplaceAllString(n.Data, "")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			strip(c)
		}
	}
	// Whether each tag's marker is anywhere in the marked tree (an
	// attribute, or text, a comment, an attribute value), read from its
	// serialization before the markers are removed.
	whole := dump(marked)
	lastUnseen = make([]bool, len(tags))
	for i := range tags {
		n := strconv.Itoa(i)
		lastUnseen[i] = !strings.Contains(whole, strconv.Quote(prefix+n)) && !strings.Contains(whole, " "+prefix+n+"=1 ")
	}
	strip(marked)
	if dump(orig) != dump(marked) {
		return trusted, false
	}
	// x/net's tree ends early at a <template> in an integration point: the
	// last tag's marker is then nowhere.
	if len(tags) > 0 && lastUnseen[len(tags)-1] {
		return trusted, false
	}
	// A dropped <template> may leave x/net's state apart from the
	// standard's while the last tag survives.
	for i, t := range tags {
		if t.name == "template" && lastUnseen[i] {
			return trusted, false
		}
	}
	for i, t := range tags {
		for j, a := range t.attrs {
			if strings.ContainsRune(a.Key, 0) || strings.ContainsRune(a.Val, 0) {
				continue
			}
			for _, el := range produced[i] {
				if sameElement(t.name, el) && hasAttr(el, a) && !earlierHas(markers[el], i, a.Key, tags) {
					trusted[i][j] = true
					break
				}
			}
		}
	}
	return trusted, true
}

// sameElement reports whether el is an element a start tag named name
// produces: the same name up to ASCII case (the tree adjusts SVG names:
// linearGradient), or img for image (the tree builder renames it).
func sameElement(name string, el *html.Node) bool {
	got := asciiLower(el.Data)
	return got == name || name == "image" && got == "img"
}

// earlierHas reports whether a tag before tag n among those whose markers
// an element carries has an attribute named key: the element took that
// tag's attribute, not tag n's.
func earlierHas(carried []int, n int, key string, tags []tag) bool {
	key = asciiLower(key)
	for _, m := range carried {
		if m >= n {
			break
		}
		for _, a := range tags[m].attrs {
			if asciiLower(a.Key) == key {
				return true
			}
		}
	}
	return false
}

// hasAttr reports whether el's first attribute of a's name has a's value.
func hasAttr(el *html.Node, a html.Attribute) bool {
	want := asciiLower(a.Key)
	for _, x := range el.Attr {
		name := x.Key
		if x.Namespace != "" {
			name = x.Namespace + ":" + x.Key
		}
		if asciiLower(name) == want {
			return x.Val == a.Val
		}
	}
	return false
}

// dump serializes a tree completely and deterministically (html.Render
// refuses some trees the parser builds).
func dump(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		fmt.Fprintf(&b, "%d %d %q %q %q\n", depth, n.Type, n.Namespace, n.Data, n.Attr)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return b.String()
}
