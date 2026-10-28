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

// Module environments (buildzig.go): an import table is a module's, and
// @import("x.zig") looks it up before the file. Each shape here compiles
// with Zig 0.17.0 as the comments say (module tables given with --dep/-M,
// the way a build passes them); the build.zig sources are read, never run.

// buildOf is a build.zig whose build function states body.
func buildOf(body string) string {
	return "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n" + body + "}\n"
}

func TestModules_ImportOfAFileNamedLikeAModule(t *testing.T) {
	const sibling = "pub fn which() u8 { return 1; }\n"
	const other = "pub fn which() u8 { return 2; }\n"
	const main = "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
	cases := []struct {
		name  string
		files map[string]string
		want  map[string]string
	}{
		{"a relative file, no module of the name", map[string]string{
			"build.zig":      buildOf("    _ = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n"),
			"src/main.zig":   main,
			"src/helper.zig": sibling,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "exact src/helper.zig",
			"src/main.zig:3 which call":        "exact src/helper.zig::which",
		}},
		{"a module bound under the name, the file beside it too", map[string]string{
			// zig test --dep helper.zig -Mroot=src/main.zig -Mhelper.zig=lib/other.zig: the module (2).
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			"src/main.zig":   main,
			"src/helper.zig": sibling,
			"lib/other.zig":  other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "candidate lib/other.zig|src/helper.zig",
			"src/main.zig:3 which call":        "candidate lib/other.zig::which|src/helper.zig::which",
		}},
		{"a module bound under the name, no file of the path", map[string]string{
			"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .imports = &.{.{ .name = \"helper.zig\", .module = other }} });\n    _ = m;\n"),
			"src/main.zig":  main,
			"lib/other.zig": other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "strong lib/other.zig",
			"src/main.zig:3 which call":        "strong lib/other.zig::which",
		}},
		{"a module bound under another name", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"other.zig\", other);\n"),
			"src/main.zig":   main,
			"src/helper.zig": sibling,
			"lib/other.zig":  other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "exact src/helper.zig",
			"src/main.zig:3 which call":        "exact src/helper.zig::which",
		}},
		{"the module binds names Ark cannot read", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(b.option([]const u8, \"n\", \"\") orelse \"\", other);\n"),
			"src/main.zig":   main,
			"src/helper.zig": sibling,
			"lib/other.zig":  other,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "candidate src/helper.zig",
			"src/main.zig:3 which call":        "candidate src/helper.zig::which",
		}},
		{"the name bound to a module Ark cannot name", map[string]string{
			"build.zig":      "const std = @import(\"std\");\npub fn build(b: *std.Build, other: *std.Build.Module) void {\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n}\n",
			"src/main.zig":   main,
			"src/helper.zig": sibling,
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "candidate src/helper.zig",
			"src/main.zig:3 which call":        "candidate src/helper.zig::which",
		}},
		{"a module of another root binds the name", map[string]string{
			// a/ binds helper.zig; b/main.zig is no file of that module.
			"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"a/main.zig\") }).addImport(\"helper.zig\", other);\n    _ = b.createModule(.{ .root_source_file = b.path(\"b/main.zig\") });\n"),
			"a/main.zig":    main,
			"b/main.zig":    main,
			"b/helper.zig":  sibling,
			"lib/other.zig": other,
		}, map[string]string{
			"b/main.zig:1 helper.zig import": "exact b/helper.zig",
			"b/main.zig:3 which call":        "exact b/helper.zig::which",
			"a/main.zig:1 helper.zig import": "strong lib/other.zig",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectResolutions(t, resolveRepo(t, c.files), c.want)
		})
	}
}

