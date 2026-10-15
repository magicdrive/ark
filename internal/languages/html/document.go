package html

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/magicdrive/ark/internal/languages/internal/weburl"
)

// tag is a start tag the tokenizer reported.
type tag struct {
	start, end int    // the tag's bytes
	nameEnd    int    // absolute offset where its name ends
	name       string // as the tokenizer reports it (ASCII lower case)
	attrs      []attr // the attributes it states, later duplicates removed
	matched    bool   // its attribute spans matched the tokenizer's attributes
	text       span   // a <script> or <style>: the raw text the tokenizer read after it (start -1 otherwise)
	closed     bool   // that text ends at an end tag (not at the end of the input)

	// Set by verify.
	verified  []bool      // per attr: on the element this tag produced
	namespace string      // the produced element's ("" for HTML)
	scope     []int       // enclosing <template> tags (indexes into tags), outermost first
	inTree    bool        // the tag produced an element (its scope is known)
	el        *xhtml.Node // the element it produced (in the marked tree)
}

// attr is an attribute of a tag: its name (ASCII lower case) and value as the
// tokenizer reports them, and their spans in the source (valS.start -1
// without a value or when the tag's spans did not match).
type attr struct {
	key, val    string
	nameS, valS span
}

// document is what the tokenizer and the tree builder say about a file.
type document struct {
	tags      []tag
	eofInTag  span // a tag cut off by the end of the input (eof-in-tag); start -1 if none
	eofEndTag bool // it is an end tag (only its closing is lost)
	failed    bool // the tokenizer stopped with an error other than io.EOF
	verified  bool // the marked tree, markers removed, equals the source's tree
	tree      treeFacts
	mismatch  []int
	hasScript bool // a <script> start tag (any, also one the tree builder ignores)
	hasBase   bool // a <base> start tag with an href
	dynAttr   bool // an on* attribute or a javascript: URL
}

// tokenize reads src with x/net's tokenizer, keeping every start tag with
// its attribute spans. Raw() is copied before Token(): Token unescapes in
// place.
func tokenize(src []byte) *document {
	d := &document{eofInTag: span{-1, -1}}
	z := xhtml.NewTokenizer(bytes.NewReader(src))
	off := 0
	pending := -1 // the <script> or <style> whose text the next text tokens are
	for {
		tt := z.Next()
		raw := slices.Clone(z.Raw())
		start := off
		off += len(raw)
		if tt == xhtml.TextToken && pending >= 0 {
			d.tags[pending].text.end = off
			continue
		}
		if tt == xhtml.EndTagToken && pending >= 0 {
			if name, _ := z.TagName(); string(name) == d.tags[pending].name {
				d.tags[pending].closed = true
			}
		}
		pending = -1
		if tt == xhtml.ErrorToken {
			// The tokenizer drops a tag cut off by the end of the input,
			// as the standard does; its bytes are the last Raw().
			if len(raw) > 0 {
				d.eofInTag = span{start, off}
				d.eofEndTag = bytes.HasPrefix(raw, []byte("</"))
			}
			if !errors.Is(z.Err(), io.EOF) || off != len(src) {
				d.failed = true
			}
			return d
		}
		if tt != xhtml.StartTagToken && tt != xhtml.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		tg := tag{start: start, end: off, nameEnd: start + tagNameEnd(raw), name: t.Data, text: span{-1, -1}}
		if tt == xhtml.StartTagToken && (t.Data == "script" || t.Data == "style") {
			tg.text = span{off, off}
			pending = len(d.tags)
		}
		switch t.Data {
		case "script":
			d.hasScript = true
		case "base":
			for _, a := range t.Attr {
				if a.Key == "href" {
					d.hasBase = true
				}
			}
		}
		for _, a := range t.Attr {
			if strings.HasPrefix(a.Key, "on") && len(a.Key) > 2 ||
				strings.HasPrefix(asciiLower(weburl.Trim(a.Val)), "javascript:") {
				d.dynAttr = true
			}
		}
		// A later attribute of a name the tag already has is removed
		// (§13.2.5.33): from both lists before they are compared. x/net
		// removes it since v0.60.0 and kept it before, so either version
		// gives the same tag.
		scanned := firstOfEachName(scanAttrs(raw), func(a scannedAttr) string { return a.name })
		xattrs := firstOfEachName(t.Attr, func(a xhtml.Attribute) string { return a.Key })
		tg.matched = len(scanned) == len(xattrs)
		for i := 0; tg.matched && i < len(scanned); i++ {
			v := ""
			if s := scanned[i].valS; s.start >= 0 {
				v = attrValue(string(raw[s.start:s.end]))
			}
			tg.matched = scanned[i].name == xattrs[i].Key && v == xattrs[i].Val
		}
		if !tg.matched {
			d.mismatch = append(d.mismatch, len(d.tags))
		}
		for i, a := range xattrs {
			at := attr{key: a.Key, val: a.Val, nameS: span{-1, -1}, valS: span{-1, -1}}
			if tg.matched {
				at.nameS = span{start + scanned[i].nameS.start, start + scanned[i].nameS.end}
				if s := scanned[i].valS; s.start >= 0 {
					at.valS = span{start + s.start, start + s.end}
				}
			}
			tg.attrs = append(tg.attrs, at)
		}
		d.tags = append(d.tags, tg)
	}
}

