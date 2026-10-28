package zig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
)

// Closed and open builds (closed.go): a build's module facts ground a
// Strong or an Exact only when Ark follows every way its code may reach
// the graph. Each world below adds one operation to a closed build; the
// cases marked RED resolved Strong/Exact before closedness was required
// (the operation was a premise Ark did not check). Every build.zig here
// type-checks with Zig 0.17.0 (never run; a dependency given as a module)
// but the three of other versions, as marked.

const worldMain = "const u = @import(\"util\");\nconst h = @import(\"helper.zig\");\npub fn main() void {\n    u.helper();\n    _ = h.which();\n}\n"

// worldBuild is a closed build binding util for src/main.zig's module,
// with top the file's declarations and extra the build function's last
// statements.
func worldBuild(top, extra string) string {
	return "const std = @import(\"std\");\n" + top + "pub fn build(b: *std.Build) void {\n" +
		"    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n" +
		"    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
		"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
		"    m.addImport(\"util\", util);\n" +
		"    _ = other.owner;\n" +
		"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n" + extra + "}\n"
}

func worldFiles(build string, more map[string]string) map[string]string {
	files := map[string]string{
		"build.zig":      build,
		"src/main.zig":   worldMain,
		"src/helper.zig": "pub fn which() u8 {\n    return inner();\n}\nfn inner() u8 {\n    return 1;\n}\n",
		"lib/util.zig":   "pub fn helper() void {}\n",
		"lib/other.zig":  "pub fn which() u8 {\n    return 2;\n}\n",
	}
	for k, v := range more {
		files[k] = v
	}
	return files
}

// openWant is what an open world allows: neither the named module nor the
// relative file is proven.
var openWant = map[string]string{
	"src/main.zig:4 helper call":       "!strong",
	"src/main.zig:5 which call":        "!strong",
	"src/main.zig:2 helper.zig import": "!strong",
	// The file's own references are its scopes', whatever a build binds.
	"src/helper.zig:2 inner call": "exact src/helper.zig::inner",
}

func TestWorld_ClosedBuildGroundsStrongAndExact(t *testing.T) {
	got := resolveRepo(t, worldFiles(worldBuild("", ""), nil))
	expectResolutions(t, got, map[string]string{
		"src/main.zig:4 helper call":       "strong lib/util.zig::helper",
		"src/main.zig:5 which call":        "exact src/helper.zig::which",
		"src/main.zig:2 helper.zig import": "exact src/helper.zig",
		"src/helper.zig:2 inner call":      "exact src/helper.zig::inner",
	})
}

