package integration

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The index cache through real, different builds of Ark (internal/cache/
// build.go): a build reuses only the entries of its own executable — or, for
// releases, of releases — so a development build whose extraction differs
// but whose CacheVersions are the same never serves another build's
// extractions, while the same binary restarted reuses its own.
func TestMCPCache_EachBuildReusesOnlyItsOwnEntries(t *testing.T) {
	binA := buildArk(t)
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bins := t.TempDir()

	// B: the same sources but for the Python provider's extraction, which
	// renames every symbol — with the same CacheVersion, as a development
	// build changes extraction before anyone bumps it.
	binB := buildRenamingArk(t, module, filepath.Join(bins, "ark-b"))
	// Two releases of the same sources, displayed as different versions.
	release := "-X github.com/magicdrive/ark/internal/cache.release=true"
	binR1 := goBuild(t, module, filepath.Join(bins, "ark-r1"), "-ldflags", release+" -X main.version=v9.0.0")
	binR2 := goBuild(t, module, filepath.Join(bins, "ark-r2"), "-ldflags", release+" -X main.version=v9.0.1")

	root := t.TempDir()
	for p, c := range map[string]string{
		"app.py":  "def greet():\n    return 1\n\n\ndef main():\n    greet()\n",
		"main.go": "package main\n\nfunc helper() {}\n\nfunc main() { helper() }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// An entry of the layout before build scopes: written by a build no one
	// can name any more, it is removed, never reused.
	cacheDir := filepath.Join(root, ".ark", "index")
	legacy := filepath.Join(cacheDir, strings.Repeat("ab", 32)+".json")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	a1 := cacheSession(t, binA, root)
	if a1.release || a1.reused != 0 || a1.extracted != 2 {
		t.Fatalf("A, first run: %+v", a1)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("the old layout's entry survived: %v", err)
	}
	if !strings.Contains(a1.greet, `"to": "main"`) || !strings.Contains(a1.greetB, "not found") {
		t.Fatalf("A's graph: greet %s / greet_B %s", a1.greet, a1.greetB)
	}
	a2 := cacheSession(t, binA, root)
	if a2.scope != a1.scope || a2.reused != 2 || a2.extracted != 0 {
		t.Fatalf("A restarted: %+v, want all of %s reused", a2, a1.scope)
	}
	if a2.greet != a1.greet || a2.greetB != a1.greetB {
		t.Errorf("A from the cache answers differently:\n%s\n%s", a1.greet, a2.greet)
	}

	b1 := cacheSession(t, binB, root)
	if b1.scope == a1.scope || b1.release || b1.reused != 0 || b1.extracted != 2 {
		t.Fatalf("B after A: %+v, want its own scope and every file extracted", b1)
	}
	if strings.Contains(b1.greetB, "not found") {
		t.Fatalf("B answers with A's extraction: greet_B %s", b1.greetB)
	}
	b2 := cacheSession(t, binB, root)
	if b2.scope != b1.scope || b2.reused != 2 || b2.greetB != b1.greetB {
		t.Fatalf("B restarted: %+v", b2)
	}
	if a3 := cacheSession(t, binA, root); a3.scope != a1.scope || a3.reused != 2 || a3.greet != a1.greet {
		t.Fatalf("A after B: %+v", a3)
	}

	r1 := cacheSession(t, binR1, root)
	if !r1.release || r1.reused != 0 || r1.extracted != 2 {
		t.Fatalf("first release: %+v", r1)
	}
	r2 := cacheSession(t, binR2, root)
	if !r2.release || r2.scope != r1.scope || r2.reused != 2 || r2.greet != a1.greet {
		t.Fatalf("next release: %+v, want the release entries reused", r2)
	}

	// Another root has a cache of its own: nothing of root is reused.
	other := t.TempDir()
	for _, p := range []string{"app.py", "main.go"} {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, p), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if o := cacheSession(t, binA, other); o.reused != 0 || o.extracted != 2 {
		t.Fatalf("A in another root: %+v", o)
	}
	if a4 := cacheSession(t, binA, root); a4.reused != 2 {
		t.Fatalf("A back in the first root: %+v", a4)
	}

	// --no-cache opens no persistent cache at all.
	bare := t.TempDir()
	c := startServer(t, bare, t.TempDir(), binA, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	if text, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "greet"}); !strings.Contains(text, "not found") {
		t.Errorf("an empty root has greet: %s", text)
	}
	_ = c.in.Close()
	_ = c.cmd.Wait()
	if log := c.stderr.String(); cacheScopeLog.MatchString(log) || cacheCountLog.MatchString(log) {
		t.Errorf("--no-cache used the cache:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(bare, ".ark")); !os.IsNotExist(err) {
		t.Errorf("--no-cache created .ark: %v", err)
	}
}

// Every graph and context answer is the same from a cold index and from one
// restored from the cache (a restarted server), across languages: the Python
// graph fixture, PHP relations and the Go baseline.
func TestMCPCache_RestoredIndexAnswersAsCold(t *testing.T) {
	bin := buildArk(t)
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for dst, src := range map[string]string{
		"py":  filepath.Join("internal", "languages", "python", "testdata", "repo"),
		"php": filepath.Join("internal", "languages", "php", "testdata", "relations"),
		"go":  filepath.Join("internal", "golden", "testdata", "go"),
	} {
		copyTree(t, filepath.Join(module, src), filepath.Join(root, dst))
	}
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"get_repository_map", map[string]any{"path": "."}},
		{"get_callers", map[string]any{"path": ".", "symbol": "User"}},
		{"get_callees", map[string]any{"path": ".", "symbol": "UserService.create"}},
		{"get_relations", map[string]any{"path": ".", "symbol": "Child"}},
		{"get_context", map[string]any{"path": ".", "symbol": "UserService.create", "format": "json"}},
		{"get_context", map[string]any{"path": ".", "symbol": "Child"}},
		{"search_context", map[string]any{"path": ".", "query": "create user store"}},
		{"analyze_change_impact", map[string]any{"path": ".", "symbol": "User"}},
		{"search_code", map[string]any{"path": ".", "kind": "method"}},
		{"get_diagnostics", map[string]any{"path": "."}},
	}
	session := func() ([]string, string) {
		c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./")
		c.handshake()
		var out []string
		for _, call := range calls {
			text, _ := c.tool(call.tool, call.args)
			out = append(out, text)
		}
		_ = c.in.Close()
		_ = c.cmd.Wait()
		return out, c.stderr.String()
	}
	cold, coldLog := session()
	warm, warmLog := session()
	if n := cacheCountLog.FindStringSubmatch(coldLog); n == nil || n[1] != "0" {
		t.Fatalf("the first server did not index cold:\n%s", coldLog)
	}
	if n := cacheCountLog.FindStringSubmatch(warmLog); n == nil || n[1] == "0" || n[2] != "0" {
		t.Fatalf("the restarted server did not restore every file from the cache:\n%s", warmLog)
	}
	for i, call := range calls {
		if cold[i] != warm[i] {
			t.Errorf("%s %v: restored index answers differently\ncold:\n%s\nwarm:\n%s", call.tool, call.args, cold[i], warm[i])
		}
		if strings.Contains(cold[i], "not found") {
			t.Errorf("%s %v: %s", call.tool, call.args, cold[i])
		}
	}
}

