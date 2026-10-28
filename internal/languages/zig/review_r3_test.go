package zig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
)

// R3 adversarial review: relative imports ({zigimp}) and completeness.
// Each case is valid Zig 0.17.0 (checked with the compiler on macOS's
// default, case-insensitive file system; build.zig type-checked, never run).

const r3Closed = "    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    _ = b.addExecutable(.{ .name = \"a\", .root_module = m });\n"

func TestReviewR3_CaseFoldInsideAChain(t *testing.T) {
	runReview(t, []reviewCase{
		// R3-1: b.zig re-exports C.zig (the file is c.zig). The compiler
		// opens src/c.zig (a broken c.zig fails main's b.c.f()). main's call
		// is a Candidate at most — and must count toward src/c.zig::f. (Ark
		// leaves it Unresolved, attributable: a path folded inside a chain
		// of re-exports is not followed — a gap of recall, not a claim.)
		{"R3-1 namespace re-export through a case-folded path", map[string]string{
			"build.zig":    buildOf(r3Closed),
			"src/main.zig": "const b = @import(\"b.zig\");\npub fn main() void {\n    b.c.f();\n}\n",
			"src/b.zig":    "pub const c = @import(\"C.zig\");\n",
			"src/c.zig":    "pub fn f() void {}\n",
		}, map[string]string{
			"src/main.zig:3 f call": "!strong",
		}, []string{"src/c.zig::f"}},
		// R3-1b: a declaration re-exported from a case-folded path.
		{"R3-1b declaration re-export through a case-folded path", map[string]string{
			"build.zig":    buildOf(r3Closed),
			"src/main.zig": "const b = @import(\"b.zig\");\npub fn main() void {\n    b.V.m();\n}\n",
			"src/b.zig":    "pub const V = @import(\"X.zig\").V;\n",
			"src/x.zig":    "pub const V = struct {\n    pub fn m() void {}\n};\n",
		}, map[string]string{
			"src/main.zig:3 m call": "!strong",
		}, []string{"src/x.zig::V::m"}},
	})
}

func TestReviewR3_CaseFoldBesideATableBinding(t *testing.T) {
	// R3-2: m1 (src/main.zig) binds "X.zig" to lib/other.zig; m2
	// (src/sub/root.zig) binds nothing. src/sub/a.zig is in either: in m1
	// `@import("X.zig")` is lib/other.zig, in m2 it is src/sub/x.zig (case
	// folded; a broken x.zig fails m2 only). Neither may be Strong.
	runReview(t, []reviewCase{
		{"R3-2 table binding hides the case-folded file", map[string]string{
			"build.zig": buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
				"    const m1 = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    m1.addImport(\"X.zig\", other);\n" +
				"    const m2 = b.createModule(.{ .root_source_file = b.path(\"src/sub/root.zig\") });\n" +
				"    _ = b.addExecutable(.{ .name = \"a\", .root_module = m1 });\n" +
				"    _ = b.addExecutable(.{ .name = \"b\", .root_module = m2 });\n"),
			"src/main.zig":     "const s = @import(\"sub/a.zig\");\npub fn main() void {\n    s.run();\n}\n",
			"src/sub/root.zig": "const a = @import(\"a.zig\");\npub fn main() void {\n    a.run();\n}\n",
			"src/sub/a.zig":    "const x = @import(\"X.zig\");\npub fn run() void {\n    x.which();\n}\n",
			"src/sub/x.zig":    "pub fn which() void {}\n",
			"lib/other.zig":    "pub fn which() void {}\n",
		}, map[string]string{
			"src/sub/a.zig:1 X.zig import": "!strong",
			"src/sub/a.zig:3 which call":   "!strong",
		}, []string{"src/sub/x.zig", "src/sub/x.zig::which"}},
	})
}

