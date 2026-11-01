package xml

import (
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/testutil/scaling"
)

// TestLinearTime: one long line, deep nesting up to the bound and many
// nodes are read in time linear in the input: four times the input takes at
// most ten times as long (a quadratic step would take sixteen), measured as
// scaling.Linear does, so that a burst of load on the machine does not fail
// it.
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
		"doctypes": func(n int) string {
			// One DOCTYPE after another, before and inside the root: each is
			// a diagnostic, and the rest of the file is scanned for entity
			// references once (the first), not once per DOCTYPE.
			return strings.Repeat("<!DOCTYPE a>", n/2) + "<r>" + strings.Repeat("<!DOCTYPE a>&e;", n/2) + "</r>"
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
		small, large := gen(20000), gen(80000)
		scaling.Linear(t, name, 10,
			func() { extract(t, "p.xml", small) },
			func() { extract(t, "p.xml", large) })
	}
}

// TestDoctypesNearMaxBytes: a file of DOCTYPE declarations just under
// xmldoc.MaxBytes is read in seconds (it took minutes when every DOCTYPE
// scanned the rest of the file).
func TestDoctypesNearMaxBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	const decl = "<!DOCTYPE a>"
	src := strings.Repeat(decl, (4<<20-16)/len(decl)) + "<r/>"
	start := time.Now()
	ex := extract(t, "d.xml", src)
	d := time.Since(start)
	t.Logf("%d bytes in %v, %d diagnostics", len(src), d, len(ex.Diagnostics))
	if d > 5*time.Second {
		t.Errorf("%d bytes of DOCTYPEs took %v", len(src), d)
	}
}
