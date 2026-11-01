package markdown

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// TestGuard_LeavesDocumentsAlone: the guard rewrites nothing of a written
// document — the fixture's, and documents dense with links and markup.
func TestGuard_LeavesDocumentsAlone(t *testing.T) {
	var srcs [][]byte
	_ = filepath.WalkDir(fixtureRepo, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".md") {
			b, _ := os.ReadFile(p)
			srcs = append(srcs, b)
		}
		return nil
	})
	srcs = append(srcs,
		[]byte(strings.Repeat(strings.Repeat("A paragraph with [a link](a.md), `code`, *emphasis* and ~~strike~~ (in parentheses).\n", 20)+"\n", 200)),
		[]byte(strings.Repeat("- [item](x.md) **bold** _it_\n", 20000)),
		[]byte(strings.Repeat("> > > quoted [x](y.md)\n> > >\n", 5000)),
		[]byte(strings.Repeat("| [a](a.md) | `b` | *c* |\n", 1)+"|---|---|---|\n"+strings.Repeat("| [a](a.md) | `b` | *c* |\n", 20000)),
		[]byte(strings.Repeat("[ref-with-a-long-label]: https://example.com/a/very/long/path_with_underscores_(and_parens)\n", maxDefinitionRun-1)+"\n[ref-with-a-long-label]\n"),
		// Definitions with their titles on the next line; a wide table
		// whose rows omit trailing cells; a deep outline.
		[]byte(strings.Repeat("[ref]: https://example.com/a\n    \"A title\"\n", maxDefinitionRun-1)+"\n[ref]\n"),
		[]byte(tableRow(40, "head")+tableRow(40, ":---:")+strings.Repeat(tableRow(20, "[a](a.md)"), 3000)),
		[]byte(strings.Repeat(outline(maxIndent/4), 100)),
	)
	for _, src := range srcs {
		g := guard(src)
		if !bytes.Equal(g.src, src) || len(g.problems()) != 0 {
			t.Errorf("the guard rewrote a document (%d bytes): %v", len(src), g.problems())
		}
	}
}

// TestGuard_Containers: on one line, the block quote or list item marker
// after the 32nd is read as a backslash — the rest of the line is a
// paragraph's text, its links still links; no byte moves.
func TestGuard_Containers(t *testing.T) {
	src := strings.Repeat("> ", 40) + "# deep [l](a.md)\n" + strings.Repeat("- 1. ", 20) + "x\n" + strings.Repeat("> ", 32) + "# kept\n"
	g := guard([]byte(src))
	want := []byte(src)
	want[2*maxContainers] = '\\'
	want[strings.Index(src, "\n")+1+5*16] = '\\' // the 33rd marker: "-" of the 17th "- 1. "
	if !bytes.Equal(g.src, want) {
		t.Errorf("guarded:\n%q\nwant\n%q", g.src, want)
	}
	x := extract(t, "a.md", src)
	if len(x.Diagnostics) != 1 || x.Diagnostics[0].Code != CodeDegenerate || x.Diagnostics[0].Location.Range.Start.Line != 1 {
		t.Errorf("diagnostics %v", x.Diagnostics)
	}
	equalLines(t, "sections", sections(x), []string{`section a.md#kept "kept" parent=a.md sig=h1 @3:65-3:71`})
	if len(refsOf(x, "resource_link")) != 1 {
		t.Errorf("links %v", refsOf(x, "resource_link"))
	}
}

// TestGuard_Footnotes: footnote definitions opened on one line count as
// its containers, their bytes as columns: the first past column maxIndent
// is read from "\\" (no 32 of them fit before it); fewer are left alone.
func TestGuard_Footnotes(t *testing.T) {
	first := strings.Repeat("[^a]: ", 40) + "x\n"
	second := strings.Repeat("[^b]: > - ", 10) + "x\n"
	third := strings.Repeat("[^c]:", 32) + "y\n"
	src := first + second + third
	g := guard([]byte(src))
	want := []byte(src)
	want[6*(maxIndent/6+1)] = '\\'
	want[len(first)+len(second)+5*(maxIndent/5+1)] = '\\'
	if !bytes.Equal(g.src, want) || g.indent.n != 2 || g.containers.n != 0 {
		t.Errorf("guarded:\n%q\nwant\n%q", g.src, want)
	}
}

// TestGuard_Definitions: of a paragraph's lines that start with "[", every
// 1000th is read as a blank line; the others stay definitions.
func TestGuard_Definitions(t *testing.T) {
	var b strings.Builder
	for i := range 2500 {
		b.WriteString("[d" + strings.Repeat("x", i%5) + "]: a.md\n")
	}
	b.WriteString("\n[dxxxx] [dx]\n")
	src := b.String()
	g := guard([]byte(src))
	if g.defs.n != 2 {
		t.Errorf("%d lines blanked, want 2", g.defs.n)
	}
	x := extract(t, "a.md", src)
	if len(refsOf(x, "resource_link")) != 2 {
		t.Errorf("links %v", refsOf(x, "resource_link"))
	}
}

