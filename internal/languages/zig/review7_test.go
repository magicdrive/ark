package zig

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
)

// Seventh review's findings (a convergence check that found more), each a
// wrong Exact/Strong edge or a list read complete before the fix: a
// re-export chain past the alias depth, a bound root the index does not
// hold, names with escapes in ordinary sources, @field by a literal name,
// paths apart in Unicode normalization or case, a build script apart in
// case, another package's build run with this b. Each is valid Zig 0.17.0
// (checked with the compiler: build.zig type-checked, never run).

const (
	nfc = "caf\u00e9"  // café, precomposed
	nfd = "cafe\u0301" // café, decomposed (what the file system holds)
)

func TestReview7_ShapesArkMustNotMisread(t *testing.T) {
	relWant := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig", "lib/other.zig::which"}
	cases := []reviewCase{
		// H1: an @import operand with an escape (Zig decodes it).
		{"H1 escaped @import operand", map[string]string{
			"src/main.zig":   "const h = @import(\"hel\\x70er.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
			"src/helper.zig": r4Sibling,
		}, nil, []string{"src/helper.zig", "src/helper.zig::which"}},
		// H2: a member called by a quoted identifier with an escape.
		{"H2 escaped member call", map[string]string{
			"src/main.zig":   "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.@\"wh\\x69ch\"();\n}\n",
			"src/helper.zig": r4Sibling,
		}, nil, []string{"src/helper.zig::which"}},
		// H2b: the head written as a quoted identifier with an escape.
		{"H2b escaped head", map[string]string{
			"src/main.zig":   "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = @\"\\x68\".which();\n}\n",
			"src/helper.zig": r4Sibling,
		}, nil, []string{"src/helper.zig::which"}},
		// H3: a relative import written NFC, the file NFD (APFS opens it).
		{"H3 NFC import of an NFD file", map[string]string{
			"src/main.zig":        "const E = @import(\"" + nfc + ".zig\");\npub fn main() void {\n    E.run();\n}\n",
			"src/" + nfd + ".zig": "pub fn run() void {}\n",
		}, nil, []string{"src/" + nfd + ".zig", "src/" + nfd + ".zig::run"}},
		// H4: a module root written NFC, its directory NFD: the module's
		// .zig name is not seen.
		{"H4 NFC root of an NFD directory", map[string]string{
			"build.zig":         r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"" + nfc + "/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			nfd + "/main.zig":   r4Main,
			nfd + "/helper.zig": r4Sibling,
			"lib/other.zig":     r4Other,
		}, map[string]string{nfd + "/main.zig:1 helper.zig import": "!strong", nfd + "/main.zig:3 which call": "!strong"}, imp},
		// H4b: final sigma: APFS folds ς to σ, strings.ToLower does not.
		{"H4b Σ root of a ς directory", map[string]string{
			"build.zig":         r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"\u03a3/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			"\u03c2/main.zig":   r4Main,
			"\u03c2/helper.zig": r4Sibling,
			"lib/other.zig":     r4Other,
		}, map[string]string{"\u03c2/main.zig:1 helper.zig import": "!strong", "\u03c2/main.zig:3 which call": "!strong"}, imp},
		// H5: G1 with the import of the other build.zig escaped.
		{"H5 escaped import of sub/build.zig", map[string]string{
			"build.zig":      "const std = @import(\"std\");\nconst sub = @import(\"sub/b\\x75ild.zig\");\npub fn build(b: *std.Build) void {\n    sub.build(b);\n}\n",
			"sub/build.zig":  "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n",
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, relWant, imp},
		// H6: G1 with the import of the other build.zig in another case.
		{"H6 sub/BUILD.zig import", map[string]string{
			"build.zig":      "const std = @import(\"std\");\nconst sub = @import(\"sub/BUILD.zig\");\npub fn build(b: *std.Build) void {\n    sub.build(b);\n}\n",
			"sub/build.zig":  "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n",
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, relWant, imp},
		// H7: the build script named Build.zig (macOS opens it for build.zig).
		{"H7 Build.zig", map[string]string{
			"Build.zig":      r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
			"lib/other.zig":  r4Other,
		}, relWant, imp},
	}
	runReview(t, cases)
}

// L: a binding to a module whose root file the index does not hold (a
// generated or ignored file): the alias is pruned (live), the in-repository
// alternative stands alone.
func TestReview7_RootsTheIndexDoesNotHold(t *testing.T) {
	runReview(t, []reviewCase{
		// L1: helper.zig bound to a module rooted in gen/other.zig (absent).
		{"L1 .zig name bound to an absent root", map[string]string{
			"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"gen/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			"src/main.zig":   r4Main,
			"src/helper.zig": r4Sibling,
		}, map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}, nil},
		// L2: module a binds util to gen/util.zig (absent), module c, rooted
		// in the same directory, to lib/util.zig.
		{"L2 named import, one root absent", map[string]string{
			"build.zig":    buildOf("    const ga = b.createModule(.{ .root_source_file = b.path(\"gen/util.zig\") });\n    const lu = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"src/a.zig\") });\n    a.addImport(\"util\", ga);\n    const c = b.createModule(.{ .root_source_file = b.path(\"src/c.zig\") });\n    c.addImport(\"util\", lu);\n"),
			"src/a.zig":    advUtilMain,
			"src/c.zig":    "pub fn main() void {}\n",
			"lib/util.zig": advUtil,
		}, map[string]string{"src/a.zig:1 util import": "!strong", "src/a.zig:3 helper call": "!strong"}, nil},
	})
}

// L1v / L2v: the same through the index, the bound root a file of vendor/
// (index.SkipDirName: never indexed) present on disk.
func TestReview7_VendoredRootsThroughTheIndex(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		bad   []string // edges the truth does not allow
	}{
		{"L1v", map[string]string{
			"build.zig":        buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"vendor/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			"src/main.zig":     r4Main,
			"src/helper.zig":   r4Sibling,
			"vendor/other.zig": r4Other,
		}, []string{"src/main.zig::h -imports-> src/helper.zig", "src/main.zig::main -calls-> src/helper.zig::which"}},
		{"L2v", map[string]string{
			"build.zig":       buildOf("    const vu = b.createModule(.{ .root_source_file = b.path(\"vendor/util.zig\") });\n    const lu = b.createModule(.{ .root_source_file = b.path(\"lib/util.zig\") });\n    const a = b.createModule(.{ .root_source_file = b.path(\"src/a.zig\") });\n    a.addImport(\"util\", vu);\n    const c = b.createModule(.{ .root_source_file = b.path(\"src/c.zig\") });\n    c.addImport(\"util\", lu);\n"),
			"src/a.zig":       advUtilMain,
			"src/c.zig":       "pub fn main() void {}\n",
			"lib/util.zig":    advUtil,
			"vendor/util.zig": advUtil,
		}, []string{"src/a.zig::u -imports-> lib/util.zig", "src/a.zig::main -calls-> lib/util.zig::helper"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idx := buildIndex(t, writeTree(t, c.files))
			lines := edgeLines(idx)
			t.Logf("edges:\n  %s", strings.Join(lines, "\n  "))
			for _, l := range lines {
				for _, b := range c.bad {
					if strings.HasPrefix(l, b+" ") && (strings.HasSuffix(l, " strong") || strings.HasSuffix(l, " exact")) {
						t.Errorf("edge the truth does not allow: %s", l)
					}
				}
			}
		})
	}
}

