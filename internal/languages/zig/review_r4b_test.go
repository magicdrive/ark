package zig

import "testing"

// Review 4B (the module configuration the compiler is given): closed
// builds whose real import tables differ from Ark's facts, read from the
// way Zig 0.17.0's Maker turns the configuration into --dep/-M.

func r4bBuild(bind string) string {
	return "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		bind +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n"
}

func r4bFiles(build, main string) map[string]string {
	return map[string]string{
		"build.zig":      build,
		"src/main.zig":   main,
		"src/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
		"lib/other.zig":  "pub fn which() u8 {\n    return 2;\n}\n",
	}
}

func TestReview4B_ImportNamesMatchRaw(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	plain := "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
	dot := "const h = @import(\"./helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
	runReview(t, []reviewCase{
		{"P1 table ./helper.zig, import helper.zig", r4bFiles(r4bBuild("    m.addImport(\"./helper.zig\", other);\n"), plain), map[string]string{
			"src/main.zig:3 which call": "exact src/helper.zig::which",
		}, nil},
		{"P2 table helper.zig, import ./helper.zig", r4bFiles(r4bBuild("    m.addImport(\"helper.zig\", other);\n"), dot), map[string]string{
			"src/main.zig:3 which call": "exact src/helper.zig::which",
		}, nil},
		{"P3 table ./helper.zig, import ./helper.zig", r4bFiles(r4bBuild("    m.addImport(\"./helper.zig\", other);\n"), dot), map[string]string{
			"src/main.zig:3 which call": "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

func TestReview4B_RootImportStaysAttributable(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, []reviewCase{
		{"R1 root imported by a file of the module", map[string]string{
			"build.zig":    "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .target = b.graph.host });\n    b.installArtifact(b.addExecutable(.{ .name = \"x\", .root_module = m }));\n}\n",
			"src/main.zig": "const u = @import(\"util.zig\");\npub fn marker() void {}\npub fn main() void {\n    u.run();\n}\n",
			"src/util.zig": "const r = @import(\"root\");\npub fn run() void {\n    r.marker();\n}\n",
		}, map[string]string{}, []string{"src/main.zig", "src/main.zig::marker"}},
	})
}

func r4bRootForm(root string) string {
	return "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = " + root + " });\n" +
		"    m.addImport(\"helper.zig\", other);\n" +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n"
}

func TestReview4B_RootFormsArkCannotNameCapTheModule(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	plain := "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
	w := map[string]string{"src/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig", "lib/other.zig::which"}
	runReview(t, []reviewCase{
		{"F1 src_path literal", r4bFiles(r4bRootForm(".{ .src_path = .{ .owner = b, .sub_path = \"src/main.zig\" } }"), plain), w, imp},
		{"F2 concatenation", r4bFiles(r4bRootForm("b.path(\"src/\" ++ \"main.zig\")"), plain), w, imp},
		{"F3 a branch", r4bFiles(r4bRootForm("if (b.option(bool, \"x\", \"\") orelse false) b.path(\"src/main.zig\") else b.path(\"lib/x.zig\")"), plain), w, imp},
		{"F4 cwd_relative", r4bFiles(r4bRootForm(".{ .cwd_relative = \"src/main.zig\" }"), plain), w, imp},
		{"F5 dupe", r4bFiles(r4bRootForm("b.path(\"src/main.zig\").dupe(b.graph)"), plain), w, imp},
		{"F6 relative", r4bFiles(r4bRootForm("std.Build.LazyPath{ .src_path = .{ .owner = b, .sub_path = \"src/main.zig\" } }"), plain), w, imp},
		{"F7 path.path", r4bFiles(r4bRootForm("b.path(\"src\").path(b, \"main.zig\")"), plain), w, imp},
	})
}

func r4bDup(top, mopts, bind string) string {
	return "const std = @import(\"std\");\n" + top + "pub fn build(b: *std.Build) void {\n" +
		"    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/u1.zig\") });\n" +
		"    const ub = b.createModule(.{ .root_source_file = b.path(\"lib/u2.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\")" + mopts + " });\n" +
		bind +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n"
}

func r4bDupFiles(build string) map[string]string {
	return map[string]string{
		"build.zig":    build,
		"src/main.zig": advUtilMain,
		"lib/u1.zig":   advUtil,
		"lib/u2.zig":   advUtil,
	}
}

func TestReview4B_RebindingANameIsLastWins(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	// Truth: the last binding (lib/u2.zig, std's array_hash_map put);
	// Ark reads both: a Candidate, never a Strong to lib/u1.zig.
	w := map[string]string{"src/main.zig:3 helper call": "candidate lib/u1.zig::helper|lib/u2.zig::helper"}
	imp := []string{"lib/u2.zig", "lib/u2.zig::helper"}
	runReview(t, []reviewCase{
		{"D1 addImport twice", r4bDupFiles(r4bDup("", "", "    m.addImport(\"util\", ua);\n    m.addImport(\"util\", ub);\n")), w, imp},
		{"D2 imports twice", r4bDupFiles(r4bDup("", ", .imports = &.{ .{ .name = \"util\", .module = ua }, .{ .name = \"util\", .module = ub } }", "")), w, imp},
		{"D3 imports then addImport", r4bDupFiles(r4bDup("", ", .imports = &.{ .{ .name = \"util\", .module = ua } }", "    m.addImport(\"util\", ub);\n")), w, imp},
		{"D4 import_table put", r4bDupFiles(r4bDup("", "", "    m.addImport(\"util\", ua);\n    m.import_table.put(b.allocator, \"util\", ub) catch @panic(\"OOM\");\n")), w, imp},
	})
}

func TestReview4B_ModuleRootDirectories(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	x := "const h = @import(\"helper.zig\");\npub fn run() void {\n    _ = h.which();\n}\n"
	b := func(root string) string {
		return "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
			"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
			"    const m = b.createModule(.{ .root_source_file = b.path(\"" + root + "\") });\n" +
			"    m.addImport(\"helper.zig\", other);\n" +
			"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n"
	}
	runReview(t, []reviewCase{
		{"G1 a root at the repository root", map[string]string{
			"build.zig":      b("main.zig"),
			"main.zig":       "const s = @import(\"sub/x.zig\");\npub fn main() void {\n    s.run();\n}\n",
			"sub/x.zig":      x,
			"sub/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
			"lib/other.zig":  "pub fn which() u8 {\n    return 2;\n}\n",
		}, map[string]string{"sub/x.zig:3 which call": "!strong"}, []string{"lib/other.zig", "lib/other.zig::which"}},
		{"G2 a build root in a directory", map[string]string{
			"pkg/build.zig":      b("main.zig"),
			"pkg/main.zig":       "const s = @import(\"sub/x.zig\");\npub fn main() void {\n    s.run();\n}\n",
			"pkg/sub/x.zig":      x,
			"pkg/sub/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
			"pkg/lib/other.zig":  "pub fn which() u8 {\n    return 2;\n}\n",
		}, map[string]string{"pkg/sub/x.zig:3 which call": "!strong"}, []string{"pkg/lib/other.zig", "pkg/lib/other.zig::which"}},
	})
}

func TestReview4B_TableCopyIsASnapshot(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	build := "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
		"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const base = b.createModule(.{ .root_source_file = b.path(\"base/base.zig\"), .target = b.graph.host });\n" +
		"    base.addImport(\"util\", util);\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .target = b.graph.host });\n" +
		"    var it = base.import_table.iterator();\n" +
		"    while (it.next()) |e| m.addImport(e.key_ptr.*, e.value_ptr.*);\n" +
		"    base.addImport(\"helper.zig\", other);\n" +
		"    b.installArtifact(b.addExecutable(.{ .name = \"x\", .root_module = m }));\n" +
		"    b.installArtifact(b.addExecutable(.{ .name = \"y\", .root_module = base }));\n}\n"
	runReview(t, []reviewCase{
		{"C1 a binding added after the copy", worldFiles(build, map[string]string{
			"base/base.zig": "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
		}), map[string]string{
			"src/main.zig:4 helper call": "strong lib/util.zig::helper",
			// Truth: src/helper.zig (the copy is a snapshot); Ark reads the
			// copy flow-insensitively: a Candidate, nothing false.
			"src/main.zig:2 helper.zig import": "candidate lib/other.zig|src/helper.zig",
			"src/main.zig:5 which call":        "candidate lib/other.zig::which|src/helper.zig::which",
		}, []string{"src/helper.zig", "src/helper.zig::which"}},
	})
}

// A module value copied into a var and reassigned (`mc = m.*`), the copy
// given a binding and compiled: a Candidate (the dereference leaves the
// modules' names unknown).
func TestReview4B_ModuleValueCopiedIntoAVar(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	build := "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
		"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const a = b.createModule(.{ .root_source_file = b.path(\"lib/a.zig\"), .target = b.graph.host });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .target = b.graph.host });\n" +
		"    m.addImport(\"util\", util);\n" +
		"    var mc: std.Build.Module = a.*;\n" +
		"    mc = m.*;\n" +
		"    mc.addImport(\"helper.zig\", other);\n" +
		"    b.installArtifact(b.addExecutable(.{ .name = \"x\", .root_module = &mc }));\n}\n"
	runReview(t, []reviewCase{
		{"V1 a module value copied into a var", worldFiles(build, map[string]string{"lib/a.zig": "pub fn main() void {}\n"}),
			map[string]string{"src/main.zig:2 helper.zig import": "!strong", "src/main.zig:5 which call": "!strong"},
			[]string{"lib/other.zig", "lib/other.zig::which"}},
	})
}
