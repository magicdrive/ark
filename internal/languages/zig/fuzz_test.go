package zig

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractZig verifies the Zig provider on arbitrary input: it never
// panics, keeps every range inside the source, never emits an empty name,
// gives an identity only to a reference without a receiver expression (but
// a tentative one; a dynamic call never has one), never marks a reference both speculative and
// tentative, nor tentative and in the repository, and is deterministic.
func FuzzExtractZig(f *testing.F) {
	seeds := []string{
		"",
		"const std = @import(\"std\");\npub fn main() void { std.debug.print(\"hi\", .{}); }",
		"const m = @import(\"m.zig\"); pub const E = struct { const Self = @This(); x: u8 = 0, pub fn init() Self { return .{}; } pub fn run(self: *Self) void { self.x += 1; } }; fn f() void { var e = E.init(); e.run(); m.g(); }",
		"fn List(comptime T: type) type { return struct { items: []T, pub fn len(s: @This()) usize { return s.items.len; } }; } const L = List(u8); fn g(l: L) usize { return l.len(); }",
		"const A = if (true) @import(\"a.zig\") else @import(\"b.zig\"); fn f() void { A.x(); }",
		"const C = enum { a, b }; const U = union(enum) { x: u8 }; const E = error{ Bad }; fn f() E!C { return .a; }",
		"fn f(x: anytype, comptime T: type) void { x.m(); T.m(); for (xs) |*it, i| it.m(i); if (o) |v| v.m() else |e| e.m(); }",
		"test \"x\" { const S = struct { fn m() void {} }; S.m(); } test f { }",
		"const std = @import(\"std\");\npub fn build(b: *std.Build) void { const m = b.createModule(.{ .root_source_file = b.path(\"src/m.zig\") }); _ = b.addModule(\"n\", .{ .root_source_file = b.path(\"x.zig\"), .imports = &.{.{ .name = \"m\", .module = m }} }); }",
		"fn broken( { } fn good() void {}",
		"const = ; pub fn ( struct { fn } @import( .{ .a = } => |*|",
		"const @\"weird name\" = struct {}; fn f(p: ?*const a.B, q: *a.B) void { q.m(); handlers[i](); @field(p, \"x\")(); }",
		"pub usingnamespace @import(\"x.zig\"); fn f() void { g(); }",
		"const S = struct { s: S, fn m(self: S) void { self.s.s.s.m(); } };",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		for _, file := range []string{"src/a/b.zig", "build.zig", "a.zig"} {
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
				if r.NameQualified != "" && (r.ReceiverExpr != "" && !r.TentativeIdentity || r.Dynamic) {
					t.Fatalf("an identity on a receiver expression or dynamic reference: %+v", r)
				}
				if r.Speculative && r.TentativeIdentity || r.TentativeIdentity && r.IdentityInRepository {
					t.Fatalf("contradictory qualifiers: %+v", r)
				}
				if q := r.NameQualified; q != "" && (strings.HasSuffix(q, "::") || strings.Contains(q, "::::") || strings.HasPrefix(q, "::")) {
					t.Fatalf("malformed identity %q", q)
				}
				if r.Location.Range.Start.Line == 0 {
					t.Fatalf("reference has 0 (non-1-based) line: %+v", r)
				}
			}
			for _, a := range ext.Aliases {
				if a.Qualified == "" || a.Target == "" || a.Qualified == a.Target {
					t.Fatalf("malformed alias %+v", a)
				}
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ext, again) {
				t.Fatalf("non-deterministic extraction of %q", src)
			}
		}
	})
}