func TestReviewR3_ModuleNamedLikeAZonFile(t *testing.T) {
	// R3-3: a table entry "data.zon" binds a Zig module (zig build-exe
	// --dep data.zon -Mroot=src/main.zig -Mdata.zon=lib/other.zig: d.f() is
	// lib/other.zig's f). Its importers must not read complete.
	main := "const d = @import(\"data.zon\");\npub fn main() void {\n    d.f();\n}\n"
	runReview(t, []reviewCase{
		{"R3-3a closed build binds data.zon", map[string]string{
			"build.zig": buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
				"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    m.addImport(\"data.zon\", other);\n" +
				"    _ = b.addExecutable(.{ .name = \"a\", .root_module = m });\n"),
			"src/main.zig":  main,
			"lib/other.zig": "pub fn f() void {}\n",
		}, nil, []string{"lib/other.zig", "lib/other.zig::f"}},
		{"R3-3b open build (helper file)", map[string]string{
			"build.zig": "const std = @import(\"std\");\nconst tools = @import(\"tools/configure.zig\");\npub fn build(b: *std.Build) void {\n" +
				"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    tools.configure(b, m);\n" +
				"    _ = b.addExecutable(.{ .name = \"a\", .root_module = m });\n}\n",
			"tools/configure.zig": "const std = @import(\"std\");\npub fn configure(b: *std.Build, m: *std.Build.Module) void {\n    m.addImport(\"data.zon\", b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") }));\n}\n",
			"src/main.zig":        main,
			"lib/other.zig":       "pub fn f() void {}\n",
		}, nil, []string{"lib/other.zig", "lib/other.zig::f"}},
		// R3-3c (no build at all, the table given on a command line) is
		// outside the boundary: the graph is the repository's builds'.
	})
}

// R3-4: the graph across the cache while files and build.zig change; each
// step compares a warm cached index with a fresh one.
func TestReviewR3_CacheAcrossChanges(t *testing.T) {
	closed := buildOf(r3Closed)
	bound := buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"c.zig\", other);\n    _ = b.addExecutable(.{ .name = \"a\", .root_module = m });\n")
	files := map[string]string{
		"build.zig":     closed,
		"src/main.zig":  "const b = @import(\"b.zig\");\nconst x = @import(\"X.zig\");\npub fn main() void {\n    b.c.f();\n    x.which();\n}\n",
		"src/b.zig":     "pub const c = @import(\"c.zig\");\n",
		"lib/other.zig": "pub fn which() void {}\npub fn f() void {}\n",
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
		return strings.Join(append(out, edgeLines(idx)...), "\n")
	}
	check := func(step string) {
		plain := render(buildIndex(t, dir))
		for i := 0; i < 2; i++ {
			idx, err := index.NewWithCache(context.Background(), dir, []language.Provider{NewProvider()}, store)
			if err != nil {
				t.Fatal(err)
			}
			if got := render(idx); got != plain {
				t.Errorf("%s pass %d: cache differs:\n%s\nwant\n%s", step, i, got, plain)
			}
		}
		t.Logf("%s:\n%s", step, plain)
	}
	write := func(name, src string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check("1 neither c.zig nor x.zig")
	write("src/c.zig", "pub fn f() void {}\n")
	write("src/x.zig", "pub fn which() void {}\n")
	check("2 c.zig and x.zig added")
	write("build.zig", bound)
	check("3 build binds c.zig")
	write("build.zig", closed)
	check("4 binding removed")
	if err := os.Remove(filepath.Join(dir, "src", "c.zig")); err != nil {
		t.Fatal(err)
	}
	check("5 c.zig removed")
}

