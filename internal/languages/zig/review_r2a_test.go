package zig

import "testing"

// R2a adversarial review (rule A: a closed build calls no other file's
// code). Each build.zig type-checks with Zig 0.17.0 (build.zig never run);
// a @compileError planted in tools/configure.zig's W.format / W.step fails
// the check, so the compiler runs that other file's method with the
// builder — which then roots a second module at src/main.zig binding util
// and helper.zig to lib/evil.zig.

const r2aTools = r1Tools +
	"pub const W = struct {\n" +
	"    b: *std.Build,\n" +
	"    pub fn format(self: W, wr: *std.Io.Writer) std.Io.Writer.Error!void {\n" +
	"        _ = wr;\n" +
	"        configure(self.b);\n" +
	"    }\n" +
	"    pub fn step(self: W) void {\n" +
	"        configure(self.b);\n" +
	"    }\n" +
	"};\n" +
	"pub const U = union(enum) { a: W };\n"

func r2aFiles(top, extra string) map[string]string {
	return worldFiles(worldBuild("const tools = @import(\"tools/configure.zig\");\n"+top, extra), map[string]string{
		"tools/configure.zig": r2aTools,
		"lib/evil.zig":        r1Evil,
	})
}

func TestReviewR2a_OtherFilesCodeWithoutACallOfIt(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	imp := []string{"lib/evil.zig"}
	const w = "    const w: tools.W = .{ .b = b };\n"
	runReview(t, []reviewCase{
		// R2a-1: std.debug.print (readOnlyStd) calls W.format by the {f}
		// convention; stdGiven taints only the file's types' methods.
		{"R2a-1 format of another file's type", r2aFiles("",
			w+"    std.debug.print(\"{f}\\n\", .{w});\n"), openWant, imp},
		// R2a-2: a switch capture's value is another file's; foreignWhy
		// reads the switch_case, not the switch's operand.
		{"R2a-2 switch capture of another file's union", r2aFiles("",
			"    const u: tools.U = .{ .a = .{ .b = b } };\n    switch (u) {\n        .a => |v| v.step(),\n    }\n"), openWant, imp},
		// R2a-3: chainRoot stops at an if expression: "a literal's, a type's".
		{"R2a-3 receiver behind an if expression", r2aFiles("",
			w+"    (if (true) w else w).step();\n"), openWant, imp},
		// R2a-4: a parameter typed by a comptime type parameter: its type
		// text names no import.
		{"R2a-4 receiver typed by a type parameter", r2aFiles(
			"fn run(comptime T: type, v: T) void {\n    v.step();\n}\n",
			w+"    run(tools.W, w);\n"), openWant, imp},
		// R2a-5: chainRoot stops at a labeled block.
		{"R2a-5 receiver behind a labeled block", r2aFiles("",
			w+"    (blk: {\n        break :blk w;\n    }).step();\n"), openWant, imp},
		// R2a-6: a file function whose result type is @TypeOf(its
		// parameter): mentionsForeign of the result type finds no import.
		{"R2a-6 result typed @TypeOf of a parameter", r2aFiles(
			"fn id(v: anytype) @TypeOf(v) {\n    return v;\n}\n",
			w+"    id(w).step();\n"), openWant, imp},
		// R2a-7: chainRoot stops at a catch expression.
		{"R2a-7 receiver behind catch", r2aFiles("",
			w+"    const e: anyerror!tools.W = w;\n    (e catch unreachable).step();\n"), openWant, imp},
	})
}

// The control: the same receiver called directly is read as another
// file's (open) — the cases above differ only in how the receiver is
// reached.
func TestReviewR2a_Control(t *testing.T) {
	debugOpenAll = func(f, why string) { t.Log("OPEN", f, why) }
	defer func() { debugOpenAll = nil }()
	runReview(t, []reviewCase{
		{"control direct method of another file's value", r2aFiles("",
			"    const w: tools.W = .{ .b = b };\n    w.step();\n"), openWant, []string{"lib/evil.zig"}},
	})
}