// C-1: operations whose effect on the graph Ark does not follow.
func TestWorld_UnknownEffectsOpenTheBuild(t *testing.T) {
	helper := map[string]string{"tools/configure.zig": "pub fn configure(b: anytype, m: anytype) void {\n    _ = b;\n    _ = m;\n}\n"}
	cases := []reviewCase{
		// RED: another file's function given the builder and a module.
		{"another file's helper", worldFiles(worldBuild("const tools = @import(\"tools/configure.zig\");\n", "    tools.configure(b, m);\n"), helper), openWant, []string{"src/helper.zig"}},
		// RED: the same through a constant naming it.
		{"another file's helper named by a constant", worldFiles(worldBuild("const configure = @import(\"tools/configure.zig\").configure;\n", "    configure(b, m);\n"), helper), openWant, nil},
		// RED: a dependency's build helper (its build.zig, imported by name).
		{"a dependency's build helper", worldFiles(worldBuild("const dep = @import(\"dep\");\n", "    dep.addTo(b, m);\n"), nil), openWant, nil},
		// RED: a module method Ark does not know (another version's, a
		// newer one's: no method of Zig 0.17.0).
		{"an unknown module method", worldFiles(worldBuild("", "    m.bindEverything(other);\n"), nil), openWant, nil},
		// RED: a std function rearranging modules (and calling the file's
		// function with them).
		{"a std function given modules", worldFiles(worldBuild("fn lessThan(_: void, a: *std.Build.Module, c: *std.Build.Module) bool {\n    return @intFromPtr(a) < @intFromPtr(c);\n}\n",
			"    var mods = [_]*std.Build.Module{ m, other };\n    std.sort.insertion(*std.Build.Module, &mods, {}, lessThan);\n"), nil), openWant, nil},
		// RED: a function of the file reached through a value Ark does not
		// follow (a field of a literal), given the builder.
		{"a function held in a literal", worldFiles(worldBuild("fn rebind(b: *std.Build, m: *std.Build.Module) void {\n    _ = b;\n    _ = m;\n}\n",
			"    const ops = .{ .f = rebind };\n    ops.f(b, m);\n"), nil), openWant, nil},
		// A pointer write of a module.
		{"a module overwritten through a pointer", worldFiles(worldBuild("", "    m.* = other.*;\n"), nil), openWant, nil},
		// A module stored in a field of a value Ark does not follow.
		{"a module stored in a struct's field", worldFiles(worldBuild("const Holder = struct { mod: *std.Build.Module };\n",
			"    var holder: Holder = .{ .mod = m };\n    holder.mod = other;\n    holder.mod.addImport(\"helper.zig\", other);\n"), nil), openWant, nil},
		// A root changed after the module is made.
		{"a root assigned", worldFiles(worldBuild("", "    m.root_source_file = b.path(\"lib/other.zig\");\n"), nil), openWant, nil},
		// A cast of a graph value: its fields are another type's.
		{"a pointer cast of a module", worldFiles(worldBuild("const Fake = struct { has_side_effects: bool };\n",
			"    const f: *Fake = @ptrCast(m);\n    f.has_side_effects = true;\n"), nil), openWant, nil},
		// A container-level var holding a module (another file reaches it
		// through the build runner's root).
		{"a container-level var", worldFiles(worldBuild("var global: ?*std.Build.Module = null;\n", "    global = m;\n"), nil), openWant, nil},
		// Module options of another Zig version (0.11's .dependencies; no
		// field of Zig 0.17.0's).
		{"another version's module options", worldFiles(worldBuild("", "    _ = b.createModule(.{ .source_file = .{ .path = \"lib/other.zig\" }, .dependencies = &.{} });\n"), nil), openWant, nil},
		// RED: a namespace bound in the function, its function named like
		// a container's method.
		{"a namespace bound in a function", worldFiles(worldBuild("", "    const h = @import(\"tools/list.zig\");\n    h.append(b, m);\n"),
			map[string]string{"tools/list.zig": "pub fn append(b: anytype, m: anytype) void {\n    _ = b;\n    _ = m;\n}\n"}), openWant, nil},
		// RED: another file's function named like a method of the file.
		{"another file's function named like the file's method", worldFiles(worldBuild("const tools = @import(\"tools/configure.zig\");\nconst S = struct {\n    fn configure(b: *std.Build, m: *std.Build.Module) void {\n        _ = b;\n        _ = m;\n    }\n};\n", "    tools.configure(b, m);\n"), helper), openWant, nil},
		// RED: a std of the file's own (std's paths by name prove nothing).
		{"a std of the file's own", worldFiles("const zstd = @import(\"std\");\nconst std = @import(\"tools/fakestd.zig\");\npub fn build(b: *zstd.Build) void {\n    const util = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", util);\n    _ = std.mem.eql(b, m);\n}\n",
			map[string]string{"tools/fakestd.zig": "pub const mem = struct {\n    pub fn eql(b: anytype, m: anytype) bool {\n        _ = b;\n        _ = m;\n        return true;\n    }\n};\n"}), openWant, nil},
		// Syntax Ark does not read (Zig before 0.15).
		{"usingnamespace", worldFiles(worldBuild("pub usingnamespace @import(\"tools/configure.zig\");\n", ""), helper), openWant, nil},
	}
	runReview(t, cases)
}