// reexportChain: src/r1.zig imports helper.zig (the table binds it to
// lib/other.zig); r2..rN re-export it; main reaches it through rN.
func reexportChain(n int) map[string]string {
	files := map[string]string{
		"build.zig":      buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
		"src/helper.zig": r4Sibling,
		"lib/other.zig":  r4Other,
		"src/r1.zig":     "pub const h = @import(\"helper.zig\");\n",
		"src/main.zig":   fmt.Sprintf("const h = @import(\"r%d.zig\").h;\npub fn main() void {\n    _ = h.which();\n}\n", n),
	}
	for i := 2; i <= n; i++ {
		files[fmt.Sprintf("src/r%d.zig", i)] = fmt.Sprintf("pub const h = @import(\"r%d.zig\").h;\n", i-1)
	}
	return files
}

// K: a re-export chain long enough that the {zigspec} alias, its last
// hop, lies past MaxAliasDepth: its cap is never seen.
func TestReview7_ReexportChainPastTheDepthBound(t *testing.T) {
	for n := 1; n <= 9; n++ {
		got := resolveRepo(t, reexportChain(n))
		t.Logf("n=%d: main which call => %s ; r1 import => %s", n, got["src/main.zig:3 which call"], got["src/r1.zig:1 helper.zig import"])
		if g := got["src/main.zig:3 which call"]; strings.HasPrefix(g, "exact") || strings.HasPrefix(g, "strong") {
			t.Errorf("n=%d: %s, truth is lib/other.zig::which", n, g)
		}
	}
}

// K2: the importers of the targets through the index, by chain length,
// with and without the build.zig.
func TestReview7_ReexportChainCompleteness(t *testing.T) {
	for _, withBuild := range []bool{true, false} {
		for _, n := range []int{7, 8, 9, 10} {
			files := reexportChain(n)
			if !withBuild {
				delete(files, "build.zig")
			}
			idx := buildIndex(t, writeTree(t, files))
			for _, f := range []string{"src/helper.zig::which", "lib/other.zig::which"} {
				e, u := importerCount(t, idx, f)
				t.Logf("build=%v n=%d %s: %d edges, %d unattributed", withBuild, n, f, e, u)
			}
		}
	}
}

