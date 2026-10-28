package zig

import "testing"

// Fifth review's findings, each a wrong Exact/Strong edge or an importer
// list read complete before the fix: shapes that passed the occurrence
// check —
// a module reached by no identifier whose own value is a module, a table
// reached by no `.import_table` field expression, a put method the shape
// switch does not list, a b.path on a builder that is not this build's.
// Each is valid Zig 0.17.0 (build.zig type-checked, never run; module
// tables given with --dep/-M).

func r5Body(s string) string {
	return buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" + s)
}

func r5Files(build string) map[string]string {
	return map[string]string{
		"build.zig":      build,
		"src/main.zig":   r4Main,
		"src/helper.zig": r4Sibling,
		"lib/other.zig":  r4Other,
	}
}

// r5Root: module m is rooted in a/a.zig and binds helper.zig; an assignment
// Ark does not see moves its root to c/main.zig.
func r5Root(s string) map[string]string {
	return map[string]string{
		"build.zig":     r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"a/a.zig\") });\n    m.addImport(\"helper.zig\", other);\n" + s),
		"a/a.zig":       "",
		"c/main.zig":    r4Main,
		"c/helper.zig":  r4Sibling,
		"lib/other.zig": r4Other,
	}
}

func TestReview5_OccurrencesArkMustNotMiss(t *testing.T) {
	relWant := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	rootWant := map[string]string{"c/main.zig:1 helper.zig import": "!strong", "c/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig", "lib/other.zig::which"}
	runReview(t, []reviewCase{
		// D1: import_table.putContext (a put the shape switch does not list;
		// the import_table use is skipped for its "put" prefix).
		{"D1 import_table.putContext", r5Files(r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.import_table.putContext(b.graph.arena, \"helper.zig\", other, .{}) catch @panic(\"OOM\");\n")), relWant, imp},
		// D2: the table reached through @field.
		{"D2 @field(m, \"import_table\").put", r5Files(r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    @field(m, \"import_table\").put(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n")), relWant, imp},
		// D3: the root assigned through a handle that is no module-valued
		// identifier.
		{"D3a root assigned through a struct literal's field", r5Root("    const s = .{ .mod = m };\n    s.mod.root_source_file = b.path(\"c/main.zig\");\n"), rootWant, imp},
		{"D3b root assigned through an if expression", r5Root("    const k = if (true) m else m;\n    k.root_source_file = b.path(\"c/main.zig\");\n"), rootWant, imp},
		{"D3c root assigned through a var", r5Root("    var k = m;\n    _ = &k;\n    k.root_source_file = b.path(\"c/main.zig\");\n"), rootWant, imp},
		{"D3d root assigned through @as", r5Root("    @as(*std.Build.Module, m).root_source_file = b.path(\"c/main.zig\");\n"), rootWant, imp},
		{"D3e root assigned through @field", r5Root("    @field(m, \"root_source_file\") = b.path(\"c/main.zig\");\n"), rootWant, imp},
		{"D3f root assigned through b.modules.get", map[string]string{
			"build.zig":     r5Body("    _ = b.addModule(\"m\", .{ .root_source_file = b.path(\"a/a.zig\"), .imports = &.{.{ .name = \"helper.zig\", .module = other }} });\n    b.modules.get(\"m\").?.root_source_file = b.path(\"c/main.zig\");\n"),
			"a/a.zig":       "",
			"c/main.zig":    r4Main,
			"c/helper.zig":  r4Sibling,
			"lib/other.zig": r4Other,
		}, rootWant, imp},
		// D4: b.path on a dependency's builder, passed to a function of
		// build.zig whose parameter is typed *std.Build.
		{"D4 b.path on dep.builder through a parameter", map[string]string{
			"build.zig":             "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const dep = b.dependency(\"foo\", .{});\n    const foo = rootOf(dep.builder);\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"foo\", foo);\n}\nfn rootOf(b: *std.Build) *std.Build.Module {\n    return b.createModule(.{ .root_source_file = b.path(\"src/root.zig\") });\n}\n",
			"build.zig.zon":         ".{ .name = .x, .version = \"0.0.0\", .fingerprint = 0x0, .dependencies = .{ .foo = .{ .path = \"deps/foo\" } }, .paths = .{\"\"} }\n",
			"src/main.zig":          "const foo = @import(\"foo\");\npub fn main() void {\n    foo.helper();\n}\n",
			"src/root.zig":          advUtil,
			"deps/foo/src/root.zig": advUtil,
		}, map[string]string{
			"src/main.zig:1 foo import":  "!strong",
			"src/main.zig:3 helper call": "!strong",
		}, []string{"deps/foo/src/root.zig", "deps/foo/src/root.zig::helper"}},
	})
}

// D5: Module.addImport reached through @field of the type: the callee is a
// builtin (no call shape), the module an argument (an occurrence read).
func TestReview5_BindingThroughFieldOfTheType(t *testing.T) {
	relWant := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig", "lib/other.zig::which"}
	runReview(t, []reviewCase{
		{"D5a @field(std.Build.Module, \"addImport\")(m, ...)", r5Files(r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    @field(std.Build.Module, \"addImport\")(m, \"helper.zig\", other);\n")), relWant, imp},
		{"D5b a constant naming @field(std.Build.Module, \"addImport\")", r5Files(r5Body("    const add = @field(std.Build.Module, \"addImport\");\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    add(m, \"helper.zig\", other);\n")), relWant, imp},
	})
}

// Shapes tried that hold (the import is capped or bound as Ark reads it).
func TestReview5_ShapesThatHold(t *testing.T) {
	want := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig"}
	m := "    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n"
	runReview(t, []reviewCase{
		{"defer addImport", r5Files(r5Body(m + "    defer m.addImport(\"helper.zig\", other);\n")), want, imp},
		{"errdefer addImport", r5Files("const std = @import(\"std\");\npub fn build(b: *std.Build) !void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" + m + "    errdefer m.addImport(\"helper.zig\", other);\n    try std.testing.expect(true);\n}\n"), want, imp},
		{"inline for over a tuple", r5Files(r5Body(m + "    inline for (.{m}) |x| x.addImport(\"helper.zig\", other);\n")), want, imp},
		{"a nested function's addImport", r5Files(r5Body(m + "    const H = struct {\n        fn add(x: *std.Build.Module, o: *std.Build.Module) void {\n            x.addImport(\"helper.zig\", o);\n        }\n    };\n    H.add(m, other);\n")), want, imp},
		{"a constant naming dep.builder", r5Files(r5Body("    const db = b.dependency(\"foo\", .{}).builder;\n    const m = b.createModule(.{ .root_source_file = db.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n")), want, imp},
		{"pointer alias deref assigned", r5Files(r5Body(m + "    const p = &m;\n    p.*.root_source_file = b.path(\"src/main.zig\");\n    p.*.addImport(\"helper.zig\", other);\n")), want, imp},
		{"import_table through a pointer", r5Files(r5Body(m + "    const t = &m.import_table;\n    t.put(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n")), want, imp},
		{"fetchPut", r5Files(r5Body(m + "    _ = m.import_table.fetchPut(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n")), want, imp},
	})
}