// firstOfEachName keeps the first of the attributes that share a name.
func firstOfEachName[A any](as []A, name func(A) string) []A {
	seen := map[string]bool{}
	var out []A
	for _, a := range as {
		if n := name(a); !seen[n] {
			seen[n] = true
			out = append(out, a)
		}
	}
	return out
}

// verify decides, for each attribute of each tag, whether the tree the
// standard builds has it on an element that tag produced (tag.verified),
// and each tag's element namespace and <template> scope.
//
// The tokenizer reads raw text by the start tag alone, the tree builder by
// its insertion mode: the tokenizer may report a tag the tree builder reads
// as text, an attribute value or a comment (a fake), one it ignores, or one
// whose attributes it merges into another element (html, body). verify
// parses the source again with a marker attribute inserted after the name
// of every reported tag. An attribute of tag n is verified only when an
// element carrying marker n (one tag n produced, or its clone) is named as
// tag n — a marker can land in another tag the tokenizer read as raw text —
// its first attribute of the name has the value, and no earlier tag whose
// marker it carries has an attribute of the name (html and body take a
// later tag's attribute only when they have none of its name). A marker in
// text, a comment or an attribute value is a fake's. The markers must not
// change how the tree is built (Noah's Ark compares formatting elements'
// attributes): unless the marked tree, markers removed, equals the source's
// tree, nothing is verified.
func (d *document) verify(src []byte) {
	for i := range d.tags {
		d.tags[i].verified = make([]bool, len(d.tags[i].attrs))
	}
	prefix := markerPrefix(src)
	var b bytes.Buffer
	last := 0
	for i, t := range d.tags {
		b.Write(src[last:t.nameEnd])
		b.WriteString(" " + prefix + strconv.Itoa(i) + "=1 ")
		last = t.nameEnd
	}
	b.Write(src[last:])
	orig, err1 := xhtml.Parse(bytes.NewReader(src))
	marked, err2 := xhtml.Parse(bytes.NewReader(b.Bytes()))
	if err1 != nil || err2 != nil {
		return
	}
	markerRE := regexp.MustCompile(" " + regexp.QuoteMeta(prefix) + "[0-9]+=1 ")
	produced := map[int][]*xhtml.Node{}
	markers := map[*xhtml.Node][]int{} // ascending
	seen := make([]bool, len(d.tags))  // the marker is somewhere in the tree
	strip1 := func(s string) string {
		for _, m := range markerRE.FindAllString(s, -1) {
			if k, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(m, " "+prefix), "=1 ")); err == nil && k >= 0 && k < len(d.tags) {
				seen[k] = true
			}
		}
		return markerRE.ReplaceAllString(s, "")
	}
	var strip func(n *xhtml.Node)
	strip = func(n *xhtml.Node) {
		switch n.Type {
		case xhtml.ElementNode:
			kept := n.Attr[:0]
			for _, a := range n.Attr {
				if a.Namespace == "" && strings.HasPrefix(a.Key, prefix) && a.Val == "1" {
					if k, err := strconv.Atoi(strings.TrimPrefix(a.Key, prefix)); err == nil && k >= 0 && k < len(d.tags) {
						produced[k] = append(produced[k], n)
						markers[n] = append(markers[n], k)
						seen[k] = true
						continue
					}
				}
				a.Val = strip1(a.Val)
				kept = append(kept, a)
			}
			n.Attr = kept
			sort.Ints(markers[n])
		case xhtml.TextNode, xhtml.CommentNode, xhtml.DoctypeNode:
			n.Data = strip1(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			strip(c)
		}
	}
	strip(marked)
	if dump(orig) != dump(marked) {
		return
	}
	// x/net's tree ends early on a <template> in an SVG or MathML
	// integration point (its test suite lists the case): every tag after it
	// is lost, with no error. A last tag whose marker is nowhere in the tree
	// shows it (or a trailing tag the tree builder ignores: a frameset
	// document's; refused alike).
	if n := len(d.tags); n > 0 && !seen[n-1] {
		return
	}
	// So does a <template> it dropped (the standard ignores a template
	// start tag in no insertion mode a document reaches outside frameset
	// documents): x/net's state after it may part from the standard's
	// (a later SVG <title>'s <base> lost) while the last tag survives.
	for i, t := range d.tags {
		if t.name == "template" && !seen[i] {
			return
		}
	}
	d.verified = true
	d.tree = factsOf(orig)
	// A tag whose marker is nowhere was ignored by this tree builder (x/net
	// parses <select> by the former standard, which drops what the current
	// one keeps): its id may be an element's in a browser, so it counts as
	// one, never as nothing.
	for i, t := range d.tags {
		if seen[i] {
			continue
		}
		for _, a := range t.attrs {
			if a.key == "id" {
				d.tree.ids[nulFree(a.val)]++
			}
		}
	}
	for i := range d.tags {
		t := &d.tags[i]
		var els []*xhtml.Node
		for _, el := range produced[i] {
			if sameElement(t.name, el) {
				els = append(els, el)
			}
		}
		if len(els) == 0 {
			continue
		}
		// Clones of a formatting element stay within one <template>'s
		// content; were they not, the scope would be unknown.
		scope := templateScope(els[0], markers)
		same := true
		for _, el := range els[1:] {
			if !equalInts(templateScope(el, markers), scope) || el.Namespace != els[0].Namespace {
				same = false
			}
		}
		if !same {
			continue
		}
		t.inTree, t.scope, t.namespace, t.el = true, scope, els[0].Namespace, els[0]
		for j, a := range t.attrs {
			if strings.ContainsRune(a.key, 0) || strings.ContainsRune(a.val, 0) {
				continue // the standard replaces NUL, x/net does not
			}
			for _, el := range els {
				if hasAttr(el, a) && !earlierHas(markers[el], i, a.key, d.tags) {
					t.verified[j] = true
					break
				}
			}
		}
	}
}

