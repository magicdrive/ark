package zig

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/testutil/scaling"
)

// TestExtractScalesLinearly: extraction of a long literal list (generated
// tables) and of a file of many declarations grows linearly with it: four
// times the size must not cost sixteen times the time (scaling.Linear: the
// sizes alternate, and only a ratio above the bound in every attempt fails).
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
	extract := func(src []byte) func() {
		return func() {
			if _, err := p.Extract(context.Background(), source.FileID("src/data.zig"), src); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, gen := range map[string]func(int) []byte{"table": table, "declarations": decls} {
		scaling.Linear(t, name+": 1k / 4k", 10, extract(gen(1000)), extract(gen(4000)))
	}
}
