package rust

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func extract(t *testing.T, file, src string) language.Extraction {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !ex.IdentityOnly {
		t.Fatal("a Rust file must be IdentityOnly")
	}
	return ex
}

// symbolLines renders each declaration as "kind qualified".
func symbolLines(ex language.Extraction) []string {
	var out []string
	for _, s := range ex.Symbols {
		out = append(out, fmt.Sprintf("%s %s", s.Kind, s.Qualified))
	}
	return out
}

// refLine renders a reference as "kind name -> identity [cap]" ("?" without
// identity), prefixed by its line.
func refLine(r language.ReferenceDraft) string {
	q := r.NameQualified
	if q == "" {
		q = "?"
	}
	s := fmt.Sprintf("%d %s %s -> %s", r.Location.Range.Start.Line, r.Kind, r.Name, q)
	if r.ConfidenceCap != "" {
		s += " [" + r.ConfidenceCap + "]"
	}
	return s
}

func refLines(ex language.Extraction) []string {
	var out []string
	for _, r := range ex.References {
		out = append(out, refLine(r))
	}
	return out
}

func equalLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// hasRef reports a reference whose rendering (refLine) is line.
func hasRef(ex language.Extraction, line string) bool {
	for _, r := range ex.References {
		if refLine(r) == line {
			return true
		}
	}
	return false
}

func mustRef(t *testing.T, ex language.Extraction, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !hasRef(ex, l) {
			t.Errorf("missing reference %q; have:\n  %s", l, strings.Join(refLines(ex), "\n  "))
		}
	}
}

// noIdentity asserts that no reference named name carries an identity.
func noIdentity(t *testing.T, ex language.Extraction, name string) {
	t.Helper()
	for _, r := range ex.References {
		if r.Name == name && r.NameQualified != "" {
			t.Errorf("%s must not be resolved, got %s", name, refLine(r))
		}
	}
}