// markerPrefix returns the name prefix of verify's marker attributes, one
// the source does not hold (so no text or attribute of the document is
// taken for a marker).
func markerPrefix(src []byte) string {
	low := bytes.ToLower(src)
	for i := 0; ; i++ {
		p := "data-ark-tok-"
		if i > 0 {
			p = "data-ark-tok" + strconv.Itoa(i) + "-"
		}
		if !bytes.Contains(low, []byte(p)) {
			return p
		}
	}
}

// sameElement reports whether el is an element a start tag named name
// produces: the same name up to ASCII case (the tree adjusts SVG names), or
// img for image (the tree builder renames it).
func sameElement(name string, el *xhtml.Node) bool {
	got := asciiLower(el.Data)
	return got == name || name == "image" && got == "img"
}

// hasAttr reports whether el's first attribute of a's name has a's value.
func hasAttr(el *xhtml.Node, a attr) bool {
	for _, x := range el.Attr {
		name := x.Key
		if x.Namespace != "" {
			name = x.Namespace + ":" + x.Key
		}
		if asciiLower(name) == a.key {
			return x.Val == a.val
		}
	}
	return false
}

// earlierHas reports whether a tag before tag n among those whose markers an
// element carries has an attribute named key: the element took that tag's.
func earlierHas(carried []int, n int, key string, tags []tag) bool {
	for _, m := range carried {
		if m >= n {
			break
		}
		for _, a := range tags[m].attrs {
			if a.key == key {
				return true
			}
		}
	}
	return false
}

