package zig

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/source"
)

// TestExtractScalesLinearly: extraction of a long literal list (generated
// tables) and of a file of many declarations grows linearly with it: four
// times the size must not cost sixteen times the time.
func TestExtractScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	table := func(n int) []byte {
		var b strings.Builder
		b.WriteString("pub const T = [_]u16{")
		for i := 0; i < n; i++ {
			b.WriteString("0x1234, ")
		}
		b.WriteString("};\n")
		return []byte(b.String())
	}
	decls := func(n int) []byte {
		var b strings.Builder
		b.WriteString("const S = struct { const Self = @This(); x: u8 = 0, pub fn m(self: *Self) void { self.x += 1; } };\n")
		for i := 0; i < n; i++ {
			b.WriteString("pub fn f")
			b.WriteString(strings.Repeat("x", i%7))
			b.WriteString("() void { var s = S{}; s.m(); S.m(&s); }\n")
		}
		return []byte(b.String())
	}
	p := NewProvider()
	best := func(src []byte) time.Duration {
		d := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			start := time.Now()
			if _, err := p.Extract(context.Background(), source.FileID("src/data.zig"), src); err != nil {
				t.Fatal(err)
			}
			if e := time.Since(start); e < d {
				d = e
			}
		}
		return d
	}
	for name, gen := range map[string]func(int) []byte{"table": table, "declarations": decls} {
		small, large := best(gen(1000)), best(gen(4000))
		t.Logf("%s: 1k %v, 4k %v (x%.1f)", name, small, large, float64(large)/float64(small))
		if large > 10*small {
			t.Errorf("%s: 4x the size took %.1fx the time (1k: %v, 4k: %v): extraction is not linear", name, float64(large)/float64(small), small, large)
		}
	}
}
