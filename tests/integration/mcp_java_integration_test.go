package integration

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyJavaFixture copies the Java provider's graph fixture into a new
// project directory and returns it.
func copyJavaFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "internal", "languages", "java", "testdata", "repo")
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

type javaEdge struct{ From, To, Kind, Confidence, Evidence string }

func javaEdges(t *testing.T, c *rpcClient, tool, sym string, extra map[string]any) []javaEdge {
	t.Helper()
	args := map[string]any{"path": ".", "symbol": sym}
	for k, v := range extra {
		args[k] = v
	}
	text, isErr := c.tool(tool, args)
	if isErr {
		t.Fatalf("%s %s: %s", tool, sym, text)
	}
	var out struct{ Edges []javaEdge }
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s %s: %v\n%s", tool, sym, err, text)
	}
	return out.Edges
}

// The Java graph through the real binary: support level, symbols, callers
// and callees by the language's lookup, overloads never picked by name,
// impact and context from those edges, and no edge into another language.
func TestMCPJava_Graph(t *testing.T) {
	bin := buildArk(t)
	proj := copyJavaFixture(t)
	// A Python function of a shared name: no Java reference may reach it.
	if err := os.WriteFile(filepath.Join(proj, "tools.py"), []byte("def touch():\n    pass\n\ndef log(x):\n    touch()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()

	support, _ := c.tool("get_language_support", map[string]any{})
	if !strings.Contains(support, `"language": "java"`) || !strings.Contains(support, `"graph"`) {
		t.Fatalf("java is not listed at graph:\n%s", support)
	}
	syms, _ := c.tool("get_symbols", map[string]any{"path": "src/main/java/com/acme/model/User.java"})
	for _, want := range []string{`"name": "User"`, `"name": "befriend"`, `"name": "Badge"`, `"name": "\u003cinit\u003e"`} {
		if !strings.Contains(syms, want) {
			t.Errorf("get_symbols lacks %s:\n%s", want, syms)
		}
	}

	callers := map[string]string{}
	for _, e := range javaEdges(t, c, "get_callers", "Entity.touch", nil) {
		callers[e.To] = e.Confidence
	}
	want := map[string]string{
		"com.acme.model.User.befriend":                 "exact",
		"com.acme.service.UserService.touchAll":        "strong",
		"com.acme.service.UserService.<anonymous>.run": "strong",
	}
	for to, conf := range want {
		if callers[to] != conf {
			t.Errorf("caller %s of Entity.touch: %q, want %q (all: %v)", to, callers[to], conf, callers)
		}
	}
	if len(callers) != len(want) {
		t.Errorf("Entity.touch has callers %v, want %v", callers, want)
	}

	// The overloads of log: the MCP lists both, never picks one.
	text, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "UserService.log"})
	if !strings.Contains(text, "Ambiguous") || strings.Count(text, "symbolId=") != 2 {
		t.Errorf("log's overloads are not an ambiguity:\n%s", text)
	}
	// register calls each overload once, told apart by its argument.
	logs := 0
	for _, e := range javaEdges(t, c, "get_callees", "UserService.register", nil) {
		if e.To == "com.acme.service.UserService.log" {
			logs++
			if e.Confidence != "exact" {
				t.Errorf("register -> log: %+v", e)
			}
		}
		if strings.HasSuffix(e.To, "grant") {
			t.Errorf("register -> grant is an edge, though its overload is not proven: %+v", e)
		}
		if strings.Contains(e.To, "tools.py") || e.To == "touch" || e.To == "log" {
			t.Errorf("an edge into Python: %+v", e)
		}
	}
	if logs != 2 {
		t.Errorf("register calls log %d times as edges, want 2", logs)
	}
	// Python's touch has its own caller only.
	py, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "touch", "filePattern": "tools.py"})
	if strings.Contains(py, "com.acme") {
		t.Errorf("a Java caller of Python's touch:\n%s", py)
	}

	impact, _ := c.tool("analyze_change_impact", map[string]any{"path": ".", "symbol": "Entity.touch", "format": "json"})
	if !strings.Contains(impact, "befriend") {
		t.Errorf("impact of Entity.touch misses befriend:\n%s", impact)
	}
	ctx, _ := c.tool("get_context", map[string]any{"path": ".", "symbol": "User.befriend"})
	if !strings.Contains(ctx, "public void touch()") {
		t.Errorf("context of befriend lacks the touch it calls:\n%s", ctx)
	}
	found, _ := c.tool("search_code", map[string]any{"path": ".", "language": "java", "kind": "method", "namePattern": "register"})
	if !strings.Contains(found, "register") {
		t.Errorf("search_code finds no Java method register:\n%s", found)
	}

	first, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "UserService.register"})
	for range 3 {
		if again, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "UserService.register"}); again != first {
			t.Fatal("get_callees output varies")
		}
	}
}

// The persistent cache does not change the Java graph: a cold build, a warm
// one from the entries it wrote, and one without a cache answer alike.
func TestMCPJava_CacheDoesNotChangeTheGraph(t *testing.T) {
	bin := buildArk(t)
	proj := copyJavaFixture(t)
	home := t.TempDir()
	query := func(args ...string) string {
		c := startServer(t, proj, home, bin, append([]string{"mcp-server", "--root", "./"}, args...)...)
		c.handshake()
		var b strings.Builder
		for _, sym := range []string{"UserService.register", "User.befriend", "Badge.show", "UserServiceTest.registers"} {
			text, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": sym})
			b.WriteString(text)
		}
		text, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "Entity.touch"})
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
