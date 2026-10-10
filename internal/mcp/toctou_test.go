package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// Phase 3-B: the tools that read files a client names, and the source text
// index-based tools return, decide and read in one operation on the
// request's pinned tree. These tests change the tree in the window between
// the path gate's decision (or the index's) and the read (beforeRead), as a
// concurrent writer would: the content decided is the content returned, or
// nothing.

func needSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink("x", filepath.Join(dir, "probe")); err != nil {
		if os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
			t.Fatalf("ARK_REQUIRE_SYMLINKS: %v", err)
		}
		t.Skipf("SKIP: symlinks unavailable on %s: %v", runtime.GOOS, err)
	}
}

func handlerFor(t *testing.T, root string, flags ...string) *ToolsHandler {
	t.Helper()
	_, serveOpt, err := commandline.ServerOptParse("test", append([]string{"--root", root}, flags...))
	if err != nil {
		t.Fatal(err)
	}
	return NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// replaceWithLink puts a symlink to target at path, in place of what was
// there.
func replaceWithLink(t *testing.T, target, path string) {
	t.Helper()
	tmp := path + ".swap"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		t.Fatal(err)
	}
	mutate(t, "replace "+filepath.Base(path), os.Rename(tmp, path))
}

// mutate fails the test on an error of a step of an attack — unless the
// system refuses the step itself (Windows: renaming over a directory or a
// file in use), where the attack is not possible and the test skips, saying
// so.
func mutate(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		t.Skipf("SKIP: %s: %v (attack not possible here)", what, err)
	}
	t.Fatalf("%s: %v", what, err)
}

// onRead runs fn once, the first time the request's snapshot is about to
// read rel.
func onRead(t *testing.T, rel string, fn func()) {
	t.Helper()
	var once sync.Once
	beforeRead = func(r string) {
		if r == rel {
			once.Do(fn)
		}
	}
	t.Cleanup(func() { beforeRead = nil })
}

// fileCalls are the calls that read path directly: every tool and resource
// that names a file.
func fileCalls(path string) []struct {
	name string
	args map[string]interface{}
} {
	return []struct {
		name string
		args map[string]interface{}
	}{
		{"get_file_content", map[string]interface{}{"path": path, "maskSecrets": false}},
		{"get_files_arklite", map[string]interface{}{"paths": []interface{}{path}, "maskSecrets": false}},
		{"get_symbols", map[string]interface{}{"path": path}},
		{"get_symbol", map[string]interface{}{"path": path, "name": "Secret"}},
		{"find_references", map[string]interface{}{"path": path, "name": "Secret"}},
		{"get_file_info", map[string]interface{}{"path": path}},
		{"file://", nil},
	}
}