func TestSymbols_EveryItemKind(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
pub mod a {
    pub fn f() {}
}
mod file_mod;
pub struct S { pub x: i32, y: u8 }
pub struct T(u8);
pub union U { a: u32, b: f32 }
pub enum E { A, B(u8), C { z: i32 } }
pub trait Tr {
    type Out;
    const N: usize;
    fn m(&self);
    fn d(&self) {}
}
impl S {
    pub const K: i32 = 1;
    pub type Alias = u8;
    pub fn new() -> Self { S { x: 0, y: 0 } }
}
impl Tr for S {
    type Out = u8;
    const N: usize = 2;
    fn m(&self) {}
}
pub type Id = u64;
pub const C: i32 = 1;
pub static G: i32 = 2;
macro_rules! mac { () => {} }
extern "C" { fn ext(); }
fn outer() {
    fn inner() {}
    struct Local;
}
`)
	equalLines(t, "symbols", symbolLines(ex), []string{
		"module crate::a",
		"function crate::a::f",
		"module crate::file_mod",
		"struct crate::S",
		"field crate::S.x",
		"field crate::S.y",
		"struct crate::T",
		"struct crate::U",
		"field crate::U.a",
		"field crate::U.b",
		"enum crate::E",
		"constant crate::E::A",
		"constant crate::E::B",
		"constant crate::E::C",
		"trait crate::Tr",
		"type_alias crate::Tr::Out",
		"constant crate::Tr::N",
		"method crate::Tr::m",
		"method crate::Tr::d",
		"impl impl crate::S",
		"constant crate::S::K",
		"type_alias crate::S::Alias",
		"method crate::S::new",
		"impl <crate::S as crate::Tr>",
		"type_alias <crate::S as crate::Tr>::Out",
		"constant <crate::S as crate::Tr>::N",
		"method <crate::S as crate::Tr>::m",
		"type_alias crate::Id",
		"constant crate::C",
		"variable crate::G",
		"macro crate::mac!",
		"function crate::ext",
		"function crate::outer",
		"function crate::outer::{block}::inner",
		"struct crate::outer::{block}::Local",
	})
	for _, s := range ex.Symbols {
		if s.Qualified == "crate::S.y" && s.Exported {
			t.Error("a private field is not exported")
		}
		if s.Qualified == "crate::S.x" && !s.Exported {
			t.Error("a pub field is exported")
		}
		if s.Qualified == "crate::S::new" && (s.Parent != "impl crate::S" || s.Receiver != "S") {
			t.Errorf("inherent method parent/receiver = %q/%q", s.Parent, s.Receiver)
		}
	}
}

// TestIdentity_TraitImplIsNotInherent: an inherent method and a trait
// implementation's method of one name are two declarations, and a path
// Type::name names the inherent one (Rust's inherent-first rule).
func TestIdentity_TraitImplIsNotInherent(t *testing.T) {
	ex := extract(t, "src/storage.rs", `
pub struct Repository;
impl Repository { pub fn save(&self) {} }
pub trait Store { fn save(&self); }
impl Store for Repository { fn save(&self) { Repository::save(self) } }
`)
	got := map[string]bool{}
	for _, s := range ex.Symbols {
		got[s.Qualified] = true
	}
	for _, q := range []string{"crate::storage::Repository::save", "<crate::storage::Repository as crate::storage::Store>::save", "crate::storage::Store::save"} {
		if !got[q] {
			t.Errorf("missing %s; have %v", q, symbolLines(ex))
		}
	}
	mustRef(t, ex, "5 call save -> crate::storage::Repository::save", "5 implementation Store -> crate::storage::Store")
}

func TestLayout_ModulePaths(t *testing.T) {
	for file, want := range map[string]string{
		"src/lib.rs":                    "crate::f",
		"src/main.rs":                   "crate::f",
		"src/a.rs":                      "crate::a::f",
		"src/a/mod.rs":                  "crate::a::f",
		"src/a/b.rs":                    "crate::a::b::f",
		"src/a/b/mod.rs":                "crate::a::b::f",
		"src/bin/tool.rs":               "src/bin/tool.rs/crate::f",
		"src/bin/tool/main.rs":          "src/bin/tool/main.rs/crate::f",
		"src/bin/tool/x.rs":             "src/bin/tool/main.rs/crate::x::f",
		"crates/util/src/lib.rs":        "crates/util/crate::f",
		"crates/util/src/text.rs":       "crates/util/crate::text::f",
		"tests/it.rs":                   "tests/it.rs/crate::f",
		"tests/it/main.rs":              "tests/it/main.rs/crate::f",
		"examples/demo.rs":              "examples/demo.rs/crate::f",
		"crates/util/benches/b.rs":      "crates/util/benches/b.rs/crate::f",
		"build.rs":                      "build.rs/crate::f",
		"tests/common/mod.rs":           "{tests/common/mod.rs}::f",
		"src/a/src/b.rs":                "{src/a/src/b.rs}::f",
		"scripts/gen.rs":                "{scripts/gen.rs}::f",
		"src/my-mod.rs":                 "{src/my-mod.rs}::f",
		"crates/util/src/bin/x/main.rs": "crates/util/src/bin/x/main.rs/crate::f",
	} {
		ex := extract(t, file, "fn f() {}")
		if len(ex.Symbols) != 1 || ex.Symbols[0].Qualified != want {
			t.Errorf("%s: %v, want %s", file, symbolLines(ex), want)
		}
	}
}

// TestIdentity_CratesNeverShare: two crates' items of one path are two
// identities.
func TestIdentity_CratesNeverShare(t *testing.T) {
	seen := map[string]string{}
	for _, file := range []string{"src/lib.rs", "crates/a/src/lib.rs", "crates/b/src/lib.rs", "src/bin/x.rs", "tests/t.rs", "examples/e.rs", "build.rs"} {
		ex := extract(t, file, "fn helper() {}")
		q := ex.Symbols[0].Qualified
		if other, ok := seen[q]; ok {
			t.Errorf("%s and %s share identity %s", file, other, q)
		}
		seen[q] = file
	}
}

func TestUse_PathsAliasesAndRelativeSegments(t *testing.T) {
	ex := extract(t, "src/a/b.rs", `
use crate::x::Y;
use super::sibling::Z as Zed;
use self::inner::W;
use super::super::top;
use crate::m::{self, n::{P, Q as Q2}};
use local::thing;
use std::collections::HashMap;
mod inner { pub struct W; }
mod local;
fn f() {
    Y::new();
    Zed::make();
    W::go();
    m::run();
    Q2::q();
    thing();
}
`)
	mustRef(t, ex,
		"2 import Y -> crate::x::Y",
		"3 import Z -> crate::a::sibling::Z",
		"4 import W -> crate::a::b::inner::W",
		"5 import top -> crate::top",
		"6 import m -> crate::m",
		"6 import P -> crate::m::n::P",
		"6 import Q -> crate::m::n::Q",
		// A plain first segment outside the crate root reads by edition.
		"7 import thing -> crate::a::b::local::thing [candidate]",
		"8 import HashMap -> ?",
		"12 call new -> crate::x::Y::new",
		"13 call make -> crate::a::sibling::Z::make",
		"14 call go -> crate::a::b::inner::W::go",
		"15 call run -> crate::m::run",
		"16 call q -> crate::m::n::Q::q",
		"17 call thing -> crate::a::b::local::thing [candidate]",
	)
}

// TestUse_ScopeIsNotInherited: a module does not see its parent's names;
// a block sees its function's module.
func TestUse_ScopeIsNotInherited(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
fn helper() {}
mod child {
    fn call() { helper(); super::helper(); }
}
fn top() { helper(); }
`)
	mustRef(t, ex, "4 call helper -> ?", "4 call helper -> crate::helper", "6 call helper -> crate::helper")
}

