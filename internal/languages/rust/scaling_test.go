package rust

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/testutil/scaling"
)

// TestExtractScalesLinearly: extraction of a long literal list (generated
// tables) grows linearly with it. Four times the elements must not cost
// sixteen times the time, as walking children with the runtime's
// NamedChild(i) did (it scans the children on every call). scaling.Linear:
// the sizes alternate, and only a ratio above the bound in every attempt
// fails.
func TestExtractScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	table := func(n int) []byte {
		var b strings.Builder
		b.WriteString("pub static T: &[u16] = &[")
		for i := 0; i < n; i++ {
			b.WriteString("0x1234, ")
		}
		b.WriteString("];\n")
		return []byte(b.String())
	}
	p := NewProvider()
	extract := func(src []byte) func() {
		return func() {
			if _, err := p.Extract(context.Background(), source.FileID("src/data.rs"), src); err != nil {
				t.Fatal(err)
			}
		}
	}
	scaling.Linear(t, "10k / 40k elements", 10, extract(table(10000)), extract(table(40000)))
}
