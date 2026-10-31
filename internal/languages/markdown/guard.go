package markdown

import (
	"fmt"
	"sort"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// The cost guard. goldmark's time grows with the square of some inputs no
// author writes but a file may hold (measured with v1.8.6, 64 KiB taking
// seconds): many block quotes or list items opened on one line ("> > > …"),
// a paragraph that starts with many link reference definitions, and, in
// one block's inline content, many unmatched emphasis, strikethrough or
// code span delimiters or link openers ("*a*a…", "“a“a…", "[a]([a](…").
// goldmark cannot be interrupted, and a time limit would make the result
// depend on the machine, so the guard bounds the input instead, from its
// text alone, before goldmark reads it — rewriting bytes in place, never
// moving one, so every position stays the file's:
//
//   - on a line, the block quote or list item marker after the
//     maxContainers-th is read as a backslash: the rest of the line is text;
//   - in a paragraph, of the lines that start with "[", every
//     maxDefinitionRun-th is read as a blank line;
//   - a first, block-only parse (no inline parsing, no definitions: linear)
//     finds each block whose inline content goldmark reads; a block costs
//     its delimiters ("*_~`[]()<") times its length, and while the file's
//     blocks cost more than inlineBudget, the costliest block's delimiters
//     are read as spaces (its links, emphasis and code spans are then text).
//
// Before them, a file's YAML front matter ("---" lines around it, at its
// start) is read as blank lines, as GitHub reads it as no Markdown.
//
// What the guard rewrote is reported (markdown_degenerate). The budgets lie
// far above what a written document needs: no Markdown file of the
// measured corpora reaches a tenth of them (docs/development/markdown.md).
const (
	maxContainers    = 32
	maxDefinitionRun = 1000
	inlineBudget     = 200_000_000
)

// CodeDegenerate reports what the cost guard read as text.
const CodeDegenerate = "markdown_degenerate"

// guarded is the guard's result: the text goldmark reads, and what was
// rewritten (offsets of the first rewrite of each kind, counts).
type guarded struct {
	src        []byte
	containers rewrite
	defs       rewrite
	inline     rewrite

	maxBlock, total int // the costliest block's and the file's inline cost, before any rewrite
}

type rewrite struct {
	first, last int
	n           int
}

func (r *rewrite) add(start, end int) {
	if r.n == 0 {
		r.first, r.last = start, end
	}
	r.n++
}

// blocksOnly reads a document's blocks only: no inline content, no link
// reference definitions (linear in the input); blocksAndDefinitions reads
// the definitions too.
var (
	blocksOnly = goldmark.New(goldmark.WithParser(parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
	)), goldmark.WithExtensions(extension.Table))
	blocksAndDefinitions = goldmark.New(goldmark.WithParser(parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	)), goldmark.WithExtensions(extension.Table))
)

// costed is a block whose inline content goldmark reads, and its cost.
type costed struct {
	segs  *text.Segments
	start int
	cost  int
}

// blocks parses buf with md and returns the blocks whose inline content
// goldmark reads, with their cost: their delimiters times their length.
// With set, it bounds each paragraph's lines that start with "[" first.
func (g *guarded) blocks(buf []byte, md goldmark.Markdown, set func(int, byte)) []costed {
	root := md.Parser().Parse(text.NewReader(buf))
	var blocks []costed
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.Paragraph:
			if set != nil {
				g.splitDefinitions(buf, n.Lines(), set)
			}
		case *ast.Heading, *ast.TextBlock, *extast.TableCell:
		default:
			return ast.WalkContinue, nil
		}
		segs := n.Lines()
		if segs.Len() == 0 {
			return ast.WalkSkipChildren, nil
		}
		k, l := 0, 0
		for i := 0; i < segs.Len(); i++ {
			s := segs.At(i)
			for _, c := range buf[clamp(s.Start, len(buf)):clamp(s.Stop, len(buf))] {
				if isDelimiter(c) {
					k++
				}
			}
			l += s.Stop - s.Start
		}
		if k > 0 {
			blocks = append(blocks, costed{segs: segs, start: segs.At(0).Start, cost: k * l})
		}
		return ast.WalkSkipChildren, nil
	})
	return blocks
}

