package zig

import "testing"

// Third review, viewpoint Y: the facts the evaluator reads from a build
// Ark reads as closed. Each build.zig type-checks with Zig 0.17.0
// (`zig build-obj -fno-emit-bin` of `_ = &@import("build.zig").build`);
// what the compiler resolves was reproduced with the command line Zig
// 0.17.0's Maker (lib/compiler/Maker/Step/Compile.zig) emits for it.

func y3Run(t *testing.T, cases []reviewCase) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, cases)
}

const (
	y3Main = "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
	y3One  = "pub fn which() u8 {\n    return 1;\n}\n"
	y3Two  = "pub fn which() u8 {\n    return 2;\n}\n"
)

func y3Build(body string) string {
	return "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" + body + "}\n"
}

// Modules the compiler makes that the evaluator does not see.
func TestReviewY3_ModulesTheEvaluatorDoesNotSee(t *testing.T) {
	y3Run(t, []reviewCase{
		// Y1: R2-4's custom test runner, its options a typed struct
		// initializer: the world checks test_runner in anonymous literals
		// only. `zig test --test-runner tools/runner.zig --dep helper.zig
		// -Mroot=src/main.zig -Mhelper.zig=lib/other.zig`: the runner's
		// helper.zig is lib/other.zig.
		{"Y1 typed TestOptions with a test runner", map[string]string{
			"build.zig": y3Build("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
				"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    m.addImport(\"helper.zig\", other);\n" +
				"    _ = b.addTest(std.Build.TestOptions{ .name = \"t\", .root_module = m, .test_runner = .{ .path = b.path(\"tools/runner.zig\"), .mode = .simple } });\n"),
			"src/main.zig":     "test {}\n",
			"tools/runner.zig": y3Main,
			"tools/helper.zig": y3One,
			"lib/other.zig":    y3Two,
		}, map[string]string{"tools/runner.zig:1 helper.zig import": "!strong", "tools/runner.zig:3 which call": "!strong"},
			[]string{"lib/other.zig", "lib/other.zig::which"}},
		// Y2: an artifact's zig_lib_dir (an option the world allows)
		// in the repository: `--zig-lib-dir zlib` makes @import("std")
		// zlib/std/std.zig, which Ark reads as imported by no file.
		{"Y2 zig_lib_dir in the repository", map[string]string{
			"build.zig": y3Build("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m, .zig_lib_dir = b.path(\"zlib\") });\n"),
			"src/main.zig":     "const std = @import(\"std\");\npub fn main() void {\n    std.vendored();\n}\n",
			"zlib/std/std.zig": "pub fn vendored() void {}\n",
		}, nil, []string{"zlib/std/std.zig", "zlib/std/std.zig::vendored"}},
		// Y2b: a module bound under "std" (bind skips the reserved name):
		// Maker names it std on the command line (`--dep std
		// -Mstd=forks/std/std.zig`), and the compilation's std is the fork.
		{"Y2b std bound to a module of the repository", map[string]string{
			"build.zig": y3Build("    const fork = b.createModule(.{ .root_source_file = b.path(\"forks/std/std.zig\") });\n" +
				"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    m.addImport(\"std\", fork);\n" +
				"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n"),
			"src/main.zig":      "const std = @import(\"std\");\npub fn main() void {\n    std.vendored();\n}\n",
			"forks/std/std.zig": "pub fn vendored() void {}\n",
		}, nil, []string{"forks/std/std.zig", "forks/std/std.zig::vendored"}},
		// Y3: an import name holding '=': Maker writes `--dep <name>` when
		// the name is the module's command-line name, and the compiler
		// splits it at '=' — `--dep helper.zig=other`: m's helper.zig is
		// the module named other (lib/other.zig). The empty module has no
		// root, so no -M of the name is written.
		{"Y3 an import name holding '='", map[string]string{
			"build.zig": y3Build("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
				"    const empty = b.createModule(.{});\n" +
				"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    m.addImport(\"helper.zig=other\", empty);\n" +
				"    m.addImport(\"other\", other);\n" +
				"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n"),
			"src/main.zig":   y3Main,
			"src/helper.zig": y3One,
			"lib/other.zig":  y3Two,
		}, map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"},
			[]string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

// B1 (a module rooted at WriteFile's copies of repository files) is outside
// the boundary: what compiles is a copy, not a file of the repository.

// Shapes tried that Ark reads right (a Candidate, or the build open).
func TestReviewY3_Holds(t *testing.T) {
	mk := func(body string) map[string]string {
		return map[string]string{
			"build.zig": y3Build("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
				"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
				"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" + body +
				"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n    _ = util.owner;\n"),
			"src/main.zig":   y3Main,
			"src/helper.zig": y3One,
			"lib/other.zig":  y3Two,
			"lib/util.zig":   "",
		}
	}
	want := map[string]string{"src/main.zig:1 helper.zig import": "!strong"}
	y3Run(t, []reviewCase{
		{"destructure with a discard", mk("    _, const x = .{ util, m };\n    x.addImport(\"helper.zig\", other);\n"), want, nil},
		{"destructure with a discard, typed", mk("    _, const x: *std.Build.Module = .{ util, m };\n    x.addImport(\"helper.zig\", other);\n"), want, nil},
		{"copy loop with a continue expression", mk("    var it = m.import_table.iterator();\n    while (it.next()) |e| : (_ = it.next()) m.addImport(e.key_ptr.*, e.value_ptr.*);\n    m.addImport(\"helper.zig\", other);\n"), want, nil},
		{"import_table.putAssumeCapacity (two arguments)", mk("    m.addImport(\"util\", util);\n    m.import_table.putAssumeCapacity(\"helper.zig\", other);\n"), want, nil},
		{"import_table.putAssumeCapacityNoClobber", mk("    m.addImport(\"util\", util);\n    m.import_table.putAssumeCapacityNoClobber(\"helper.zig\", other);\n"), want, nil},
	})
}
