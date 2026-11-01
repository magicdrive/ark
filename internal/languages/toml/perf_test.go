package toml

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestExtract_Linear: a file of many tables, one line of many keys (an
// inline table), deep nesting of arrays and inline tables, a long array of
// requirements and many array-of-tables elements are extracted in time
// linear in their size (16x the input within 40x the time).
func TestExtract_Linear(t *testing.T) {
	gen := map[string]struct {
		file string
		src  func(n int) string
	}{
		"tables": {"a.toml", func(n int) string {
			var b strings.Builder
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, "[t%d.u]\nk = %d\n", i, i)
			}
			return b.String()
		}},
		"one line": {"a.toml", func(n int) string {
			var b strings.Builder
			b.WriteString("x = {")
			for i := 0; i < n; i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "k%d = \"v\"", i)
			}
			b.WriteString("}\n")
			return b.String()
		}},
		"nested": {"a.toml", func(n int) string {
			d := n / 8
			return "x = " + strings.Repeat("{a = [", d) + strings.Repeat("]}", d) + "\n"
		}},
		"requirements": {"pyproject.toml", func(n int) string {
			var b strings.Builder
			b.WriteString("[project]\nname = \"p\"\ndependencies = [\n")
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, "  \"dist%d>=1\",\n", i%3000)
			}
			b.WriteString("]\n")
			return b.String()
		}},
		"array": {"a.toml", func(n int) string {
			return "x = [" + strings.Repeat("1, ", 4*n) + "1]\n"
		}},
		"string": {"a.toml", func(n int) string {
			return "x = \"" + strings.Repeat("ab\\u00e9", 4*n) + "\"\ny = \"\"\"\n" + strings.Repeat("line\n", n) + "\"\"\"\n"
		}},
		"dotted": {"a.toml", func(n int) string {
			var b strings.Builder
			for i := 0; i < n/10; i++ {
				fmt.Fprintf(&b, "%sk%d = 1\n", strings.Repeat("p.", 100), i)
			}
			return b.String()
		}},
		"elements": {"Cargo.toml", func(n int) string {
			var b strings.Builder
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, "[[bin]]\nname = \"b%d\"\n", i%50)
			}
			return b.String()
		}},
	}
	for name, g := range gen {
		small, large := g.src(1000), g.src(16000)
		ts, tl := timeExtract(t, g.file, small), timeExtract(t, g.file, large)
		t.Logf("%s: %d bytes %v, %d bytes %v", name, len(small), ts, len(large), tl)
		if tl > 40*ts+50*time.Millisecond {
			t.Errorf("%s: 16x the input took %v vs %v", name, tl, ts)
		}
	}
}

func timeExtract(t *testing.T, file, s string) time.Duration {
	best := time.Hour
	for i := 0; i < 3; i++ {
		start := time.Now()
		extract(t, file, s)
		if d := time.Since(start); d < best {
			best = d
		}
	}
	return best
}