// TestGuard_InlineBudget: past the budget, the costliest blocks' delimiters
// are read as spaces — theirs only; the other blocks keep their links.
func TestGuard_InlineBudget(t *testing.T) {
	hostile := strings.Repeat("*a", 20000)
	src := "# Title\n\n[kept](k.md)\n\n" + hostile + "[lost](l.md)\n\n[also kept](k2.md)\n"
	x := extract(t, "a.md", src)
	equalLines(t, "links", refsOf(x, "resource_link"), []string{
		`resource_link "k.md" -> k.md [file repo] in a.md#title @3:1-3:13`,
		`resource_link "k2.md" -> k2.md [file repo] in a.md#title @7:1-7:19`,
	})
	if len(x.Diagnostics) != 1 || x.Diagnostics[0].Code != CodeDegenerate || x.Diagnostics[0].Location.Range.Start.Line != 5 {
		t.Errorf("diagnostics %v", x.Diagnostics)
	}
}

// tableRow is a table row of n cells "cell".
func tableRow(n int, cell string) string { return "|" + strings.Repeat(cell+"|", n) + "\n" }

// tablesOf returns the column counts of the tables goldmark's reading of
// the guarded src builds.
func tablesOf(src string) []int {
	g := guard([]byte(src))
	var out []int
	_ = ast.Walk(md.Parser().Parse(text.NewReader(g.src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := n.(*extast.Table); ok && entering {
			out = append(out, len(t.Alignments))
		}
		return ast.WalkContinue, nil
	})
	return out
}

// TestGuard_TableColumns: a table of maxTableColumns columns is a table;
// with one more its delimiter row's first byte is read as "\\" — a
// paragraph, whose links stay links —, in the guard's first parse already
// (no cell is made), after the paragraph's definitions too.
func TestGuard_TableColumns(t *testing.T) {
	ok := tableRow(maxTableColumns, "a") + tableRow(maxTableColumns, "-") + tableRow(maxTableColumns, "[x](x.md)")
	wideHead := tableRow(maxTableColumns+1, "a")
	wide := wideHead + tableRow(maxTableColumns+1, "-") + tableRow(maxTableColumns+1, "[x](x.md)")
	src := ok + "\n" + wide + "\n[d]: d.md\n" + wide
	g := guard([]byte(src))
	want := []byte(src)
	want[len(ok)+1+len(wideHead)] = '\\'
	want[len(ok)+1+len(wide)+len("\n[d]: d.md\n")+len(wideHead)] = '\\'
	if !bytes.Equal(g.src, want) {
		t.Errorf("guarded:\n%q\nwant\n%q", g.src, want)
	}
	if got := tablesOf(src); len(got) != 1 || got[0] != maxTableColumns {
		t.Errorf("tables %v, want one of %d columns", got, maxTableColumns)
	}
	x := extract(t, "a.md", src)
	if len(x.Diagnostics) != 1 || x.Diagnostics[0].Code != CodeDegenerate || x.Diagnostics[0].Location.Range.Start.Line != 6 ||
		!strings.Contains(x.Diagnostics[0].Message, "2 tables have more than 128 columns") {
		t.Errorf("diagnostics %v", x.Diagnostics)
	}
	if n := len(refsOf(x, "resource_link")); n != 3*maxTableColumns+2 {
		t.Errorf("%d links, want %d", n, 3*maxTableColumns+2)
	}
}

// TestGuard_TablePadding: the cells goldmark adds to rows that lack some
// are bounded per file: a table that would pass what maxTablePadding has
// left is a paragraph; the tables before it stay tables.
func TestGuard_TablePadding(t *testing.T) {
	rows := maxTablePadding / (maxTableColumns - 1) * 3 / 4 // three quarters of the budget
	table := tableRow(maxTableColumns, "a") + tableRow(maxTableColumns, "-") + strings.Repeat("a\n", rows)
	full := tableRow(4, "a") + tableRow(4, "-") + strings.Repeat(tableRow(4, "b"), 1000)
	src := table + "\n" + full + "\n" + table + "\n" + full
	if got := tablesOf(src); !slices.Equal(got, []int{maxTableColumns, 4, 4}) {
		t.Errorf("tables %v", got)
	}
	g := guard([]byte(src))
	if g.tables.cells.n != 1 || g.tables.columns.n != 0 || g.tables.padding != rows*(maxTableColumns-1) {
		t.Errorf("guard %+v", g.tables)
	}
	x := extract(t, "a.md", src)
	if len(x.Diagnostics) != 1 || !strings.Contains(x.Diagnostics[0].Message, "missing cells") {
		t.Errorf("diagnostics %v", x.Diagnostics)
	}
	// A header row with fewer cells than the delimiter row is a table
	// too (goldmark adds the header's missing cells as well): its rows
	// count all the same.
	short := "a\n" + tableRow(maxTableColumns, "-") + strings.Repeat("a\n", 2*rows)
	if got := tablesOf(short); len(got) != 0 {
		t.Errorf("tables %v of a short header's table past the budget", got)
	}
	one := "a\n" + tableRow(maxTableColumns, "-") + "a\n"
	if got := tablesOf(one); !slices.Equal(got, []int{maxTableColumns}) {
		t.Errorf("tables %v", got)
	}
	if p := guard([]byte(one)).tables.padding; p != 2*(maxTableColumns-1) {
		t.Errorf("padding %d, want %d", p, 2*(maxTableColumns-1))
	}
}

// TestTableGuard_ReadsAsGoldmark: tableGuard's reading of a delimiter row
// and of a row's cells is goldmark's: for each header, delimiter and row,
// stock goldmark (its table extension) builds a table exactly when the
// guard reads one, of the guard's columns, with the guard's cells in the
// row (the others goldmark adds, with no text).
func TestTableGuard_ReadsAsGoldmark(t *testing.T) {
	stock := goldmark.New(goldmark.WithExtensions(extension.Table))
	heads := []string{"| a | b |", "a | b", "|a|b|c|", "a", "| a |", "|", "a\\|b | c", "`a\\|b` | c", "| a | b | c | d |", " | a | b | ", "a||b"}
	delims := []string{"|---|---|", "--- | ---", "|:-|-:|:-:|", ":-", "-", "|-|", "--|--", "| - | - | - | - |", "-:|:-", " --- | --- ", "-||-", "-|-a", ":--:|", "- -|-", "-|-|-\t"}
	rowsOf := []string{"| x | y |", "x", "", "|", "x | y | z | w", "x\\|y", "`x\\|y` | z", "|x|", "||", " x | y "}
	for _, h := range heads {
		for _, d := range delims {
			if strings.Trim(d, "-") == "" {
				continue // a setext heading's underline: no paragraph
			}
			for _, r := range rowsOf {
				src := h + "\n" + d + "\n" + r + "\n"
				cols := delimiterColumns([]byte(d + "\n"))
				guardTable := cols > 0 && rowCells([]byte(h+"\n"), cols+1) <= cols
				var tables []*extast.Table
				root := stock.Parser().Parse(text.NewReader([]byte(src)))
				_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
					if t, ok := n.(*extast.Table); ok && entering {
						tables = append(tables, t)
					}
					return ast.WalkContinue, nil
				})
				if guardTable != (len(tables) == 1) || len(tables) > 1 {
					t.Errorf("%q: guard reads a table %v, goldmark builds %d", src, guardTable, len(tables))
					continue
				}
				if !guardTable {
					continue
				}
				if len(tables[0].Alignments) != cols {
					t.Errorf("%q: %d columns, goldmark %d", src, cols, len(tables[0].Alignments))
				}
				if r == "" {
					continue // no row: goldmark reads the blank line as the table's end
				}
				body := tables[0].LastChild()
				real := 0
				for c := body.FirstChild(); c != nil; c = c.NextSibling() {
					if c.Lines().Len() > 0 {
						real++
					}
				}
				if got := rowCells([]byte(r+"\n"), cols); got != real || body.ChildCount() != cols {
					t.Errorf("%q: row cells %d, goldmark %d of %d", src, got, real, body.ChildCount())
				}
			}
		}
	}
}