// callFile runs one of fileCalls against h, the way a client does: a tool
// call, or a resource read.
func callFile(t *testing.T, h *ToolsHandler, name string, args map[string]interface{}, path string) string {
	t.Helper()
	if name == "file://" {
		res, err := NewResourcesHandler(h.rootDir, h.opt).ReadResource("file://" + path)
		if err != nil {
			return "error: " + err.Error()
		}
		return res.Contents[0].Text
	}
	res, err := h.CallTool(name, args)
	if err != nil {
		return "error: " + err.Error()
	}
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

const secretGo = "package p\n\nfunc Secret() string { return \"%s\" }\n"

func secretSize(marker string) int { return len(fmt.Sprintf(secretGo, marker)) }

// leaked reports whether a fileCalls output shows the secret file: its
// marker, or what each tool derives from it — its symbol, a reference in
// it, its size (none of which the public file has).
func leaked(tool, out, marker string, size int) bool {
	switch {
	case strings.Contains(out, marker):
		return true
	case tool == "get_symbols":
		return strings.Contains(out, "Secret")
	case tool == "get_symbol":
		return strings.Contains(out, "startLine")
	case tool == "find_references":
		return strings.Contains(out, "\"file\"")
	case tool == "get_file_info":
		return strings.Contains(out, fmt.Sprintf("\"size\": %d,", size))
	}
	return false
}

// B1: the root symlink is retargeted from A to B — whose .arkignore excludes
// the file — between the gate's decision and the read. B's file is never
// read with A's rules.
func TestB1_RootSwapBetweenGateAndRead(t *testing.T) {
	needSymlinks(t)
	ws := t.TempDir()
	put(t, filepath.Join(ws, "A/s.go"), "package p\n\nfunc Public() string { return \"A_PUBLIC\" }\n")
	put(t, filepath.Join(ws, "B/.arkignore"), "s.go\n")
	put(t, filepath.Join(ws, "B/s.go"), fmt.Sprintf(secretGo, "B_SECRET_MARKER"))
	root := filepath.Join(ws, "root")
	replaceWithLink(t, filepath.Join(ws, "A"), root)
	h := handlerFor(t, root)
	for _, c := range fileCalls("s.go") {
		replaceWithLink(t, filepath.Join(ws, "A"), root)
		onRead(t, "s.go", func() { replaceWithLink(t, filepath.Join(ws, "B"), root) })
		if out := callFile(t, h, c.name, c.args, "s.go"); leaked(c.name, out, "B_SECRET", secretSize("B_SECRET_MARKER")) {
			t.Errorf("%s returned B's excluded file:\n%s", c.name, out)
		}
	}
}

// B2: the file is replaced by a symlink to a file outside the root
// (--allow-external-symlinks off) after the gate admitted it.
func TestB2_ExternalSymlinkSwap(t *testing.T) {
	needSymlinks(t)
	ws := t.TempDir()
	repo := filepath.Join(ws, "repo")
	put(t, filepath.Join(ws, "outside/secret.go"), fmt.Sprintf(secretGo, "OUTSIDE_SECRET_MARKER"))
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, repo)
	for _, c := range fileCalls("x.go") {
		put(t, filepath.Join(repo, "x.go"), "package p\n\nfunc Public() string { return \"PUBLIC\" }\n")
		onRead(t, "x.go", func() { replaceWithLink(t, filepath.Join(ws, "outside/secret.go"), filepath.Join(repo, "x.go")) })
		if out := callFile(t, h, c.name, c.args, "x.go"); leaked(c.name, out, "OUTSIDE_SECRET", secretSize("OUTSIDE_SECRET_MARKER")) {
			t.Errorf("%s returned the outside file:\n%s", c.name, out)
		}
		os.Remove(filepath.Join(repo, "x.go"))
	}
}

// B3: the file is replaced by a symlink to an excluded file inside the root.
func TestB3_IgnoredSymlinkSwap(t *testing.T) {
	needSymlinks(t)
	for _, flags := range [][]string{nil, {"--allow-external-symlinks", "on"}} {
		repo := t.TempDir()
		put(t, filepath.Join(repo, ".arkignore"), "secret.go\n")
		put(t, filepath.Join(repo, "secret.go"), fmt.Sprintf(secretGo, "IGNORED_SECRET_MARKER"))
		h := handlerFor(t, repo, flags...)
		for _, c := range fileCalls("x.go") {
			put(t, filepath.Join(repo, "x.go"), "package p\n\nfunc Public() string { return \"PUBLIC\" }\n")
			onRead(t, "x.go", func() { replaceWithLink(t, "secret.go", filepath.Join(repo, "x.go")) })
			if out := callFile(t, h, c.name, c.args, "x.go"); leaked(c.name, out, "IGNORED_SECRET", secretSize("IGNORED_SECRET_MARKER")) {
				t.Errorf("%v %s returned the excluded file:\n%s", flags, c.name, out)
			}
			os.Remove(filepath.Join(repo, "x.go"))
		}
	}
}