func TestModules_TablesArePerModule(t *testing.T) {
	const util = "pub fn helper() void {}\n"
	const imp = "const util = @import(\"util\");\npub fn main() void {\n    util.helper();\n}\n"
	cases := []struct {
		name  string
		files map[string]string
		want  map[string]string
	}{
		{"one module", map[string]string{
			"build.zig":    buildOf("    const u = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const exe = b.addExecutable(.{ .name = \"x\", .root_module = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }) });\n    exe.root_module.addImport(\"util\", u);\n"),
			"src/main.zig": imp, "lib/util.zig": util,
		}, map[string]string{"src/main.zig:3 helper call": "strong lib/util.zig::helper"}},
		{"two modules binding the name to two files", map[string]string{
			// zig test --dep util=libA --dep b -Mroot=app/main.zig --dep util=libB -Mb=b/b.zig: each its own.
			"build.zig":    buildOf("    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const ub = b.createModule(.{ .root_source_file = b.path(\"tools/util.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"app/main.zig\") }).addImport(\"util\", ua);\n    b.createModule(.{ .root_source_file = b.path(\"tools/main.zig\") }).addImport(\"util\", ub);\n"),
			"app/main.zig": imp, "tools/main.zig": imp,
			"lib/util.zig": util, "tools/util.zig": util,
		}, map[string]string{
			"app/main.zig:3 helper call":   "strong lib/util.zig::helper",
			"tools/main.zig:3 helper call": "strong tools/util.zig::helper",
		}},
		{"one literal binding, one computed (the second review's F7)", map[string]string{
			"build.zig":    buildOf("    const util_a = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    _ = b.createModule(.{ .root_source_file = b.path(\"app/main.zig\"), .imports = &.{.{ .name = \"util\", .module = util_a }} });\n    const tool_root = \"tools/util.zig\";\n    const tool = b.createModule(.{ .root_source_file = b.path(\"tools/main.zig\") });\n    tool.addImport(\"util\", b.createModule(.{ .root_source_file = b.path(tool_root) }));\n"),
			"app/main.zig": imp, "tools/main.zig": imp,
			"lib/util.zig": util, "tools/util.zig": util,
		}, map[string]string{
			"app/main.zig:3 helper call":   "strong lib/util.zig::helper",
			"tools/main.zig:3 helper call": "unresolved",
		}},
		{"a registration binds nothing", map[string]string{
			"build.zig":    buildOf("    _ = b.addModule(\"util\", .{ .root_source_file = b.path(\"lib/util.zig\") });\n    _ = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n"),
			"src/main.zig": imp, "lib/util.zig": util,
		}, map[string]string{"src/main.zig:3 helper call": "unresolved"}},
		{"a chain of modules", map[string]string{
			"build.zig":         buildOf("    const u = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const core = b.addModule(\"core\", .{ .root_source_file = b.path(\"src/core/root.zig\"), .imports = &.{.{ .name = \"util\", .module = u }} });\n    b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }).addImport(\"core\", core);\n"),
			"src/main.zig":      "const core = @import(\"core\");\npub fn main() void {\n    core.run();\n}\n",
			"src/core/root.zig": imp + "pub fn run() void {}\n",
			"lib/util.zig":      util,
		}, map[string]string{
			"src/main.zig:3 run call":         "strong src/core/root.zig::run",
			"src/core/root.zig:3 helper call": "strong lib/util.zig::helper",
		}},
		{"modules importing each other", map[string]string{
			"build.zig": buildOf("    const a = b.createModule(.{ .root_source_file = b.path(\"a/a.zig\") });\n    const c = b.createModule(.{ .root_source_file = b.path(\"c/c.zig\") });\n    a.addImport(\"c\", c);\n    c.addImport(\"a\", a);\n"),
			"a/a.zig":   "const c = @import(\"c\");\npub fn fa() void {\n    c.fc();\n}\n",
			"c/c.zig":   "const a = @import(\"a\");\npub fn fc() void {\n    a.fa();\n}\n",
		}, map[string]string{
			"a/a.zig:3 fc call": "strong c/c.zig::fc",
			"c/c.zig:3 fa call": "strong a/a.zig::fa",
		}},
		{"a module passed through a parameter and a tuple (TigerBeetle's shape)", map[string]string{
			"build.zig":         "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const stdx = b.addModule(\"stdx\", .{ .root_source_file = b.path(\"src/stdx/stdx.zig\") });\n    _, const vsr = vsrModule(b, .{ .stdx = stdx });\n    const exe = b.addExecutable(.{ .name = \"x\", .root_module = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }) });\n    addDeps(exe, .{ .stdx = stdx, .vsr = vsr });\n}\nfn vsrModule(b: *std.Build, options: struct { stdx: *std.Build.Module }) struct { u8, *std.Build.Module } {\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/vsr.zig\") });\n    m.addImport(\"stdx\", options.stdx);\n    return .{ 0, m };\n}\nfn addDeps(exe: *std.Build.Step.Compile, options: struct { stdx: *std.Build.Module, vsr: *std.Build.Module }) void {\n    exe.root_module.addImport(\"stdx\", options.stdx);\n    exe.root_module.addImport(\"vsr\", options.vsr);\n}\n",
			"src/main.zig":      "const vsr = @import(\"vsr\");\nconst stdx = @import(\"stdx\");\npub fn main() void {\n    vsr.fatal();\n    stdx.ok();\n}\n",
			"src/vsr.zig":       "const stdx = @import(\"stdx\");\npub fn fatal() void {\n    stdx.ok();\n}\n",
			"src/stdx/stdx.zig": "pub fn ok() void {}\n",
		}, map[string]string{
			"src/main.zig:4 fatal call": "strong src/vsr.zig::fatal",
			"src/main.zig:5 ok call":    "strong src/stdx/stdx.zig::ok",
			"src/vsr.zig:3 ok call":     "strong src/stdx/stdx.zig::ok",
		}},
		{"a binding Ark cannot name, another it can", map[string]string{
			"build.zig":    "const std = @import(\"std\");\npub fn build(b: *std.Build, extra: *std.Build.Module) void {\n    const u = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", u);\n    m.addImport(\"extra\", extra);\n}\n",
			"src/main.zig": imp + "const extra = @import(\"extra\");\npub fn more() void {\n    extra.helper();\n}\n",
			"lib/util.zig": util,
		}, map[string]string{
			"src/main.zig:3 helper call": "strong lib/util.zig::helper",
			"src/main.zig:7 helper call": "unresolved",
		}},
		{"a file two modules may hold, binding the name to two files", map[string]string{
			// src/main.zig is in src/'s module and in the repository root's.
			"build.zig":    buildOf("    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const ub = b.createModule(.{ .root_source_file = b.path(\"tools/util.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }).addImport(\"util\", ua);\n    b.createModule(.{ .root_source_file = b.path(\"root.zig\") }).addImport(\"util\", ub);\n"),
			"src/main.zig": imp, "root.zig": "pub const main = @import(\"src/main.zig\");\n",
			"lib/util.zig": util, "tools/util.zig": util,
		}, map[string]string{"src/main.zig:3 helper call": "candidate lib/util.zig::helper|tools/util.zig::helper"}},
		{"a comptime choice of tables", map[string]string{
			"build.zig":    buildOf("    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    _ = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .imports = if (b.graph.host.result.os.tag == .macos) &.{.{ .name = \"util\", .module = ua }} else &.{} });\n"),
			"src/main.zig": imp, "lib/util.zig": util,
		}, map[string]string{"src/main.zig:3 helper call": "strong lib/util.zig::helper"}},
		{"a copy of another module's table", map[string]string{
			"build.zig":     buildOf("    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const lib = b.createModule(.{ .root_source_file = b.path(\"src/lib.zig\") });\n    lib.addImport(\"util\", ua);\n    const t = b.addTest(.{ .root_module = b.createModule(.{ .root_source_file = b.path(\"test/main.zig\") }) });\n    var it = lib.import_table.iterator();\n    while (it.next()) |entry| t.root_module.addImport(entry.key_ptr.*, entry.value_ptr.*);\n"),
			"test/main.zig": imp, "src/lib.zig": "", "lib/util.zig": util,
		}, map[string]string{"test/main.zig:3 helper call": "strong lib/util.zig::helper"}},
		{"a file no module of the build holds", map[string]string{
			"build.zig":    buildOf("    const u = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"app/main.zig\") }).addImport(\"util\", u);\n"),
			"app/main.zig": imp, "other/main.zig": imp, "lib/util.zig": util,
		}, map[string]string{
			"app/main.zig:3 helper call":   "strong lib/util.zig::helper",
			"other/main.zig:3 helper call": "unresolved",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectResolutions(t, resolveRepo(t, c.files), c.want)
		})
	}
}

