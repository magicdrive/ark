package java

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractJava verifies the Java provider on arbitrary input: it never
// panics, keeps every range inside the source, never emits an empty name or
// identity, states only well-formed lookups (the notation of
// language.ReferenceDraft.Lookup), and is deterministic.
func FuzzExtractJava(f *testing.F) {
	seeds := []string{
		"",
		"package p; class A { void m() { m(); } }",
		"package p; import java.util.*; import static q.U.*; class A<T extends B> extends C implements D, E.F { T t; A() { this(1); } A(int i) { super(i); } }",
		"class A { int x; void m(int x) { x = 1; this.x = 2; new Object() { void n() { x++; } }; } }",
		"record P(int x, int y) { P { } static P o() { return new P(0, 0); } }",
		"enum E implements I { A(1) { void f() {} }, B; E(int i) {} E() {} }",
		"sealed interface S permits X {} final class X implements S {}",
		"class A { void m(Object o) { if (o instanceof String s && s.isEmpty()) {} switch (o) { case Integer i when i > 0 -> {} default -> {} } } }",
		"class A { Runnable r = () -> m(); java.util.function.Function<String, Integer> f = String::length; Object o = A.B.C.d(); }",
		"@interface Ann { String value() default \"\"; } @Ann(value = \"x\") class Z {}",
		"class A { void m() { class L { void n() { m(); } } new L().n(); int[] a = new int[]{1}; a.clone(); } }",
		"class A { void m( { } void n() { } }",
		"import ; class { void ( ) { } } ::: <<< >>>",
		"class A { String t = \"\"\"\n text\"\"\"; char c = '\\u0041'; long l = 1L; float f = 1f; }",
		"class Outer { class Inner extends Outer.Base<String>.Node {} static class Base<T> { class Node {} } }",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		for _, file := range []string{"src/main/java/p/A.java", "A.java"} {
			ext, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				return // an honest error is fine; it must not panic
			}
			n := uint32(len(src))
			for _, s := range ext.Symbols {
				if s.Name == "" || s.Qualified == "" {
					t.Fatalf("empty name/qualified: %+v", s)
				}
				if s.StartByte > s.EndByte || s.EndByte > n {
					t.Fatalf("symbol out-of-bounds range [%d,%d] len=%d: %q", s.StartByte, s.EndByte, n, s.Name)
				}
				if s.Location.Range.Start.Line == 0 {
					t.Fatalf("symbol has 0 (non-1-based) line: %q", s.Name)
				}
				if s.Parameters != "" && (!strings.HasPrefix(s.Parameters, "(") || !strings.HasSuffix(s.Parameters, ")")) {
					t.Fatalf("malformed parameters %q", s.Parameters)
				}
			}
			for _, r := range ext.References {
				if r.Name == "" {
					t.Fatalf("empty reference name: %+v", r)
				}
				if r.Location.Range.Start.Line == 0 {
					t.Fatalf("reference has 0 (non-1-based) line: %+v", r)
				}
				for _, l := range []string{r.Lookup, r.ReceiverLookup} {
					if l != "" && !wellFormedLookup(l) {
						t.Fatalf("malformed lookup %q of %+v", l, r)
					}
				}
				if r.NameQualified != "" || r.ReceiverTypeQualified != "" {
					t.Fatalf("a qualified identity (Java states lookups): %+v", r)
				}
				if r.ArgumentTypes != "" && strings.Count(r.ArgumentTypes, ",")+1 != r.ArgumentCount {
					t.Fatalf("argument types %q for %d arguments", r.ArgumentTypes, r.ArgumentCount)
				}
			}
			if !ext.IdentityOnly {
				t.Fatal("a Java file must be IdentityOnly")
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ext, again) {
				t.Fatalf("non-deterministic extraction of %q", src)
			}
		}
	})
}

// wellFormedLookup checks Lookup notation: steps of alternatives, each "?",
// "!", or "=K:Q" / "@K:T.N" with an optional member path of non-empty names.
func wellFormedLookup(l string) bool {
	for _, step := range strings.Split(l, ";") {
		for _, alt := range strings.Split(step, "|") {
			if alt == "?" || alt == "!" {
				continue
			}
			base := alt
			if i := strings.IndexAny(alt, "/~"); i >= 0 {
				base = alt[:i]
				rest := alt[i:]
				for rest != "" {
					j := strings.IndexAny(rest[1:], "/~")
					seg := rest
					if j >= 0 {
						seg = rest[:j+1]
					}
					if len(seg) < 2 {
						return false
					}
					rest = rest[len(seg):]
				}
			}
			if len(base) < 4 || (base[0] != '=' && base[0] != '@') || base[2] != ':' || !strings.ContainsRune("TVMC", rune(base[1])) {
				return false
			}
			if base[0] == '@' {
				q := base[3:]
				if i := strings.LastIndexByte(q, '.'); i <= 0 || i == len(q)-1 {
					return false
				}
			}
		}
	}
	return true
}