// B4: a directory whose .arkignore excludes a file is swapped in for a decoy
// without rules, after the decoy's rules were captured for the request. The
// excluded file is not read, and — get_files_arklite answering all or
// nothing — no other file of the call is returned with the refusal.
func TestB4_NestedRulesConcealed(t *testing.T) {
	ws := t.TempDir()
	repo := filepath.Join(ws, "repo")
	put(t, filepath.Join(ws, "d-real/.arkignore"), "x.go\n")
	put(t, filepath.Join(ws, "d-real/x.go"), fmt.Sprintf(secretGo, "NESTED_SECRET_MARKER"))
	put(t, filepath.Join(ws, "d-real/ok.go"), "package d\n")
	put(t, filepath.Join(repo, "d/x.go"), "package d // DECOY\n")
	put(t, filepath.Join(repo, "d/ok.go"), "package d // DECOY_OK\n")
	h := handlerFor(t, repo)
	onRead(t, "d/x.go", func() {
		mutate(t, "move the decoy out", os.Rename(filepath.Join(repo, "d"), filepath.Join(ws, "d-decoy")))
		mutate(t, "move the real directory in", os.Rename(filepath.Join(ws, "d-real"), filepath.Join(repo, "d")))
	})
	res, err := h.CallTool("get_files_arklite", map[string]interface{}{"paths": []interface{}{"d/ok.go", "d/x.go"}})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Content[0].Text
	if strings.Contains(out, "NESTED_SECRET") || !res.IsError || strings.Contains(out, "DECOY_OK") {
		t.Errorf("concealed rules: error %v:\n%s", res.IsError, out)
	}
}

// B7: the file an index-based tool's snippet comes from is replaced — by a
// symlink outside the root, or to an excluded file — after the index was
// built, before the snippet is read. The swap is made when the tool has its
// index (afterIndex), so it happens whatever reads the snippet.
func TestB7_SnippetReread(t *testing.T) {
	needSymlinks(t)
	const public = "package p\n\n// Target calls Helper.\nfunc Target() string { return Helper() }\n\n// Helper is public.\nfunc Helper() string { return \"PUBLIC_SNIPPET\" }\n"
	secret := "package p\n\n// SNIPPET_SECRET_MARKER line 3\n// SNIPPET_SECRET_MARKER line 4\n// SNIPPET_SECRET_MARKER line 5\n// SNIPPET_SECRET_MARKER line 6\n// SNIPPET_SECRET_MARKER line 7\n// SNIPPET_SECRET_MARKER line 8\n"
	for _, target := range []string{"outside", "excluded"} {
		for _, call := range []struct {
			name string
			args map[string]interface{}
		}{
			{"get_context", map[string]interface{}{"path": ".", "symbol": "Target"}},
			{"search_context", map[string]interface{}{"query": "Target", "contextLimit": 3}},
		} {
			ws := t.TempDir()
			repo := filepath.Join(ws, "repo")
			put(t, filepath.Join(repo, "go.mod"), "module example.com/p\n\ngo 1.22\n")
			put(t, filepath.Join(repo, ".arkignore"), "secret.go\n")
			put(t, filepath.Join(repo, "a.go"), public)
			put(t, filepath.Join(ws, "outside/secret.go"), secret)
			put(t, filepath.Join(repo, "secret.go"), secret)
			link := filepath.Join(ws, "outside/secret.go")
			if target == "excluded" {
				link = "secret.go"
			}
			h := handlerFor(t, repo)
			var once sync.Once
			afterIndex = func() { once.Do(func() { replaceWithLink(t, link, filepath.Join(repo, "a.go")) }) }
			t.Cleanup(func() { afterIndex = nil })
			res, err := h.CallTool(call.name, call.args)
			if err != nil {
				t.Fatal(err)
			}
			out := res.Content[0].Text
			if strings.Contains(out, "SNIPPET_SECRET") {
				t.Errorf("%s, link to %s: snippet from the swapped file:\n%s", call.name, target, out)
			}
			if res.IsError || strings.Contains(out, "index_unavailable") || strings.Contains(out, "Error building index") {
				t.Errorf("%s, link to %s: the index's target was not used:\n%s", call.name, target, out)
			}
		}
	}
}

