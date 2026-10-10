package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// request begins a request on h, as CallTool does, and ends it when the test
// ends — before the test's temporary directories are removed (cleanups run
// last-registered first). A request holds the server root pinned until it
// ends: an open directory handle, which on Windows keeps the directory from
// being deleted.
func request(t *testing.T, h *ToolsHandler) *ToolsHandler {
	t.Helper()
	r, end := h.beginRequest()
	t.Cleanup(end)
	return r
}

// handlesUnder reports the descriptors this process holds of files or
// directories below dir, where the system names them (fdTarget: Linux,
// macOS); ok is false where it does not.
func handlesUnder(dir string) (held []string, ok bool) {
	if fdDir == "" {
		return nil, false
	}
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil, false
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real // the system names the object, not the path it was opened by
	}
	for _, e := range entries {
		if target, ok := fdTarget(e.Name()); ok &&
			(target == dir || strings.HasPrefix(target, dir+string(filepath.Separator))) {
			held = append(held, target)
		}
	}
	return held, true
}

// A request releases the server root it pinned when it ends: CallTool's
// requests, and a request begun and ended by hand. Removing the repository
// right afterwards — once, without the retries of t.TempDir's cleanup —
// succeeds; on Windows an unreleased root handle fails it ("being used by
// another process"). The collector is off, so no finalizer of a leaked
// handle can hide the leak.
func TestRequestAccess_ReleasedWhenRequestEnds(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	fixture := func() (ws, root string) {
		ws = t.TempDir()
		root = filepath.Join(ws, "repo")
		write(t, root, ".arkignore", "secret.txt\n")
		write(t, root, "secret.txt", "secret\n")
		write(t, root, "src/.arkignore", "private.txt\n")
		write(t, root, "src/open.go", "package src\n\n// MARK\nfunc Open() {}\n")
		write(t, root, "src/private.txt", "private\n")
		return ws, root
	}
	removed := func(what, ws, root string) {
		t.Helper()
		if held, ok := handlesUnder(root); ok && len(held) > 0 {
			t.Errorf("%s: descriptors still open below the repository: %q", what, held)
		}
		if err := os.RemoveAll(ws); err != nil {
			t.Errorf("%s: the repository cannot be removed: %v", what, err)
		}
	}

	// The probe sees a request that has not ended (Linux: its descriptors;
	// Windows: the removal fails, as in t.TempDir's cleanup), and stops
	// seeing it when the request ends.
	ws, root := fixture()
	r, end := handlerFor(t, root).beginRequest()
	t.Cleanup(end) // if the test stops early; ending twice is ending once
	if _, _, err := r.resolveToolPath("src/open.go"); err != nil {
		t.Fatal(err)
	}
	if held, ok := handlesUnder(root); ok && len(held) == 0 {
		t.Error("a request in progress holds no descriptor below the repository: the probe sees nothing")
	}
	if runtime.GOOS == "windows" {
		if err := os.RemoveAll(ws); err == nil {
			t.Error("the repository was removed while a request held its root: the probe sees nothing")
		}
	}
	end()
	removed("a request ended by hand", ws, root)

	// CallTool: tools that walk and read through the request's access.
	ws, root = fixture()
	h := handlerFor(t, root)
	for _, c := range []struct {
		name string
		args map[string]interface{}
	}{
		{"get_file_content", map[string]interface{}{"path": "src/open.go"}},
		{"get_file_content", map[string]interface{}{"path": "SRC/PRIVATE.TXT"}},
		{"get_file_content", map[string]interface{}{"path": "secret.txt"}},
		{"get_file_info", map[string]interface{}{"path": "src/open.go"}},
		{"get_files_arklite", map[string]interface{}{"paths": []interface{}{"src/open.go"}}},
		{"get_symbols", map[string]interface{}{"path": "src/open.go"}},
		{"list_files", map[string]interface{}{"path": "."}},
		{"search_in_files", map[string]interface{}{"path": ".", "query": "MARK"}},
		{"get_directory_tree", map[string]interface{}{"path": "src"}},
		{"get_project_stats", map[string]interface{}{"path": "."}},
	} {
		callOut(t, h, c.name, c.args)
	}
	removed("CallTool", ws, root)
}