func TestSuper_AboveCrateRootIsUnresolved(t *testing.T) {
	ex := extract(t, "src/lib.rs", "fn f() { super::g(); }")
	noIdentity(t, ex, "g")
	ex = extract(t, "src/a.rs", "fn f() { super::super::g(); super::g(); }")
	mustRef(t, ex, "1 call g -> ?", "1 call g -> crate::g")
}

// TestShadowing: a local binding or a generic parameter hides an item of
// the same name; such names are not resolved to the item.
func TestShadowing(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
fn item() {}
struct T;
const K: i32 = 1;
fn f<T>(item: fn(), t: T) {
    item();
    let k = K;
    let g = |K: i32| K;
}
fn h() { let item = 1; item(); }
fn ok() { item(); let x: T = T; }
`)
	for _, l := range refLines(ex) {
		if strings.HasPrefix(l, "6 ") || strings.HasPrefix(l, "10 ") {
			if strings.Contains(l, "crate::item") {
				t.Errorf("a parameter / local shadows the function: %s", l)
			}
		}
		if strings.HasPrefix(l, "5 type_use T -> crate::T") {
			t.Errorf("a generic parameter shadows the struct: %s", l)
		}
	}
	mustRef(t, ex, "11 call item -> crate::item", "11 type_use T -> crate::T", "11 value_reference T -> crate::T")
	// K is shadowed by the closure parameter: neither K of line 8 is the
	// constant (the function's locals over-approximate: line 7 is lost too).
	for _, l := range refLines(ex) {
		if strings.HasPrefix(l, "8 ") && strings.Contains(l, "crate::K") {
			t.Errorf("closure parameter shadows the constant: %s", l)
		}
	}
}

// TestCfgAlternatives: two #[cfg] alternatives of one item share their
// identity: both are declarations, a reference is ambiguous (the resolver
// makes it a Candidate), never one of them.
func TestCfgAlternatives(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
#[cfg(unix)]
fn platform() {}
#[cfg(windows)]
fn platform() {}
fn f() { platform(); }
`)
	n := 0
	for _, s := range ex.Symbols {
		if s.Qualified == "crate::platform" {
			n++
			if !strings.HasPrefix(s.Signature, "#[cfg(") {
				t.Errorf("the cfg is not in the signature: %q", s.Signature)
			}
		}
	}
	if n != 2 {
		t.Errorf("%d declarations of crate::platform, want 2", n)
	}
	mustRef(t, ex, "6 call platform -> crate::platform")
}

func TestGlob(t *testing.T) {
	// One glob into a module of this file, all of whose items are known.
	ex := extract(t, "src/lib.rs", `
mod m { pub fn a() {} }
mod n { pub fn b() {} }
mod one { use super::m::*; fn f() { a(); } }
mod two { use super::m::*; use super::n::*; fn f() { a(); } }
mod file { use crate::other::*; fn f() { a(); } }
mod shadow { use super::m::*; fn a() {} fn f() { a(); } }
`)
	mustRef(t, ex, "4 call a -> crate::m::a", "5 call a -> ?", "6 call a -> ?", "7 call a -> crate::shadow::a")
}

