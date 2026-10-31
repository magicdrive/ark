package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	xhtml "golang.org/x/net/html"
)

// The document layer: what github.com/yuin/goldmark (CommonMark 0.31.2, and
// the GitHub Flavored Markdown extensions GitHub renders — tables,
// strikethrough, extended autolinks, task lists — and footnotes, which keep
// "[^1]: note" from reading as a link reference definition) builds of the
// bytes, located in them. goldmark is never asked to render, and nothing of
// the document is fetched or run.

// heading is an ATX or setext heading.
type heading struct {
	level      int
	start, end int    // the heading's own bytes: its line(s), the setext underline included
	text       string // its text content, as GitHub's anchor rule reads it (soft breaks as "\n")
	nested     bool   // inside a container block (a block quote, a list item)
}

// link is an inline link, an image, a resolved reference link or an
// autolink.
type link struct {
	start, end int
	dest       string // the destination, escapes and entities resolved
	image      bool
	auto       bool // an autolink (<url>, or an extended autolink): its text is the URL
}

// codeBlock is a fenced code block.
type codeBlock struct {
	start, end int
	lang       string // the info string's first word, escapes and entities resolved
}

// htmlID is an id (or an <a name>) of raw HTML: a fragment can name it.
type htmlID struct {
	start, end int
	id         string
}

// doc is what the document layer reads of a file, in document order.
type doc struct {
	headings []heading
	links    []link
	codes    []codeBlock
	ids      []htmlID
	tables   tableGuardState // what tableGuard rewrote in this reading
}

// md is goldmark's CommonMark parser with the GFM extensions and footnotes,
// its link parser wrapped to record where each link ends (goldmark records
// where a node starts only). A goldmark parser is safe for concurrent use:
// its state is in the parser.Context of each parse.
var md = newMarkdown()

func newMarkdown() goldmark.Markdown {
	inline := parser.DefaultInlineParsers()
	for i, v := range inline {
		if ip, ok := v.Value.(parser.InlineParser); ok && string(ip.Trigger()) == "![]" {
			inline[i].Value = &linkEnds{ip}
		}
	}
	// GFM's extensions (extension.GFM) but its tables, read through the
	// cost guard's tableGuard (guard.go): the guard has already rewritten
	// what it bounds, so it rewrites nothing more here.
	p := parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithInlineParsers(inline...),
		parser.WithParagraphTransformers(paragraphTransformers()...),
		parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)),
	)
	return goldmark.New(goldmark.WithParser(p), goldmark.WithExtensions(extension.Linkify, extension.Strikethrough, extension.TaskList, extension.Footnote))
}

var endsKey = parser.NewContextKey()

// linkEnds wraps goldmark's link parser: where the reader stands after a
// link or an image is parsed is where it ends.
type linkEnds struct{ parser.InlineParser }

func (l *linkEnds) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	n := l.InlineParser.Parse(parent, block, pc)
	switch n.(type) {
	case *ast.Link, *ast.Image:
		_, seg := block.Position()
		ends, _ := pc.Get(endsKey).(map[ast.Node]int)
		if ends == nil {
			ends = map[ast.Node]int{}
			pc.Set(endsKey, ends)
		}
		ends[n] = seg.Start
	}
	return n
}

func (l *linkEnds) CloseBlock(parent ast.Node, block text.Reader, pc parser.Context) {
	if c, ok := l.InlineParser.(parser.CloseBlocker); ok {
		c.CloseBlock(parent, block, pc)
	}
}

// parse reads src. It never fails: every byte sequence is a Markdown
// document.
func parse(src []byte) *doc {
	pc := parser.NewContext()
	root := md.Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	ends, _ := pc.Get(endsKey).(map[ast.Node]int)
	d := &doc{}
	if st, ok := pc.Get(tableGuardKey).(*tableGuardState); ok {
		d.tables = *st
	}
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			d.headings = append(d.headings, readHeading(n, src))
		case *ast.Link:
			d.addLink(n, src, ends, string(n.Destination), false)
		case *ast.Image:
			d.addLink(n, src, ends, string(n.Destination), true)
			// An image's description is its alt text: no link it holds is
			// one (CommonMark renders it as plain text).
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink:
			start, end := segmentsSpan(n, src)
			if start >= 0 {
				d.links = append(d.links, link{start: start, end: end, dest: string(n.URL(src)), auto: true})
			}
		case *ast.FencedCodeBlock:
			d.codes = append(d.codes, readCode(n, src))
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			return ast.WalkSkipChildren, nil
		case *ast.HTMLBlock:
			d.htmlIDs(n.Lines(), src)
			if n.HasClosure() {
				segs := text.NewSegments()
				segs.Append(n.ClosureLine)
				d.htmlIDs(segs, src)
			}
		case *ast.RawHTML:
			d.htmlIDs(n.Segments, src)
		}
		return ast.WalkContinue, nil
	})
	return d
}

