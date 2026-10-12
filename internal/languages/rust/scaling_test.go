package rust

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/source"
)

// TestExtractScalesLinearly: extraction of a long literal list (generated
// tables) grows linearly with it. Four times the elements must not cost
// sixteen times the time, as walking children with the runtime's
// NamedChild(i) did (it scans the children on every call).
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
	best := func(src []byte) time.Duration {
		d := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			start := time.Now()
			if _, err := p.Extract(context.Background(), source.FileID("src/data.rs"), src); err != nil {
				t.Fatal(err)
			}
			if e := time.Since(start); e < d {
				d = e
			}
		}
		return d
	}
	small, large := best(table(10000)), best(table(40000))
	t.Logf("10k elements %v, 40k elements %v (x%.1f)", small, large, float64(large)/float64(small))
	if large > 10*small {
		t.Errorf("4x the elements took %.1fx the time (10k: %v, 40k: %v): extraction is not linear", float64(large)/float64(small), small, large)
	}
}
