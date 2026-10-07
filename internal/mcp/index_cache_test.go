package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/index"
)

// Repository index reuse: one build serves every request while the sources are
// unchanged; any change, a different directory or a different configuration
// means another index; failures are never cached; concurrent requests share a
// build without serializing unrelated ones.

func reuseRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/Policy.php":     "<?php\nnamespace App;\n\nclass Policy\n{\n    public function allowed() { return true; }\n}\n",
		"app/Controller.php": "<?php\nnamespace App;\n\nclass Controller\n{\n    public function run(Policy $p) { return $p->allowed(); }\n}\n",
		"app/notes.txt":      "not a source file\n",
	})
	return root
}

func callersOf(t *testing.T, h *ToolsHandler, path, symbol string) string {
	t.Helper()
	text, isErr := callText(t, h, "get_callers", map[string]interface{}{"path": path, "symbol": symbol})
	if isErr {
		return "ERROR: " + text
	}
	return text
}

func cacheStats(h *ToolsHandler) (builds, reuses, rebuilds uint64, entries int) {
	if h.indexes == nil {
		return 0, 0, 0, 0
	}
	return h.indexes.stats()
}

func TestIndexReuse_SameStateBuildsOnce(t *testing.T) {
	root := reuseRepo(t)
	h := NewToolsHandler(root, nil)
	first := callersOf(t, h, ".", "Policy.allowed")
	for _, tool := range []string{"get_callees", "get_relations", "get_context", "analyze_change_impact", "get_repository_map"} {
		callText(t, h, tool, map[string]interface{}{"path": ".", "symbol": "Controller.run"})
	}
	if again := callersOf(t, h, ".", "Policy.allowed"); again != first {
		t.Errorf("warm result differs from cold:\n%s\n---\n%s", first, again)
	}
	builds, reuses, _, _ := cacheStats(h)
	if builds != 1 || reuses != 6 {
		t.Errorf("builds=%d reuses=%d, want 1 build serving 6 more requests", builds, reuses)
	}
}

func TestIndexReuse_ChangesRebuild(t *testing.T) {
	type step struct {
		name   string
		change func(t *testing.T, root string)
		want   string // substring of get_callers(Policy.allowed) after the change
		reject string // substring that must be gone
	}
	steps := []step{
		{"content edit of the same size", func(t *testing.T, root string) {
			p := filepath.Join(root, "app/Controller.php")
			b, _ := os.ReadFile(p)
			// Same length, same mtime second: only content tells.
			info, _ := os.Stat(p)
			if err := os.WriteFile(p, []byte(strings.Replace(string(b), "public function run(", "public function ran(", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		}, `App\\Controller.ran`, `App\\Controller.run`},
		{"file added", func(t *testing.T, root string) {
			writeTree(t, root, map[string]string{"app/Extra.php": "<?php\nnamespace App;\n\nclass Extra\n{\n    public function go(Policy $p) { return $p->allowed(); }\n}\n"})
		}, `App\\Extra.go`, ""},
		{"file renamed", func(t *testing.T, root string) {
			if err := os.Rename(filepath.Join(root, "app/Extra.php"), filepath.Join(root, "app/Extra2.php")); err != nil {
				t.Fatal(err)
			}
		}, `App\\Extra.go`, ""}, // content unchanged, path changed: still a caller (path checked below)
		{"file removed", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "app/Extra2.php")); err != nil {
				t.Fatal(err)
			}
		}, `App\\Controller.ran`, `App\\Extra.go`},
		{"directory added", func(t *testing.T, root string) {
			writeTree(t, root, map[string]string{"lib/Use.php": "<?php\nnamespace App;\n\nclass UseIt\n{\n    public function x(Policy $p) { return $p->allowed(); }\n}\n"})
		}, `App\\UseIt.x`, ""},
		{"directory removed", func(t *testing.T, root string) {
			if err := os.RemoveAll(filepath.Join(root, "lib")); err != nil {
				t.Fatal(err)
			}
		}, `App\\Controller.ran`, `App\\UseIt.x`},
	}
	root := reuseRepo(t)
	h := NewToolsHandler(root, nil)
	callersOf(t, h, ".", "Policy.allowed")
	for i, s := range steps {
		s.change(t, root)
		got := callersOf(t, h, ".", "Policy.allowed")
		if !strings.Contains(got, s.want) || (s.reject != "" && strings.Contains(got, s.reject)) {
			t.Fatalf("%s: stale result:\n%s", s.name, got)
		}
		if s.name == "file renamed" {
			rel, _ := callText(t, h, "get_relations", map[string]interface{}{"path": ".", "symbol": "Policy.allowed"})
			if !strings.Contains(rel, "app/Extra2.php") || strings.Contains(rel, "app/Extra.php") {
				t.Fatalf("rename: stale file path:\n%s", rel)
			}
		}
		builds, _, rebuilds, _ := cacheStats(h)
		if builds != uint64(i+2) || rebuilds != uint64(i+1) {
			t.Errorf("%s: builds=%d rebuilds=%d, want %d and %d", s.name, builds, rebuilds, i+2, i+1)
		}
		// And reused again while unchanged.
		if again := callersOf(t, h, ".", "Policy.allowed"); again != got {
			t.Errorf("%s: warm result differs", s.name)
		}
	}
	// A non-source file never forces a rebuild.
	before, _, _, _ := cacheStats(h)
	writeTree(t, root, map[string]string{"app/notes.txt": "changed\n"})
	callersOf(t, h, ".", "Policy.allowed")
	if after, _, _, _ := cacheStats(h); after != before {
		t.Errorf("a non-source file change triggered a rebuild")
	}
}