// importerCount is a file's importers: graph edges and unattributed ones.
func importerCount(t *testing.T, idx *index.RepositoryIndex, file string) (edges, unattributed int) {
	t.Helper()
	s := symbolByQualified(t, idx, file)
	in, _ := idx.Unattributed(s.ID)
	return len(idx.GetCallers(s.ID)), in
}

func TestModules_ImportersNeverReadComplete(t *testing.T) {
	type count struct{ edges, unattributed int }
	cases := []struct {
		name  string
		files map[string]string
		want  map[string]count // file → importers
	}{
		{"a relative import", map[string]string{
			"src/main.zig": "const e = @import(\"engine.zig\");\n", "src/engine.zig": "",
		}, map[string]count{"src/engine.zig": {1, 0}, "src/main.zig": {0, 0}}},
		{"a named module no build maps", map[string]string{
			"src/cli.zig": "const vsr = @import(\"vsr\");\n", "src/vsr.zig": "", "src/other.zig": "",
		}, map[string]count{"src/vsr.zig": {0, 1}, "src/other.zig": {0, 1}, "src/cli.zig": {0, 1}}},
		{"a module named apart from its root file", map[string]string{
			"build.zig":   buildOf("    const v = b.createModule(.{ .root_source_file = b.path(\"src/vsr.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"src/cli.zig\") }).addImport(\"replica\", v);\n"),
			"src/cli.zig": "const r = @import(\"replica\");\n", "src/vsr.zig": "", "src/other.zig": "",
		}, map[string]count{"src/vsr.zig": {1, 0}, "src/other.zig": {0, 0}}},
		{"one name bound by two modules a file may be in", map[string]string{
			"build.zig":    buildOf("    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/a.zig\") });\n    const ub = b.createModule(.{ .root_source_file = b.path(\"lib/b.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }).addImport(\"util\", ua);\n    b.createModule(.{ .root_source_file = b.path(\"root.zig\") }).addImport(\"util\", ub);\n"),
			"src/main.zig": "const u = @import(\"util\");\n", "root.zig": "", "lib/a.zig": "", "lib/b.zig": "",
		}, map[string]count{"lib/a.zig": {0, 1}, "lib/b.zig": {0, 1}}},
		{"an import in another case", map[string]string{
			// Zig on macOS (case-insensitive) opens engine.zig.
			"main.zig": "const E = @import(\"Engine.zig\");\npub fn main() void {\n    E.run();\n}\n", "engine.zig": "pub fn run() void {}\n",
		}, map[string]count{"engine.zig": {0, 1}, "engine.zig::run": {0, 1}}},
		{"an import cycle and a re-export", map[string]string{
			"a.zig": "const b = @import(\"b.zig\");\npub const run = b.run;\n", "b.zig": "const a = @import(\"a.zig\");\npub fn run() void {}\n",
			"main.zig": "const a = @import(\"a.zig\");\npub fn main() void {\n    a.run();\n}\n",
		}, map[string]count{"a.zig": {2, 0}, "b.zig": {1, 0}, "b.zig::run": {1, 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idx := buildIndex(t, writeTree(t, c.files))
			for file, want := range c.want {
				e, u := importerCount(t, idx, file)
				if e != want.edges || u != want.unattributed {
					t.Errorf("%s: %d importers, %d unattributed; want %d, %d", file, e, u, want.edges, want.unattributed)
				}
			}
		})
	}
}

// Two files apart in case alone (a case-sensitive file system holds both;
// resolved in memory, so the test's own file system does not matter).
func TestModules_PathsApartInCase(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"main.zig":   "const a = @import(\"ENGINE.zig\");\nconst b = @import(\"engine.zig\");\npub fn main() void {\n    a.run();\n    b.run();\n}\n",
		"engine.zig": "pub fn run() void {}\n",
		"Engine.zig": "pub fn run() void {}\n",
	})
	expectResolutions(t, got, map[string]string{
		"main.zig:1 ENGINE.zig import": "candidate Engine.zig|engine.zig",
		"main.zig:2 engine.zig import": "exact engine.zig",
		"main.zig:4 run call":          "candidate Engine.zig::run|engine.zig::run",
		"main.zig:5 run call":          "exact engine.zig::run",
	})
}

