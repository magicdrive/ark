package zig

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/symbol"
)

func TestSymbols_EveryDeclarationKind(t *testing.T) {
	ex := extract(t, "src/a.zig", `const std = @import("std");
const cfg = @import("config.zon");
pub const Engine = struct {
    const Self = @This();
    count: usize = 0,
    pub fn init() Self { return .{}; }
};
const Color = enum(u8) { red, green = 2, _ };
const U = union(enum) { a: u32, b: void };
const E = error{ Oops, Bad };
const O = opaque {};
const Both = E || error{Other};
const Ptr = *const Engine;
const Alias = Engine;
const Num = 3;
extern fn puts(s: [*:0]const u8) c_int;
export fn exported() void {}
pub var counter: u32 = 1;
threadlocal var tl: u8 = 0;
test "basic" {}
test exported {}
comptime {}
fn List(comptime T: type) type {
    return struct { items: []T, pub fn len(self: @This()) usize { return self.items.len; } };
}
fn Box(comptime T: type) type {
    const Inner = struct { value: T };
    return Inner;
}
`)
	equalLines(t, "symbols", symbolLines(ex), []string{
		"module src/a.zig",
		"module src/a.zig::std",
		"constant src/a.zig::cfg",
		"struct src/a.zig::Engine",
		"type_alias src/a.zig::Engine::Self",
		"field src/a.zig::Engine.count",
		"method src/a.zig::Engine::init",
		"enum src/a.zig::Color",
		"field src/a.zig::Color::red",
		"field src/a.zig::Color::green",
		"struct src/a.zig::U",
		"field src/a.zig::U.a",
		"field src/a.zig::U.b",
		"type src/a.zig::E",
		"constant src/a.zig::E::Oops",
		"constant src/a.zig::E::Bad",
		"type src/a.zig::O",
		"type_alias src/a.zig::Both",
		"constant src/a.zig::Both::{error_set}::Other",
		"type_alias src/a.zig::Ptr",
		"type_alias src/a.zig::Alias",
		"constant src/a.zig::Num",
		"function src/a.zig::puts",
		"function src/a.zig::exported",
		"variable src/a.zig::counter",
		"variable src/a.zig::tl",
		`function src/a.zig::test "basic"`,
		"function src/a.zig::test exported",
		"function src/a.zig::List",
		"struct src/a.zig::List::()",
		"field src/a.zig::List::().items",
		"method src/a.zig::List::()::len",
		"function src/a.zig::Box",
		"struct src/a.zig::Box::Inner",
		"field src/a.zig::Box::Inner.value",
	})
	exported := map[string]bool{}
	for _, s := range ex.Symbols {
		exported[s.Qualified] = s.Exported
	}
	for q, want := range map[string]bool{
		"src/a.zig::Engine": true, "src/a.zig::Engine::Self": false, "src/a.zig::Engine::init": true,
		"src/a.zig::exported": true, "src/a.zig::counter": true, "src/a.zig::puts": false, "src/a.zig::Color": false,
	} {
		if exported[q] != want {
			t.Errorf("%s exported = %v, want %v", q, exported[q], want)
		}
	}
}