// The index does not read ignore files, so changing one keeps the index; the
// file tools, which do, see the change at once.
func TestIndexReuse_GitignoreChange(t *testing.T) {
	root := reuseRepo(t)
	h := NewToolsHandler(root, nil)
	callersOf(t, h, ".", "Policy.allowed")
	if text, _ := callText(t, h, "list_files", map[string]interface{}{"path": "app"}); !strings.Contains(text, "Controller.php") {
		t.Fatalf("list_files: %q", text)
	}
	writeTree(t, root, map[string]string{".gitignore": "app/Controller.php\n"})
	if text, _ := callText(t, h, "list_files", map[string]interface{}{"path": "app"}); strings.Contains(text, "Controller.php") {
		t.Errorf("list_files ignores the new .gitignore: %q", text)
	}
	got := callersOf(t, h, ".", "Policy.allowed")
	if !strings.Contains(got, `App\\Controller.run`) {
		t.Errorf("index changed by .gitignore:\n%s", got)
	}
	if builds, _, _, _ := cacheStats(h); builds != 1 {
		t.Errorf("builds = %d, want 1 (ignore files are not index inputs)", builds)
	}
}

func TestIndexReuse_RootIdentity(t *testing.T) {
	root := reuseRepo(t)
	if err := os.Symlink(filepath.Join(root, "app"), filepath.Join(root, "applink")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"x/Secret.php": "<?php\nclass Secret { public function allowed() {} }\n"})
	if err := os.Symlink(filepath.Join(outside, "x"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(root, nil)
	want := callersOf(t, h, "app", "Policy.allowed")
	for _, alias := range []string{"app/", "./app", "app/../app", filepath.Join(root, "app"), "applink"} {
		if got := callersOf(t, h, alias, "Policy.allowed"); got != want {
			t.Errorf("%s: %s, want %s", alias, got, want)
		}
	}
	if builds, _, _, _ := cacheStats(h); builds != 1 {
		t.Errorf("aliases of one directory built %d indexes, want 1", builds)
	}
	// A directory with the same base name elsewhere is another index.
	writeTree(t, root, map[string]string{"nested/app/Other.php": "<?php\nnamespace App;\n\nclass Policy\n{\n    public function denied() {}\n}\n"})
	if got := callersOf(t, h, "nested/app", "Policy.allowed"); !strings.Contains(got, "not found") {
		t.Errorf("nested/app answered from app: %s", got)
	}
	if builds, _, _, _ := cacheStats(h); builds != 2 {
		t.Errorf("builds = %d, want 2 (app and nested/app)", builds)
	}
	// A different directory is a different index.
	if got := callersOf(t, h, ".", "Policy.allowed"); !strings.Contains(got, `App\\Controller.run`) {
		t.Errorf("root: %s", got)
	}
	if builds, _, _, _ := cacheStats(h); builds != 3 {
		t.Errorf("builds = %d, want 3 (app, nested/app and root)", builds)
	}
	// A symlink leaving the server root is refused, never indexed.
	if got := callersOf(t, h, "escape", "Secret.allowed"); !strings.Contains(got, "outside the server root") {
		t.Errorf("escaping symlink: %s", got)
	}
	// Another repository never shares an index with this one.
	other := reuseRepo(t)
	writeTree(t, other, map[string]string{"app/Policy.php": "<?php\nnamespace App;\n\nclass Policy\n{\n    public function denied() {}\n}\n"})
	h2 := NewToolsHandler(other, nil)
	if got := callersOf(t, h2, ".", "Policy.allowed"); !strings.Contains(got, "not found") {
		t.Errorf("other repository answered from this one: %s", got)
	}
}

// --- indexCache unit tests with controlled builds ---

func testIndex(t *testing.T, fingerprint string) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = fingerprint
	return idx
}

