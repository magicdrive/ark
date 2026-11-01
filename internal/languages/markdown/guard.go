package markdown

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The cost guard. goldmark's time (and memory) grows faster than its input
// for some inputs no author writes but a file may hold (measured with
// v1.8.6, 24 KiB to 4 MiB taking seconds to minutes, or gigabytes):
// many block quotes, list items or footnote definitions opened on one line
// ("> > > …", "[^a]: [^a]: …"), lists nested ever deeper line by line
// (time with the cube of the depth), a paragraph that starts with many link reference definitions or whose
// definitions span many lines (its definitions times its lines), a table
// whose rows lack cells (goldmark adds the missing ones: its columns times
// its rows), and, in one block's inline content, many unmatched emphasis,
// strikethrough or code span delimiters or link openers ("*a*a…", "“a“a…",
// "[a]([a](…"). goldmark cannot be interrupted, and a time limit would
// make the result depend on the machine, so the guard bounds the input
// instead, from its text alone, before goldmark reads it — rewriting bytes
// in place, never moving one, so every position stays the file's:
//
//   - on a line, the block quote, list item or footnote definition marker
//     after the maxContainers-th, or past column maxIndent, is read as a
//     backslash: the rest of the line is text;
//   - in a paragraph, a line is read as a blank line when it is the
//     maxDefinitionRun-th that starts with "[" since the paragraph's start
//     or the last such rewrite, or the maxDefinitionLines-th line since
//     then when the first of them starts with "[" (definitionSplitter);
//   - a table's delimiter row ("|---|---|") with more than maxTableColumns
//     columns, or whose table would make goldmark add more missing cells
//     than the file's maxTablePadding has left, is no delimiter row (its
//     first byte is read as a backslash): the table is a paragraph
//     (tableGuard, which goldmark's own reading of the document runs too);
//   - a parse without inline content (linear, its lines bounded as above)
//     finds each block whose inline content goldmark reads; a block costs
//     its delimiters ("*_~`[]()<") times its length, and while the file's blocks cost more than
//     inlineBudget, the costliest block's delimiters are read as spaces
//     (its links, emphasis and code spans are then text).
//
// Before them, a byte order mark that starts the file is read as line
// breaks (blank lines before the first block, which CommonMark ignores),
// and a file's YAML front matter ("---" lines around it, at its start) as
// blank lines, as GitHub reads it as no Markdown.
//
// What the guard rewrote is reported (markdown_degenerate). The budgets lie
// far above what a written document needs: no Markdown file of the
// measured corpora reaches a tenth of them (docs/development/markdown.md).
const (
	maxContainers      = 32
	maxIndent          = 128
	maxDefinitionRun   = 1000
	maxDefinitionLines = 2000
	maxTableColumns    = 128
	maxTablePadding    = 1 << 17
	inlineBudget       = 200_000_000
)

// CodeDegenerate reports what the cost guard read as text.
const CodeDegenerate = "markdown_degenerate"

// guarded is the guard's result: the text goldmark reads, and what was
// rewritten (offsets of the first rewrite of each kind, counts).
type guarded struct {
	src        []byte
	containers rewrite
	indent     rewrite
	defs       rewrite
	tables     tableGuardState
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

func (r *rewrite) merge(o rewrite) {
	if o.n == 0 {
		return
	}
	if r.n == 0 || o.first < r.first {
		r.first, r.last = o.first, o.last
	}
	r.n += o.n
}

// blockParsers are the block parsers of goldmark's reading of a document
// (parse.go): CommonMark's and the footnote definitions'.
func blockParsers() []util.PrioritizedValue {
	return append(parser.DefaultBlockParsers(), util.Prioritized(extension.NewFootnoteBlockParser(), 999))
}

// paragraphTransformers are the paragraph transformers of goldmark's
// reading of a document: the link reference definitions', then the
// tables', behind tableGuard.
func paragraphTransformers() []util.PrioritizedValue {
	return append(parser.DefaultParagraphTransformers(), util.Prioritized(tableGuard{}, 200))
}

// guardBlocks is the guard's parser: goldmark's reading of a document's
// blocks, its definitions and tables (paragraphTransformers), with no
// inline parsers, and definitionSplitter before the definitions are read.
var guardBlocks = parser.NewParser(
	parser.WithBlockParsers(blockParsers()...),
	parser.WithParagraphTransformers(append(paragraphTransformers(), util.Prioritized(definitionSplitter{}, 50))...),
)

// costed is a block whose inline content goldmark reads, and its cost.
type costed struct {
	segs  *text.Segments
	start int
	cost  int
}

// blocks returns the blocks of root whose inline content goldmark reads,
// with their cost: their delimiters times their length.
func blocks(root ast.Node, buf []byte) []costed {
	var out []costed
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.Paragraph, *ast.Heading, *ast.TextBlock, *extast.TableCell:
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
			out = append(out, costed{segs: segs, start: segs.At(0).Start, cost: k * l})
		}
		return ast.WalkSkipChildren, nil
	})
	return out
}