// TestMacros: macro_rules! by textual scope, #[macro_export] at the crate
// root, a path-based invocation; an item-level macro makes its module's
// names unknown; macro arguments are never parsed.
func TestMacros(t *testing.T) {
	ex := extract(t, "src/a.rs", `
fn before() { m!(); }
macro_rules! m { () => { 1 } }
#[macro_export]
macro_rules! exported { () => {} }
fn after() { m!(); exported!(); crate::exported!(); helper(); }
fn helper() {}
fn args() { m!(helper()); }
`)
	mustRef(t, ex,
		"2 call m! -> ?",
		"6 call m! -> crate::a::m!",
		"6 call exported! -> crate::exported!",
		"6 call exported! -> crate::exported!",
		"6 call helper -> crate::a::helper",
	)
	for _, l := range refLines(ex) {
		if strings.HasPrefix(l, "8 call helper") {
			t.Errorf("a macro's token tree is not parsed: %s", l)
		}
	}
	for _, s := range ex.Symbols {
		if s.Kind == symbol.KindMacro && s.Name != "m" && s.Name != "exported" {
			t.Errorf("an invocation is not a definition: %s", s.Qualified)
		}
	}

	ex = extract(t, "src/lib.rs", `
fn helper() {}
some_crate::declare!();
lazy_static! { static ref X: u8 = 1; }
fn f() { helper(); }
mod clean { fn helper() {} fn g() { helper(); } }
`)
	for _, s := range ex.Symbols {
		if s.Kind == symbol.KindMacro || strings.Contains(s.Qualified, "declare") {
			t.Errorf("an item-level invocation is not a definition: %s %s", s.Kind, s.Qualified)
		}
	}
	// A name the module declares is that item: a macro expanding to another
	// item of that name would not compile.
	mustRef(t, ex, "5 call helper -> crate::helper", "6 call helper -> crate::clean::helper")
	ex = extract(t, "src/lib.rs", `
fn helper() {}
some_crate::declare!();
fn f() { other(); }
`)
	mustRef(t, ex, "4 call other -> ?")

	// A statement macro of unknown expansion hides its block's names; one of
	// this file whose expansions are never items does not.
	ex = extract(t, "src/lib.rs", `
macro_rules! quiet { ($e:expr) => { let _ = $e; } }
macro_rules! loud { ($n:ident) => { fn $n() {} } }
fn helper() {}
fn a() { other::mac!(); helper(); }
fn b() { quiet!(1); helper(); }
fn c() { loud!(helper); helper(); }
`)
	mustRef(t, ex, "5 call helper -> ?", "6 call helper -> crate::helper", "7 call helper -> ?")

	// A nested standard expression macro keeps the expansion item-free —
	// unless the file defines a macro of that name.
	ex = extract(t, "src/lib.rs", `
macro_rules! check { ($e:expr) => { assert!($e) } }
fn helper() {}
fn a() { check!(true); helper(); }
`)
	mustRef(t, ex, "4 call helper -> crate::helper")
	ex = extract(t, "src/lib.rs", `
macro_rules! check { ($e:expr) => { assert!($e) } }
macro_rules! assert { ($e:expr) => { fn helper() {} } }
fn helper() {}
fn a() { check!(true); helper(); }
`)
	mustRef(t, ex, "5 call helper -> ?")
}

