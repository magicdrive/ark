package xml

import (
	"strings"
	"testing"
	"time"
)

// TestLinearTime: one long line, deep nesting up to the bound and many
// nodes are read in time linear in the input: doubling the input at most
// roughly doubles the time (a quadratic step would quadruple it).
func TestLinearTime(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	inputs := map[string]func(n int) string{
		"one line": func(n int) string {
			return "<r>" + strings.Repeat(`<e a="1" b="é"/>`, n) + "</r>"
		},
		"deep": func(n int) string {
			// Nesting to the bound, repeated: n/200 subtrees of depth 200.
			sub := strings.Repeat("<a x='1'>", 200) + strings.Repeat("</a>", 200)
			return "<r>" + strings.Repeat(sub, n/200) + "</r>"
		},
		"attributes": func(n int) string {
			var b strings.Builder
			b.WriteString("<r>\n")
			for i := 0; i < n/100; i++ {
				b.WriteString("<e")
				for j := 0; j < 100; j++ {
					b.WriteString(" a" + strings.Repeat("x", j%7) + string(rune('a'+j%26)) + string(rune('a'+j/26)) + `="v"`)
				}
				b.WriteString("/>\n")
			}
			b.WriteString("</r>")
			return b.String()
		},
	}
	for name, gen := range inputs {
		small, large := gen(20000), gen(40000)
		ts, tl := best(t, small), best(t, large)
		if tl > 3*ts+20*time.Millisecond {
			t.Errorf("%s: %v for %d bytes, %v for %d bytes", name, ts, len(small), tl, len(large))
		}
		t.Logf("%s: %v for %d bytes, %v for %d bytes", name, ts, len(small), tl, len(large))
	}
}

func best(t *testing.T, src string) time.Duration {
	var min time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		extract(t, "p.xml", src)
		if d := time.Since(start); i == 0 || d < min {
			min = d
		}
	}
	return min
}