// templateScope returns the HTML <template> elements enclosing el, outermost
// first, as the indexes of the tags that produced them. An enclosing
// template no reported tag produced cannot be: every element comes from a
// tag (the tree builder creates only html, head and body itself).
func templateScope(el *xhtml.Node, markers map[*xhtml.Node][]int) []int {
	var scope []int
	for p := el.Parent; p != nil; p = p.Parent {
		if p.Type == xhtml.ElementNode && p.Namespace == "" && p.Data == "template" {
			if m := markers[p]; len(m) > 0 {
				scope = append([]int{m[0]}, scope...)
			} else {
				scope = append([]int{-1}, scope...)
			}
		}
	}
	return scope
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dump serializes a tree completely and deterministically.
func dump(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(n *xhtml.Node, depth int)
	walk = func(n *xhtml.Node, depth int) {
		b.WriteString(strconv.Itoa(depth))
		b.WriteByte(' ')
		b.WriteString(strconv.Itoa(int(n.Type)))
		b.WriteByte(' ')
		b.WriteString(strconv.Quote(n.Namespace))
		b.WriteByte(' ')
		b.WriteString(strconv.Quote(n.Data))
		for _, a := range n.Attr {
			b.WriteByte(' ')
			b.WriteString(strconv.Quote(a.Namespace))
			b.WriteString(strconv.Quote(a.Key))
			b.WriteString(strconv.Quote(a.Val))
		}
		b.WriteByte('\n')
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return b.String()
}

// treeFacts are what the source's tree says about the whole document. The
// tokenizer does not see every element (text in a raw-text element to it
// may be elements to the tree builder: `<svg><style><script>`), so facts
// about the document are taken from the tree.
type treeFacts struct {
	dynamic bool           // the DOM may change at run time
	base    bool           // a <base href> outside template content
	ids     map[string]int // id value → elements carrying it (clones too)
}

// frameworkAttr reports whether an attribute name is a framework's binding
// (Angular [x] (x) *x #x, Vue :x @x v-, Alpine x-, Thymeleaf th:, AngularJS
// ng-, Knockout data-bind, htmx hx-): the element's attributes or its
// presence are decided at run time.
func frameworkAttr(key string) bool {
	if key == "" {
		return false
	}
	switch key[0] {
	case '[', '(', '*', ':', '@', '#':
		return true
	}
	for _, p := range []string{"v-", "x-", "th:", "ng-", "data-ng-", "hx-", "data-hx-"} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return key == "data-bind"
}

// embedsDocument reports whether an element's attribute loads a document
// into it (iframe srcdoc and src, frame src, object data, embed src): one of
// the same origin may change this document's DOM.
func embedsDocument(element, key string) bool {
	switch element {
	case "iframe":
		return key == "srcdoc" || key == "src"
	case "frame", "embed":
		return key == "src"
	case "object":
		return key == "data"
	}
	return false
}

func factsOf(doc *xhtml.Node) treeFacts {
	f := treeFacts{ids: map[string]int{}}
	var walk func(n *xhtml.Node, inTemplate bool)
	walk = func(n *xhtml.Node, inTemplate bool) {
		if n.Type == xhtml.ElementNode {
			if asciiLower(n.Data) == "script" { // HTML and SVG scripts run
				f.dynamic = true
			}
			if n.Namespace == "" && n.Data == "base" && !inTemplate {
				for _, a := range n.Attr {
					if a.Namespace == "" && a.Key == "href" {
						f.base = true
					}
				}
			}
			id := false
			for _, a := range n.Attr {
				key := asciiLower(a.Key)
				if a.Namespace == "" && key == "id" && !id {
					f.ids[nulFree(a.Val)]++
					id = true
				}
				if strings.HasPrefix(key, "on") && len(key) > 2 || frameworkAttr(key) ||
					strings.HasPrefix(asciiLower(weburl.Trim(a.Val)), "javascript:") ||
					embedsDocument(n.Data, key) {
					f.dynamic = true
				}
			}
			inTemplate = inTemplate || n.Namespace == "" && n.Data == "template"
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inTemplate)
		}
	}
	walk(doc, false)
	return f
}

// nulFree is an attribute value as the standard has it: NUL replaced by
// U+FFFD (x/net keeps the NUL).
func nulFree(v string) string { return strings.ReplaceAll(v, "\x00", "\uFFFD") }
