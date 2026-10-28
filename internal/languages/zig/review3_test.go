package zig

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Third review's findings on named-module resolution, each a wrong Strong
// or Exact edge or a file whose importers read complete before the fix.
// Each case is valid Zig 0.17.0 (checked with the compiler, module tables
// given by --dep/-M; the build.zig sources type-checked, never run); want
// states what the truth allows.

type reviewCase struct {
	name  string
	files map[string]string
	// want: reference key → a resolution the truth allows (exact text), or
	// "!strong" / "!exact": anything but a Strong/Exact resolution.
	want map[string]string
	// importers: file → true importers exist; the file must not read
	// complete (0 edges and 0 unattributed).
	importers []string
}

func runReview(t *testing.T, cases []reviewCase) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dumpCase(t, c.files)
			got := resolveRepo(t, c.files)
			var keys []string
			for k := range c.want {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				w := c.want[k]
				g := got[k]
				switch w {
				case "!strong":
					if strings.HasPrefix(g, "strong") || strings.HasPrefix(g, "exact") {
						t.Errorf("%s: got %q, truth allows no Strong/Exact", k, g)
					}
				default:
					if g != w {
						t.Errorf("%s: got %q, want %q", k, g, w)
					}
				}
			}
			var all []string
			for k, v := range got {
				all = append(all, k+" => "+v)
			}
			sort.Strings(all)
			t.Logf("resolutions:\n  %s", strings.Join(all, "\n  "))
			if len(c.importers) > 0 {
				idx := buildIndex(t, writeTree(t, c.files))
				for _, f := range c.importers {
					e, u := importerCount(t, idx, f)
					t.Logf("%s: %d importer edges, %d unattributed", f, e, u)
					if e == 0 && u == 0 {
						t.Errorf("%s reads complete with no importer, but has one", f)
					}
				}
				t.Logf("edges:\n  %s", strings.Join(edgeLines(idx), "\n  "))
			}
		})
	}
}

const advHelperMain = "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n"
const advUtilMain = "const u = @import(\"util\");\npub fn main() void {\n    u.helper();\n}\n"
const advUtil = "pub fn helper() void {}\n"

