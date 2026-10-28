package zig

import (
	"strings"
	"testing"
)

// R1 adversarial review: the world (closed.go) reads these builds as
// closed, but each gives the builder to another file's code through a
// value the taint does not follow. tools/configure.zig then makes a second
// module rooted at src/main.zig binding util and helper.zig to
// lib/evil.zig.

const r1Tools = "const std = @import(\"std\");\n" +
	"pub fn configure(b: *std.Build) void {\n" +
	"    const evil = b.createModule(.{ .root_source_file = b.path(\"lib/evil.zig\") });\n" +
	"    const m2 = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .imports = &.{ .{ .name = \"util\", .module = evil }, .{ .name = \"helper.zig\", .module = evil } } });\n" +
	"    b.installArtifact(b.addExecutable(.{ .name = \"evil\", .root_module = m2 }));\n" +
	"}\n"

const r1Evil = "pub fn helper() void {}\npub fn which() u8 {\n    return 3;\n}\n"

func r1Files(top, extra string) map[string]string {
	return worldFiles(worldBuild("const tools = @import(\"tools/configure.zig\");\n"+top, extra), map[string]string{
		"tools/configure.zig": r1Tools,
		"lib/evil.zig":        r1Evil,
	})
}

func TestReviewR1_LaunderedBuilder(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	imp := []string{"lib/evil.zig"}
	runReview(t, []reviewCase{
		// R1-1: @intFromPtr is "pure", @ptrFromInt of a pure result is free.
		{"R1-1 ptrFromInt of intFromPtr", r1Files("",
			"    const b2: *std.Build = @ptrFromInt(@intFromPtr(b));\n    tools.configure(b2);\n"), openWant, imp},
		// R1-2: pointer arithmetic (a binary + is "arithmetic: no graph value").
		{"R1-2 many-pointer arithmetic", r1Files("",
			"    const arr = [_]*std.Build{b};\n    const mp: [*]const *std.Build = &arr;\n    tools.configure((mp + 0)[0]);\n"), openWant, imp},
		// R1-3: a container written through a pointer parameter: the
		// parameter is tainted, the list it points to is not.
		{"R1-3 container through a pointer parameter", r1Files(
			"fn stash(list: *std.ArrayList(*std.Build), gpa: std.mem.Allocator, b: *std.Build) void {\n    list.append(gpa, b) catch @panic(\"OOM\");\n}\n",
			"    var list: std.ArrayList(*std.Build) = .empty;\n    stash(&list, b.allocator, b);\n    tools.configure(list.items[0]);\n"), openWant, imp},
		// R1-4: LazyPath.dupe is a "graph-free result" (named like
		// Build.dupe); its src_path.owner is the builder.
		{"R1-4 LazyPath.dupe owner", r1Files("",
			"    const p = b.path(\"lib/util.zig\").dupe(b.graph);\n    tools.configure(p.src_path.owner);\n"), openWant, imp},
		// R1-5: a graph-free field name on an anonymous literal.
		{"R1-5 graph-free field of a literal", r1Files("",
			"    const t = .{ .name = b };\n    tools.configure(t.name);\n"), openWant, imp},
		// R1-6: a graph-free field of a std options struct (CSourceFiles.root
		// is a LazyPath holding the builder).
		{"R1-6 root of std options", r1Files("",
			"    const o: std.Build.Module.AddCSourceFilesOptions = .{ .root = b.path(\".\"), .files = &.{} };\n    tools.configure(o.root.?.src_path.owner);\n"), openWant, imp},
	})
}

// R1-7: the build function named by a constant: no function named build,
// nothing tainted.
func TestReviewR1_BuildByAlias(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	src := strings.Replace(worldBuild("const tools = @import(\"tools/configure.zig\");\npub const build = realBuild;\n", "    tools.configure(b);\n"), "pub fn build(", "fn realBuild(", 1)
	runReview(t, []reviewCase{
		{"R1-7 build named by a constant", worldFiles(src, map[string]string{"tools/configure.zig": r1Tools, "lib/evil.zig": r1Evil}), openWant, []string{"lib/evil.zig"}},
	})
}

// R1-8: another file's function through a tainted receiver holding its
// namespace, named like a function of the file: read as the file's method.
func TestReviewR1_NamespaceInATaintedLiteral(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, []reviewCase{
		{"R1-8 namespace in a tainted literal", r1Files("const Local = struct {\n    fn configure(b: *std.Build) void {\n        _ = b;\n    }\n};\n",
			"    const pair = .{ .ns = tools, .b = b };\n    pair.ns.configure(pair.b);\n    Local.configure(b);\n"), openWant, []string{"lib/evil.zig"}},
	})
}

// Probes that did not break the world (kept as regression checks).
func TestReviewR1_Probes(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, []reviewCase{
		{"P pointer capture", r1Files("", "    var arr = [_]*std.Build{b};\n    for (&arr) |*p| tools.configure(p.*);\n"), openWant, nil},
		{"P inline for over functions", r1Files("", "    inline for (.{tools.configure}) |f| f(b);\n"), openWant, nil},
		{"P labeled block", r1Files("", "    const x = blk: {\n        break :blk b;\n    };\n    tools.configure(x);\n"), openWant, nil},
		{"P defer", r1Files("", "    defer tools.configure(b);\n"), openWant, nil},
		{"P returned through a file function", r1Files("fn id(x: *std.Build) *std.Build {\n    return x;\n}\n", "    tools.configure(id(b));\n"), openWant, nil},
		{"P orelse", r1Files("", "    const o: ?*std.Build = b;\n    tools.configure(o orelse unreachable);\n"), openWant, nil},
		{"P @call tuple variable", r1Files("", "    const args = .{b};\n    @call(.auto, tools.configure, args);\n"), openWant, nil},
		{"P method of a file type given b", r1Files("const W = struct {\n    b: *std.Build,\n    fn go(self: W) void {\n        tools.configure(self.b);\n    }\n};\n", "    const w: W = .{ .b = b };\n    w.go();\n"), openWant, nil},
		{"P error union capture", r1Files("fn get(b: *std.Build) !*std.Build {\n    return b;\n}\n", "    if (get(b)) |x| tools.configure(x) else |_| {}\n"), openWant, nil},
		{"P switch capture", r1Files("", "    const U = union(enum) { a: *std.Build, n: void };\n    const u: U = .{ .a = b };\n    switch (u) {\n        .a => |x| tools.configure(x),\n        .n => {},\n    }\n"), openWant, nil},
	})
}