// TestMacros_LaterDefinitionOutsideScope: a definition between the one in
// scope and the invocation, in a module that may export it (#[macro_use]),
// leaves the invocation unresolved.
func TestMacros_LaterDefinitionOutsideScope(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
macro_rules! m { () => {} }
#[macro_use]
mod inner { macro_rules! m { () => { 2 } } }
fn f() { m!(); }
`)
	noIdentity(t, ex, "m!")
}

// TestMacros_Hazards: between a definition and an invocation, a
// #[macro_use] module of another file or an item-level invocation of
// unknown expansion may bring another macro of the name.
func TestMacros_Hazards(t *testing.T) {
	for _, src := range []string{
		"macro_rules! m { () => {} }\n#[macro_use]\nmod inner;\nfn f() { m!(); }\n",
		"macro_rules! m { () => {} }\nother::gen!();\nfn f() { m!(); }\n",
		"macro_rules! m { () => {} }\ngen! { x }\nfn f() { m!(); }\n",
		"macro_rules! m { () => {} }\nfn f() { other::gen!(); m!(); }\n",
	} {
		ex := extract(t, "src/lib.rs", src)
		noIdentity(t, ex, "m!")
	}
	// A hazard in another function's body does not reach this one.
	ex := extract(t, "src/lib.rs", "macro_rules! m { () => {} }\nfn g() { other::gen!(); }\nfn f() { m!(); }\n")
	mustRef(t, ex, "3 call m! -> crate::m!")
}

func TestMethodCallsAreNeverResolved(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
struct S;
impl S { fn go(&self) {} fn run(&self) { self.go(); } }
fn go() {}
fn f(s: S) { s.go(); S.go(); }
`)
	for _, r := range ex.References {
		if r.Name == "go" && r.ReceiverExpr != "" && r.NameQualified != "" {
			t.Errorf("a method call is bound by name: %s", refLine(r))
		}
	}
}

func TestSelfInImplAndTrait(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
struct S;
enum E { A }
impl S { fn new() -> Self { Self } fn other() { Self::new(); } }
impl E { fn a() -> Self { Self::A } }
trait Tr { fn req(&self); fn def(&self) { Self::req(self); Self::missing(); } }
impl<T> Tr for Vec<T> { fn req(&self) { Self::new(); } }
`)
	mustRef(t, ex,
		"4 type_use Self -> crate::S",
		"4 call new -> crate::S::new",
		"5 value_reference A -> crate::E::A",
		"6 call req -> crate::Tr::req",
		"6 call missing -> ?",
		"7 call new -> ?",
	)
}

func TestImplOfUnresolvableTypeHasNoReachableIdentity(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
impl<T> Tr for T { fn m(&self) {} }
impl std::fmt::Display for Local { fn fmt(&self) {} }
struct Local;
`)
	for _, s := range ex.Symbols {
		if s.Name == "m" && !strings.HasPrefix(s.Qualified, "<{crate}T as {crate}Tr>") {
			t.Errorf("generic impl item %s", s.Qualified)
		}
		if s.Name == "fmt" && s.Qualified != "<crate::Local as {crate}std::fmt::Display>::fmt" {
			t.Errorf("foreign-trait impl item %s", s.Qualified)
		}
	}
}

func TestPathAttributeModuleIsOpaque(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
#[path = "imp_unix.rs"]
mod imp;
mod plain;
fn f() { imp::go(); plain::go(); }
`)
	mustRef(t, ex, "5 call go -> ?", "5 call go -> crate::plain::go")
}

func TestStructAndPatternReferences(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
struct P { x: i32 }
enum E { A(i32), B { y: i32 } }
fn f(e: E) -> P {
    match e {
        E::A(v) => P { x: v },
        E::B { y } => P { x: y },
    }
}
`)
	mustRef(t, ex,
		"4 type_use E -> crate::E",
		"4 type_use P -> crate::P",
		"6 value_reference A -> crate::E::A",
		"6 construction P -> crate::P",
		"7 type_use B -> crate::E::B",
	)
	// Field names (struct expressions, patterns, field access) are members:
	// never observed as references.
	for _, r := range ex.References {
		if r.Name == "x" || r.Name == "y" || r.Name == "v" {
			t.Errorf("a field or binding is observed: %s", refLine(r))
		}
	}
}

func TestUnknownLayoutResolvesNothingAcrossFiles(t *testing.T) {
	ex := extract(t, "scripts/gen.rs", "fn a() {} fn b() { a(); crate::a(); }")
	for _, r := range ex.References {
		if r.NameQualified != "" {
			t.Errorf("a file without layout resolves %s", refLine(r))
		}
	}
}