func guard(src []byte) guarded {
	g := guarded{src: src}
	buf, copied := src, false // copied on the first rewrite
	set := func(i int, c byte) {
		if !copied {
			buf, copied = append([]byte(nil), src...), true
		}
		buf[i] = c
	}
	if len(src) == 0 {
		return g
	}
	// Front matter is the file's metadata, not its text (GitHub renders
	// it as a table, never as headings).
	if end := frontMatter(src); end > 0 {
		for i := 0; i < end; i++ {
			if src[i] != '\n' && src[i] != '\r' {
				set(i, ' ')
			}
		}
	}
	// Container markers on each line.
	for off := 0; off < len(buf); {
		end := lineEnd(buf, off)
		g.capContainers(buf, off, end, set)
		off = nextLine(buf, off)
	}
	// A block-only parse (no definitions read) finds the paragraphs, and
	// the blocks whose inline content goldmark reads.
	blocks := g.blocks(buf, blocksOnly, set)
	total := 0
	for _, b := range blocks {
		total += b.cost
	}
	if total > inlineBudget {
		// Link reference definitions are no inline content: read them
		// (their runs bounded now) and count what is left.
		blocks = g.blocks(buf, blocksAndDefinitions, nil)
	}
	total = 0
	for _, b := range blocks {
		total += b.cost
		g.maxBlock = max(g.maxBlock, b.cost)
	}
	g.total = total
	if total > inlineBudget {
		sort.SliceStable(blocks, func(i, j int) bool {
			if blocks[i].cost != blocks[j].cost {
				return blocks[i].cost > blocks[j].cost
			}
			return blocks[i].start < blocks[j].start
		})
		for _, b := range blocks {
			if total <= inlineBudget {
				break
			}
			total -= b.cost
			first, last := len(buf), 0
			for i := 0; i < b.segs.Len(); i++ {
				s := b.segs.At(i)
				start, stop := clamp(s.Start, len(buf)), clamp(s.Stop, len(buf))
				first, last = min(first, start), max(last, stop)
				for j := start; j < stop; j++ {
					if isDelimiter(buf[j]) {
						set(j, ' ')
					}
				}
			}
			g.inline.add(first, last)
		}
	}
	g.src = buf
	return g
}

// frontMatter returns the end of the YAML front matter a file starts with
// — a line "---", then lines up to one that is "---" or "..." — or 0.
func frontMatter(src []byte) int {
	first := lineEnd(src, 0)
	if string(trimRight(src[:first])) != "---" {
		return 0
	}
	for off := nextLine(src, 0); off < len(src); off = nextLine(src, off) {
		end := lineEnd(src, off)
		if l := string(trimRight(src[off:end])); l == "---" || l == "..." {
			return end
		}
	}
	return 0
}

func trimRight(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	return b
}

func isDelimiter(c byte) bool {
	switch c {
	case '*', '_', '~', '`', '[', ']', '(', ')', '<':
		return true
	}
	return false
}

// capContainers ends the containers of the line [off, end) at the
// maxContainers-th marker: the next marker's first byte is read as "\\",
// which starts a paragraph whose text is the rest of the line. Markers are
// "> ", ">", and the list item markers "-", "+", "*" and "1." / "1)"
// followed by a space or a tab.
func (g *guarded) capContainers(buf []byte, off, end int, set func(int, byte)) {
	n := 0
	for i := off; i < end; {
		c := buf[i]
		width := 0
		switch {
		case c == ' ' || c == '\t':
			i++
			continue
		case c == '>':
			width = 1
		case (c == '-' || c == '+' || c == '*') && i+1 < end && (buf[i+1] == ' ' || buf[i+1] == '\t'):
			width = 1
		case '0' <= c && c <= '9':
			j := i
			for j < end && j-i < 9 && '0' <= buf[j] && buf[j] <= '9' {
				j++
			}
			if j < end && (buf[j] == '.' || buf[j] == ')') && j+1 < end && (buf[j+1] == ' ' || buf[j+1] == '\t') {
				width = j + 1 - i
			}
		}
		if width == 0 {
			return
		}
		if n++; n > maxContainers {
			set(i, '\\')
			g.containers.add(off, end)
			return
		}
		i += width
	}
}

// splitDefinitions reads, of a paragraph's lines that start with "[" (a
// link reference definition may), every maxDefinitionRun-th as a blank
// line: goldmark reads a paragraph's leading definitions in time that grows
// with the square of their number.
func (g *guarded) splitDefinitions(buf []byte, segs *text.Segments, set func(int, byte)) {
	if segs.Len() < maxDefinitionRun {
		return
	}
	n := 0
	for i := 0; i < segs.Len(); i++ {
		s := segs.At(i)
		start, stop := clamp(s.Start, len(buf)), clamp(s.Stop, len(buf))
		j := start
		for j < stop && j-start < 3 && buf[j] == ' ' {
			j++
		}
		if j >= stop || buf[j] != '[' {
			continue
		}
		n++
		if n%maxDefinitionRun != 0 {
			continue
		}
		for x := start; x < stop; x++ {
			if buf[x] != '\n' && buf[x] != '\r' {
				set(x, ' ')
			}
		}
		g.defs.add(start, stop)
	}
}

// problems reports what the guard rewrote, each kind once: its message and
// the bytes of its first rewrite.
func (g *guarded) problems() []problem {
	var out []problem
	if g.containers.n > 0 {
		out = append(out, problem{fmt.Sprintf("%d lines open more than %d block quotes or list items: the rest of each is read as text", g.containers.n, maxContainers), g.containers.first, g.containers.last})
	}
	if g.defs.n > 0 {
		out = append(out, problem{fmt.Sprintf("a paragraph has more than %d lines that start with \"[\": %d of them are read as blank lines, to bound the time its link reference definitions take", maxDefinitionRun, g.defs.n), g.defs.first, g.defs.last})
	}
	if g.inline.n > 0 {
		out = append(out, problem{fmt.Sprintf("the file's inline markup is too costly to read: in %d blocks it is read as text (no links, emphasis or code spans)", g.inline.n), g.inline.first, g.inline.last})
	}
	return out
}

type problem struct {
	message    string
	start, end int
}
