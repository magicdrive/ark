package rust

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractRust verifies the Rust provider on arbitrary input: it never
// panics, keeps every range inside the source, never emits an empty name,
// gives an identity only to a reference whose Name is a declaration name
// (no receiver; a dynamic call never has one), and is deterministic.
func FuzzExtractRust(f *testing.F) {
	seeds := []string{
		"",
		"fn main() { println!(\"hi\"); }",
		"mod a { pub fn f() {} } use a::f as g; fn h() { g(); a::f(); self::a::f(); }",
		"struct S { x: i32 } impl S { fn new() -> Self { Self { x: 0 } } } impl T for S { fn m(&self) { self.m() } }",
		"trait T { fn m(&self); fn d(&self) { Self::m(self) } }",
		"#[cfg(unix)] fn p() {} #[cfg(windows)] fn p() {} fn q() { p() }",
		"macro_rules! m { ($e:expr) => { $e }; } fn f() { m!(1); crate::m!(2); }",
		"use super::super::*; use crate::{a::{self, b as c}, d::*};",
		"enum E { A, B(u8), C { y: i32 } } fn f(e: E) { match e { E::A => {}, E::B(v) => {}, E::C { y } => {} } }",
		"impl<T: Clone> Tr for Vec<T> where T: Copy { type Out = T; const N: usize = 1; }",
		"fn f() { let v = |x| x + 1; fn inner() {} inner(); m[k](); }",
		"#[path = \"x.rs\"] mod imp; extern crate alloc as al; extern \"C\" { fn ext(); }",
		"fn broken( { } fn good() {}",
		"impl { fn } struct { mod ::: <<< >>> ;;; r#type r#fn",
		"fn f() { 'a: loop { break 'a; } async { }.await; unsafe { } }",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		for _, file := range []string{"src/a/b.rs", "src/lib.rs", "scripts/x.rs"} {
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
			}
			for _, r := range ext.References {
				if r.Name == "" {
					t.Fatalf("empty reference name: %+v", r)
				}
				if r.NameQualified != "" && (r.ReceiverExpr != "" || r.Dynamic) {
					t.Fatalf("an identity on a member or dynamic reference: %+v", r)
				}
				if r.NameQualified != "" && (strings.HasSuffix(r.NameQualified, "::") || strings.Contains(r.NameQualified, "::::")) {
					t.Fatalf("malformed identity %q", r.NameQualified)
				}
				if r.Location.Range.Start.Line == 0 {
					t.Fatalf("reference has 0 (non-1-based) line: %+v", r)
				}
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ext, again) {
				t.Fatalf("non-deterministic extraction of %q", src)
			}
		}
	})
}
