package zig

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// r6Dump, with ARK_R6_DUMP set to a directory, writes each case's files to
// <dir>/<case>/repo (for the compiler oracle); the cases still run.
func r6Dump(t *testing.T, cases []reviewCase) {
	dir := os.Getenv("ARK_R6_DUMP")
	if dir == "" {
		return
	}
	id := regexp.MustCompile(`^[A-Z][0-9]+`)
	for _, c := range cases {
		root := filepath.Join(dir, id.FindString(c.name), "repo")
		for f, src := range c.files {
			p := filepath.Join(root, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Sixth review's findings, each a wrong Exact edge or an importer list read
// complete before the fix — escaped names, Module.init, memory rewritten,
// another build.zig's build run with this b, a region past the walk's
// depth bound. Each is valid Zig 0.17.0, checked as review3_test.go's are.

func TestReview6_BindingsArkMustNotMiss(t *testing.T) {
	relWant := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	rootWant := map[string]string{"c/main.zig:1 helper.zig import": "!strong", "c/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig", "lib/other.zig::which"}
	m := "    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n"
	cases := []reviewCase{
		// E1: `.imports` written as a quoted identifier with an escape.
		{"E1 .@\"imp\\x6frts\" options field", r5Files(r5Body("    _ = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .@\"imp\\x6frts\" = &.{.{ .name = \"helper.zig\", .module = other }} });\n")), relWant, imp},
		// E2: import_table written with an escape.
		{"E2 m.@\"import\\x5ftable\".put", r5Files(r5Body(m + "    m.@\"import\\x5ftable\".put(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n")), relWant, imp},
		// E3: root_source_file written with an escape.
		{"E3 m.@\"root\\x5fsource\\x5ffile\" assigned", r5Root("    m.@\"root\\x5fsource\\x5ffile\" = b.path(\"c/main.zig\");\n"), rootWant, imp},
		// E4: addImport through the type, its name escaped.
		{"E4 std.Build.Module.@\"add\\x49mport\"(m, ...)", r5Files(r5Body(m + "    std.Build.Module.@\"add\\x49mport\"(m, \"helper.zig\", other);\n")), relWant, imp},
		// E5: a build function called by an escaped name only.
		{"E5 @\"bi\\x6ed\"(m, other)", r5Files("const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" + m + "    @\"bi\\x6ed\"(m, other);\n}\nfn bind(x: *std.Build.Module, o: *std.Build.Module) void {\n    x.addImport(\"helper.zig\", o);\n}\n"), relWant, imp},
		// E6: a labeled block's break written with an escaped label.
		{"E6 break :@\"b\\x6ck\"", map[string]string{
			"build.zig":     r5Body("    const a = b.createModule(.{ .root_source_file = b.path(\"a/a.zig\") });\n    const c = b.createModule(.{ .root_source_file = b.path(\"c/main.zig\") });\n    const pick = b.option(bool, \"c\", \"\") orelse false;\n    const k = blk: {\n        if (pick) break :@\"b\\x6ck\" c;\n        break :blk a;\n    };\n    k.addImport(\"helper.zig\", other);\n"),
			"a/a.zig":       "",
			"c/main.zig":    r4Main,
			"c/helper.zig":  r4Sibling,
			"lib/other.zig": r4Other,
		}, rootWant, imp},
		// F1: Module.init(.existing) over a module that already has a root.
		{"F1 t.init(b, .{ .existing = lib })", r5Files(r5Body(m + "    const t = b.createModule(.{ .root_source_file = b.path(\"test/t.zig\") });\n    t.init(b, .{ .existing = m });\n    t.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = t });\n")), relWant, imp},
		// F2: std.mem.swap of two modules.
		{"F2 std.mem.swap", map[string]string{
			"build.zig":     r5Body("    const a = b.createModule(.{ .root_source_file = b.path(\"a/a.zig\") });\n    const c = b.createModule(.{ .root_source_file = b.path(\"c/main.zig\") });\n    std.mem.swap(std.Build.Module, a, c);\n    a.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = a });\n"),
			"a/a.zig":       "",
			"c/main.zig":    r4Main,
			"c/helper.zig":  r4Sibling,
			"lib/other.zig": r4Other,
		}, rootWant, imp},
		// F3: a module overwritten through an alias Ark does not evaluate (an
		// if expression), from a struct field's module.
		{"F3 deref assignment through an if alias", r5Files(r5Body(m + "    const t = b.createModule(.{ .root_source_file = b.path(\"test/t.zig\") });\n    const s = .{ .src = m };\n    const k = if (true) t else t;\n    k.* = s.src.*;\n    t.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = t });\n")), relWant, imp},
		// F4: @memcpy of a module's bytes.
		{"F4 @memcpy(std.mem.asBytes(t), std.mem.asBytes(m))", r5Files(r5Body(m + "    const t = b.createModule(.{ .root_source_file = b.path(\"test/t.zig\") });\n    @memcpy(std.mem.asBytes(t), std.mem.asBytes(m));\n    t.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = t });\n")), relWant, imp},
		// G1: another build.zig's build function, called with this build's b.
		{"G1 sub/build.zig's build called with the root's b", map[string]string{
			"build.zig":      "const std = @import(\"std\");\nconst sub = @import(\"sub/build.zig\");\npub fn build(b: *std.Build) void {\n    sub.build(b);\n}\n",
			"sub/build.zig":  "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n",
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, relWant, imp},
	}
	r6Dump(t, cases)
	runReview(t, cases)
}

// G2: a binding nested past the walk's depth bound.
func TestReview6_DeepNesting(t *testing.T) {
	n := 140
	body := "    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    " +
		strings.Repeat("{ ", n) + "m.addImport(\"helper.zig\", other);" + strings.Repeat(" }", n) + "\n"
	cases := []reviewCase{
		{"G2 deeply nested addImport", r5Files(r5Body(body)),
			map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"},
			[]string{"lib/other.zig", "lib/other.zig::which"}},
	}
	r6Dump(t, cases)
	runReview(t, cases)
}

// Shapes tried that hold (the import is capped or bound as Ark reads it).
func TestReview6_ShapesThatHold(t *testing.T) {
	want := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig"}
	m := "    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n"
	runReview(t, []reviewCase{
		{"quoted field without an escape", r5Files(r5Body("    _ = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .@\"imports\" = &.{.{ .name = \"helper.zig\", .module = other }} });\n")), want, imp},
		{"quoted import_table without an escape", r5Files(r5Body(m + "    m.@\"import_table\".put(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n")), want, imp},
		{"escaped method name on the module", r5Files(r5Body(m + "    m.@\"add\\x49mport\"(\"helper.zig\", other);\n")), want, imp},
		{"escaped @field name", r5Files(r5Body(m + "    @field(m, \"import\\x5ftable\").put(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n")), want, imp},
		{"multiline string name", r5Files(r5Body(m + "    m.addImport(\n        \\\\helper.zig\n    , other);\n")), want, imp},
		{"escaped string name", r5Files(r5Body(m + "    m.addImport(\"helper\\x2ezig\", other);\n")), want, imp},
		{"inline for over names", r5Files(r5Body(m + "    inline for (.{\"helper.zig\"}) |n| m.addImport(n, other);\n")), want, imp},
		{"@field with a comptime name", r5Files(r5Body(m + "    const f = \"import_table\";\n    @field(m, f).put(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n")), want, imp},
		{"switch prong capture", r5Files(r5Body(m + "    const U = union(enum) { a: *std.Build.Module };\n    const u: U = .{ .a = m };\n    switch (u) {\n        .a => |x| x.addImport(\"helper.zig\", other),\n    }\n")), want, imp},
		{"labeled block breaking a non-module", r5Files(r5Body(m + "    const k = blk: {\n        if (b.option(bool, \"x\", \"\") orelse false) break :blk @as(*std.Build.Module, m);\n        break :blk m;\n    };\n    k.addImport(\"helper.zig\", other);\n")), want, imp},
		{"var reassigned in a while continue expression", r5Files(r5Body("    const a = b.createModule(.{ .root_source_file = b.path(\"a/a.zig\") });\n" + m + "    var k = a;\n    var i: u8 = 0;\n    while (i < 1) : (k = m) i += 1;\n    k.addImport(\"helper.zig\", other);\n")), want, imp},
		{"Module.init with options over a module", r5Files(r5Body("    const t = b.createModule(.{ .root_source_file = b.path(\"test/t.zig\") });\n    t.init(b, .{ .options = .{ .root_source_file = b.path(\"src/main.zig\") } });\n    t.addImport(\"helper.zig\", other);\n")), want, imp},
		// E7 holds: the module assigned (`= c`) is an occurrence written: any module's table is unknown.
		{"exe.@\"root\\x5fmodule\" assigned", map[string]string{
			"build.zig":  buildOf("    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const uy = b.createModule(.{ .root_source_file = b.path(\"lib/y.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"a/main.zig\") });\n    const c = b.createModule(.{ .root_source_file = b.path(\"c/main.zig\") });\n    c.addImport(\"util\", uy);\n    const exe = b.addExecutable(.{ .name = \"x\", .root_module = a });\n    exe.@\"root\\x5fmodule\" = c;\n    exe.root_module.addImport(\"util\", ux);\n"),
			"a/main.zig": "pub fn main() void {}\n",
			"c/main.zig": advUtilMain,
			"lib/x.zig":  advUtil, "lib/y.zig": advUtil,
		}, map[string]string{"c/main.zig:3 helper call": "!strong"}, []string{"lib/x.zig", "lib/x.zig::helper"}},
		{"helper fn with *std.Build from dep.builder and b", r5Files("const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = mk(b);\n    _ = mk(b.dependency(\"x\", .{}).builder);\n    m.addImport(\"helper.zig\", other);\n}\nfn mk(bb: *std.Build) *std.Build.Module {\n    return bb.createModule(.{ .root_source_file = bb.path(\"src/main.zig\") });\n}\n"), want, imp},
	})
}