func TestReview3_ModuleBindingsArkMustNotMiss(t *testing.T) {
	runReview(t, []reviewCase{
		// A1: addAnonymousImport binds "helper.zig" in the module's table.
		{"A1 addAnonymousImport of a .zig name", map[string]string{
			"build.zig":      buildOf("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addAnonymousImport(\"helper.zig\", .{ .root_source_file = b.path(\"lib/other.zig\") });\n"),
			"src/main.zig":   advHelperMain,
			"src/helper.zig": "pub fn which() u8 { return 1; }\n",
			"lib/other.zig":  "pub fn which() u8 { return 2; }\n",
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
			"src/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// A2: addAnonymousImport on module b (src/b.zig); module a, rooted in
		// the same directory, binds util to lib/x.zig.
		{"A2 addAnonymousImport beside another module's binding", map[string]string{
			"build.zig": buildOf("    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"src/a.zig\") });\n    a.addImport(\"util\", ux);\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/b.zig\") });\n    m.addAnonymousImport(\"util\", .{ .root_source_file = b.path(\"lib/y.zig\") });\n"),
			"src/a.zig": advUtilMain, "src/b.zig": advUtilMain,
			"lib/x.zig": advUtil, "lib/y.zig": advUtil,
		}, map[string]string{
			"src/b.zig:3 helper call": "!strong",
		}, []string{"lib/y.zig", "lib/y.zig::helper"}},
		// A3: addOptions binds a generated module under the name.
		{"A3 addOptions beside another module's binding", map[string]string{
			"build.zig":      buildOf("    const cx = b.createModule(.{ .root_source_file = b.path(\"lib/config.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"src/a.zig\") });\n    a.addImport(\"config\", cx);\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/b.zig\") });\n    const opts = b.addOptions();\n    opts.addOption(bool, \"on\", true);\n    m.addOptions(\"config\", opts);\n"),
			"src/a.zig":      "const config = @import(\"config\");\npub fn main() void {\n    _ = config.on;\n}\n",
			"src/b.zig":      "const config = @import(\"config\");\npub fn main() void {\n    _ = config.on;\n}\n",
			"lib/config.zig": "pub const on = false;\n",
		}, map[string]string{
			"src/b.zig:3 on value_reference": "!strong",
		}, nil},
		// A4: createModule(options) with options a constant holding .imports.
		{"A4 module options through a constant", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const opts: std.Build.Module.CreateOptions = .{ .root_source_file = b.path(\"src/main.zig\"), .imports = &.{.{ .name = \"helper.zig\", .module = other }} };\n    _ = b.createModule(opts);\n"),
			"src/main.zig":   advHelperMain,
			"src/helper.zig": "pub fn which() u8 { return 1; }\n",
			"lib/other.zig":  "pub fn which() u8 { return 2; }\n",
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
			"src/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig"}},
		// A5: std.Build.Module.create with .imports.
		{"A5 std.Build.Module.create", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    _ = std.Build.Module.create(b, .{ .root_source_file = b.path(\"src/main.zig\"), .imports = &.{.{ .name = \"helper.zig\", .module = other }} });\n"),
			"src/main.zig":   advHelperMain,
			"src/helper.zig": "pub fn which() u8 { return 1; }\n",
			"lib/other.zig":  "pub fn which() u8 { return 2; }\n",
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
			"src/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig"}},
		// A6: import_table.put on the module.
		{"A6 import_table.put", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.import_table.put(b.graph.arena, \"helper.zig\", other) catch @panic(\"OOM\");\n"),
			"src/main.zig":   advHelperMain,
			"src/helper.zig": "pub fn which() u8 { return 1; }\n",
			"lib/other.zig":  "pub fn which() u8 { return 2; }\n",
		}, map[string]string{
			"src/main.zig:1 helper.zig import": "!strong",
			"src/main.zig:3 which call":        "!strong",
		}, []string{"lib/other.zig"}},
		// A7: a build function called directly and through @call: the
		// @call's arguments are unseen.
		{"A7 @call of a build function", map[string]string{
			"build.zig": "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const uy = b.createModule(.{ .root_source_file = b.path(\"lib/y.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"src/a.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/b.zig\") });\n    addUtil(a, ux);\n    @call(.auto, addUtil, .{ m, uy });\n}\nfn addUtil(m: *std.Build.Module, u: *std.Build.Module) void {\n    m.addImport(\"util\", u);\n}\n",
			"src/a.zig": advUtilMain, "src/b.zig": advUtilMain,
			"lib/x.zig": advUtil, "lib/y.zig": advUtil,
		}, map[string]string{
			"src/b.zig:3 helper call": "!strong",
		}, []string{"lib/y.zig", "lib/y.zig::helper"}},
		// A9: a typed destructuring: the type's identifier shifts the index.
		{"A9 typed tuple destructuring", map[string]string{
			"build.zig":    buildOf("    const P = *std.Build.Module;\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    const first: P, const ua, const ub = .{ m, b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") }), b.createModule(.{ .root_source_file = b.path(\"lib/y.zig\") }) };\n    first.addImport(\"util\", ua);\n    _ = ub;\n"),
			"src/main.zig": advUtilMain,
			"lib/x.zig":    advUtil, "lib/y.zig": advUtil,
		}, map[string]string{
			"src/main.zig:3 helper call": "strong lib/x.zig::helper", // the second name, past the annotated first
		}, []string{"lib/x.zig", "lib/x.zig::helper"}},
		// A10: a dependency's module: a second build.zig of the repository
		// registers the name with a root Ark cannot read.
		{"A10 registration with an unread root", map[string]string{
			"build.zig":             buildOf("    _ = b.addModule(\"util\", .{ .root_source_file = b.path(\"src/util.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", b.dependency(\"sub\", .{}).module(\"util\"));\n"),
			"src/main.zig":          advUtilMain,
			"src/util.zig":          advUtil,
			"deps/sub/build.zig":    buildOf("    const root = \"src/real.zig\";\n    _ = b.addModule(\"util\", .{ .root_source_file = b.path(root) });\n"),
			"deps/sub/src/real.zig": advUtil,
		}, map[string]string{
			"src/main.zig:3 helper call": "!strong",
		}, []string{"deps/sub/src/real.zig", "deps/sub/src/real.zig::helper"}},
		// A11: a name Ark cannot read beside a literal one: the import may be
		// the other module (-Dn=util); its file must not read complete.
		{"A11 dynamic name beside a literal binding", map[string]string{
			"build.zig":    buildOf("    const ua = b.createModule(.{ .root_source_file = b.path(\"lib/a.zig\") });\n    const ub = b.createModule(.{ .root_source_file = b.path(\"lib/b.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", ua);\n    m.addImport(b.option([]const u8, \"n\", \"\") orelse \"\", ub);\n"),
			"src/main.zig": advUtilMain,
			"lib/a.zig":    advUtil, "lib/b.zig": advUtil,
		}, map[string]string{}, []string{"lib/b.zig", "lib/b.zig::helper"}},
		// A12: the same for a relative import (the existing test's shape).
		{"A12 dynamic name and a relative import", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(b.option([]const u8, \"n\", \"\") orelse \"\", other);\n"),
			"src/main.zig":   advHelperMain,
			"src/helper.zig": "pub fn which() u8 { return 1; }\n",
			"lib/other.zig":  "pub fn which() u8 { return 2; }\n",
		}, map[string]string{}, []string{"lib/other.zig", "lib/other.zig::which"}},
		// A13: module b binds util to a fetched package's module (no
		// registration in the repository); module a, rooted in the same
		// directory, binds util to lib/x.zig.
		{"A13 a fetched module beside another module's binding", map[string]string{
			"build.zig": buildOf("    const ux = b.createModule(.{ .root_source_file = b.path(\"lib/x.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"src/a.zig\") });\n    a.addImport(\"util\", ux);\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/b.zig\") });\n    m.addImport(\"util\", b.dependency(\"foo\", .{}).module(\"util\"));\n"),
			"src/a.zig": advUtilMain, "src/b.zig": advUtilMain,
			"lib/x.zig": advUtil,
		}, map[string]string{
			"src/b.zig:3 helper call": "!strong",
		}, nil},
	})
}

// dumpCase writes a case's files under ARK_ZIG_DUMP_CASES/<test name>
// (for checking them with the compiler); nothing when it is not set.
func dumpCase(t *testing.T, files map[string]string) {
	dir := os.Getenv("ARK_ZIG_DUMP_CASES")
	if dir == "" {
		return
	}
	root := filepath.Join(dir, filepath.FromSlash(strings.NewReplacer(" ", "_", "'", "").Replace(t.Name())))
	for p, src := range files {
		f := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