// TestDuplicateImportIsUnresolvedNotEither: a name two `use` declarations
// bind (#[cfg] alternatives, typically) resolves to neither path.
func TestDuplicateImportIsUnresolvedNotEither(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
mod a { pub fn x() {} }
mod b { pub fn x() {} }
#[cfg(unix)]
use a::x;
#[cfg(not(unix))]
use b::x;
fn f() { x(); }
`)
	mustRef(t, ex, "8 call x -> ?")
}

func TestDynamicCall(t *testing.T) {
	ex := extract(t, "src/lib.rs", "fn g() {}\nfn run(m: &[fn()], k: usize) { m[k](); }\n")
	n := 0
	for _, r := range ex.References {
		if r.Dynamic {
			n++
			if r.NameQualified != "" {
				t.Errorf("a dynamic call has an identity: %s", refLine(r))
			}
		}
	}
	if n != 1 {
		t.Errorf("%d dynamic references, want 1", n)
	}
}

// TestCfgModules: #[cfg] alternatives of a module each resolve their own
// body in their own scope, and a path through the module (which one is
// configuration-dependent) resolves nothing; a name an item and a #[cfg]
// import both bind is ambiguous.
func TestCfgModules(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
mod other { pub fn x() {} }
#[cfg(unix)]
mod imp { pub fn x() {} pub fn run() { x(); } }
#[cfg(windows)]
mod imp { use super::other::x; pub fn run() { x(); } }
fn main() { imp::run(); }
#[cfg(unix)]
mod sys;
#[cfg(not(unix))]
use other as sys;
fn f() { sys::x(); }
`)
	mustRef(t, ex,
		"4 call x -> crate::imp::x",
		"6 call x -> crate::other::x",
		"7 call run -> ?",
		"12 call x -> ?",
	)
	if hasRef(ex, "4 call x -> crate::other::x") || hasRef(ex, "6 call x -> crate::imp::x") {
		t.Errorf("a cfg alternative's body resolved in the other's scope:\n  %s", strings.Join(refLines(ex), "\n  "))
	}
}

func TestAttributesSurviveComments(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
struct S;
impl S {
    #[cfg(unix)]
    // the unix one
    fn f() {}
}
enum E {
    #[cfg(unix)]
    /* unix */
    A,
}
`)
	for _, s := range ex.Symbols {
		if (s.Name == "f" || s.Name == "A") && !strings.HasPrefix(s.Signature, "#[cfg(unix)]") {
			t.Errorf("%s lost its attribute: %q", s.Qualified, s.Signature)
		}
	}
}

// TestGenericPathPrefix: T::f() of a generic parameter T is never the item
// of T's name.
func TestGenericPathPrefix(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
struct T;
impl T { fn make() -> T { T } }
fn f<T: Default>() -> T { T::make(); T::default() }
fn g() { T::make(); }
`)
	mustRef(t, ex, "4 call make -> ?", "5 call make -> crate::T::make")
}

