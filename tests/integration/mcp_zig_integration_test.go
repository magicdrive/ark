package integration

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyZigFixture copies the Zig provider's graph fixture
// (internal/languages/zig/testdata/repo) into a new project directory.
func copyZigFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "internal", "languages", "zig", "testdata", "repo")
	proj := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

type zigEdge struct{ From, To, Kind, Confidence string }

func zigEdges(t *testing.T, c *rpcClient, tool, sym string, extra map[string]any) []zigEdge {
	t.Helper()
	args := map[string]any{"path": ".", "symbol": sym}
	for k, v := range extra {
		args[k] = v
	}
	text, isErr := c.tool(tool, args)
	if isErr {
		t.Fatalf("%s %s: %s", tool, sym, text)
	}
	var out struct{ Edges []zigEdge }
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s %s: %v\n%s", tool, sym, err, text)
	}
	return out.Edges
}

// The Zig graph through the real binary: support level, symbols, callers and
// callees by identity (through imports, re-exports, @This() and declared
// result types), a comptime choice left a Candidate, impact and context from
// those edges, and no edge into another language.
func TestMCPZig_Graph(t *testing.T) {
	bin := buildArk(t)
	proj := copyZigFixture(t)
	// A Python function of a shared name: no Zig reference may reach it.
	if err := os.WriteFile(filepath.Join(proj, "tools.py"), []byte("def run():\n    pass\n\ndef add(a, b):\n    run()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()

	support, _ := c.tool("get_language_support", map[string]any{})
	if !strings.Contains(support, `"language": "zig"`) || !strings.Contains(support, `"graph"`) {
		t.Fatalf("zig is not listed at graph:\n%s", support)
	}
	syms, _ := c.tool("get_symbols", map[string]any{"path": "src/engine.zig"})
	for _, want := range []string{`"name": "Engine"`, `"name": "init"`, `"name": "run"`, `"name": "step"`, `"name": "ticks"`, `"name": "Self"`} {
		if !strings.Contains(syms, want) {
			t.Errorf("get_symbols lacks %s:\n%s", want, syms)
		}
	}

	callers := map[string]string{}
	for _, e := range zigEdges(t, c, "get_callers", "Engine.run", nil) {
		callers[e.To] = e.Confidence
	}
	if len(callers) != 1 || callers["src/main.zig::main"] != "exact" {
		t.Errorf("callers of Engine.run: %v, want main (exact)", callers)
	}
	callees := map[string]string{}
	for _, e := range zigEdges(t, c, "get_callees", "main", map[string]any{"filePattern": "src/main.zig"}) {
		callees[e.To+" "+e.Kind] = e.Confidence
		if strings.Contains(e.To, "Backend::open") {
			t.Errorf("an edge to one alternative of a comptime choice: %+v", e)
		}
		if strings.Contains(e.To, "tools.py") || e.To == "run" || e.To == "add" {
			t.Errorf("an edge into Python: %+v", e)
		}
	}
	for _, want := range []string{
		"src/engine.zig::Engine::init calls", "src/engine.zig::Engine::run calls", "src/math.zig::add calls",
		"src/vec.zig::Vec::init calls", "src/vec.zig::Vec::length calls", "src/list.zig::List::()::count calls",
	} {
		if callees[want] != "exact" {
			t.Errorf("callee %s of main: %q (all: %v)", want, callees[want], callees)
		}
	}
	// Backend::open of either platform: a candidate caller, never an edge.
	open, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "Backend.open", "filePattern": "src/posix.zig"})
	if !strings.Contains(open, "unattributed") || strings.Contains(open, "src/main.zig::main\n") {
		t.Errorf("posix Backend.open's caller is not reported as unattributed only:\n%s", open)
	}
	// The two Backend.open are an ambiguity, never one of them.
	amb, isErr := c.tool("get_callers", map[string]any{"path": ".", "symbol": "Backend.open"})
	if !isErr || !strings.Contains(amb, "Ambiguous") {
		t.Errorf("two Backend.open must be ambiguous:\n%s", amb)
	}
	// Python's run has its own caller only.
	py, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "run", "filePattern": "tools.py"})
	if strings.Contains(py, "src/main.zig") {
		t.Errorf("a Zig caller of Python's run:\n%s", py)
	}

	impact, _ := c.tool("analyze_change_impact", map[string]any{"path": ".", "symbol": "Vec.length", "format": "json"})
	if !strings.Contains(impact, "src/main.zig::main") {
		t.Errorf("impact of Vec.length misses main:\n%s", impact)
	}
	ctx, _ := c.tool("get_context", map[string]any{"path": ".", "symbol": "Engine.run"})
	if !strings.Contains(ctx, "fn step(self: *Self) void") || !strings.Contains(ctx, "pub fn upper(") {
		t.Errorf("context of Engine.run lacks what it calls:\n%s", ctx)
	}
	refs, _ := c.tool("find_references", map[string]any{"path": ".", "name": "Engine"})
	if !strings.Contains(refs, "src/main.zig") {
		t.Errorf("find_references Engine:\n%s", refs)
	}
	found, _ := c.tool("search_code", map[string]any{"path": ".", "language": "zig", "kind": "method", "namePattern": "length"})
	if !strings.Contains(found, "length") {
		t.Errorf("search_code finds no Zig method length:\n%s", found)
	}
	diag, _ := c.tool("get_diagnostics", map[string]any{"path": "."})
	if strings.Contains(diag, ".zig") {
		t.Errorf("diagnostics on the valid fixture:\n%s", diag)
	}

	first, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "main", "filePattern": "src/main.zig"})
	for range 3 {
		if again, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "main", "filePattern": "src/main.zig"}); again != first {
			t.Fatal("get_callees output varies")
		}
	}

	// An edit is seen by the next request: the index is rebuilt for the
	// changed content, never served stale.
	engine := filepath.Join(proj, "src", "engine.zig")
	b, err := os.ReadFile(engine)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(b), "        self.step();\n", "        self.step();\n        self.extra();\n", 1)
	edited = strings.Replace(edited, "    fn step(self: *Self) void {", "    fn extra(self: *Self) void {\n        _ = self;\n    }\n\n    fn step(self: *Self) void {", 1)
	if err := os.WriteFile(engine, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	after := map[string]bool{}
	for _, e := range zigEdges(t, c, "get_callees", "Engine.run", nil) {
		after[e.To] = true
	}
	if !after["src/engine.zig::Engine::extra"] || !after["src/engine.zig::Engine::step"] {
		t.Errorf("callees of Engine.run after the edit: %v", after)
	}
}

