package java

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/source"
)

// TestExtractScalesLinearly: extraction grows linearly with the source — a
// long literal table (generated code) and a class of many methods calling
// one another. Four times the size must not cost sixteen times the time.
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
	best := func(src []byte) time.Duration {
		d := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			start := time.Now()
			if _, err := p.Extract(context.Background(), source.FileID("src/p/T.java"), src); err != nil {
				t.Fatal(err)
			}
			if e := time.Since(start); e < d {
				d = e
			}
		}
		return d
	}
	for name, gen := range map[string]func(int) []byte{"table": table, "methods": methods} {
		n := 5000
		if name == "methods" {
			n = 500
		}
		small, large := best(gen(n)), best(gen(4*n))
		t.Logf("%s: %d %v, %d %v (x%.1f)", name, n, small, 4*n, large, float64(large)/float64(small))
		if large > 10*small {
			t.Errorf("%s: 4x the size took %.1fx the time (%v, %v): extraction is not linear", name, float64(large)/float64(small), small, large)
		}
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
	best := func(files map[string]string) time.Duration {
		d := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			start := time.Now()
			resolveRepo(t, files)
			if e := time.Since(start); e < d {
				d = e
			}
		}
		return d
	}
	small, large := best(nested(500)), best(nested(2000))
	t.Logf("500 types %v, 2000 types %v (x%.1f)", small, large, float64(large)/float64(small))
	if large > 10*small {
		t.Errorf("4x the types took %.1fx the time (%v, %v): resolution is not linear", float64(large)/float64(small), small, large)
	}
}
