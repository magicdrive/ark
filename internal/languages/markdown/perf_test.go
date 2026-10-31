package markdown

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/source"
)

// TestPerformance_Linear: extraction time grows linearly with the input —
// a huge line, many headings, many links, deep block quotes and lists,
// lists nested line by line (spaces, tabs, block quotes), unbalanced
// brackets and emphasis delimiters, many reference definitions and
// definitions whose titles span many lines, wide tables and tables whose
// rows lack cells:
// four times the input takes well under sixteen times as long (a quadratic
// cost would take sixteen). Each shape is measured at n and 4n, best of
// three.
func TestPerformance_Linear(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	shapes := map[string]func(n int) string{
		"one huge line":       func(n int) string { return strings.Repeat("word [l](a.md) `c` *e* ", n/24) },
		"many headings":       func(n int) string { return strings.Repeat("## heading\n\ntext\n\n", n/20) },
		"repeated headings":   func(n int) string { return strings.Repeat("# same\n", n/7) },
		"many links":          func(n int) string { return strings.Repeat("[x](#y) ![i](i.png) <https://e.x/p>\n", n/38) },
		"deep block quotes":   func(n int) string { return strings.Repeat("> ", n/4) + "# deep [l](a.md)\n" },
		"deep lists":          func(n int) string { return nestedList(n / 8) },
		"open brackets":       func(n int) string { return strings.Repeat("[", n) },
		"brackets and parens": func(n int) string { return strings.Repeat("[a](", n/4) },
		"emphasis":            func(n int) string { return strings.Repeat("*a _b ", n/6) },
		"definitions":         func(n int) string { return refDefs(n / 16) },
		"html ids":            func(n int) string { return strings.Repeat("<a id=\"x\"></a>\n", n/16) },
		"code fences":         func(n int) string { return strings.Repeat("```go\nx\n```\n", n/12) },
		"footnotes on a line": func(n int) string { return strings.Repeat("[^a]: ", n/6) + "x\n" },
		"wide table":          func(n int) string { return wideTable(n / 6) },
		"short table rows": func(n int) string {
			return wideTable(maxTableColumns)[:4*maxTableColumns+4] + strings.Repeat("a\n", n/2)
		},
		"tables in defs": func(n int) string {
			return strings.Repeat("[d]: d.md\n"+wideTable(maxTableColumns)[:4*maxTableColumns+4]+strings.Repeat("a\n", 500)+"\n", n/1530)
		},
		"definition titles": func(n int) string { return strings.Repeat("[d]: /u \""+strings.Repeat("t\n", 200)+"\"\n", n/410) },
		"unclosed titles":   func(n int) string { return strings.Repeat("[d]: /u \"\nt\nt\n", n/16) },
		"lists by tabs": func(n int) string {
			return stairs(n, func(i int) string { return strings.Repeat("\t", i/2) + strings.Repeat("  ", i%2) + "- a\n" })
		},
		"lists by spaces": func(n int) string { return stairs(n, func(i int) string { return strings.Repeat(" ", 2*i) + "- a\n" }) },
		"quotes and lists": func(n int) string {
			return stairs(n, func(i int) string { return strings.Repeat("> ", min(i, 20)) + strings.Repeat("\t", i/2) + "1. a\n" })
		},
		"capped lists": func(n int) string { return strings.Repeat(outline(maxIndent/4)+"\n", n/2466) },
	}
	n := 1 << 18
	if v, err := strconv.Atoi(os.Getenv("ARK_MD_PERF_N")); err == nil {
		n = v
	}
	names := make([]string, 0, len(shapes))
	for name := range shapes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		gen := shapes[name]
		small, large := []byte(gen(n)), []byte(gen(4*n))
		ts, tl := best(small), best(large)
		t.Logf("%-20s %7d bytes %8v   %7d bytes %8v   x%.1f", name, len(small), ts, len(large), tl, float64(tl)/float64(ts))
		if tl > 10*ts && tl > 200*time.Millisecond {
			t.Errorf("%s: %v for %d bytes, %v for %d: not linear", name, ts, len(small), tl, len(large))
		}
	}
}

func best(src []byte) time.Duration {
	p := NewProvider()
	d := time.Duration(1 << 62)
	for range 3 {
		start := time.Now()
		_, _ = p.Extract(context.Background(), source.FileID("a.md"), src)
		d = min(d, time.Since(start))
	}
	return d
}

func nestedList(depth int) string {
	var b strings.Builder
	for i := range depth {
		b.WriteString(strings.Repeat("  ", i%200))
		b.WriteString("- item [l](a.md)\n")
	}
	return b.String()
}

func refDefs(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("[r")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("]: a.md\n[r]\n")
	}
	return b.String()
}

// wideTable is a table of cols columns whose rows have one cell each: the
// cells goldmark adds grow with cols times its rows.
func wideTable(cols int) string {
	return "|" + strings.Repeat("a|", cols) + "\n|" + strings.Repeat("-|", cols) + "\n" + strings.Repeat("a\n", cols)
}

// stairs is line(0), line(1), … up to n bytes: lines nested one level more
// each (their size, and so the depth, grows with the square root of n).
func stairs(n int, line func(i int) string) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteString(line(i))
	}
	return b.String()
}