// Servers of two builds — and two servers of one build — running on one
// root at the same time, indexing at the same time, each answer with their
// own build's extraction and leave each build its own entries.
func TestMCPCache_ConcurrentServersOnOneRoot(t *testing.T) {
	binA := buildArk(t)
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binB := buildRenamingArk(t, module, filepath.Join(t.TempDir(), "ark-b"))
	root := t.TempDir()
	src := "def greet():\n    return 1\n\n\ndef main():\n    greet()\n"
	for i := 0; i < 8; i++ {
		if err := os.WriteFile(filepath.Join(root, "m"+strconv.Itoa(i)+".py"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	servers := []*rpcClient{}
	for _, bin := range []string{binA, binB, binA} {
		c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./")
		c.handshake()
		servers = append(servers, c)
	}
	// Every server is asked before any answer is read: their builds overlap.
	for i, c := range servers {
		req := `{"jsonrpc":"2.0","id":` + strconv.Itoa(100+i) + `,"method":"tools/call","params":{"name":"search_code","arguments":{"path":".","kind":"function","namePattern":"greet"}}}` + "\n"
		if _, err := c.in.Write([]byte(req)); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range servers {
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			t.Fatalf("server %d: %v\n%s", i, err, c.stderr)
		}
		if !strings.Contains(string(line), "Found 8 symbol") {
			t.Errorf("server %d: %s", i, line)
		}
	}
	greetB := func(c *rpcClient) string {
		text, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "greet_B"})
		return text
	}
	if text := greetB(servers[0]); !strings.Contains(text, "not found") {
		t.Errorf("A answers with B's extraction: %s", text)
	}
	if text := greetB(servers[1]); strings.Contains(text, "not found") {
		t.Errorf("B answers with A's extraction: %s", text)
	}
	for _, c := range servers {
		_ = c.in.Close()
		_ = c.cmd.Wait()
	}
	for name, bin := range map[string]string{"A": binA, "B": binB} {
		if r := cacheSession(t, bin, root); r.reused != 8 || r.extracted != 0 {
			t.Errorf("%s restarted after the concurrent servers: %+v, want all 8 files reused", name, r)
		}
	}
	// No temporary file of an interrupted or racing write is left.
	err = filepath.WalkDir(filepath.Join(root, ".ark"), func(path string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".tmp") {
			t.Errorf("left behind: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// buildRenamingArk builds ark from a copy of module whose Python provider
// renames every symbol it extracts (name + "_B") under the same
// CacheVersion: a development build whose extraction differs from the
// module's while every cache key it makes is the same.
func buildRenamingArk(t *testing.T, module, out string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	copyModule(t, module, src)
	provider := filepath.Join(src, "internal", "languages", "python", "provider.go")
	patchFile(t, provider, "\te.moduleBindings()\n",
		"\te.moduleBindings()\n\tfor i := range e.symbols {\n\t\te.symbols[i].Name += \"_B\"\n\t}\n")
	return goBuild(t, src, out)
}

type cacheRun struct {
	scope             string
	release           bool
	reused, extracted int
	greet, greetB     string
}

var (
	cacheScopeLog = regexp.MustCompile(`ark: index cache (\S+) \((release|development) build`)
	cacheCountLog = regexp.MustCompile(`ark: indexed \S+: (\d+) files from the cache, (\d+) extracted`)
)

// cacheSession runs one server over root, asks for greet's and greet_B's
// callers, stops it, and reads from its log the scope it used and what its
// index took from the cache.
func cacheSession(t *testing.T, bin, root string) cacheRun {
	t.Helper()
	c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./")
	c.handshake()
	var r cacheRun
	r.greet, _ = c.tool("get_callers", map[string]any{"path": ".", "symbol": "greet"})
	r.greetB, _ = c.tool("get_callers", map[string]any{"path": ".", "symbol": "greet_B"})
	_ = c.in.Close()
	_ = c.cmd.Wait()
	log := c.stderr.String()
	m := cacheScopeLog.FindStringSubmatch(log)
	n := cacheCountLog.FindAllStringSubmatch(log, -1)
	if m == nil || len(n) != 1 {
		t.Fatalf("%s: no cache scope or not one index build in the log:\n%s", filepath.Base(bin), log)
	}
	r.scope, r.release = filepath.Base(m[1]), m[2] == "release"
	r.reused, _ = strconv.Atoi(n[0][1])
	r.extracted, _ = strconv.Atoi(n[0][2])
	return r
}

// goBuild builds the ark main package in dir.
func goBuild(t *testing.T, dir, out string, flags ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", append(append([]string{"build"}, flags...), "-o", out, ".")...)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build in %s: %v\n%s", dir, err, b)
	}
	return out
}

// copyModule copies what building ark reads from the module at src: every
// file but tests, test data and the version control and cache directories.
func copyModule(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".ark", ".agent-work", "testdata", "node_modules", "dist":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(filepath.Join(dst, rel))
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// copyTree copies the regular files below src to dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func patchFile(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), old) != 1 {
		t.Fatalf("%s: %q does not occur exactly once", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}
