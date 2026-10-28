package zig

import "testing"

// Review X (third adversarial round, the whole of decision 8): closed
// builds whose real import tables, roots or module membership differ from
// Ark's facts. Every build.zig here type-checks with Zig 0.17.0 (never
// run); the resolution each case states was checked with the compiler.

// X1: an artifact's zig_lib_dir (an optionField and a neutralField) roots
// the std module at a file of the repository: `@import("std")` in the
// artifact's modules is zl/std/std.zig (zig build-exe --zig-lib-dir zl).
func TestReviewX1_ZigLibDirRootsStdInTheRepository(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	main := "const std = @import(\"std\");\npub fn main() void {\n    std.marker();\n}\n"
	stdFile := "pub fn marker() void {}\n"
	imp := []string{"zl/std/std.zig", "zl/std/std.zig::marker"}
	runReview(t, []reviewCase{
		{"X1a zig_lib_dir in the artifact's options", map[string]string{
			"build.zig":      "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .target = b.graph.host });\n    const exe = b.addExecutable(.{ .name = \"x\", .root_module = m, .zig_lib_dir = b.path(\"zl\") });\n    b.installArtifact(exe);\n}\n",
			"src/main.zig":   main,
			"zl/std/std.zig": stdFile,
		}, map[string]string{"src/main.zig:3 marker call": "!strong"}, imp},
		{"X1b zig_lib_dir assigned", map[string]string{
			"build.zig":      "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .target = b.graph.host });\n    const exe = b.addExecutable(.{ .name = \"y\", .root_module = m });\n    exe.zig_lib_dir = b.path(\"zl\");\n    b.installArtifact(exe);\n}\n",
			"src/main.zig":   main,
			"zl/std/std.zig": stdFile,
		}, map[string]string{"src/main.zig:3 marker call": "!strong"}, imp},
	})
}

// X2 (a Run step running the compiler with its own module table) is outside
// the boundary: compilations by commands a build runs are a command line's.