func (d *doc) addLink(n ast.Node, src []byte, ends map[ast.Node]int, dest string, image bool) {
	start := n.Pos()
	if start < 0 || start > len(src) {
		return
	}
	end, ok := ends[n]
	if !ok || end < start || end > len(src) {
		end = start
	}
	d.links = append(d.links, link{start: start, end: end, dest: dest, image: image})
}

// readHeading locates a heading: an ATX heading's line, a setext heading's
// text lines and underline.
func readHeading(h *ast.Heading, src []byte) heading {
	start := h.Pos()
	lines := h.Lines()
	if start < 0 && lines.Len() > 0 {
		start = lines.At(0).Start
	}
	start = clamp(start, len(src))
	_, nested := h.Parent().(*ast.Document)
	out := heading{level: h.Level, start: start, nested: !nested}
	out.text = headingText(h, src)
	if atx(src[start:]) {
		out.end = lineEnd(src, start)
		return out
	}
	// A setext heading ends with its underline, the line after its text.
	last := start
	if lines.Len() > 0 {
		last = clamp(lines.At(lines.Len()-1).Stop, len(src))
	}
	end := lineEnd(src, last)
	if next := nextLine(src, last); next < len(src) {
		end = lineEnd(src, next)
	}
	out.end = end
	return out
}

// atx reports whether a heading's bytes start as an ATX heading does: up to
// three spaces, one to six '#', then a space, a tab or the line's end
// (anything else is a setext heading's text, "#tag" too).
func atx(b []byte) bool {
	i := 0
	for i < 3 && i < len(b) && b[i] == ' ' {
		i++
	}
	n := 0
	for i < len(b) && b[i] == '#' && n < 7 {
		i++
		n++
	}
	if n == 0 || n > 6 {
		return false
	}
	return i == len(b) || b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r'
}

// headingText is a heading's text content as a browser reads it from the
// rendered HTML: text, code spans as written, a link's text, an image
// none (an <img> holds no text), raw HTML's tags none; a soft line break
// is "\n".
func headingText(h ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				v := c.Segment.Value(src)
				if c.IsRaw() {
					b.Write(v)
				} else {
					b.Write(inlineText(v))
				}
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte('\n')
				}
			case *ast.String:
				if c.IsCode() || c.IsRaw() {
					b.Write(c.Value)
				} else {
					b.Write(inlineText(c.Value))
				}
			case *ast.CodeSpan:
				for t := c.FirstChild(); t != nil; t = t.NextSibling() {
					switch t := t.(type) {
					case *ast.Text:
						v := t.Segment.Value(src)
						// A line ending in a code span is a space (CommonMark 6.1).
						v = bytes.ReplaceAll(bytes.ReplaceAll(v, []byte("\r\n"), []byte(" ")), []byte("\n"), []byte(" "))
						b.Write(v)
					case *ast.String:
						b.Write(t.Value)
					}
				}
			case *ast.AutoLink:
				b.Write(c.Label(src))
			case *ast.Image, *ast.RawHTML, *extast.FootnoteLink:
				// No text content of their own: an <img>, a tag; a footnote
				// reference renders as its number, which GitHub's anchor
				// rule reads — but its number depends on the whole file.
				// It is left out.
			default:
				walk(c)
			}
		}
	}
	walk(h)
	return b.String()
}

// inlineText resolves what CommonMark resolves in text: backslash escapes,
// entity and numeric character references.
func inlineText(v []byte) []byte {
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
}