// The example of the task: Engine, Engine.init, Engine.run, main, the
// import alias math and math.calculate are distinct declarations, and every
// reference reaches the one it names.
func TestIdentity_EngineExample(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/main.zig": `const math = @import("math.zig");

pub const Engine = struct {
    const Self = @This();

    pub fn init() Self {
        return .{};
    }

    pub fn run(self: *Self) void {
        _ = self;
    }
};

pub fn main() void {
    var engine = Engine.init();
    engine.run();
    math.calculate();
}
`,
		"src/math.zig": `pub fn calculate() void {}
pub fn run() void {}
pub fn init() void {}
`,
	})
	expectResolutions(t, got, map[string]string{
		"src/main.zig:1 math.zig import":         "exact src/math.zig",
		"src/main.zig:6 Self type_use":           "exact src/main.zig::Engine::Self",
		"src/main.zig:16 Engine value_reference": "exact src/main.zig::Engine",
		"src/main.zig:16 init call":              "exact src/main.zig::Engine::init",
		"src/main.zig:17 run call":               "exact src/main.zig::Engine::run",
		"src/main.zig:18 math value_reference":   "exact src/main.zig::math",
		"src/main.zig:18 calculate call":         "exact src/math.zig::calculate",
		"src/main.zig:4 @This type_use":          "exact src/main.zig::Engine",
		"src/main.zig:10 Self type_use":          "exact src/main.zig::Engine::Self",
	})
}

// Two files declaring one name: each reference reaches its own file's, and
// a member of the same name in two containers is never the other's.
func TestNegative_SameNamesInOtherModulesAndContainers(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a.zig": `const b = @import("b.zig");
pub fn helper() void {}
const X = struct { pub fn go() void {} };
const Y = struct { pub fn go() void {} };
pub fn main() void {
    helper();
    b.helper();
    X.go();
    Y.go();
}
`,
		"src/b.zig": `pub fn helper() void {}
pub fn only() void {}
`,
		"src/c.zig": `pub fn main() void {
    helper();
    only();
}
`,
	})
	expectResolutions(t, got, map[string]string{
		"src/a.zig:6 helper call": "exact src/a.zig::helper",
		"src/a.zig:7 helper call": "exact src/b.zig::helper",
		"src/a.zig:8 go call":     "exact src/a.zig::X::go",
		"src/a.zig:9 go call":     "exact src/a.zig::Y::go",
		// c.zig declares neither and imports nothing: a name match is no
		// evidence, however unique the name is in the repository.
		"src/c.zig:2 helper value_reference": "",
		"src/c.zig:2 helper call":            "unresolved",
		"src/c.zig:3 only call":              "unresolved",
	})
}

// A method call resolves only through a receiver whose type the file
// proves; a unique method name in the repository is no evidence.
func TestNegative_UnprovenReceivers(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a.zig": `const other = @import("other.zig");
const Counter = struct {
    value: usize,
    callback: *const fn () void,
    pub fn increment(self: *Counter) void { self.value += 1; }
};
fn untyped(x: anytype) void { x.increment(); }
fn generic(comptime T: type, v: T) void { v.increment(); T.increment(); }
fn fromOther() void {
    var c = other.make();
    c.increment();
}
fn captured(items: []Counter) void {
    for (items) |*item| item.increment();
}
fn field(c: Counter) void { c.callback(); }
fn optional(c: ?*Counter) void { c.?.increment(); }
`,
		"src/other.zig": `pub fn make() u32 { return 0; }
pub fn increment() void {}
`,
	})
	for _, line := range []int{7, 8, 11, 14, 17} {
		key := "src/a.zig:" + strconv.Itoa(line) + " increment call"
		if v := got[key]; strings.ReplaceAll(v, "unresolved ; ", "") != "unresolved" {
			t.Errorf("%s: %q, want unresolved (the receiver's type is not proven)", key, v)
		}
	}
	noStrongTo(t, map[string]string{"src/a.zig:16 callback": got["src/a.zig:16 c.callback call"]}, "callback")
	ex := extract(t, "src/a.zig", `const C = struct { callback: *const fn () void };
fn field(c: C) void { c.callback(); }
`)
	equalLines(t, "field call", refLines(ex)[1:], []string{"2 type_use C -> src/a.zig::C", "2 call callback -> ? !"}[1:])
}

// Receivers the file proves: a declared type, a pointer to it, a
// construction, a call of a function of the file whose result type is
// written (through try for an error union), a field's declared type, a
// constant naming the type.
func TestReceivers_ProvenTypes(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a.zig": `const Inner = struct {
    pub fn poke(self: Inner) void { _ = self; }
};
const Counter = struct {
    inner: Inner,
    pub fn init() Counter { return .{ .inner = .{} }; }
    pub fn create() !*Counter { return error.Nope; }
    pub fn increment(self: *Counter) void { self.inner.poke(); }
};
const C = Counter;
pub fn main() !void {
    var a: Counter = undefined;
    a.increment();
    var b = Counter{ .inner = .{} };
    b.increment();
    var c = Counter.init();
    c.increment();
    const d = try Counter.create();
    d.increment();
    var e: *const C = &a;
    e.increment();
    var f = C.init();
    f.increment();
    const g = Counter.create() catch unreachable;
    g.increment();
}
`,
	})
	want := map[string]string{"src/a.zig:8 poke call": "exact src/a.zig::Inner::poke"}
	for _, line := range []int{13, 15, 17, 19, 21, 23, 25} {
		want["src/a.zig:"+strconv.Itoa(line)+" increment call"] = "exact src/a.zig::Counter::increment"
	}
	expectResolutions(t, got, want)
}

// A receiver typed by a container of another file is looked up there: the
// member may be a field holding a function, which is no declaration, so an
// identity no declaration carries leaves the call Unresolved — observed,
// neither outside nor ruling out a same-named method.
func TestReceivers_TypeOfAnotherFile(t *testing.T) {
	files := map[string]string{
		"src/a.zig": `const eng = @import("engine.zig");
fn f(e: *eng.Engine, g: eng.Engine) void {
    e.run();
    g.hook();
    e.missing();
}
`,
		"src/engine.zig": `pub const Engine = struct {
    hook: *const fn () void,
    pub fn run(self: *Engine) void { _ = self; }
};
`,
	}
	got := resolveRepo(t, files)
	expectResolutions(t, got, map[string]string{
		"src/a.zig:3 run call":     "exact src/engine.zig::Engine::run",
		"src/a.zig:4 hook call":    "unresolved",
		"src/a.zig:5 missing call": "unresolved",
	})
	ex := extract(t, "src/a.zig", files["src/a.zig"])
	for _, l := range []string{"3 call run -> src/engine.zig::Engine::run ?", "4 call hook -> src/engine.zig::Engine::hook ?"} {
		if !strings.Contains(strings.Join(refLines(ex), "\n"), l) {
			t.Errorf("missing %q in\n  %s", l, strings.Join(refLines(ex), "\n  "))
		}
	}
}

// Locals, parameters and captures shadow nothing they do not declare, and
// are not observed: a container's function of the same name is not their
// target.
func TestShadowing(t *testing.T) {
	ex := extract(t, "src/a.zig", `fn helper() void {}
const Outer = struct {
    fn inner(helper2: u8) void { _ = helper2; }
};
pub fn main(arg: u8) void {
    const local = helper;
    local();
    _ = arg;
    if (maybe()) |helperCapture| { helperCapture(); }
    blk: {
        const helper3 = 1;
        _ = helper3;
        break :blk;
    }
    helper();
}
fn maybe() ?*const fn () void { return null; }
`)
	equalLines(t, "references", refLines(ex), []string{
		"6 value_reference helper -> src/a.zig::helper",
		"9 call maybe -> src/a.zig::maybe",
		"15 call helper -> src/a.zig::helper",
	})
}

// A block's local is visible after its declaration only; a sibling block's
// local is not visible.
func TestScopes_BlockLocals(t *testing.T) {
	ex := extract(t, "src/a.zig", `const later = struct { fn f() void {} };
fn g() void {
    later.f();
    {
        const later2 = later;
        later2.f();
    }
    later.f();
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"3 value_reference later -> src/a.zig::later",
		"3 call f -> src/a.zig::later::f",
		"5 value_reference later -> src/a.zig::later",
		"6 call f -> src/a.zig::later::f",
		"8 value_reference later -> src/a.zig::later",
		"8 call f -> src/a.zig::later::f",
	})
}