// Uses of graph values Ark models keep the build closed.
func TestWorld_ModeledUsesKeepTheBuildClosed(t *testing.T) {
	closed := map[string]string{
		"src/main.zig:4 helper call": "strong lib/util.zig::helper",
		"src/main.zig:5 which call":  "exact src/helper.zig::which",
	}
	cases := []reviewCase{
		{"neutral std.Build methods", worldFiles(worldBuild("", "    const exe = b.addExecutable(.{ .name = \"y\", .root_module = m });\n    b.installArtifact(exe);\n    const run = b.addRunArtifact(exe);\n    run.addArgs(&.{\"--x\"});\n    b.step(\"run\", \"\").dependOn(&run.step);\n    m.link_libc = true;\n    m.addIncludePath(b.path(\"include\"));\n"), nil), closed, nil},
		{"options and strings", worldFiles(worldBuild("", "    const name = b.option([]const u8, \"name\", \"\") orelse \"x\";\n    if (std.mem.eql(u8, name, \"y\")) std.log.info(\"{s}\", .{b.fmt(\"{s}\", .{name})});\n"), nil), closed, nil},
		{"a function of the file given the builder", worldFiles(worldBuild("fn install(b: *std.Build, m: *std.Build.Module) void {\n    b.installArtifact(b.addExecutable(.{ .name = \"z\", .root_module = m }));\n}\n", "    install(b, m);\n"), nil), closed, nil},
		{"a list of modules", worldFiles(worldBuild("", "    var list: std.ArrayList(*std.Build.Module) = .empty;\n    list.append(b.allocator, m) catch @panic(\"OOM\");\n    for (list.items) |x| x.link_libc = true;\n"), nil), closed, nil},
		{"a step made through its type", worldFiles(worldBuild("", "    const run = std.Build.Step.Run.create(b, \"x\");\n    run.addArg(\"y\");\n"), nil), closed, nil},
	}
	runReview(t, cases)
}

// C-2: choices and evaluation. Every branch's binding is read (none is
// chosen, none is proven unreachable).
func TestWorld_BranchesAndEvaluation(t *testing.T) {
	alt := "    const util2 = b.createModule(.{ .root_source_file = b.path(\"lib/util2.zig\") });\n"
	util2 := map[string]string{"lib/util2.zig": "pub fn helper() void {}\n"}
	cases := []reviewCase{
		{"one binding on every path", worldFiles(worldBuild("const builtin = @import(\"builtin\");\n", "    if (builtin.os.tag == .linux) m.addImport(\"util\", util) else m.addImport(\"util\", util);\n"), nil),
			map[string]string{"src/main.zig:4 helper call": "strong lib/util.zig::helper"}, nil},
		{"an option choosing between bindings", worldFiles(worldBuild("", alt+"    if (b.option(bool, \"alt\", \"\") orelse false) m.addImport(\"util\", util2);\n"), util2),
			map[string]string{"src/main.zig:4 helper call": "candidate lib/util.zig::helper|lib/util2.zig::helper"}, nil},
		{"a comptime switch between bindings", worldFiles(worldBuild("const builtin = @import(\"builtin\");\n", alt+"    switch (builtin.os.tag) {\n        .linux => m.addImport(\"util\", util2),\n        else => {},\n    }\n"), util2),
			map[string]string{"src/main.zig:4 helper call": "candidate lib/util.zig::helper|lib/util2.zig::helper"}, nil},
		// Ark proves no branch unreachable: a binding under `if (false)` is
		// read (a Candidate, never a wrong Strong).
		{"a branch never taken", worldFiles(worldBuild("", alt+"    if (false) m.addImport(\"util\", util2);\n"), util2),
			map[string]string{"src/main.zig:4 helper call": "candidate lib/util.zig::helper|lib/util2.zig::helper"}, nil},
		{"recursion", worldFiles(worldBuild("fn add(b: *std.Build, m: *std.Build.Module, n: u32) void {\n    if (n > 0) add(b, m, n - 1);\n    m.link_libc = true;\n}\n", "    add(b, m, 3);\n"), nil),
			map[string]string{"src/main.zig:4 helper call": "strong lib/util.zig::helper"}, nil},
		// RED: a module a function returns, given to another file's code.
		{"a returned module given away", worldFiles(worldBuild("const tools = @import(\"tools/configure.zig\");\nfn make(b: *std.Build) *std.Build.Module {\n    return b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n}\n", "    tools.configure(make(b), m);\n"),
			map[string]string{"tools/configure.zig": "pub fn configure(a: anytype, c: anytype) void {\n    _ = a;\n    _ = c;\n}\n"}), openWant, nil},
		// RED: the builder captured by a loop over values holding it.
		{"a builder captured by a loop", worldFiles(worldBuild("const tools = @import(\"tools/configure.zig\");\n", "    for ([_]*std.Build{b}) |x| tools.configure(x, x);\n"),
			map[string]string{"tools/configure.zig": "pub fn configure(a: anytype, c: anytype) void {\n    _ = a;\n    _ = c;\n}\n"}), openWant, nil},
	}
	runReview(t, cases)
}