func TestBracedVariantsAndGuards(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
enum E { B { y: i32 } }
fn ok(v: i32) -> bool { v > 0 }
fn f(e: E) -> E {
    match e {
        E::B { y } if ok(y) => E::B { y },
    }
}
`)
	mustRef(t, ex, "6 type_use B -> crate::E::B", "6 call ok -> crate::ok", "6 construction B -> crate::E::B")
	for _, r := range ex.References {
		if r.Name == "B" && !strings.Contains(r.TargetKinds, "constant") {
			t.Errorf("a braced variant cannot reach the variant: %s %q", refLine(r), r.TargetKinds)
		}
	}
}

// TestUse_Edition2015Reading: a use path whose first segment the scope does
// not name is another crate's in Rust 2018 and the crate root's in Rust 2015;
// the edition is in Cargo.toml, so the 2015 reading is a Candidate — never
// for std, core or alloc, another crate in both.
func TestUse_Edition2015Reading(t *testing.T) {
	ex := extract(t, "src/hir/visitor.rs", `
use ast::Span;
use std::fmt;
use core::mem;
fn f(s: Span) { fmt::Debug; mem::swap(); }
`)
	mustRef(t, ex,
		"2 import Span -> crate::ast::Span [candidate]",
		"3 import fmt -> ?",
		"4 import mem -> ?",
		"5 type_use Span -> crate::ast::Span [candidate]",
	)
	// In the crate root both editions read it from the root: no reading to add.
	ex = extract(t, "src/lib.rs", "use ast::Span;\nfn f(s: Span) {}\n")
	mustRef(t, ex, "1 import Span -> ?", "2 type_use Span -> ?")
}

// TestGlob_EnumOfThisFile: a glob of an enum this file declares once brings
// exactly its variants; a name it does not bring is looked up outside it.
func TestGlob_EnumOfThisFile(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
pub enum Kind { A, B(u8), C { x: u8 } }
fn helper() {}
fn f(k: Kind) {
    use self::Kind::*;
    match k { A => {}, B(_) => {}, C { .. } => {} }
    let _ = B(1);
    helper();
}
fn g() { use Kind::*; fn A() {} A(); }
#[cfg(unix)]
enum Two { X }
#[cfg(not(unix))]
enum Two { Y }
fn h() { use Two::*; X(); }
fn i() { use other::Remote::*; Thing(); helper(); }
fn j() { use Kind::*; let _ = B(2); }
`)
	mustRef(t, ex,
		"6 type_use C -> crate::Kind::C",
		"7 call B -> crate::Kind::B",
		"8 call helper -> crate::helper",
		"10 call A -> crate::g::{block}::A",
		"16 call Thing -> ?",
		"16 call helper -> ?",
		"17 import Kind -> crate::Kind",
		"17 call B -> crate::Kind::B",
		"10 import Kind -> crate::Kind",
	)
	for _, r := range ex.References {
		if strings.Contains(r.NameQualified, "Two::") {
			t.Errorf("a glob of a #[cfg]-duplicated enum was followed: %s", refLine(r))
		}
	}
}

// TestAliases: a module's `use` bindings fixed without edition doubt are
// aliases; a binding of edition-dependent reading, a glob, a block's
// binding, a binding of nothing in the crate and a #[cfg]-duplicated one are
// not.
func TestAliases(t *testing.T) {
	ex := extract(t, "src/a.rs", `
pub use crate::storage::Repository;
pub use self::inner::Thing as Renamed;
use super::model::*;
use plain::Name;
use std::fmt;
#[cfg(unix)]
use crate::x::Dup;
#[cfg(not(unix))]
use crate::y::Dup;
mod inner { pub struct Thing; pub use crate::z::Deep; }
fn f() { use crate::q::Local; }
`)
	var got []string
	for _, a := range ex.Aliases {
		got = append(got, a.Qualified+" -> "+a.Target)
	}
	equalLines(t, "aliases", got, []string{
		"crate::a::Renamed -> crate::a::inner::Thing",
		"crate::a::Repository -> crate::storage::Repository",
		"crate::a::inner::Deep -> crate::z::Deep",
	})
}

// In a block, a use path's first segment the block itself declares reads
// differently by edition (2015: the crate root's): a Candidate.
func TestUse_BlockOwnNameIsEditionDependent(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
mod m { pub fn f() {} }
fn g() { mod m { pub fn f() {} } use m::f; f(); }
fn h() { use m::f; f(); }
`)
	mustRef(t, ex, "3 import f -> crate::g::{block}::m::f [candidate]", "4 import f -> crate::m::f", "4 call f -> crate::m::f")
}

// TestReview_GlobBringsOnlyVisibleNames: from outside a module, its glob
// brings its public names only — a private item is not brought, and the
// name is the prelude's (unresolved), never the private item.
func TestReview_GlobBringsOnlyVisibleNames(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
mod imp { type Result<T> = std::result::Result<T, ()>; fn drop(_x: u8) {} pub fn open() {} pub(super) fn half() {} }
pub use imp::*;
pub fn g() -> Result<(), String> { drop(1u8); open(); half(); Ok(()) }
mod child { fn private() {} mod inner { use super::*; fn f() { private(); } } }
`)
	mustRef(t, ex,
		"4 type_use Result -> ?",
		"4 call drop -> ?",
		"4 call open -> crate::imp::open",
		"4 call half -> ?",
		"5 call private -> crate::child::private", // a descendant sees its ancestor's private items
	)
}