// @This() is the innermost container it is written in, never an outer one.
func TestThis_InnermostContainer(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a.zig": `const Outer = struct {
    const Self = @This();
    pub fn go() void {}
    const Inner = struct {
        const Self = @This();
        pub fn go() void {}
        fn call() void { Self.go(); }
    };
    fn call() void { Self.go(); }
};
`,
		"src/file.zig": `const File = @This();
x: u8,
pub fn get(self: File) u8 { return self.x; }
pub fn set(self: *@This()) void { _ = self.get(); }
`,
	})
	expectResolutions(t, got, map[string]string{
		"src/a.zig:7 go call":           "exact src/a.zig::Outer::Inner::go",
		"src/a.zig:9 go call":           "exact src/a.zig::Outer::go",
		"src/file.zig:4 get call":       "exact src/file.zig::get",
		"src/file.zig:1 @This type_use": "exact src/file.zig",
	})
}

// Relative imports name the file by its path from the importer's
// directory; a named module never names a repository file, even one of its
// name; a missing file is unresolved (not outside); a path out of the
// repository is outside.
func TestImports_RelativeNamedMissingOutside(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a.zig": `const std = @import("std");
const local = @import("std.zig");
const sub = @import("sub/b.zig");
const up = @import("../top.zig");
const gone = @import("missing.zig");
const out = @import("../../outside.zig");
const builtin = @import("builtin");
const root = @import("root");
const opts = @import("build_options");
const zon = @import("build.zig.zon");
pub fn main() void {
    std.debug.print("", .{});
    local.print();
    sub.f();
    up.g();
    gone.h();
    out.i();
}
`,
		"src/std.zig":   "pub fn print() void {}\npub const debug = struct { pub fn print() void {} };\n",
		"src/sub/b.zig": "pub fn f() void {}\n",
		"top.zig":       "pub fn g() void {}\n",
	})
	expectResolutions(t, got, map[string]string{
		"src/a.zig:1 std import":               "unresolved",
		"src/a.zig:2 std.zig import":           "exact src/std.zig",
		"src/a.zig:3 sub/b.zig import":         "exact src/sub/b.zig",
		"src/a.zig:4 ../top.zig import":        "exact top.zig",
		"src/a.zig:5 missing.zig import":       "unresolved",
		"src/a.zig:6 ../../outside.zig import": "unresolved outside",
		"src/a.zig:7 builtin import":           "unresolved",
		"src/a.zig:9 build_options import":     "unresolved",
		"src/a.zig:10 build.zig.zon import":    "unresolved",
		"src/a.zig:12 print call":              "unresolved",
		"src/a.zig:13 print call":              "exact src/std.zig::print",
		"src/a.zig:14 f call":                  "exact src/sub/b.zig::f",
		"src/a.zig:15 g call":                  "exact top.zig::g",
		"src/a.zig:16 h call":                  "unresolved",
		"src/a.zig:17 i call":                  "unresolved outside",
	})
	noStrongTo(t, got, "debug")
}

// Re-exports and alias chains, within a file and across files, reach the
// declaration; a cycle names nothing; the alias itself is its own
// declaration.
func TestAliases_ChainsAndCycles(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/main.zig": `const x = @import("x.zig");
const A = B;
const B = C;
const C = struct { pub fn m() void {} };
const cyc = @import("cyc1.zig");
pub fn main() void {
    A.m();
    x.T.m();
    const t: x.T = undefined;
    _ = t;
    cyc.P.m();
}
`,
		"src/x.zig":    "const y = @import(\"y.zig\");\npub const T = y.T;\n",
		"src/y.zig":    "const z = @import(\"z.zig\");\npub const T = z.T;\n",
		"src/z.zig":    "pub const T = struct { pub fn m() void {} };\n",
		"src/cyc1.zig": "const c2 = @import(\"cyc2.zig\");\npub const P = c2.Q;\n",
		"src/cyc2.zig": "const c1 = @import(\"cyc1.zig\");\npub const Q = c1.P;\n",
	})
	expectResolutions(t, got, map[string]string{
		"src/main.zig:7 m call":             "exact src/main.zig::C::m",
		"src/main.zig:8 T value_reference":  "exact src/x.zig::T",
		"src/main.zig:8 m call":             "exact src/z.zig::T::m",
		"src/main.zig:9 T type_use":         "exact src/x.zig::T",
		"src/main.zig:11 P value_reference": "exact src/cyc1.zig::P",
		"src/main.zig:11 m call":            "unresolved",
		"src/cyc1.zig:2 Q value_reference":  "exact src/cyc2.zig::Q",
		"src/cyc2.zig:2 P value_reference":  "exact src/cyc1.zig::P",
	})
}

// A namespace a comptime condition chooses (conditional compilation) names
// every alternative: a member through it is a Candidate, never one of them.
func TestConditionalCompilation_IsCandidate(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a.zig": `const builtin = @import("builtin");
const posix = @import("posix.zig");
const win = @import("win.zig");
pub const Impl = if (builtin.os.tag == .windows) win.Impl else posix.Impl;
pub const Same = if (builtin.os.tag == .windows) posix.Impl else posix.Impl;
const Local = switch (builtin.os.tag) { .windows => struct { fn f() void {} }, else => struct { fn f() void {} } };
pub fn main() void {
    Impl.open();
    Same.open();
    Local.f();
}
`,
		"src/b.zig":     "const a = @import(\"a.zig\");\npub fn main() void { a.Impl.open(); }\n",
		"src/posix.zig": "pub const Impl = struct { pub fn open() void {} };\n",
		"src/win.zig":   "pub const Impl = struct { pub fn open() void {} };\n",
	})
	expectResolutions(t, got, map[string]string{
		"src/a.zig:8 open call": "candidate src/posix.zig::Impl::open|src/win.zig::Impl::open",
		"src/a.zig:9 open call": "exact src/posix.zig::Impl::open",
		"src/a.zig:10 f call":   "candidate src/a.zig::Local::{struct}::f|src/a.zig::Local::{struct}::f",
		"src/b.zig:2 open call": "candidate src/posix.zig::Impl::open|src/win.zig::Impl::open",
	})
}

// Generic container factories: a function whose one return is a container
// declares F::(); a call of it (any arguments) is that container. A
// factory with two returns, or a type a comptime parameter gives, is not
// followed.
func TestGenerics_ContainerFactories(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/list.zig": `pub fn List(comptime T: type) type {
    return struct {
        const Self = @This();
        items: []T,
        pub fn append(self: *Self, item: T) void { _ = item; self.grow(); }
        fn grow(self: *Self) void { _ = self; }
    };
}
pub fn Two(comptime b: bool) type {
    if (b) return struct { pub fn f() void {} };
    return struct { pub fn f() void {} };
}
pub fn Named(comptime T: type) type {
    const Impl = struct { value: T, pub fn get(self: Impl) T { return self.value; } };
    return Impl;
}
pub fn make(comptime T: type) T { return T.init(); }
`,
		"src/main.zig": `const list = @import("list.zig");