// Nesting past what Ark reads opens the build.
func TestWorld_NestingPastTheBoundOpensTheBuild(t *testing.T) {
	deep := strings.Repeat("(", maxDepth) + "1" + strings.Repeat(")", maxDepth)
	runReview(t, []reviewCase{{"deep nesting", worldFiles(worldBuild("", "    _ = "+deep+";\n"), nil), openWant, nil}})
}

// C-3: another build root of the repository, open, may make a module of
// any file.
func TestWorld_AnOpenBuildElsewhereOpensEveryModule(t *testing.T) {
	runReview(t, []reviewCase{
		// RED: tools/build.zig gives its builder to a dependency's code.
		{"an open build of another package", worldFiles(worldBuild("", ""), map[string]string{
			"tools/build.zig": "const std = @import(\"std\");\nconst dep = @import(\"dep\");\npub fn build(b: *std.Build) void {\n    dep.addTo(b);\n}\n",
		}), openWant, []string{"src/helper.zig", "lib/util.zig"}},
		// A closed build elsewhere changes nothing.
		{"a closed build of another package", worldFiles(worldBuild("", ""), map[string]string{
			"tools/build.zig": "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    _ = b.addExecutable(.{ .name = \"t\", .root_module = b.createModule(.{ .root_source_file = b.path(\"t.zig\") }) });\n}\n",
			"tools/t.zig":     "pub fn main() void {}\n",
		}), map[string]string{"src/main.zig:4 helper call": "strong lib/util.zig::helper", "src/main.zig:5 which call": "exact src/helper.zig::which"}, nil},
	})
}

// C-5: the world follows the build through the cache and reindexing: an
// unknown operation added opens it, removed closes it again.
func TestWorld_CacheAndReindex(t *testing.T) {
	files := worldFiles(worldBuild("", ""), map[string]string{"tools/configure.zig": "pub fn configure(b: anytype, m: anytype) void {\n    _ = b;\n    _ = m;\n}\n"})
	dir := writeTree(t, files)
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	build := func(cached bool) string {
		var idx *index.RepositoryIndex
		var err error
		if cached {
			idx, err = index.NewWithCache(context.Background(), dir, []language.Provider{NewProvider()}, store)
		} else {
			idx, err = index.New(context.Background(), dir, []language.Provider{NewProvider()})
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(edgeLines(idx), "\n")
	}
	const strongEdge = "src/main.zig::main -calls-> lib/util.zig::helper strong"
	const exactEdge = "src/main.zig::main -calls-> src/helper.zig::which exact"
	const ownEdge = "src/helper.zig::which -calls-> src/helper.zig::inner exact"
	closed := build(false)
	for _, e := range []string{strongEdge, exactEdge, ownEdge} {
		if !strings.Contains(closed, e) {
			t.Fatalf("closed: no %q in\n%s", e, closed)
		}
	}
	if cold, warm := build(true), build(true); cold != closed || warm != closed {
		t.Fatal("the cache changes the closed graph")
	}
	write := func(src string) {
		if err := os.WriteFile(filepath.Join(dir, "build.zig"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(worldBuild("const tools = @import(\"tools/configure.zig\");\n", "    tools.configure(b, m);\n"))
	opened := build(false)
	if strings.Contains(opened, strongEdge) || strings.Contains(opened, exactEdge) || !strings.Contains(opened, ownEdge) {
		t.Errorf("an unknown operation added: want neither the Strong nor the import's Exact edge, the file's own kept:\n%s", opened)
	}
	if build(true) != opened {
		t.Error("the cache disagrees with the opened build")
	}
	write(files["build.zig"])
	if build(true) != closed || build(false) != closed {
		t.Error("the operation removed, the graph is not the closed one")
	}
}