// TestReview_MacroLetShadows: a macro of this file whose rules bind a name
// the invoker supplies (`let $n = …`) may shadow an item: what follows its
// invocation in the block is not resolved to the item; a binding the rules
// name themselves is hygienic and shadows nothing.
func TestReview_MacroLetShadows(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
macro_rules! bind { ($n:ident, $e:expr) => { let $n = $e; }; }
macro_rules! own { ($e:expr) => { let tmp = $e; }; }
pub fn helper() -> i32 { 1 }
pub fn f() -> i32 { bind!(helper, || 2); helper() }
pub fn g() -> i32 { own!(1); helper() }
`)
	mustRef(t, ex, "5 call helper -> ?", "6 call helper -> crate::helper")
}

// TestReview_CfgItemAgainstGlob: a #[cfg] item in a scope with a glob may be
// the glob's name in another configuration: unresolved, never the item.
func TestReview_CfgItemAgainstGlob(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
#[cfg(feature = "std")]
pub fn now() -> u64 { 0 }
#[cfg(not(feature = "std"))]
use self::nostd::*;
mod nostd { pub fn now() -> u64 { 1 } }
pub fn tick() -> u64 { now() }
`)
	mustRef(t, ex, "7 call now -> ?")
}

// TestReview_GeneratedNames: a derive other than the compiler's, or an
// attribute macro, may add names: a glob of its module brings unknown names,
// and a block with such an item is opaque.
func TestReview_GeneratedNames(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
mod m { #[derive(Debug, strum::EnumIter)] pub enum Color { Red } }
mod plain { #[derive(Debug, Clone)] #[allow(dead_code)] pub enum Shade { Dark } }
struct ColorIter;
impl ColorIter { fn new() {} }
fn f() { use m::*; ColorIter::new(); }
fn g() { use plain::*; Shade::Dark; ColorIter::new(); }
fn h() { #[my_attr] fn local() {} ColorIter::new(); }
`)
	mustRef(t, ex,
		"6 call new -> ?",
		"7 call new -> crate::ColorIter::new",
		"8 call new -> ?",
	)
}

// TestReview2_DuplicateBindingKeepsVisibility: a name two #[cfg] imports bind
// in a module is brought by a glob of the module (to resolve to nothing),
// never left to an outer scope's item; so is an `extern crate`'s.
func TestReview2_DuplicateBindingKeepsVisibility(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
mod a { pub struct X; } mod b { pub struct X; }
mod m { #[cfg(unix)] pub use crate::a::X; #[cfg(not(unix))] pub use crate::b::X; }
pub struct X;
pub fn f() { use m::*; let _v: X = X; }
mod n { pub extern crate serde; }
mod serde { pub fn x() {} }
fn g() { use n::*; serde::x(); }
`)
	for _, r := range ex.References {
		if r.Location.Range.Start.Line == 5 && r.NameQualified == "crate::X" || r.Location.Range.Start.Line == 8 && r.NameQualified == "crate::serde::x" {
			t.Errorf("a glob's name resolved to an outer item: %s", refLine(r))
		}
	}
}

// TestReview2_SpacedFragments: `$n: ident` is a fragment like `$n:ident`.
func TestReview2_SpacedFragments(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
macro_rules! bind { ($n: ident, $e: expr) => { let $n = $e; }; }
macro_rules! it { ($i: item) => { $i }; }
macro_rules! tt { ($($t: tt)*) => { $($t)* }; }
pub fn helper() -> i32 { 1 }
pub fn f() -> i32 { bind!(helper, || 2); helper() }
pub fn g() -> i32 { it!(fn helper() -> i32 { 3 }); helper() }
pub fn h() -> i32 { tt!(let helper = 4;); helper() }
`)
	mustRef(t, ex, "6 call helper -> ?", "7 call helper -> ?", "8 call helper -> ?")
}

// TestReview2_UseSelfInListIsCfgMarked: `#[cfg] use a::b::{self}` marks b.
func TestReview2_UseSelfInListIsCfgMarked(t *testing.T) {
	ex := extract(t, "src/lib.rs", `
mod x { pub mod b { pub fn f() {} } }
pub mod b { pub fn f() {} }
fn g() { b::f(); }
#[cfg(unix)]
use x::{b::{self}};
`)
	noIdentity(t, ex, "f")
}