// TestGuard_DefinitionLines: a paragraph that starts with "[" — the
// definitions goldmark reads — is cut (a line read as blank) at its
// maxDefinitionLines-th line, a definition's title spanning many lines
// too; a paragraph that starts otherwise is not.
func TestGuard_DefinitionLines(t *testing.T) {
	title := "[d]: /u \"" + strings.Repeat("t\n", 3*maxDefinitionLines) + "\"\n"
	g := guard([]byte(title + "\n" + "text [x]\n" + strings.Repeat("t [x]\n", 3*maxDefinitionLines)))
	if g.defs.n != 1 {
		t.Fatalf("%d lines read as blank, want 1", g.defs.n)
	}
	if line := bytes.Count(g.src[:g.defs.first], []byte("\n")) + 1; line != maxDefinitionLines {
		t.Errorf("the line read as blank is line %d, want %d", line, maxDefinitionLines)
	}
	// The cut ends the paragraph in goldmark's reading: what follows is a
	// paragraph whose definitions are read again.
	src := strings.Repeat("[a]: a.md\n", 10) + strings.Repeat("x\n", maxDefinitionLines-10) + "[b]: b.md\n\n[a] [b]\n"
	x := extract(t, "a.md", src)
	equalLines(t, "links", refsOf(x, "resource_link"), []string{
		`resource_link "a.md" -> a.md [file repo] in a.md @2003:1-2003:4`,
		`resource_link "b.md" -> b.md [file repo] in a.md @2003:5-2003:8`,
	})
	if len(x.Diagnostics) != 1 || x.Diagnostics[0].Location.Range.Start.Line != maxDefinitionLines {
		t.Errorf("diagnostics %v", x.Diagnostics)
	}
}