// The module environment survives the extraction cache and an edit: a
// cached build, one after an edit of build.zig, and the edit undone answer
// as builds without a cache do.
func TestModules_CacheAndReindex(t *testing.T) {
	files := map[string]string{
		"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }).addImport(\"helper.zig\", other);\n"),
		"src/main.zig":   "const h = @import(\"helper.zig\");\nconst u = @import(\"util\");\npub fn main() void {\n    _ = h.which();\n    u.helper();\n}\n",
		"src/helper.zig": "pub fn which() u8 { return 1; }\n",
		"lib/other.zig":  "pub fn which() u8 { return 2; }\n",
		"lib/util.zig":   "pub fn helper() void {}\n",
	}
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
	before := build(false)
	if strings.Contains(before, "-> src/helper.zig::which exact") || strings.Contains(before, "lib/util.zig::helper") {
		t.Fatalf("an edge nothing proves:\n%s", before)
	}
	cold, warm := build(true), build(true) // writes the cache, then reads it
	if cold != before || warm != before {
		t.Fatal("the cache changes the graph")
	}
	// Bind util too: the next build sees it, cached or not.
	edited := buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const u = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n    m.addImport(\"util\", u);\n")
	if err := os.WriteFile(filepath.Join(dir, "build.zig"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	after := build(false)
	if !strings.Contains(after, "src/main.zig::main -calls-> lib/util.zig::helper strong") || build(true) != after {
		t.Errorf("after the edit (cached equal: %v):\n%s", build(true) == after, after)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.zig"), []byte(files["build.zig"]), 0o644); err != nil {
		t.Fatal(err)
	}
	if build(true) != before {
		t.Error("the edit undone, the cached graph differs")
	}
}

// A function the build calls as a member (`Self.add(m)`): its parameters
// are unknown, so what it binds may be any module's — never ignored.
func TestModules_BindingsOfAFunctionCalledAsAMember(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"build.zig":    "const std = @import(\"std\");\nconst Self = @This();\npub fn build(b: *std.Build) void {\n    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/a.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", ua);\n    Self.rebind(b, m);\n}\nfn rebind(b: *std.Build, m: *std.Build.Module) void {\n    m.addImport(\"util\", b.createModule(.{ .root_source_file = b.path(\"lib/b.zig\") }));\n}\n",
		"src/main.zig": "const util = @import(\"util\");\npub fn main() void {\n    util.helper();\n}\n",
		"lib/a.zig":    "pub fn helper() void {}\n",
		"lib/b.zig":    "pub fn helper() void {}\n",
	})
	// rebind's b is no build Ark can prove this one (a member call passes
	// it): the module it binds is unknown — an open Candidate, never Strong.
	expectResolutions(t, got, map[string]string{
		"src/main.zig:3 helper call": "candidate lib/a.zig::helper",
	})
	idx := buildIndex(t, writeTree(t, map[string]string{
		"build.zig":    "const std = @import(\"std\");\nconst Self = @This();\npub fn build(b: *std.Build) void {\n    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/a.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", ua);\n    Self.rebind(b, m);\n}\nfn rebind(b: *std.Build, m: *std.Build.Module) void {\n    m.addImport(\"util\", b.createModule(.{ .root_source_file = b.path(\"lib/b.zig\") }));\n}\n",
		"src/main.zig": "const util = @import(\"util\");\npub fn main() void {\n    util.helper();\n}\n",
		"lib/a.zig":    "pub fn helper() void {}\n",
		"lib/b.zig":    "pub fn helper() void {}\n",
	}))
	if in, _ := idx.Unattributed(symbolByQualified(t, idx, "lib/b.zig::helper").ID); in != 1 {
		t.Errorf("lib/b.zig::helper: unattributed %d, want 1 (the open resolution may be it)", in)
	}
}