var bom = []byte("\xef\xbb\xbf")

func guard(src []byte) guarded {
	g := guarded{src: src}
	if len(src) == 0 {
		return g
	}
	buf := append([]byte(nil), src...)
	set := func(i int, c byte) { buf[i] = c }
	// A byte order mark is no text, but goldmark would read it as the
	// first line's (no heading would start there). As line breaks, it adds
	// no indentation to the line it starts.
	start := 0
	if bytes.HasPrefix(src, bom) {
		for i := range bom {
			set(i, '\n')
		}
		start = len(bom)
	}
	// Front matter is the file's metadata, not its text (GitHub renders
	// it as a table, never as headings).
	if end := frontMatter(src[start:]); end > 0 {
		for i := start; i < start+end; i++ {
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
	// The blocks whose inline content goldmark reads, the paragraphs'
	// definitions bounded and read, their tables bounded (both rewrite buf
	// as goldmark reads it).
	pc := parser.NewContext()
	bs := blocks(guardBlocks.Parse(text.NewReader(buf), parser.WithContext(pc)), buf)
	if r, ok := pc.Get(definitionSplitterKey).(*rewrite); ok {
		g.defs = *r
	}
	if st, ok := pc.Get(tableGuardKey).(*tableGuardState); ok {
		g.tables = *st
	}
	total := 0
	for _, b := range bs {
		total += b.cost
		g.maxBlock = max(g.maxBlock, b.cost)
	}
	g.total = total
	if total > inlineBudget {
		sort.SliceStable(bs, func(i, j int) bool {
			if bs[i].cost != bs[j].cost {
				return bs[i].cost > bs[j].cost
			}
			return bs[i].start < bs[j].start
		})
		for _, b := range bs {
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
// maxContainers-th marker, or at the first marker past column maxIndent
// (a tab stops at the next multiple of 4; a marker's bytes are columns):
// that marker's first byte is read as "\\", which starts a paragraph whose
// text is the rest of the line. Markers are "> ", ">", the list item
// markers "-", "+", "*" and "1." / "1)" followed by a space or a tab, and
// the footnote definitions' "[^label]:" (up to the first "]:", which
// counts a marker more than goldmark may, never fewer).
// Containers nest no deeper than their markers' columns allow — a nested
// list item is indented to its parent's content, a block quote's content
// follows its ">" — so the column bounds the nesting a document builds
// line by line, as maxContainers bounds it on one line.
func (g *guarded) capContainers(buf []byte, off, end int, set func(int, byte)) {
	n, col := 0, 0
	for i := off; i < end; {
		c := buf[i]
		width := 0
		switch {
		case c == ' ':
			i++
			col++
			continue
		case c == '\t':
			i++
			col += 4 - col%4
			continue
		case c == '>':
			width = 1
		case (c == '-' || c == '+' || c == '*') && i+1 < end && (buf[i+1] == ' ' || buf[i+1] == '\t'):
			width = 1
		case c == '[' && i+1 < end && buf[i+1] == '^':
			if k := bytes.Index(buf[i+2:end], []byte("]:")); k >= 0 {
				width = k + 4
			}
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
		if col > maxIndent {
			set(i, '\\')
			g.indent.add(off, end)
			return
		}
		if n++; n > maxContainers {
			set(i, '\\')
			g.containers.add(off, end)
			return
		}
		i += width
		col += width
	}
}

// definitionSplitter bounds what goldmark's reading of a paragraph's
// leading link reference definitions costs: its time grows with the lines
// of its definitions times the paragraph's lines (each line of a label or
// a title is found from the paragraph's end, and each definition taken out
// moves the lines after it). Before goldmark reads them, a paragraph's line
// is read as a blank line — in goldmark's reading of the document, the
// paragraph ends there — when it is the maxDefinitionRun-th line that
// starts with "[" (a definition may) since the paragraph's start or the
// last such rewrite, or, when the first line since then starts with "["
// (only then are definitions read), the maxDefinitionLines-th line since
// then. In the guard's own parse, the lines after it are made paragraphs
// of their own, as goldmark's reading of the document reads them (whose
// definitions and tables the guard's parse does not read: their inline
// cost is counted all the same). Its state is the parse's (parser.Context).
type definitionSplitter struct{}

var definitionSplitterKey = parser.NewContextKey()

func (definitionSplitter) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	r, _ := pc.Get(definitionSplitterKey).(*rewrite)
	if r == nil {
		r = &rewrite{}
		pc.Set(definitionSplitterKey, r)
	}
	src := reader.Source()
	segs := node.Lines()
	var cuts []int // the lines read as blank
	defs, lines, leading := 0, 0, false
	for i := 0; i < segs.Len(); i++ {
		s := segs.At(i)
		start, stop := clamp(s.Start, len(src)), clamp(s.Stop, len(src))
		j := start
		for j < stop && (src[j] == ' ' || src[j] == '\t') {
			j++
		}
		bracket := j < stop && src[j] == '['
		if lines == 0 {
			leading = bracket
		}
		lines++
		if bracket {
			defs++
		}
		if defs < maxDefinitionRun && (!leading || lines < maxDefinitionLines) {
			continue
		}
		for x := start; x < stop; x++ {
			if src[x] != '\n' && src[x] != '\r' {
				src[x] = ' '
			}
		}
		r.add(start, stop)
		cuts = append(cuts, i)
		defs, lines = 0, 0
	}
	if len(cuts) == 0 {
		return
	}
	all := segs.Sliced(0, segs.Len())
	parent, prev := node.Parent(), ast.Node(node)
	cuts = append(cuts, len(all))
	for k := 0; k+1 < len(cuts); k++ {
		if cuts[k]+1 >= cuts[k+1] {
			continue
		}
		p := ast.NewParagraph()
		for _, s := range all[cuts[k]+1 : cuts[k+1]] {
			p.Lines().Append(s)
		}
		parent.InsertAfter(parent, prev, p)
		prev = p
	}
	node.Lines().SetSliced(0, cuts[0])
}

// tableGuard is goldmark's table paragraph transformer behind the guard's
// bound on tables. Before goldmark reads a paragraph's lines as a table, it
// reads them as goldmark will — the table is the first delimiter row's,
// unless its previous line, the header row, has more cells (then the
// paragraph has none) — and makes a delimiter row no
// delimiter row (its first byte that is no space is read as "\\") when it
// has more than maxTableColumns columns, or when the cells goldmark would
// add to its rows that lack some pass what the document's maxTablePadding
// has left. goldmark then reads the next delimiter row, as it would have;
// once a paragraph's row was rewritten for its cells, every later one is
// (each line is read at most twice). It rewrites the reader's source — the
// guard's copy of the document —, its state is the parse's
// (parser.Context).
type tableGuard struct{}

var tableGuardKey = parser.NewContextKey()

// tableGuardState is what tableGuard read and rewrote in one parse.
type tableGuardState struct {
	padding int     // the missing cells of the tables read
	columns rewrite // delimiter rows with too many columns
	cells   rewrite // delimiter rows whose tables would add too many cells
}

// merge adds what another parse rewrote: goldmark's reading of the
// document, after the guard's, rewrites only rows the guard's parse read
// in another paragraph (one that a line read as blank ends).
func (st *tableGuardState) merge(o tableGuardState) {
	st.columns.merge(o.columns)
	st.cells.merge(o.cells)
}

var tableTransformer = extension.NewTableParagraphTransformer()

func (tableGuard) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	st, _ := pc.Get(tableGuardKey).(*tableGuardState)
	if st == nil {
		st = &tableGuardState{}
		pc.Set(tableGuardKey, st)
	}
	src := reader.Source()
	lines := node.Lines()
	exhausted := false
	for i := 1; i < lines.Len(); i++ {
		d := lines.At(i)
		cols := delimiterColumns(d.Value(src))
		if cols == 0 {
			continue
		}
		head := rowCells(value(lines.At(i-1), src), cols+1)
		if head > cols {
			break // goldmark reads no table of the paragraph
		}
		if cols > maxTableColumns {
			noDelimiter(src, d)
			st.columns.add(d.Start, d.Stop)
			continue
		}
		padding := cols - head // goldmark adds a header row's missing cells too
		for j := i + 1; j < lines.Len() && !exhausted; j++ {
			padding += cols - rowCells(value(lines.At(j), src), cols)
			exhausted = st.padding+padding > maxTablePadding
		}
		if exhausted {
			noDelimiter(src, d)
			st.cells.add(d.Start, d.Stop)
			continue
		}
		st.padding += padding
		break
	}
	tableTransformer.Transform(node, reader, pc)
}

// value returns the bytes of s (its padding, a tab's columns, as spaces).
func value(s text.Segment, src []byte) []byte { return s.Value(src) }

// noDelimiter reads the delimiter row s as no delimiter row: its first byte
// that is no space is read as "\\".
func noDelimiter(src []byte, s text.Segment) {
	for i := s.Start; i < s.Stop && i < len(src); i++ {
		if !util.IsSpace(src[i]) {
			src[i] = '\\'
			return
		}
	}
}

// delimiterColumns returns the columns of a table delimiter row as goldmark
// reads one (extension/table.go: isTableDelim, parseDelimiter), or 0 when
// line is none.
func delimiterColumns(line []byte) int {
	if w, _ := util.IndentWidth(line, 0); w > 3 {
		return 0
	}
	allSep := true
	for _, b := range line {
		if b != '-' {
			allSep = false
		}
		if !util.IsSpace(b) && b != '-' && b != '|' && b != ':' {
			return 0
		}
	}
	if allSep {
		return 0
	}
	cols := bytes.Split(line, []byte{'|'})
	if util.IsBlank(cols[0]) {
		cols = cols[1:]
	}
	if len(cols) > 0 && util.IsBlank(cols[len(cols)-1]) {
		cols = cols[:len(cols)-1]
	}
	for _, c := range cols {
		// goldmark's patterns: ^\s*:?-+:?\s*$ (\s: tab, line feed, form
		// feed, carriage return, space).
		c = bytes.Trim(c, " \t\n\f\r")
		c = bytes.TrimPrefix(c, []byte(":"))
		c = bytes.TrimSuffix(c, []byte(":"))
		if len(c) == 0 || len(bytes.Trim(c, "-")) != 0 {
			return 0
		}
	}
	return len(cols)
}

// rowCells returns the cells of a table row as goldmark reads them
// (extension/table.go: parseRow), at most limit.
func rowCells(line []byte, limit int) int {
	line = util.TrimRightSpace(util.TrimLeftSpace(line))
	pos, end := 0, len(line)
	if end > 0 && line[0] == '|' {
		pos++
	}
	if end > 0 && line[end-1] == '|' {
		end--
	}
	n := 0
	for ; pos < end && n < limit; n++ {
		closure := pos
		for ; closure < end; closure++ {
			if line[closure] == '|' && (closure == 0 || line[closure-1] != '\\') {
				break
			}
		}
		pos = closure + 1
	}
	return n
}

// problems reports what the guard rewrote, each kind once: its message and
// the bytes of its first rewrite.
func (g *guarded) problems() []problem {
	var out []problem
	if g.containers.n > 0 {
		out = append(out, problem{fmt.Sprintf("%d lines open more than %d block quotes or list items: the rest of each is read as text", g.containers.n, maxContainers), g.containers.first, g.containers.last})
	}
	if g.indent.n > 0 {
		out = append(out, problem{fmt.Sprintf("%d lines open a block quote or list item past column %d: the rest of each is read as text, to bound how deep lists and block quotes nest", g.indent.n, maxIndent), g.indent.first, g.indent.last})
	}
	if g.defs.n > 0 {
		out = append(out, problem{fmt.Sprintf("a paragraph has more than %d lines that start with \"[\", or starts with one and has more than %d lines: %d lines are read as blank lines, to bound the time its link reference definitions take", maxDefinitionRun, maxDefinitionLines, g.defs.n), g.defs.first, g.defs.last})
	}
	if c := g.tables.columns; c.n > 0 {
		out = append(out, problem{fmt.Sprintf("%d tables have more than %d columns: each is read as a paragraph", c.n, maxTableColumns), c.first, c.last})
	}
	if c := g.tables.cells; c.n > 0 {
		out = append(out, problem{fmt.Sprintf("%d tables would add more than %d missing cells to their rows: each is read as a paragraph", c.n, maxTablePadding), c.first, c.last})
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