// TestGuard_Indentation: a block quote or list item marker past column
// maxIndent (a tab to the next multiple of 4, a marker's bytes counted) is
// read as "\\": lists nested line by line, with spaces, tabs or block
// quotes, nest no deeper than the column allows.
func TestGuard_Indentation(t *testing.T) {
	for _, c := range []struct {
		line string
		cut  int // the offset read as "\\", or -1
	}{
		{strings.Repeat(" ", maxIndent) + "- a", -1},
		{strings.Repeat(" ", maxIndent+1) + "- a", maxIndent + 1},
		{strings.Repeat("\t", maxIndent/4) + "- a", -1},
		{strings.Repeat("\t", maxIndent/4) + " 1. a", maxIndent/4 + 1},
		{"  \t" + strings.Repeat("\t", maxIndent/4-1) + "> a", -1},
		{"   \t" + strings.Repeat("\t", maxIndent/4-1) + " > a", maxIndent/4 + 4},
		{strings.Repeat("> ", maxIndent/8) + strings.Repeat(" ", maxIndent*3/4) + "- a", -1},
		{strings.Repeat("> ", maxIndent/8) + strings.Repeat(" ", maxIndent*3/4+1) + "- a", maxIndent + 1},
		{strings.Repeat(" ", 2*maxIndent) + "text - a", -1},
	} {
		g := guard([]byte(c.line + "\n"))
		want := []byte(c.line + "\n")
		if c.cut >= 0 {
			want[c.cut] = '\\'
		}
		if !bytes.Equal(g.src, want) {
			t.Errorf("%q: guarded %q", c.line, g.src)
		}
	}
	var b strings.Builder
	for i := range 2 * maxIndent {
		b.WriteString(strings.Repeat("\t", i/2) + strings.Repeat(" ", 2*(i%2)) + "- [l](a.md)\n")
	}
	deepest := 0
	_ = ast.Walk(md.Parser().Parse(text.NewReader(guard([]byte(b.String())).src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if _, ok := n.(*ast.ListItem); ok && entering {
			d := 0
			for p := n; p != nil; p = p.Parent() {
				if _, ok := p.(*ast.ListItem); ok {
					d++
				}
			}
			deepest = max(deepest, d)
		}
		return ast.WalkContinue, nil
	})
	if deepest != maxIndent/2+1 {
		t.Errorf("lists nest %d deep, want %d", deepest, maxIndent/2+1)
	}
	x := extract(t, "a.md", b.String())
	if len(x.Diagnostics) != 1 || x.Diagnostics[0].Location.Range.Start.Line != maxIndent/2+2 || len(refsOf(x, "resource_link")) != 2*maxIndent {
		t.Errorf("diagnostics %v, %d links", x.Diagnostics, len(refsOf(x, "resource_link")))
	}
}

// TestGuard_ByteOrderMark: a byte order mark that starts the file is read
// as line breaks: the first line's heading is one, its indentation
// unchanged (no indented code block), front matter after it skipped; the
// positions stay the file's.
func TestGuard_ByteOrderMark(t *testing.T) {
	x := extract(t, "a.md", "\xef\xbb\xbf# Title\n\nSee [t](#title)\n")
	equalLines(t, "sections", sections(x), []string{`section a.md#title "Title" parent=a.md sig=h1 @1:4-3:16`})
	equalLines(t, "refs", refsOf(x, "id_reference"), []string{`id_reference "title" -> a.md#title [repo kinds=section,anchor] in a.md#title @3:5-3:16`})
	x = extract(t, "a.md", "\xef\xbb\xbf   # Indented\n")
	equalLines(t, "sections", sections(x), []string{`section a.md#indented "Indented" parent=a.md sig=h1 @1:7-1:17`})
	x = extract(t, "a.md", "\xef\xbb\xbf---\ntitle: x\n---\n# After\n")
	equalLines(t, "sections", sections(x), []string{`section a.md#after "After" parent=a.md sig=h1 @4:1-4:8`})
	if len(x.Diagnostics) != 0 {
		t.Errorf("diagnostics %v", x.Diagnostics)
	}
}

// outline is a list nested depth deep, four columns a level.
func outline(depth int) string {
	var b strings.Builder
	for i := range depth {
		b.WriteString(strings.Repeat("    ", i) + "- [item](a.md)\n")
	}
	return b.String()
}