func controlledCache(t *testing.T, build func(ctx context.Context, root string) (*index.RepositoryIndex, error)) *indexCache {
	t.Helper()
	c := newIndexCache(nil, build)
	empty := testIndex(t, "").Fingerprint()
	c.fingerprint = func(context.Context, string) (string, error) { return empty, nil }
	return c
}

func TestIndexCache_SingleFlight(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	idx := testIndex(t, "")
	c := controlledCache(t, func(context.Context, string) (*index.RepositoryIndex, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return idx, nil
	})
	var wg sync.WaitGroup
	results := make([]*index.RepositoryIndex, 16)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = c.get(context.Background(), "/repo")
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls != 1 {
		t.Errorf("same key built %d times, want 1", calls)
	}
	for i, r := range results {
		if r != idx {
			t.Errorf("waiter %d got a different index", i)
		}
	}
}

func TestIndexCache_DifferentKeysBuildInParallel(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	idx := testIndex(t, "")
	c := controlledCache(t, func(_ context.Context, root string) (*index.RepositoryIndex, error) {
		started <- root
		<-release
		return idx, nil
	})
	go c.get(context.Background(), "/a")
	go c.get(context.Background(), "/b")
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("builds for different keys are serialized")
		}
	}
	close(release)
	// A different configuration is a different key too.
	if (indexKey{root: "/a", config: "x"}) == (indexKey{root: "/a", config: "y"}) {
		t.Fatal("configuration is not part of the key")
	}
	c2 := newIndexCache(nil, nil)
	if c2.config == newIndexCache(defaultProviders(), nil).config {
		t.Error("the provider configuration does not distinguish caches")
	}
}

func TestIndexCache_FailuresAreNotCached(t *testing.T) {
	idx := testIndex(t, "")
	var calls int32
	mode := "error"
	c := controlledCache(t, func(context.Context, string) (*index.RepositoryIndex, error) {
		atomic.AddInt32(&calls, 1)
		switch mode {
		case "error":
			return nil, errors.New("boom")
		case "panic":
			panic("kaboom")
		}
		return idx, nil
	})
	if _, err := c.get(context.Background(), "/r"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error not reported: %v", err)
	}
	mode = "panic"
	if _, err := c.get(context.Background(), "/r"); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("panic not reported: %v", err)
	}
	mode = "ok"
	got, err := c.get(context.Background(), "/r")
	if err != nil || got != idx {
		t.Fatalf("after failures: %v %v", got, err)
	}
	if calls != 3 {
		t.Errorf("builds = %d, want 3 (failed builds are not cached)", calls)
	}
	if _, _, _, n := c.stats(); n != 1 {
		t.Errorf("entries = %d, want 1", n)
	}
}