// M: module options held in a var whose .imports is assigned before the
// module is made.
func TestReview7_OptionsVar(t *testing.T) {
	relWant := map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}
	imp := []string{"lib/other.zig", "lib/other.zig::which"}
	runReview(t, []reviewCase{
		{"M1 var options, .imports assigned", r5Files(r5Body("    var opts: std.Build.Module.CreateOptions = .{ .root_source_file = b.path(\"src/main.zig\") };\n    opts.imports = &.{.{ .name = \"helper.zig\", .module = other }};\n    _ = b.createModule(opts);\n")), relWant, imp},
		{"M2 var options untyped, .imports assigned", r5Files(r5Body("    var opts = std.Build.Module.CreateOptions{ .root_source_file = b.path(\"src/main.zig\") };\n    opts.imports = &.{.{ .name = \"helper.zig\", .module = other }};\n    _ = b.createModule(opts);\n")), relWant, imp},
		{"M3 const options with an imports slice var", r5Files(r5Body("    var imps = [_]std.Build.Module.Import{.{ .name = \"x.zig\", .module = other }};\n    imps[0].name = \"helper.zig\";\n    _ = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\"), .imports = &imps });\n")), relWant, imp},
	})
}

// C: completeness (not only edges) through the cache, cold and warm.
func TestReview7_CacheCompleteness(t *testing.T) {
	files := map[string]string{
		"build.zig":      buildOf("    const d = b.dependency(\"pkg\", .{});\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"dep\", d.module(\"unregistered\"));\n    m.addImport(b.option([]const u8, \"n\", \"\") orelse \"x\", m);\n"),
		"src/main.zig":   "const v = @import(\"vsr\");\nconst d = @import(\"dep\");\nconst E = @import(\"Engine.zig\");\nconst h = @import(\"helper.zig\");\npub fn main() void {\n    v.f();\n    d.f();\n    E.run();\n    h.which();\n}\n",
		"src/engine.zig": "pub fn run() void {}\n",
		"src/helper.zig": "pub fn which() void {}\n",
		"lib/vsr.zig":    "pub fn f() void {}\n",
	}
	dir := writeTree(t, files)
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	render := func(idx *index.RepositoryIndex) string {
		var out []string
		for _, s := range idx.FindSymbols("") {
			in, _ := idx.Unattributed(s.ID)
			out = append(out, fmt.Sprintf("%s %d %d", s.Qualified, len(idx.GetCallers(s.ID)), in))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	plain := render(buildIndex(t, dir))
	for i := 0; i < 2; i++ {
		idx, err := index.NewWithCache(context.Background(), dir, []language.Provider{NewProvider()}, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := render(idx); got != plain {
			t.Errorf("pass %d differs:\n%s\nwant\n%s", i, got, plain)
		}
	}
	t.Logf("\n%s", plain)
}

// H8: G1 through build.zig's named import: a path dependency's build run
// with the root's b.
func TestReview7_DependencyBuildRunWithTheRootsB(t *testing.T) {
	runReview(t, []reviewCase{
		{"H8 @import(\"sub\").build(b)", map[string]string{
			"build.zig":          "const std = @import(\"std\");\nconst sub = @import(\"sub\");\npub fn build(b: *std.Build) void {\n    sub.build(b);\n}\n",
			"deps/sub/build.zig": "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n}\n",
			"src/main.zig":       r4Main,
			"src/helper.zig":     r4Sibling,
			"lib/other.zig":      r4Other,
		}, map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}, []string{"lib/other.zig", "lib/other.zig::which"}},
	})
}

// H2c: a member reached by @field with a literal name (static in Zig).
func TestReview7_FieldWithALiteralName(t *testing.T) {
	runReview(t, []reviewCase{
		{"H2c @field(h, \"which\")()", map[string]string{
			"src/main.zig":   "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = @field(h, \"which\")();\n}\n",
			"src/helper.zig": r4Sibling,
		}, nil, []string{"src/helper.zig::which"}},
	})
}

// P: deep and long paths (held: the hops do not grow with the path).
func TestReview7_DeepPaths(t *testing.T) {
	d := strings.Repeat("d/", 60) + strings.Repeat("x", 200)
	runReview(t, []reviewCase{
		{"P1 deep root, deeper importer", map[string]string{
			"build.zig":             r5Body("    const m = b.createModule(.{ .root_source_file = b.path(\"" + d + "/main.zig\") });\n    m.addImport(\"helper.zig\", other);\n"),
			d + "/main.zig":         "const s = @import(\"s/s/s/inner.zig\");\n",
			d + "/s/s/s/inner.zig":  r4Main,
			d + "/s/s/s/helper.zig": r4Sibling,
			"lib/other.zig":         r4Other,
		}, map[string]string{d + "/s/s/s/inner.zig:1 helper.zig import": "!strong", d + "/s/s/s/inner.zig:3 which call": "!strong"}, nil},
	})
}