const IntList = list.List(u32);
pub fn main() void {
    var l: IntList = undefined;
    l.append(1);
    var l2: list.List(u8) = undefined;
    l2.append(2);
    list.Two(true).f();
    var n: list.Named(u8) = undefined;
    _ = n.get();
}
`,
	})
	expectResolutions(t, got, map[string]string{
		"src/list.zig:5 grow call":   "exact src/list.zig::List::()::grow",
		"src/main.zig:5 append call": "exact src/list.zig::List::()::append",
		"src/main.zig:7 append call": "exact src/list.zig::List::()::append",
		"src/main.zig:8 f call":      "unresolved",
		"src/main.zig:10 get call":   "exact src/list.zig::Named::Impl::get",
		"src/list.zig:17 init call":  "unresolved",
	})
}

// A name a usingnamespace of an enclosing container may bring is not
// resolved; one a nearer scope declares is.
func TestUsingnamespace_Unknown(t *testing.T) {
	ex := extract(t, "src/a.zig", `pub usingnamespace @import("other.zig");
const Own = struct { pub fn f() void {} };
pub fn main() void {
    helper();
    Own.f();
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"1 import other.zig -> src/other.zig",
		"4 call helper -> ?",
		"5 value_reference Own -> src/a.zig::Own",
		"5 call f -> src/a.zig::Own::f",
	})
}