// X3: build.zig is a struct too. std given a value of it (`{f}`) calls its
// top-level format (std/Io/Writer.zig:1181 `'f' => return
// value.format(w)`), which taintMethods leaves untainted (it taints only
// nested types' methods) and buildEval reads as a function no call of the
// file passes (unreached: its bindings skipped). The format binds
// helper.zig in src/main.zig's module.
func TestReviewX3_TopLevelFormatOfTheBuildStruct(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	build := "const std = @import(\"std\");\nm: *std.Build.Module,\no: *std.Build.Module,\n" +
		"pub fn format(self: @This(), w: *std.Io.Writer) std.Io.Writer.Error!void {\n    self.m.addImport(\"helper.zig\", self.o);\n    try w.writeAll(\"x\");\n}\n" +
		"pub fn build(b: *std.Build) void {\n" +
		"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"    m.addImport(\"util\", util);\n" +
		"    _ = other.owner;\n" +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n" +
		"    const s: @This() = .{ .m = m, .o = other };\n" +
		"    _ = b.fmt(\"{f}\", .{s});\n}\n"
	runReview(t, []reviewCase{
		{"X3 the file's own format", worldFiles(build, nil), map[string]string{
			"src/main.zig:2 helper.zig import": "!strong",
			"src/main.zig:5 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

// X4: a name ending in .zon is looked up in the build module's table
// first, and `zig build` puts every build.zig.zon dependency there under
// its name: with `.dependencies = .{ .@"cfg.zon" = .{ .path = "../shared" } }`
// `@import("cfg.zon")` is the dependency's build.zig (checked with
// `zig build`, the build failing to compile at a probe in the dependency).
// foreignImport takes it for ZON data; a value of its type given the
// graph is called by a container method's name.
func TestReviewX4_ZonNamedDependencyIsCode(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, []reviewCase{
		{"X4 a dependency named cfg.zon", worldFiles(worldBuild("const cfg = @import(\"cfg.zon\");\n",
			"    const h: cfg.H = .{ .m = m, .o = other };\n    h.append();\n"), nil), openWant, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

// X5: a table binding a name ending in .zon: `@import("data.zon")` is the
// module bound (lib/other.zig), looked up before the ZON file beside the
// importer. Probed as a counterexample; it holds: Ark resolves the import
// through the table (Strong lib/other.zig) and leaves d.which()
// unresolved and unattributed (kept as a pin).
func TestReviewX5_TableBindsAZonName(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, []reviewCase{
		{"X5 addImport(\"data.zon\", other)", map[string]string{
			"build.zig":     "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"data.zon\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n",
			"src/main.zig":  "const d = @import(\"data.zon\");\npub fn main() void {\n    _ = d.which();\n}\n",
			"src/data.zon":  ".{ .x = 1 }\n",
			"lib/other.zig": "pub fn which() u8 {\n    return 2;\n}\n",
		}, map[string]string{"src/main.zig:3 which call": "!strong"}, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

// X6: the getOrPut slot exception is by shape: a method of the file's type
// named getOrPut may return any pointer as value_ptr — here to a module's
// root_source_file. The write re-roots module x (table: helper.zig →
// lib/other.zig) at src/main.zig; the world allows it as a slot, and
// buildEval reads `gop.value_ptr.* = ...` as no module's write.
func TestReviewX6_SlotWriteThroughAFileGetOrPut(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	top := "const Fake = struct {\n    m: *std.Build.Module,\n    fn getOrPut(self: Fake) struct { value_ptr: *?std.Build.LazyPath } {\n        return .{ .value_ptr = &self.m.root_source_file };\n    }\n};\n"
	extra := "    const x = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    x.addImport(\"util\", util);\n    x.addImport(\"helper.zig\", other);\n" +
		"    _ = b.addExecutable(.{ .name = \"y\", .root_module = x });\n    const f: Fake = .{ .m = x };\n    const gop = f.getOrPut();\n    gop.value_ptr.* = b.path(\"src/main.zig\");\n"
	runReview(t, []reviewCase{
		{"X6 a root written through a slot", worldFiles(worldBuild(top, extra), map[string]string{"lib/x.zig": "pub fn main() void {}\n"}), map[string]string{
			"src/main.zig:2 helper.zig import": "!strong",
			"src/main.zig:5 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

// X7: `root` is a graph-free field (graphFreeFields), and an assignment
// of a graph-free value to it is no use of a graph value: the world reads
// nothing. But a package's b.path is resolved against b.root: Serialize's
// builderToPackage stores `b.root.toString()` as the package's root_path
// and the Maker resolves a src_path as join(root_path, sub_path)
// (lib/compiler/Maker.zig:3767). sub/ is a path dependency of the root
// (build.zig.zon: .sub = .{ .path = "sub" }); its b.path("util.zig") is
// lib/util.zig. (Read from std 0.17.0's source; both build.zig type-check,
// the root one with a stub of @dependencies; not run.)
func TestReviewX7_BuildRootReassigned(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, []reviewCase{
		{"X7 b.root of a dependency's build", map[string]string{
			"build.zig":     "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const dep = b.dependency(\"sub\", .{});\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", dep.module(\"util\"));\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n",
			"sub/build.zig": "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    b.root = b.root.join(b.allocator, \"../lib\") catch @panic(\"OOM\");\n    _ = b.addModule(\"util\", .{ .root_source_file = b.path(\"util.zig\") });\n}\n",
			"src/main.zig":  advUtilMain,
			"sub/util.zig":  advUtil,
			"lib/util.zig":  advUtil,
		}, map[string]string{"src/main.zig:3 helper call": "!strong"}, []string{"lib/util.zig", "lib/util.zig::helper"}},
	})
}

// X8: `b.dependency(name, .{})` given no graph value still runs the
// dependency's build with a child builder sharing the graph
// (std/Build.zig:416 `.graph = parent.graph`; it runs at the call,
// :2122-2125). The graph lists every step owning a generated file
// (graph.generated_files; installArtifact → getEmittedBin →
// addGeneratedFile, Compile.zig:683), so the fetched build reaches the
// root's Compile (Step.cast) and its root_module, and binds helper.zig in
// it (its owner is the root's builder: owner.path roots the module in
// the repository). The dependency's build.zig (outside the repository):
//
//	for (b.graph.generated_files.items) |step| {
//	    const c = step.cast(std.Build.Step.Compile) orelse continue;
//	    const root_b = c.root_module.owner;
//	    c.root_module.addImport("helper.zig", root_b.createModule(.{ .root_source_file = root_b.path("lib/other.zig") }));
//	}
//
// (Both type-check; the effect read from std 0.17.0's source, not run.)
func TestReviewX8_DependencyReachesTheSharedGraph(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	extra := "    const exe2 = b.addExecutable(.{ .name = \"y\", .root_module = m });\n    b.installArtifact(exe2);\n    _ = b.dependency(\"evil\", .{});\n"
	runReview(t, []reviewCase{
		{"X8 a fetched dependency given nothing", worldFiles(worldBuild("", extra), nil), map[string]string{
			"src/main.zig:2 helper.zig import": "!strong",
			"src/main.zig:5 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}