// A table a comptime condition chooses binds what every branch binds; a
// var holding a module may hold another by then.
func TestModules_ChoicesAndVariablesOfABuild(t *testing.T) {
	const imp = "const util = @import(\"util\");\npub fn main() void {\n    util.helper();\n}\n"
	const util = "pub fn helper() void {}\n"
	got := resolveRepo(t, map[string]string{
		"build.zig":    buildOf("    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/a.zig\") });\n    const ub = b.createModule(.{ .root_source_file = b.path(\"lib/b.zig\") });\n    _ = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .imports = if (b.graph.host.result.os.tag == .macos) &.{.{ .name = \"util\", .module = ua }} else &.{.{ .name = \"util\", .module = ub }} });\n"),
		"src/main.zig": imp, "lib/a.zig": util, "lib/b.zig": util,
	})
	expectResolutions(t, got, map[string]string{"src/main.zig:3 helper call": "candidate lib/a.zig::helper|lib/b.zig::helper"})
	got = resolveRepo(t, map[string]string{
		"build.zig":    buildOf("    var u = b.createModule(.{ .root_source_file = b.path(\"lib/a.zig\") });\n    u = b.createModule(.{ .root_source_file = b.path(\"lib/b.zig\") });\n    b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") }).addImport(\"util\", u);\n"),
		"src/main.zig": imp, "lib/a.zig": util, "lib/b.zig": util,
	})
	noStrongTo(t, got, "helper")
}

