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
// unbalanced brackets and emphasis delimiters, many reference definitions:
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