// The persistent cache does not change the Zig graph: a cold build, a warm
// one from the entries it wrote, and one without a cache answer alike.
func TestMCPZig_CacheDoesNotChangeTheGraph(t *testing.T) {
	bin := buildArk(t)
	proj := copyZigFixture(t)
	home := t.TempDir()
	query := func(args ...string) string {
		c := startServer(t, proj, home, bin, append([]string{"mcp-server", "--root", "./"}, args...)...)
		c.handshake()
		var b strings.Builder
		for _, sym := range []string{"Engine.run", "List.count", "Vec.length", "Backend"} {
			text, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": sym})
			b.WriteString(text)
		}
		text, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "main", "filePattern": "src/main.zig"})
		b.WriteString(text)
		text, _ = c.tool("get_context", map[string]any{"path": ".", "symbol": "Engine.run"})
		b.WriteString(text)
		return b.String()
	}
	cold := query()
	warm := query()
	none := query("--no-cache")
	if cold != warm || cold != none {
		t.Errorf("the graph depends on the cache:\ncold:\n%s\nwarm:\n%s\nno cache:\n%s", cold, warm, none)
	}
}

// Named modules through the real binary: a module a build.zig binds is a
// Strong edge; a module no build Ark reads binds leaves every file's
// importers and every same-named function's callers unattributed, never
// complete; a module named like a file beside it is no Exact edge.
func TestMCPZig_NamedModulesNeverReadComplete(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	for name, src := range map[string]string{
		"build.zig":      "const std = @import(\"std\");\npub fn build(b: *std.Build) void {\n    const core = b.createModule(.{ .root_source_file = b.path(\"lib/core.zig\") });\n    const other = b.createModule(.{ .root_source_file = b.path(\"lib/other.zig\") });\n    const m = b.createModule(.{ .root_source_file = b.path(\"src/main.zig\") });\n    m.addImport(\"core\", core);\n    m.addImport(\"helper.zig\", other);\n}\n",
		"src/main.zig":   "const core = @import(\"core\");\nconst h = @import(\"helper.zig\");\npub fn main() void {\n    core.run();\n    h.which();\n}\n",
		"src/helper.zig": "pub fn which() void {}\n",
		"lib/core.zig":   "pub fn run() void {}\n",
		"lib/other.zig":  "pub fn which() void {}\n",
		"tools/cli.zig":  "const vsr = @import(\"vsr\");\npub fn cli() void {\n    vsr.fatal();\n}\n",
		"tools/vsr.zig":  "pub fn fatal() void {}\n",
	} {
		p := filepath.Join(proj, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()

	callers := func(sym, file string) (edges []zigEdge, unattributed int) {
		text, isErr := c.tool("get_callers", map[string]any{"path": ".", "symbol": sym, "filePattern": file})
		if isErr {
			t.Fatalf("get_callers %s: %s", sym, text)
		}
		var out struct {
			Edges        []zigEdge
			Unattributed int
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("get_callers %s: %v\n%s", sym, err, text)
		}
		return out.Edges, out.Unattributed
	}
	if e, _ := callers("run", "lib/core.zig"); len(e) != 1 || e[0].Confidence != "strong" {
		t.Errorf("core.run: callers %+v, want main (strong)", e)
	}
	// helper.zig is the module the table binds or (were it not) the file:
	// two Candidates, no edge to either.
	for _, file := range []string{"src/helper.zig", "lib/other.zig"} {
		if e, u := callers("which", file); len(e) != 0 || u != 1 {
			t.Errorf("which in %s: %d edges, %d unattributed; want 0, 1", file, len(e), u)
		}
	}
	// vsr: no build binds it. fatal's caller and every file's importers
	// stay open.
	if e, u := callers("fatal", "tools/vsr.zig"); len(e) != 0 || u != 1 {
		t.Errorf("fatal: %d edges, %d unattributed; want 0, 1", len(e), u)
	}
	for _, file := range []string{"tools/vsr.zig", "lib/core.zig", "src/helper.zig"} {
		if _, u := callers(file, ""); u == 0 {
			t.Errorf("importers of %s read complete while @import(\"vsr\") may be one", file)
		}
	}
	impact, _ := c.tool("analyze_change_impact", map[string]any{"path": ".", "symbol": "run", "filePattern": "lib/core.zig", "format": "json"})
	if !strings.Contains(impact, "src/main.zig::main") {
		t.Errorf("impact of core.run misses main:\n%s", impact)
	}
	ctx, _ := c.tool("get_context", map[string]any{"path": ".", "symbol": "main", "filePattern": "src/main.zig"})
	if !strings.Contains(ctx, "pub fn run()") {
		t.Errorf("context of main lacks core.run:\n%s", ctx)
	}
}