func TestIndexCache_CancelledWaiterDoesNotBreakTheBuild(t *testing.T) {
	release := make(chan struct{})
	idx := testIndex(t, "")
	c := controlledCache(t, func(ctx context.Context, _ string) (*index.RepositoryIndex, error) {
		<-release
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return idx, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := c.get(ctx, "/r"); errc <- err }()
	done := make(chan *index.RepositoryIndex, 1)
	go func() { r, _ := c.get(context.Background(), "/r"); done <- r }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled waiter: %v", err)
	}
	close(release)
	if r := <-done; r != idx {
		t.Error("the other waiter lost the shared build")
	}
}

func TestIndexCache_BoundedLifecycle(t *testing.T) {
	idx := testIndex(t, "")
	block := make(chan struct{})
	c := controlledCache(t, func(_ context.Context, root string) (*index.RepositoryIndex, error) {
		if root == "/inflight" {
			<-block
		}
		return idx, nil
	})
	go c.get(context.Background(), "/inflight")
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < maxIndexes+3; i++ {
		if _, err := c.get(context.Background(), fmt.Sprintf("/repo%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	completed, inflight := 0, false
	for k, e := range c.entries {
		select {
		case <-e.done:
			completed++
		default:
			inflight = inflight || k.root == "/inflight"
		}
	}
	_, recent := c.entries[indexKey{root: fmt.Sprintf("/repo%d", maxIndexes+2), config: c.config}]
	c.mu.Unlock()
	if completed > maxIndexes {
		t.Errorf("%d completed indexes retained, bound is %d", completed, maxIndexes)
	}
	if !inflight {
		t.Error("an in-flight build was evicted")
	}
	if !recent {
		t.Error("the most recently used index was evicted")
	}
	close(block)
}

// Output is the same whether it comes from a cold build, a warm hit, a rebuild
// after a change was reverted, or many concurrent requests.
func TestIndexReuse_Deterministic(t *testing.T) {
	root := reuseRepo(t)
	calls := []struct {
		tool string
		args map[string]interface{}
	}{
		{"get_callers", map[string]interface{}{"path": ".", "symbol": "Policy.allowed"}},
		{"get_relations", map[string]interface{}{"path": ".", "symbol": "Policy.allowed"}},
		{"get_context", map[string]interface{}{"path": ".", "symbol": "Controller.run"}},
		{"get_repository_map", map[string]interface{}{"path": "."}},
	}
	render := func(h *ToolsHandler) string {
		var sb strings.Builder
		for _, c := range calls {
			text, _ := callText(t, h, c.tool, c.args)
			sb.WriteString(text + "\n")
		}
		return sb.String()
	}
	cold := render(NewToolsHandler(root, nil))
	h := NewToolsHandler(root, nil)
	render(h)
	if warm := render(h); warm != cold {
		t.Fatalf("warm differs from cold")
	}
	p := filepath.Join(root, "app/Policy.php")
	orig, _ := os.ReadFile(p)
	writeTree(t, root, map[string]string{"app/Policy.php": string(orig) + "\n// edit\n"})
	render(h)
	writeTree(t, root, map[string]string{"app/Policy.php": string(orig)})
	if reverted := render(h); reverted != cold {
		t.Fatalf("after invalidation differs from cold")
	}
	h2 := NewToolsHandler(root, nil)
	var wg sync.WaitGroup
	outs := make([]string, 8)
	for i := range outs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); outs[i] = render(h2) }(i)
	}
	wg.Wait()
	for i, o := range outs {
		if o != cold {
			t.Fatalf("concurrent request %d differs from cold", i)
		}
	}
	if builds, _, _, _ := cacheStats(h2); builds != 1 {
		t.Errorf("concurrent same-key requests built %d indexes, want 1", builds)
	}
}
