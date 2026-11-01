package java

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/testutil/scaling"
)

// TestExtractScalesLinearly: extraction grows linearly with the source — a
// long literal table (generated code) and a class of many methods calling
// one another. Four times the size must not cost sixteen times the time
// (scaling.Linear: the sizes alternate, and only a ratio above the bound in
// every attempt fails), nor sixteen times the allocations — a count, the
// same under any load; a quadratic cost that allocates nothing is left to
// the time.
func TestExtractScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	table := func(n int) []byte {
		var b strings.Builder
		b.WriteString("class T { static final int[] T = {")
		for i := 0; i < n; i++ {
			b.WriteString("0x1234, ")
		}
		b.WriteString("}; }\n")
		return []byte(b.String())
	}
	methods := func(n int) []byte {
		var b strings.Builder
		b.WriteString("package p;\nclass M extends Base {\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "  int m%d(int x) { String s = \"a\" + x; return m%d(x + 1) + s.length() + this.f%d; }\n  int f%d;\n", i, (i+1)%n, i, i)
		}
		b.WriteString("}\n")
		return []byte(b.String())
	}
	p := NewProvider()
	extract := func(src []byte) func() {
		return func() {
			if _, err := p.Extract(context.Background(), source.FileID("src/p/T.java"), src); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, gen := range map[string]func(int) []byte{"table": table, "methods": methods} {
		n := 5000
		if name == "methods" {
			n = 500
		}
		small, large := extract(gen(n)), extract(gen(4*n))
		what := fmt.Sprintf("%s: %d / %d", name, n, 4*n)
		scaling.Linear(t, what, 10, small, large)
		// Extraction allocates a constant number per element, plus a few
		// regrowths: four times the input makes about four times the
		// allocations (a quadratic cost, sixteen).
		scaling.LinearAllocs(t, what, 4.5, small, large)
	}
}

// TestResolveScalesLinearly: resolving a file of many nested types (generated
// code: protobuf messages), each extending another, calling an inherited
// method and its own private overload, grows linearly — a type's supertypes
// are found from an index of its file's relations, and a private member's
// access from an index of its file's declarations, not by scans per
// reference.
func TestResolveScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	nested := func(n int) map[string]string {
		var b strings.Builder
		b.WriteString("package p;\nclass Outer {\n  static class T0 { void m() {} }\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "  static class T%d extends T%d { private void p(int x) {} void p(long x) {} void g() { m(); p(1); } }\n", i, i-1)
		}
		b.WriteString("}\n")
		return map[string]string{"src/p/Outer.java": b.String()}
	}
	small, large := func() { resolveRepo(t, nested(500)) }, func() { resolveRepo(t, nested(2000)) }
	scaling.Linear(t, "500 / 2000 types", 10, small, large)
	// As extraction: about four times the allocations. A per-reference
	// scan that allocates (an index rebuilt for every type) shows here
	// first: its time stays under the bound until it costs more than the
	// rest of the resolution.
	scaling.LinearAllocs(t, "500 / 2000 types", 4.5, small, large)
}