// Bindings Ark reads resolve; operations it does not read leave the
// module's names unknown. The last two are defensive: a method no std
// release has (a newer API), and module options handed to a function of
// another file, which may make a module of that root and table.
func TestModules_OperationsOnATable(t *testing.T) {
	const imp = "const util = @import(\"util\");\npub fn main() void {\n    util.helper();\n}\n"
	const rel = "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
	got := resolveRepo(t, map[string]string{
		"build.zig":    buildOf("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addAnonymousImport(\"util\", .{ .root_source_file = b.path(\"lib/y.zig\") });\n"),
		"src/main.zig": imp, "lib/y.zig": "pub fn helper() void {}\n",
	})
	expectResolutions(t, got, map[string]string{"src/main.zig:3 helper call": "strong lib/y.zig::helper"})
	for name, build := range map[string]string{
		"a method Ark does not know":            buildOf("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImportsFromNewerZig(b);\n"),
		"options another file's function takes": "const std = @import(\"std\");\nconst helper = @import(\"build/helper.zig\");\npub fn build(b: *std.Build) void {\n    helper.module(b, .{ .root_source_file = b.path(\"src/main.zig\"), .imports = &.{} });\n}\n",
	} {
		got := resolveRepo(t, map[string]string{"build.zig": build, "src/main.zig": rel, "src/helper.zig": "pub fn which() u8 { return 1; }\n"})
		if r := got["src/main.zig:3 which call"]; r != "candidate src/helper.zig::which" {
			t.Errorf("%s: which call %q, want a Candidate (the table's names are unknown)", name, r)
		}
	}
}

// A module held in a var may be changed where Ark does not follow it (its
// root reassigned through the var): what any module binds is unknown.
func TestModules_AModuleHeldInAVar(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    var m = b.createModule(.{ .root_source_file = b.path(\"a/a.zig\") });\n    m.addImport(\"helper.zig\", other);\n    m.root_source_file = b.path(\"c/main.zig\");\n"),
		"c/main.zig":    "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
		"c/helper.zig":  "pub fn which() u8 { return 1; }\n",
		"lib/other.zig": "pub fn which() u8 { return 2; }\n",
	})
	noStrongTo(t, got, "which")
}
