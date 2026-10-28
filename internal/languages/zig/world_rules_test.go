package zig

import (
	"sort"
	"strings"
	"testing"
)

// Each rule of a closed build alone (closed.go): a build with one form
// only that rule reads opens, and the taint flows where the rules say.
// The adversarial reviews' shapes (review_*_test.go) are caught by several
// rules at once; these pin each.

func TestWorldRules_EachOpensAlone(t *testing.T) {
	cases := []struct{ name, top, body string }{
		{"a pointer made from an integer", "", "    const p: *u8 = @ptrFromInt(0x1000);\n    _ = p;\n"},
		{"a function named like a std.Build call", "fn createModule() void {}\n", ""},
		{"a write through a pointer", "", "    var x: u8 = 0;\n    const p = &x;\n    p.* = 1;\n"},
		{"a field of the builder assigned", "", "    b.root = b.root;\n"},
		{"another standard library", "", "    _ = b.addExecutable(.{ .name = \"z\", .root_module = m, .zig_lib_dir = b.path(\"zl\") });\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := ""
			debugOpen = func(_, w string) { why = w }
			defer func() { debugOpen = nil }()
			extract(t, "build.zig", worldBuild(c.top, c.body))
			if why == "" {
				t.Fatalf("read closed")
			}
			t.Log(why)
		})
	}
	// The world itself, with none of them, is closed.
	why := ""
	debugOpen = func(_, w string) { why = w }
	defer func() { debugOpen = nil }()
	extract(t, "build.zig", worldBuild("", ""))
	if why != "" {
		t.Fatalf("the plain world opens: %s", why)
	}
}

// tainted are the declarations of a build holding a graph value.
func taintedOf(t *testing.T, src string) []string {
	t.Helper()
	var got map[string]bool
	debugWorld = func(w *world) { got = w.taintedDecls() }
	defer func() { debugWorld = nil }()
	extract(t, "build.zig", src)
	var out []string
	for k := range got {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestWorldRules_TaintFlows(t *testing.T) {
	cases := []struct {
		name, top, body string
		want            string // a declaration that must hold the taint
	}{
		// A literal's field named like a graph-free field of the builder.
		{"a literal's .name", "", "    const t = .{ .name = b };\n    const x = t.name;\n    _ = x;\n", "x"},
		// An address is the value's.
		{"arithmetic on an address", "", "    const n = @intFromPtr(b) + 1;\n    _ = n;\n", "n"},
		// A container written through a parameter pointing to it.
		{"a container given by pointer", "fn stash(list: *std.ArrayList(*std.Build), gpa: std.mem.Allocator, x: *std.Build) void {\n    list.append(gpa, x) catch {};\n}\n",
			"    var list: std.ArrayList(*std.Build) = .empty;\n    stash(&list, b.allocator, b);\n", "list"},
		// A container written through a pointer to it.
		{"a container through `&`", "", "    var list: std.ArrayList(*std.Build) = .empty;\n    const p = &list;\n    p.append(b.allocator, b) catch {};\n", "list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := taintedOf(t, worldBuild(c.top, c.body))
			if !strings.Contains(" "+strings.Join(got, " ")+" ", " "+c.want+" ") {
				t.Errorf("%s holds no graph value; tainted: %v", c.want, got)
			}
		})
	}
}

// A method of the file's is one only on the file's types: a namespace of
// std held with a graph value is std's.
func TestWorldRules_MethodsOfTheFileOnItsTypesOnly(t *testing.T) {
	why := ""
	debugOpen = func(_, w string) { why = w }
	defer func() { debugOpen = nil }()
	extract(t, "build.zig", worldBuild("const S = struct {\n    fn dump(x: anytype) void {\n        _ = x;\n    }\n};\n",
		"    const pair = .{ .ns = std.debug, .b = b };\n    pair.ns.dump(pair.b);\n    S.dump(b);\n"))
	if why == "" {
		t.Fatal("read closed: std.debug's dump taken for the file's")
	}
	t.Log(why)
}