// A name declared twice in one scope is ambiguous: a Candidate of both.
func TestDuplicateDeclarations_AreCandidates(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a.zig": `fn dup() void {}
fn dup() void {}
const S = struct { fn m() void {} };
const S = struct { fn m() void {} };
pub fn main() void {
    dup();
    S.m();
}
`,
	})
	expectResolutions(t, got, map[string]string{
		"src/a.zig:6 dup call": "candidate src/a.zig::dup|src/a.zig::dup",
		"src/a.zig:7 m call":   "candidate src/a.zig::S::m|src/a.zig::S::m",
	})
}

// Decl literals and enum literals resolve where a declared type fixes
// their result type; elsewhere they are not references.
func TestDeclAndEnumLiterals(t *testing.T) {
	ex := extract(t, "src/a.zig", `const Color = enum { red, green };
const Engine = struct {
    color: Color = .green,
    pub fn init() Engine { return .{}; }
    pub fn make() Engine { return .init(); }
};
pub fn main() void {
    const e: Engine = .init();
    const c: Color = .red;
    const d = .red;
    _ = e; _ = c; _ = d;
    var x: Engine = undefined;
    x = .init();
}
`)
	got := strings.Join(refLines(ex), "\n")
	for _, want := range []string{
		"3 value_reference green -> src/a.zig::Color::green",
		"5 call init -> src/a.zig::Engine::init",
		"8 call init -> src/a.zig::Engine::init",
		"9 value_reference red -> src/a.zig::Color::red",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n  %s", want, strings.ReplaceAll(got, "\n", "\n  "))
		}
	}
	for _, line := range []string{"10 ", "13 "} {
		for _, l := range refLines(ex) {
			if strings.HasPrefix(l, line) && !strings.Contains(l, "-> ?") && !strings.Contains(l, " x -> ") {
				t.Errorf("unexpected identity: %s", l)
			}
		}
	}
}

// Enum fields and error set names are declarations a type path reaches.
func TestEnumFieldsAndErrorSets(t *testing.T) {
	ex := extract(t, "src/a.zig", `const Color = enum { red, green };
const E = error{ Oops };
fn f() E!Color {
    if (false) return E.Oops;
    return Color.red;
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"3 type_use E -> src/a.zig::E",
		"3 type_use Color -> src/a.zig::Color",
		"4 value_reference E -> src/a.zig::E",
		"4 value_reference Oops -> src/a.zig::E::Oops",
		"5 value_reference Color -> src/a.zig::Color",
		"5 value_reference red -> src/a.zig::Color::red",
	})
}

