package markdown

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
