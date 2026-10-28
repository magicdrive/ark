package zig

import "testing"

// Adversarial review R3-C (taint and memory): builds Ark reads as closed
// whose code makes a module's table differ from Ark's facts. Every
// build.zig here type-checks with Zig 0.17.0 (never run).

var r3cWant = map[string]string{
	"src/main.zig:4 helper call": "!strong",
}

// r3cConst: util and other as above; src/main.zig's module binds "util"
// to util, a constant.
func r3cConst(top, body string) string {
	return "const std = @import(\"std\");\n" + top + "pub fn build(b: *std.Build) void {\n" +
		"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" + body +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"    m.addImport(\"util\", util);\n" +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n" +
		"}\n"
}

const r3cLaunder = "    const iu = @intFromPtr(util);\n" +
	"    const io = @intFromPtr(other);\n" +
	"    const pu = mem.bytesToValue(*M, mem.asBytes(&iu));\n" +
	"    const po = mem.bytesToValue(*M, mem.asBytes(&io));\n"

// r3cLaunderFmt: the modules' addresses read back from their formatted
// pointers (`{*}`): no builtin at all.
const r3cLaunderFmt = "    const su = std.fmt.allocPrint(b.allocator, \"{*}\", .{util}) catch @panic(\"OOM\");\n" +
	"    const so = std.fmt.allocPrint(b.allocator, \"{*}\", .{other}) catch @panic(\"OOM\");\n" +
	"    const iu = std.fmt.parseInt(usize, su[std.mem.lastIndexOfScalar(u8, su, '@').? + 1 ..], 16) catch unreachable;\n" +
	"    const io = std.fmt.parseInt(usize, so[std.mem.lastIndexOfScalar(u8, so, '@').? + 1 ..], 16) catch unreachable;\n" +
	"    const pu = mem.bytesToValue(*M, mem.asBytes(&iu));\n" +
	"    const po = mem.bytesToValue(*M, mem.asBytes(&io));\n"

// r3cConstX: src/main.zig's module binds "util" to x, a constant body
// states.
func r3cConstX(body string) string {
	return "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
		"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" + body +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"    m.addImport(\"util\", x);\n" +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n" +
		"}\n"
}

// R3C-1: a pointer to a module's memory std makes from an integer (the
// module's address, @intFromPtr or its formatted `{*}`): no builtin of the
// file makes it, no argument of the std calls is tainted, so the world
// checks none; mem.swap(Module, ...) then swaps util's and other's
// bodies — util's root is lib/other.zig (checked on an analog with Zig
// 0.17.0: `util.root = lib/other.zig`). Ark: closed, util Strong
// lib/util.zig; lib/other.zig::helper reads complete with no importer.
func TestReviewR3C_ModuleMemoryThroughAPointerStdMakes(t *testing.T) {
	const top = "const mem = std.mem;\nconst M = std.Build.Module;\n"
	r2Run(t, []reviewCase{
		{"R3C-1a @intFromPtr, mem.bytesToValue, mem.swap", r2Files(r3cConst(top, r3cLaunder+"    mem.swap(M, pu, po);\n"), nil), r3cWant, []string{"lib/other.zig::helper"}},
		{"R3C-1b the same spelled std.mem.swap (a type alias)", r2Files(r3cConst(top, r3cLaunder+"    std.mem.swap(M, pu, po);\n"), nil), r3cWant, []string{"lib/other.zig::helper"}},
		{"R3C-1c no builtin: the address formatted and parsed", r2Files(r3cConst(top, r3cLaunderFmt+"    mem.swap(M, pu, po);\n"), nil), r3cWant, []string{"lib/other.zig::helper"}},
	})
}

// What held: the value flows Ark reads (each truth from the program's
// semantics: util and other both declare helper).
func TestReviewR3C_Holds(t *testing.T) {
	cand := "candidate lib/other.zig::helper|lib/util.zig::helper"
	r2Run(t, []reviewCase{
		{"labeled block", r2Files(r3cConstX("    const x = blk: {\n        if (b.option(bool, \"a\", \"\") orelse false) break :blk util;\n        break :blk other;\n    };\n"), nil), map[string]string{"src/main.zig:4 helper call": cand}, nil},
		{"nested label", r2Files(r3cConstX("    const x = outer: {\n        _ = inner: {\n            break :outer other;\n        };\n        break :outer util;\n    };\n"), nil), map[string]string{"src/main.zig:4 helper call": cand}, nil},
		{"array element", r2Files(r3cConstX("    const pair = [_]*std.Build.Module{ util, other };\n    const x = pair[1];\n"), nil), r3cWant, nil},
		{"struct field", r2Files(r3cConstX("    const S = struct { a: *std.Build.Module, c: *std.Build.Module };\n    const s: S = .{ .a = util, .c = other };\n    const x = s.c;\n"), nil), map[string]string{"src/main.zig:4 helper call": "strong lib/other.zig::helper"}, nil},
		{"tuple element", r2Files(r3cConstX("    const t = .{ util, other };\n    const x = t[1];\n"), nil), r3cWant, nil},
		{"destructure", r2Files(r3cConstX("    const y, const x = .{ util, other };\n    _ = y;\n"), nil), map[string]string{"src/main.zig:4 helper call": "strong lib/other.zig::helper"}, nil},
		{"catch value", r2Files(r3cConstX("    const E = error{A};\n    const r: E!*std.Build.Module = error.A;\n    const x = r catch other;\n    _ = util;\n"), nil), r3cWant, nil},
		{"orelse", r2Files(r3cConstX("    const r: ?*std.Build.Module = null;\n    const x = r orelse other;\n    _ = util;\n"), nil), r3cWant, nil},
	})
}