// The grammar derives `?*const a.B` as `(?*const a).B`; the members belong
// to the path, which the prefixes make a type of.
func TestPrefixTypesBindLooserThanMembers(t *testing.T) {
	ex := extract(t, "src/a.zig", `const a = struct { const B = struct { fn m(self: *const B) void { _ = self; } }; };
fn f(x: ?*const a.B, y: *a.B, z: []const a.B) void {
    y.m();
    _ = x; _ = z;
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"1 type_use B -> src/a.zig::a::B",
		"2 value_reference a -> src/a.zig::a",
		"2 type_use B -> src/a.zig::a::B",
		"2 value_reference a -> src/a.zig::a",
		"2 type_use B -> src/a.zig::a::B",
		"2 value_reference a -> src/a.zig::a",
		"2 type_use B -> src/a.zig::a::B",
		"3 call m -> src/a.zig::a::B::m",
	})
}

// Computed callees are observed as dynamic; builtins naming fields by
// string resolve nothing.
func TestDynamicCalls(t *testing.T) {
	ex := extract(t, "src/a.zig", `const handlers = [_]*const fn () void{};
const S = struct { fn m() void {} };
fn f(i: usize, s: S) void {
    handlers[i]();
    @field(s, "m")();
    @call(.auto, S.m, .{});
}
`)
	got := strings.Join(refLines(ex), "\n")
	if !strings.Contains(got, "4 call handlers[i] -> ? !") {
		t.Errorf("subscripted call not dynamic:\n  %s", strings.ReplaceAll(got, "\n", "\n  "))
	}
	if !strings.Contains(got, "6 value_reference m -> src/a.zig::S::m") {
		t.Errorf("@call's callee not referenced:\n  %s", strings.ReplaceAll(got, "\n", "\n  "))
	}
	for _, r := range ex.References {
		if r.Location.Range.Start.Line == 5 && r.NameQualified != "" && r.Name == "m" {
			t.Errorf("@field resolved a name: %s", refLine(r))
		}
	}
}

// A primitive or a builtin is no reference; @"..." names the identifier.
func TestPrimitivesAndQuotedIdentifiers(t *testing.T) {
	ex := extract(t, "src/a.zig", `const @"weird name" = struct { fn @"fn"() void {} };
fn f(a: u8, b: i128, c: usize, d: c_int, e: bool, g: anyopaque) void {
    @"weird name".@"fn"();
    _ = .{ a, b, c, d, e, g, true, null, undefined };
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"3 value_reference weird name -> src/a.zig::weird name",
		"3 call fn -> src/a.zig::weird name::fn",
	})
}

// Source positions are the name's, 1-based, in bytes of the line; a
// non-ASCII text before a name moves its column by its UTF-8 length.
func TestSourcePositions(t *testing.T) {
	ex := extract(t, "src/a.zig", "const s = \"日本\"; const T = struct { fn m() void {} };\nfn f() void { T.m(); }\n")
	var found bool
	for _, r := range ex.References {
		if r.Name == "m" {
			found = true
			if r.Location.Range.Start.Line != 2 || r.Location.Range.Start.Column != 17 {
				t.Errorf("m at %d:%d, want 2:17", r.Location.Range.Start.Line, r.Location.Range.Start.Column)
			}
		}
	}
	if !found {
		t.Fatal("no reference m")
	}
	for _, s := range ex.Symbols {
		if s.Qualified == "src/a.zig::T" && (s.Location.Range.Start.Line != 1 || s.Location.Range.Start.Column != 21) {
			t.Errorf("T at %d:%d, want 1:21 (the declaration's start)", s.Location.Range.Start.Line, s.Location.Range.Start.Column)
		}
	}
}

// A file identity is its clean, slash-separated path: an import computes
// it whatever the spelling.
func TestFileKeysAndImportPaths(t *testing.T) {
	for _, tc := range []struct {
		from, spec, want string
		inRepo, ok       bool
	}{
		{"src/a.zig", "b.zig", "src/b.zig", true, true},
		{"src/a.zig", "./x/../b.zig", "src/b.zig", true, true},
		{"a.zig", "../b.zig", "../b.zig", false, true},
		{"src/a.zig", "std", "{module}std", true, true},
		{"src/a.zig", "my_mod", "{module}my_mod", true, true},
		{"src/a.zig", "a/b", "", false, false},
		{"src/a.zig", "data.zon", "", false, false},
		{"src/a.zig", `sub\b.zig`, "", false, false},
		{"src/a.zig", "/abs/b.zig", "", false, false},
	} {
		got, inRepo, ok := importTarget(tc.from, tc.spec)
		if got != tc.want || inRepo != tc.inRepo || ok != tc.ok {
			t.Errorf("importTarget(%q, %q) = %q,%v,%v want %q,%v,%v", tc.from, tc.spec, got, inRepo, ok, tc.want, tc.inRepo, tc.ok)
		}
	}
	if k := fileKey("src/./a.zig"); k != "src/a.zig" {
		t.Errorf("fileKey = %q", k)
	}
}

// Broken source never panics, keeps ranges inside the source and reports
// the parser's errors.
func TestMalformedSource(t *testing.T) {
	for _, src := range []string{
		"const = ;",
		"pub fn (",
		"const S = struct { fn m( ",
		"fn f() void { x.y.z.( }",
		"const a = @import(;\nconst b = @import(\"x.zig\").",
		"test \"x\" {",
		"\x00\xff\xfe",
		"",
	} {
		ex := extract(t, "src/bad.zig", src)
		for _, s := range ex.Symbols {
			if int(s.EndByte) > len(src) || s.StartByte > s.EndByte {
				t.Errorf("%q: symbol %s out of range", src, s.Qualified)
			}
		}
		if src != "" && len(ex.Diagnostics) == 0 {
			t.Errorf("%q: no diagnostic", src)
		}
	}
}