// R3-5: shapes that hold (each checked against the compiler): self import,
// ../, one file from two directories, a bound root in another case.
func TestReviewR3_Holds(t *testing.T) {
	runReview(t, []reviewCase{
		{"R3-5a self import", map[string]string{
			"build.zig":    buildOf(r3Closed),
			"src/main.zig": "const Self = @import(\"main.zig\");\npub fn main() void {\n    Self.g();\n}\npub fn g() void {}\n",
		}, map[string]string{
			"src/main.zig:3 g call":          "exact src/main.zig::g",
			"src/main.zig:1 main.zig import": "exact src/main.zig",
		}, nil},
		{"R3-5b ../ from a subdirectory", map[string]string{
			"build.zig":     buildOf(r3Closed),
			"src/main.zig":  "const s = @import(\"sub/a.zig\");\npub fn main() void {\n    s.run();\n}\n",
			"src/sub/a.zig": "const u = @import(\"../util.zig\");\npub fn run() void {\n    u.f();\n}\n",
			"src/util.zig":  "pub fn f() void {}\n",
		}, map[string]string{
			"src/sub/a.zig:3 f call": "exact src/util.zig::f",
		}, nil},
		{"R3-5c one file from two directories, one spec bound", map[string]string{
			// m binds "x.zig" (so src/lib/b.zig's import is other.zig); src/a.zig
			// writes "lib/x.zig", no table entry: the compiler opens src/lib/x.zig.
			"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"other/y.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"x.zig\", other);\n    _ = b.addExecutable(.{ .name = \"a\", .root_module = m });\n"),
			"src/main.zig":  "const a = @import(\"a.zig\");\nconst b = @import(\"lib/b.zig\");\npub fn main() void {\n    a.run();\n    b.run();\n}\n",
			"src/a.zig":     "const x = @import(\"lib/x.zig\");\npub fn run() void {\n    x.which();\n}\n",
			"src/lib/b.zig": "const x = @import(\"x.zig\");\npub fn run() void {\n    x.which();\n}\n",
			"src/lib/x.zig": "pub fn which() void {}\n",
			"other/y.zig":   "pub fn which() void {}\n",
		}, map[string]string{
			"src/lib/b.zig:3 which call": "!strong",
		}, []string{"src/lib/x.zig", "other/y.zig", "other/y.zig::which", "src/lib/x.zig::which"}},
		{"R3-5d bound root in another case", map[string]string{
			"build.zig":    buildOf("    const u = b.createModule(.{ .root_source_file = b.path(\"lib/Util.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"util\", u);\n    _ = b.addExecutable(.{ .name = \"a\", .root_module = m });\n"),
			"src/main.zig": "const u = @import(\"util\");\npub fn main() void {\n    u.helper();\n}\n",
			"lib/util.zig": "pub fn helper() void {}\n",
		}, map[string]string{
			"src/main.zig:3 helper call": "!strong",
		}, []string{"lib/util.zig", "lib/util.zig::helper"}},
	})
}

// R3-2u: R3-2 with a path apart in Unicode normalization only (the import
// writes NFD, the file is NFC; APFS opens it: a broken \u00e9.zig fails m2).
func TestReviewR3_NormalizationBesideATableBinding(t *testing.T) {
	const nfd, nfc = "e\u0301.zig", "\u00e9.zig"
	runReview(t, []reviewCase{
		{"R3-2u table binding hides the normalization-folded file", map[string]string{
			"build.zig": buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n" +
				"    const m1 = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    m1.addImport(\"" + nfd + "\", other);\n" +
				"    const m2 = b.createModule(.{ .root_source_file = b.path(\"src/sub/root.zig\") });\n" +
				"    _ = b.addExecutable(.{ .name = \"a\", .root_module = m1 });\n" +
				"    _ = b.addExecutable(.{ .name = \"b\", .root_module = m2 });\n"),
			"src/main.zig":     "const s = @import(\"sub/a.zig\");\npub fn main() void {\n    s.run();\n}\n",
			"src/sub/root.zig": "const a = @import(\"a.zig\");\npub fn main() void {\n    a.run();\n}\n",
			"src/sub/a.zig":    "const x = @import(\"" + nfd + "\");\npub fn run() void {\n    x.which();\n}\n",
			"src/sub/" + nfc:   "pub fn which() void {}\n",
			"lib/other.zig":    "pub fn which() void {}\n",
		}, map[string]string{
			"src/sub/a.zig:3 which call": "!strong",
		}, []string{"src/sub/" + nfc + "::which"}},
	})
}
