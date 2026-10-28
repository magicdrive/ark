package zig

import "testing"

// Adversarial review R2 (allow-list meaning and the evaluator's facts):
// builds Ark reads as closed whose code reaches the graph in a way Ark does
// not follow, or whose facts buildEval misreads. Every build.zig here
// type-checks with Zig 0.17.0 (never run); the module semantics are
// checked with `zig build-exe -fno-emit-bin --dep ... -M...`.

const r2Main = "const u = @import(\"util\");\nconst h = @import(\"helper.zig\");\npub fn main() void {\n    u.helper();\n    _ = h.which();\n}\n"

// r2Files: the build binds util → lib/util.zig for src/main.zig's module;
// lib/other.zig declares helper and lib/alt.zig which, so a second module
// rooted at src/main.zig binding util and helper.zig to them compiles too
// (one file in two modules of a compilation is an error).
func r2Files(build string, more map[string]string) map[string]string {
	files := map[string]string{
		"build.zig":      build,
		"src/main.zig":   r2Main,
		"src/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
		"lib/util.zig":   "pub fn helper() void {}\n",
		"lib/other.zig":  "pub fn helper() void {}\n",
		"lib/alt.zig":    "pub fn which() u8 {\n    return 2;\n}\n",
	}
	for k, v := range more {
		files[k] = v
	}
	return files
}

func r2Build(top, extra string) string {
	return "const std = @import(\"std\");\n" + top + "pub fn build(b: *std.Build) void {\n" +
		"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"    m.addImport(\"util\", util);\n" +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n" + extra + "}\n"
}

// A helper file given the builder: it makes a second module rooted at
// src/main.zig binding util to lib/other.zig and helper.zig to lib/alt.zig.
const r2Configure = "const std = @import(\"std\");\n" +
	"pub fn configure(b: *std.Build) void {\n" +
	"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
	"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
	"    const alt = b.createModule(.{ .root_source_file = b.path(\"lib/alt.zig\") });\n" +
	"    m.addImport(\"util\", other);\n" +
	"    m.addImport(\"helper.zig\", alt);\n" +
	"    _ = b.addExecutable(.{ .name = \"y\", .root_module = m });\n" +
	"}\n"

const r2ConfigureAddr = "const std = @import(\"std\");\n" +
	"const impl = @import(\"configure.zig\");\n" +
	"pub fn configure(addr: usize) void {\n" +
	"    const b: *std.Build = @ptrFromInt(addr);\n" +
	"    impl.configure(b);\n" +
	"}\n"

// r2Run is runReview with every reason a build is open logged (a closed
// build logs none).
func r2Run(t *testing.T, cases []reviewCase) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, cases)
}

var r2OpenWant = map[string]string{
	"src/main.zig:4 helper call":       "!strong",
	"src/main.zig:5 which call":        "!strong",
	"src/main.zig:2 helper.zig import": "!strong",
}

// R2-1..3: values std (or the allow-list) states hold no graph value, but
// do: the builder reaches another file's code.
func TestReviewR2_GraphValuesLaundered(t *testing.T) {
	r2Run(t, []reviewCase{
		// @intFromPtr is a pure builtin to the world: the integer is the
		// builder's address, @ptrFromInt in the helper makes it the builder.
		{"R2-1 the builder passed as @intFromPtr", r2Files(r2Build("const tools = @import(\"tools/addr.zig\");\n", "    tools.configure(@intFromPtr(b));\n"),
			map[string]string{"tools/configure.zig": r2Configure, "tools/addr.zig": r2ConfigureAddr}), r2OpenWant, []string{"lib/other.zig::helper", "lib/alt.zig", "lib/alt.zig::which"}},
		// graphFreeResults lists "dupe" (std.Build.dupe: a string), but
		// LazyPath.dupe returns a LazyPath whose src_path.owner is the
		// builder.
		{"R2-2 the builder through LazyPath.dupe", r2Files(r2Build("const tools = @import(\"tools/configure.zig\");\n", "    tools.configure(b.path(\"x\").dupe(b.graph).src_path.owner);\n"),
			map[string]string{"tools/configure.zig": r2Configure}), r2OpenWant, []string{"lib/other.zig::helper", "lib/alt.zig", "lib/alt.zig::which"}},
	})
}

// R2-3: b.dependency is in neutralMethods, but it runs the dependency's
// build with its arguments, and a LazyPath argument keeps its owner (this
// build's *std.Build: addPackageOptionFromArg → dupeInner): the fetched
// package's build may root a module at src/main.zig with its own table, or
// use the owner.
func TestReviewR2_DependencyGivenAPath(t *testing.T) {
	r2Run(t, []reviewCase{
		{"R2-3 a LazyPath given to a fetched dependency", r2Files(r2Build("", "    _ = b.dependency(\"pkg\", .{ .root = b.path(\"src/main.zig\") });\n"), nil),
			map[string]string{"src/main.zig:2 helper.zig import": "!strong", "src/main.zig:5 which call": "!strong"}, nil},
	})
}

// R2-4: a custom test runner is the root of a module the compiler makes
// with the root module's table (zig test --test-runner): Ark knows no
// module rooted at tools/runner.zig, and reads its relative import as the
// file.
func TestReviewR2_TestRunnerModule(t *testing.T) {
	build := "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"    m.addImport(\"helper.zig\", other);\n" +
		"    _ = b.addTest(.{ .name = \"t\", .root_module = m, .test_runner = .{ .path = b.path(\"tools/runner.zig\"), .mode = .simple } });\n" +
		"}\n"
	r2Run(t, []reviewCase{
		{"R2-4 a test runner outside the root module's directory", map[string]string{
			"build.zig":        build,
			"src/main.zig":     "test {}\n",
			"tools/runner.zig": "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
			"tools/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
			"lib/other.zig":    "pub fn which() u8 {\n    return 2;\n}\n",
		}, map[string]string{"tools/runner.zig:1 helper.zig import": "!strong", "tools/runner.zig:3 which call": "!strong"}, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

// R2-5: isBuild takes the build function's parameter as this build's
// builder unconditionally, but the build calls itself with a dependency's
// builder: b.path is then relative to deps/sub.
func TestReviewR2_BuildCalledWithAnotherBuilder(t *testing.T) {
	build := "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"    m.addImport(\"helper.zig\", other);\n" +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n" +
		"    if (b.pkg_hash.len == 0) build(b.dependency(\"sub\", .{}).builder);\n" +
		"}\n"
	sub := map[string]string{
		"build.zig":      build,
		"src/main.zig":   "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
		"src/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
		"lib/other.zig":  "pub fn which() u8 {\n    return 2;\n}\n",
		// The path dependency: its own build makes nothing.
		"deps/sub/build.zig":      "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    _ = b;\n}\n",
		"deps/sub/src/main.zig":   "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
		"deps/sub/src/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
		"deps/sub/lib/other.zig":  "pub fn which() u8 {\n    return 2;\n}\n",
	}
	r2Run(t, []reviewCase{
		{"R2-5 build(dep.builder)", sub, map[string]string{"deps/sub/src/main.zig:1 helper.zig import": "!strong", "deps/sub/src/main.zig:3 which call": "!strong"},
			[]string{"deps/sub/lib/other.zig", "deps/sub/lib/other.zig::which"}},
	})
}