// readCode locates a fenced code block: its opening fence line to its
// closing fence line (to its last line when the block closes with its
// container or the document), and its language.
func readCode(c *ast.FencedCodeBlock, src []byte) codeBlock {
	start := c.Pos()
	lines := c.Lines()
	if c.Info != nil && (start < 0 || start > c.Info.Segment.Start) {
		start = c.Info.Segment.Start
	}
	if start < 0 && lines.Len() > 0 {
		start = lines.At(0).Start
	}
	start = lineStart(src, clamp(start, len(src)))
	out := codeBlock{start: start}
	if c.Info != nil {
		info := c.Info.Segment.Value(src)
		if i := bytes.IndexAny(info, " \t"); i >= 0 {
			info = info[:i]
		}
		out.lang = string(inlineText(info))
	}
	fence := fenceOf(src[start:])
	last := lineEnd(src, start) // the opening fence line
	if lines.Len() > 0 {
		last = lineEnd(src, clamp(lines.At(lines.Len()-1).Start, len(src)))
	}
	end := last
	if next := nextLine(src, last); fence != "" && next < len(src) {
		if e := lineEnd(src, next); closes(src[next:e], fence) {
			end = e
		}
	}
	out.end = end
	return out
}

// fenceOf returns the opening fence that starts line b (after any container
// markers and indentation): a run of three or more '`' or '~'.
func fenceOf(b []byte) string {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '>') {
		i++
	}
	j := i
	for j < len(b) && (b[j] == '`' || b[j] == '~') && b[j] == b[i] {
		j++
	}
	if j-i < 3 {
		return ""
	}
	return string(b[i:j])
}

// closes reports whether line closes a fence: a run of its character at
// least as long, after container markers and indentation, then spaces only.
func closes(line []byte, fence string) bool {
	t := bytes.TrimLeft(line, " \t>")
	n := 0
	for n < len(t) && t[n] == fence[0] {
		n++
	}
	return n >= len(fence) && len(bytes.TrimSpace(t[n:])) == 0
}

// htmlIDs states the id attributes (and <a name>) of raw HTML's start tags.
// Each segment is tokenized on its own: an attribute is stated only where
// one segment holds its whole tag, so its position is its bytes'.
func (d *doc) htmlIDs(segs *text.Segments, src []byte) {
	for i := 0; i < segs.Len(); i++ {
		s := segs.At(i)
		start, stop := clamp(s.Start, len(src)), clamp(s.Stop, len(src))
		if start >= stop {
			continue
		}
		raw := src[start:stop]
		if bytes.IndexByte(raw, '<') < 0 {
			continue
		}
		z := xhtml.NewTokenizer(bytes.NewReader(raw))
		off := 0
		for {
			tt := z.Next()
			if tt == xhtml.ErrorToken {
				break
			}
			tok := z.Raw()
			tstart := off
			off += len(tok)
			if tt != xhtml.StartTagToken && tt != xhtml.SelfClosingTagToken {
				continue
			}
			name, more := z.TagName()
			for more {
				var k, v []byte
				k, v, more = z.TagAttr()
				if string(k) == "id" || string(k) == "name" && string(name) == "a" {
					d.ids = append(d.ids, htmlID{start: start + tstart, end: start + off, id: string(v)})
				}
			}
		}
	}
}

// segmentsSpan returns the bytes an autolink's text occupies.
func segmentsSpan(n *ast.AutoLink, src []byte) (int, int) {
	start := n.Pos()
	if start < 0 || start >= len(src) {
		return -1, -1
	}
	label := n.Label(src)
	if src[start] == '<' {
		return start, clamp(start+len(label)+2, len(src)) // <url>
	}
	// An extended autolink starts where its trigger is (the space or
	// delimiter before it): its text follows within the line.
	end := min(lineEnd(src, start), len(src))
	i := bytes.Index(src[start:end], label)
	if i < 0 || len(label) == 0 {
		return -1, -1
	}
	return start + i, start + i + len(label)
}

func clamp(i, n int) int {
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}

// lineEnd returns the offset of the '\n' that ends the line holding off (a
// preceding '\r' excluded), or len(src).
func lineEnd(src []byte, off int) int {
	i := bytes.IndexByte(src[off:], '\n')
	if i < 0 {
		return len(src)
	}
	end := off + i
	if end > off && src[end-1] == '\r' {
		end--
	}
	return end
}

// nextLine returns the offset of the line after the one holding off, or
// len(src).
func nextLine(src []byte, off int) int {
	i := bytes.IndexByte(src[off:], '\n')
	if i < 0 {
		return len(src)
	}
	return off + i + 1
}

func lineStart(src []byte, off int) int {
	return bytes.LastIndexByte(src[:off], '\n') + 1
}