// B8: concurrent requests on two servers with different roots and rules
// (and on one server): each request reads its own root under its own rules,
// with no data race (go test -race), and every request's access is
// released.
func TestB8_ConcurrentRequests(t *testing.T) {
	r1, r2 := t.TempDir(), t.TempDir()
	put(t, filepath.Join(r1, ".arkignore"), "s.go\n")
	put(t, filepath.Join(r1, "s.go"), "package p // R1_SECRET\n")
	put(t, filepath.Join(r1, "pub.go"), "package p // R1_PUBLIC\n")
	put(t, filepath.Join(r2, "s.go"), "package p // R2_PUBLIC\n")
	h1, h2 := handlerFor(t, r1), handlerFor(t, r2)
	before := openFDs()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				out1 := callFile(t, h1, "get_file_content", map[string]interface{}{"path": "s.go"}, "s.go")
				out1p := callFile(t, h1, "get_files_arklite", map[string]interface{}{"paths": []interface{}{"pub.go"}}, "pub.go")
				out2 := callFile(t, h2, "file://", nil, "s.go")
				if strings.Contains(out1, "R1_SECRET") || strings.Contains(out1, "R2_") || !strings.Contains(out1p, "R1_PUBLIC") {
					t.Errorf("root 1: %q %q", out1, out1p)
				}
				if !strings.Contains(out2, "R2_PUBLIC") || strings.Contains(out2, "R1_") {
					t.Errorf("root 2: %q", out2)
				}
			}
		}(g)
	}
	wg.Wait()
	if after := openFDs(); before >= 0 && after > before+4 {
		t.Errorf("open descriptors: %d before, %d after 960 requests", before, after)
	}
}

// openFDs counts the process's open descriptors (-1 where unknown).
func openFDs() int {
	for _, d := range []string{"/dev/fd", "/proc/self/fd"} {
		if es, err := os.ReadDir(d); err == nil {
			return len(es)
		}
	}
	return -1
}

// B10: the files read through the request's access are masked as the
// operator set: a request's maskSecrets: false does not turn it off.
func TestB10_MaskingOnRequestReads(t *testing.T) {
	repo := t.TempDir()
	const secret = "hunter2hunter2hunter2"
	put(t, filepath.Join(repo, "go.mod"), "module example.com/p\n\ngo 1.22\n")
	put(t, filepath.Join(repo, "cfg.go"), "package p\n\n// Conf holds the password.\nfunc Conf() string {\n\tpassword := \""+secret+"\"\n\treturn password\n}\n")
	for _, flags := range [][]string{nil, {"--mask-secrets", "off"}} {
		h := handlerFor(t, repo, flags...)
		masked := flags == nil
		for _, c := range []struct {
			name string
			args map[string]interface{}
		}{
			{"get_file_content", map[string]interface{}{"path": "cfg.go", "maskSecrets": false}},
			{"get_files_arklite", map[string]interface{}{"paths": []interface{}{"cfg.go"}, "maskSecrets": false}},
			{"get_symbol", map[string]interface{}{"path": "cfg.go", "name": "Conf"}},
			{"get_context", map[string]interface{}{"path": ".", "symbol": "Conf"}},
			{"file://", nil},
		} {
			out := callFile(t, h, c.name, c.args, "cfg.go")
			if strings.Contains(out, secret) == masked {
				t.Errorf("masking %v: %s:\n%s", masked, c.name, out)
			}
		}
	}
}

// B7 (repository map): a package's file is replaced by a symlink to an
// outside generated file after the index was built: the map's generated
// flag is not derived from the outside file.
func TestB7_RepomapReread(t *testing.T) {
	needSymlinks(t)
	ws := t.TempDir()
	repo := filepath.Join(ws, "repo")
	put(t, filepath.Join(repo, "go.mod"), "module example.com/p\n\ngo 1.22\n")
	put(t, filepath.Join(repo, "pkg/a.go"), "package pkg\n\n// A is public.\nfunc A() {}\n")
	put(t, filepath.Join(ws, "outside/gen.go"), "// Code generated by a tool. DO NOT EDIT.\n\npackage pkg\n")
	h := handlerFor(t, repo)
	var once sync.Once
	afterIndex = func() {
		once.Do(func() { replaceWithLink(t, filepath.Join(ws, "outside/gen.go"), filepath.Join(repo, "pkg/a.go")) })
	}
	t.Cleanup(func() { afterIndex = nil })
	res, err := h.CallTool("get_repository_map", map[string]interface{}{"path": ".", "format": "json"})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Content[0].Text
	if res.IsError || !strings.Contains(out, "pkg") {
		t.Fatalf("no map:\n%s", out)
	}
	if strings.Contains(out, `"isGenerated": true`) || strings.Contains(out, `"IsGenerated": true`) {
		t.Errorf("generated flag from the outside file:\n%s", out)
	}
}
