package zig

import "testing"

// Fourth review's findings: each a wrong Strong/Exact edge or a file whose
// importers read complete before the fix — an operation on a module Ark
// misread (an assignment, a call through the type, a function reached by
// @field), a root of a dependency (dep.path), build.zig's own imports,
// module names that look like paths, a root directory in another case.
// Each is valid Zig 0.17.0, checked as review3_test.go's are.

const r4Main = "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
const r4Sibling = "pub fn which() u8 { return 1; }\n"
const r4Other = "pub fn which() u8 { return 2; }\n"

func TestReview4_ModuleUsesArkMustNotMisread(t *testing.T) {
	runReview(t, []reviewCase{
		// C1: Module.addImport called through the type (three arguments).
		{"C1 Module.addImport through the type", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    std.Build.Module.addImport(m, \"helper.zig\", other);\n"),
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
			"src/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// C2: the module's root_source_file assigned after creation.
		{"C2 root_source_file assigned", map[string]string{
			"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"a/a.zig\") });\n    m.addImport(\"helper.zig\", other);\n    m.root_source_file = b.path(\"c/main.zig\");\n"),
			"a/a.zig":       "",
			"c/main.zig":    r4Main,
			"c/helper.zig":  r4Sibling,
			"lib/other.zig": r4Other,
		}, map[string]string{
			"c/main.zig:1 helper.zig import": "!strong",
			"c/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// C3: an artifact's root_module assigned after creation.
		{"C3 root_module assigned", map[string]string{
			"build.zig":  buildOf("    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const uy = b.createModule(.{ .root_source_file = b.path(\"lib/y.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"a/main.zig\") });\n    const c = b.createModule(.{ .root_source_file = b.path(\"c/main.zig\") });\n    c.addImport(\"util\", uy);\n    const exe = b.addExecutable(.{ .name = \"x\", .root_module = a });\n    exe.root_module = c;\n    exe.root_module.addImport(\"util\", ux);\n"),
			"a/main.zig": "pub fn main() void {}\n",
			"c/main.zig": advUtilMain,
			"lib/x.zig":  advUtil, "lib/y.zig": advUtil,
		}, map[string]string{
			"c/main.zig:3 helper call": "!strong",
		}, []string{"lib/x.zig", "lib/x.zig::helper"}},
		// C4: the table's values rewritten through its iterator.
		{"C4 import_table values rewritten through the iterator", map[string]string{
			"build.zig":    buildOf("    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const uy = b.createModule(.{ .root_source_file = b.path(\"lib/y.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", ux);\n    var it = m.import_table.iterator();\n    while (it.next()) |entry| entry.value_ptr.* = uy;\n"),
			"src/main.zig": advUtilMain,
			"lib/x.zig":    advUtil, "lib/y.zig": advUtil,
		}, map[string]string{
			"src/main.zig:3 helper call": "!strong",
		}, []string{"lib/y.zig", "lib/y.zig::helper"}},
		// C5: a module rooted in a dependency's file (dep.path).
		{"C5 a root in a dependency (dep.path)", map[string]string{
			"build.zig":    buildOf("    const dep = b.dependency(\"foo\", .{});\n    const foo = b.createModule(.{ .root_source_file = dep.path(\"src/root.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"foo\", foo);\n"),
			"src/main.zig": "const foo = @import(\"foo\");\npub fn main() void {\n    foo.helper();\n}\n",
			"src/root.zig": advUtil,
		}, map[string]string{
			"src/main.zig:1 foo import":  "!strong",
			"src/main.zig:3 helper call": "!strong",
		}, nil},
		// C6: build.zig's own named import is a dependency's build.zig.
		{"C6 build.zig's named import", map[string]string{
			"build.zig":            "const std = @import(\"std\");\nconst sub = @import(\"sub\");\npub fn build(b: *std.Build) void {\n    const dep = b.dependency(\"sub\", .{});\n    const m = b.createModule(.{ .root_source_file = b.path(\"main.zig\") });\n    m.addImport(\"sub\", dep.module(\"sub\"));\n    sub.helper(b);\n}\n",
			"main.zig":             "const sub = @import(\"sub\");\npub fn main() void {\n    sub.helper();\n}\n",
			"deps/sub/build.zig":   "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    _ = b.addModule(\"sub\", .{ .root_source_file = b.path(\"src/sub.zig\") });\n}\npub fn helper(b: *std.Build) void {\n    _ = b;\n}\n",
			"deps/sub/src/sub.zig": "pub fn helper() void {}\n",
		}, map[string]string{
			"build.zig:2 sub import":  "!strong",
			"build.zig:7 helper call": "!strong",
		}, []string{"deps/sub/build.zig", "deps/sub/build.zig::helper"}},
		// C7: a module name with a slash.
		{"C7 a module name with a slash", map[string]string{
			"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"vendor/other\", other);\n"),
			"src/main.zig":  "const o = @import(\"vendor/other\");\npub fn main() void {\n    _ = o.which();\n}\n",
			"lib/other.zig": r4Other,
		}, map[string]string{}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// C7b: a module name ending in .zon.
		{"C7b a module name ending in .zon", map[string]string{
			"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"other.zon\", other);\n"),
			"src/main.zig":  "const o = @import(\"other.zon\");\npub fn main() void {\n    _ = o.which();\n}\n",
			"lib/other.zig": r4Other,
		}, map[string]string{}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// C8: a build function called through @field by its name.
		{"C8 @field call of a build function", map[string]string{
			"build.zig": "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const uy = b.createModule(.{ .root_source_file = b.path(\"lib/y.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"src/a.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/b.zig\") });\n    addUtil(a, ux);\n    @field(@This(), \"addUtil\")(m, uy);\n}\nfn addUtil(m: *std.Build.Module, u: *std.Build.Module) void {\n    m.addImport(\"util\", u);\n}\n",
			"src/a.zig": advUtilMain, "src/b.zig": advUtilMain,
			"lib/x.zig": advUtil, "lib/y.zig": advUtil,
		}, map[string]string{
			"src/b.zig:3 helper call": "!strong",
		}, []string{"lib/y.zig", "lib/y.zig::helper"}},
		// C9: a module's struct copied over another's.
		{"C9 a module copied by value", map[string]string{
			"build.zig":     buildOf("    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const uy = b.createModule(.{ .root_source_file = b.path(\"lib/y.zig\") });\n    const lib = b.createModule(.{ .root_source_file = b.path(\"src/lib.zig\") });\n    lib.addImport(\"util\", ux);\n    const t = b.createModule(.{ .root_source_file = b.path(\"test/main.zig\") });\n    t.* = lib.*;\n    t.import_table = .empty;\n    t.addImport(\"util\", uy);\n"),
			"src/lib.zig":   advUtilMain,
			"test/main.zig": "",
			"lib/x.zig":     advUtil, "lib/y.zig": advUtil,
		}, map[string]string{
			"src/lib.zig:3 helper call": "!strong",
		}, []string{"lib/y.zig", "lib/y.zig::helper"}},
		// C10: a root's directory written in another case (a case-insensitive
		// file system opens it): the module's .zig name is not seen.
		{"C10 a root directory in another case", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"Src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
			"src/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// C11: a function called only through @field: its parameter reads
		// as unreached, the binding is dropped.
		{"C11 a function called only through @field", map[string]string{
			"build.zig":      "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    @field(@This(), \"bind\")(m, other);\n}\nfn bind(m: *std.Build.Module, o: *std.Build.Module) void {\n    m.addImport(\"helper.zig\", o);\n}\n",
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
			"src/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// C12: Module.addImport through a constant naming the method.
		{"C12 addImport through a function constant", map[string]string{
			"build.zig":      buildOf("    const addImport = std.Build.Module.addImport;\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    addImport(m, \"helper.zig\", other);\n"),
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
		}, []string{"lib/other.zig"}},
	})
}

// Shapes tried that hold: each binding Ark cannot follow caps the import.
func TestReview4_ShapesThatHold(t *testing.T) {
	body := func(s string) map[string]string {
		return map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" + s),
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}
	}
	want := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	runReview(t, []reviewCase{
		{"labeled block", body("    const m = blk: {\n        break :blk b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    };\n    m.addImport(\"helper.zig\", other);\n"), want, []string{"lib/other.zig"}},
		{"orelse", body("    const m = b.modules.get(\"m\") orelse b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"), want, []string{"lib/other.zig"}},
		{"for capture", body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    for ([_]*std.Build.Module{m}) |x| x.addImport(\"helper.zig\", other);\n"), want, []string{"lib/other.zig"}},
		{"var options with a field assigned", body("    var o: std.Build.Module.CreateOptions = .{ .root_source_file = b.path(\"src/main.zig\") };\n    o.imports = &.{.{ .name = \"helper.zig\", .module = other }};\n    _ = b.createModule(o);\n"), want, []string{"lib/other.zig"}},
		{"typed options literal", body("    _ = b.createModule(std.Build.Module.CreateOptions{ .root_source_file = b.path(\"src/main.zig\"), .imports = &.{.{ .name = \"helper.zig\", .module = other }} });\n"), want, []string{"lib/other.zig"}},
		{"@as", body("    const m = @as(*std.Build.Module, b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }));\n    m.addImport(\"helper.zig\", other);\n"), want, []string{"lib/other.zig"}},
		{"pointer deref", body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    const p = &m;\n    p.*.addImport(\"helper.zig\", other);\n"), want, []string{"lib/other.zig"}},
		{"name through a constant", body("    const n = \"helper.zig\";\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(n, other);\n"), want, []string{"lib/other.zig"}},
		{"root through ++", body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/\" ++ \"main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"), want, []string{"lib/other.zig"}},
		{"cwd_relative root", body("    const m = b.createModule(.{ .root_source_file = .{ .cwd_relative = \"src/main.zig\" } });\n    m.addImport(\"helper.zig\", other);\n"), want, []string{"lib/other.zig"}},
		{"import_table.getPtr", body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", m);\n    m.import_table.getPtr(\"helper.zig\").?.* = other;\n"), want, []string{"lib/other.zig"}},
		{"Module.init existing", body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n    const t = b.allocator.create(std.Build.Module) catch @panic(\"OOM\");\n    t.init(b, .{ .existing = m });\n"), want, []string{"lib/other.zig"}},
	})
}