// Deep nesting is bounded: a diagnostic, no stack exhaustion.
func TestDeepNesting(t *testing.T) {
	src := "fn f() void { _ = " + strings.Repeat("(", 2000) + "1" + strings.Repeat(")", 2000) + "; }\n" +
		"const S = " + strings.Repeat("struct { const S = ", 400) + "u8" + strings.Repeat("; }", 400) + ";\n"
	ex := extract(t, "src/deep.zig", src)
	found := false
	for _, d := range ex.Diagnostics {
		found = found || d.Code == diagNestingTooDeep
	}
	if !found {
		t.Error("no nesting_too_deep diagnostic")
	}
}

// An empty container is valid source: no diagnostic, no field.
func TestEmptyContainers(t *testing.T) {
	ex := extract(t, "src/a.zig", "const A = opaque {};\nconst B = struct {};\nconst C = enum {};\nconst D = union {};\nfn g() void { const x = struct {}; _ = x; }\n")
	if len(ex.Diagnostics) != 0 {
		t.Errorf("diagnostics on valid source: %v", ex.Diagnostics)
	}
	for _, s := range ex.Symbols {
		if s.Name == "" {
			t.Errorf("symbol without a name: %s", s.Qualified)
		}
	}
}

// build.zig's literal module declarations map a named import to its root
// file (Strong at most); a module of a dependency, one a struct field or a
// computed name gives, and the compiler's std / builtin / root are not
// mapped.
func TestNamedModules_FromBuildZig(t *testing.T) {
	files := map[string]string{
		"build.zig": `const std = @import("std");
pub fn build(b: *std.Build, options: anytype) void {
    const util = b.createModule(.{ .root_source_file = b.path("lib/util.zig") });
    _ = b.addModule("core", .{ .root_source_file = b.path("src/core/root.zig"), .imports = &.{.{ .name = "helpers", .module = util }} });
    const exe = b.addExecutable(.{ .name = "x", .root_module = b.createModule(.{ .root_source_file = b.path("src/main.zig") }) });
    exe.root_module.addImport("util", util);
    exe.root_module.addImport("dep", b.dependency("dep", .{}).module("dep"));
    exe.root_module.addImport("computed", options.mod);
    const name = "dyn";
    _ = b.addModule(name, .{ .root_source_file = b.path("src/dyn.zig") });
    _ = b.addModule("std", .{ .root_source_file = b.path("src/fake_std.zig") });
    _ = b.addModule("escape", .{ .root_source_file = b.path("../outside.zig") });
}
`,
		"src/main.zig": `const std = @import("std");
const util = @import("util");
const core = @import("core");
const dep = @import("dep");
const dyn = @import("dyn");
pub fn main() void {
    util.helper();
    core.run();
    dep.x();
    dyn.y();
    std.debug.print();
}
`,
		"src/core/root.zig": "const helpers = @import(\"helpers\");\npub fn run() void { helpers.helper(); }\n",
		"lib/util.zig":      "pub fn helper() void {}\n",
		"src/dyn.zig":       "pub fn y() void {}\n",
		"src/fake_std.zig":  "pub const debug = struct { pub fn print() void {} };\n",
	}
	got := resolveRepo(t, files)
	expectResolutions(t, got, map[string]string{
		"src/main.zig:1 std import":          "unresolved",
		"src/main.zig:2 util import":         "strong lib/util.zig",
		"src/main.zig:3 core import":         "strong src/core/root.zig",
		"src/main.zig:4 dep import":          "unresolved",
		"src/main.zig:5 dyn import":          "unresolved",
		"src/main.zig:7 helper call":         "strong lib/util.zig::helper",
		"src/main.zig:8 run call":            "strong src/core/root.zig::run",
		"src/main.zig:9 x call":              "unresolved",
		"src/main.zig:10 y call":             "unresolved",
		"src/main.zig:11 print call":         "unresolved",
		"src/core/root.zig:2 helper call":    "strong lib/util.zig::helper",
		"src/core/root.zig:1 helpers import": "strong lib/util.zig",
	})
	ex := extract(t, "build.zig", files["build.zig"])
	var aliases []string
	for _, a := range ex.Aliases {
		if strings.HasPrefix(a.Qualified, "{module}") {
			aliases = append(aliases, a.Qualified+" -> "+a.Target+" "+a.ConfidenceCap)
		}
	}
	equalLines(t, "aliases", aliases, []string{
		"{module}core -> src/core/root.zig strong",
		"{module}helpers -> lib/util.zig strong",
		"{module}util -> lib/util.zig strong",
	})

	// Two builds mapping one name to two files: an import of it is either.
	files["pkg/build.zig"] = "pub fn build(b: anytype) void { _ = b.addModule(\"util\", .{ .root_source_file = b.path(\"u.zig\") }); }\n"
	files["pkg/u.zig"] = "pub fn helper() void {}\n"
	got = resolveRepo(t, files)
	expectResolutions(t, got, map[string]string{
		"src/main.zig:7 helper call": "candidate lib/util.zig::helper|pkg/u.zig::helper",
	})
	// A file named build.zig elsewhere is read the same way; one of
	// another name never maps a module.
	ex = extract(t, "src/not_build.zig", files["build.zig"])
	for _, a := range ex.Aliases {
		if strings.HasPrefix(a.Qualified, "{module}") {
			t.Errorf("a file that is no build.zig mapped a module: %v", a)
		}
	}
}

