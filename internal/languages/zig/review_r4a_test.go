package zig

import "testing"

// R4a-1/2 (a build rewriting a source file, at configure time or through
// UpdateSourceFiles) are outside the boundary: the files are the indexed ones.

// R4a-3 (a refuted hypothesis, kept as a guard): `pub const build =
// S.build;` names a nested function whose builder the reviewer expected
// the world not to taint (it taints a top-level `fn build`'s parameters
// by position). It does taint it: an unknown method (addTempFiles) and a
// write of a module's root path (`lp.src_path.sub_path = ...`, a word
// buildEval's lexical checks do not hold) open the build. Type-checks
// with Zig 0.17.0.
func TestReviewR4a3_NestedBuildIsNeverTainted(t *testing.T) {
	var opened []string
	debugOpenAll = func(f, why string) { opened = append(opened, f+": "+why) }
	defer func() { debugOpenAll = nil }()
	build := "const std = @import(\"std\");\nconst S = struct {\n    fn build(b: *std.Build) void {\n" +
		"        const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"        _ = b.addTempFiles();\n" +
		"        if (m.root_source_file) |*lp| lp.src_path.sub_path = \"src/other.zig\";\n" +
		"        b.installArtifact(b.addExecutable(.{ .name = \"x\", .root_module = m }));\n    }\n};\npub const build = S.build;\n"
	resolveRepo(t, map[string]string{"build.zig": build, "src/main.zig": "pub fn main() void {}\n", "src/other.zig": "pub fn main() void {}\n"})
	if len(opened) == 0 {
		t.Errorf("build.zig reads closed: the builder of a nested build function is not tainted")
	}
	t.Log(opened)
}
