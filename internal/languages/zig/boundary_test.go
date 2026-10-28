package zig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Paths the walk does not reach and copies the build compiles: statically
// detectable where the build names them (decision 8). A module root the
// index does not hold — named through a symbolic link, under a directory
// the index skips — may hold any file: every import is a Candidate then
// (AliasDraft.Absent); a copy of a repository's Zig source opens the build.

// rootThroughLink is a repository whose module root is named through
// link, a symbolic link to b: the index holds b/main.zig only.
func rootThroughLink(t *testing.T, root string) (string, map[string]string) {
	files := map[string]string{
		"build.zig":     buildOf("    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"" + root + "\") });\n    m.addImport(\"helper.zig\", other);\n    _ = b.addExecutable(.{ .name = \"a\", .root_module = m });\n"),
		"b/main.zig":    "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
		"b/helper.zig":  "pub fn which() u8 {\n    return 1;\n}\n",
		"lib/other.zig": "pub fn which() u8 {\n    return 2;\n}\n",
	}
	return writeTree(t, files), files
}

func TestBoundary_RootThroughASymlink(t *testing.T) {
	dir, _ := rootThroughLink(t, "link/main.zig")
	if err := os.Symlink("b", filepath.Join(dir, "link")); err != nil {
		t.Skip("no symbolic links here:", err)
	}
	idx := buildIndex(t, dir)
	edges := edgeLines(idx)
	for _, e := range edges {
		if strings.HasPrefix(e, "b/main.zig::") && strings.Contains(e, "-> b/helper.zig") {
			t.Errorf("%s: link/main.zig's module (b/main.zig) binds helper.zig to lib/other.zig", e)
		}
	}
	if e, u := importerCount(t, idx, "lib/other.zig"); e == 0 && u == 0 {
		t.Errorf("lib/other.zig reads complete, but b/main.zig (as link/main.zig) imports it")
	}
	t.Logf("edges:\n  %s", strings.Join(edges, "\n  "))
}

// The same root with the file present (b/main.zig rooted directly) keeps
// the rule: b/main.zig's helper.zig is the module.
func TestBoundary_RootTheIndexHolds(t *testing.T) {
	_, files := rootThroughLink(t, "b/main.zig")
	got := resolveRepo(t, files)
	expectResolutions(t, got, map[string]string{
		"b/main.zig:1 helper.zig import": "candidate b/helper.zig|lib/other.zig",
	})
	// A root nothing names stays out of every other file's way.
	files["src/x.zig"] = "const h = @import(\"y.zig\");\npub fn f() u8 {\n    return h.g();\n}\n"
	files["src/y.zig"] = "pub fn g() u8 {\n    return 0;\n}\n"
	got = resolveRepo(t, files)
	expectResolutions(t, got, map[string]string{"src/x.zig:3 g call": "exact src/y.zig::g"})
}

// A root under a directory the index skips (generated, ignored): its
// module may hold any file.
func TestBoundary_RootTheIndexDoesNotHold(t *testing.T) {
	_, files := rootThroughLink(t, "gen/main.zig")
	got := resolveRepo(t, files)
	expectResolutions(t, got, map[string]string{
		"b/main.zig:3 which call": "candidate b/helper.zig::which|lib/other.zig::which",
	})
}

// A WriteFile copy of the repository's sources compiled as a module:
// src/main.zig's text with lib/other.zig's as its helper.zig.
func TestBoundary_ModuleRootedAtCopies(t *testing.T) {
	runReview(t, []reviewCase{
		{"copies of Zig sources", map[string]string{
			"build.zig": buildOf("    const wf = b.addWriteFiles();\n" +
				"    _ = wf.addCopyFile(b.path(\"src/main.zig\"), \"main.zig\");\n" +
				"    _ = wf.addCopyFile(b.path(\"lib/other.zig\"), \"helper.zig\");\n" +
				"    const m = b.createModule(.{ .root_source_file = wf.getDirectory().path(b, \"main.zig\") });\n" +
				"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n"),
			"src/main.zig":   "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
			"src/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
			"lib/other.zig":  "pub fn which() u8 {\n    return 2;\n}\n",
		}, map[string]string{"src/main.zig:1 helper.zig import": "!strong", "src/main.zig:3 which call": "!strong"}, []string{"lib/other.zig"}},
		// A copy of data keeps the build closed.
		{"a copy of data", map[string]string{
			"build.zig": buildOf("    const wf = b.addWriteFiles();\n" +
				"    _ = wf.addCopyFile(b.path(\"data/table.json\"), \"table.json\");\n" +
				"    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n" +
				"    _ = b.addExecutable(.{ .name = \"x\", .root_module = m });\n"),
			"src/main.zig":   "const h = @import(\"helper.zig\");\npub fn main() void {\n    _ = h.which();\n}\n",
			"src/helper.zig": "pub fn which() u8 {\n    return 1;\n}\n",
		}, map[string]string{"src/main.zig:3 which call": "exact src/helper.zig::which"}, nil},
	})
}