// A call through a value another file types, whose identity no declaration
// carries (a field holding a function, a function stating no result type),
// leaves every same-named method's callers incomplete: it is observed, never
// ruled out as their caller.
func TestCompleteness_UnprovenCallsStayAttributable(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/engine.zig", "pub const Engine = struct {\n    hook: *const fn () void,\n};\npub fn make() Engine { return undefined; }\n")
	write("src/worker.zig", "pub const Worker = struct {\n    pub fn hook(self: Worker) void { _ = self; }\n};\n")
	write("src/main.zig", "const eng = @import(\"engine.zig\");\npub fn main() void {\n    var e = eng.make();\n    e.hook();\n}\n")
	idx := buildIndex(t, dir)
	var hook symbol.Symbol
	for _, s := range idx.FindSymbols("hook") {
		if s.Qualified == "src/worker.zig::Worker::hook" {
			hook = s
		}
	}
	if hook.ID == "" {
		t.Fatal("no Worker.hook")
	}
	if in, _ := idx.Unattributed(hook.ID); in != 1 {
		t.Errorf("Worker.hook: unattributed incoming %d, want 1 (e.hook() may be it)", in)
	}
	if len(idx.GetCallers(hook.ID)) != 0 {
		t.Error("an edge to Worker.hook: nothing proves it")
	}
}

// Optional, slice and array types have no members of their element's
// container: a method call on such a value is not resolved (Zig needs .?
// or an index first).
func TestReceivers_WrappedTypesAreNoReceivers(t *testing.T) {
	ex := extract(t, "src/a.zig", `const B = struct { fn m(self: *const B) void { _ = self; } };
const a = struct {
    const B = struct { fn m(self: *const B) void { _ = self; } };
    fn m() void {}
};
fn f(x: ?*const a.B, z: []const a.B, w: [2]a.B, o: ?a.B, p: ?*a.B) void {
    x.m();
    z.m();
    w.m();
    o.m();
    p.m();
}
fn g(q: ?B, r: []B, s: [*]B, u: ?*B, v: [3]B, e: anyerror!B) void {
    q.m();
    r.m();
    s.m();
    u.m();
    v.m();
    e.m();
}
`)
	for _, r := range ex.References {
		if r.Name == "m" && r.NameQualified != "" {
			t.Errorf("a method of a wrapped type resolved: %s", refLine(r))
		}
	}
}

// A usingnamespace of a nearer container may bring a name a farther scope
// declares: the name is not resolved to the farther declaration.
func TestUsingnamespace_HidesOuterDeclarations(t *testing.T) {
	ex := extract(t, "src/a.zig", `fn helper() void {}
const Inner = struct {
    usingnamespace @import("other.zig");
    fn f() void { helper(); }
};
fn g() void { helper(); }
`)
	got := refLines(ex)
	equalLines(t, "references", got, []string{
		"3 import other.zig -> src/other.zig",
		"4 call helper -> ?",
		"6 call helper -> src/a.zig::helper",
	})
}

// A path through std, builtin or root is observed by its last member only:
// none of its members can be a repository declaration.
func TestReservedModules_LastMemberOnly(t *testing.T) {
	ex := extract(t, "src/a.zig", `const std = @import("std");
const builtin = @import("builtin");
const stdx = @import("stdx");
fn f() void {
    std.debug.print("", .{});
    _ = builtin.os.tag;
    stdx.mem.copy();
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"1 import std -> {module}std",
		"2 import builtin -> {module}builtin",
		"3 import stdx -> {module}stdx",
		"5 value_reference std -> src/a.zig::std",
		"5 call print -> {module}std::debug::print",
		"6 value_reference builtin -> src/a.zig::builtin",
		"6 value_reference tag -> {module}builtin::os::tag",
		"7 value_reference stdx -> src/a.zig::stdx",
		"7 value_reference mem -> {module}stdx::mem",
		"7 call copy -> {module}stdx::mem::copy",
	})
}
