package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Assets through the MCP tools (hardening Phase B): a page's image is a
// target of get_callers / analyze_change_impact / get_context by its path,
// listed without its content.

func assetRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range map[string][]byte{
		"site/index.html":     []byte(`<link rel="stylesheet" href="s.css"><img src="img/logo.png">` + "\n"),
		"site/s.css":          []byte(".a { background: url(img/logo.png) }\n"),
		"site/img/logo.png":   {0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe},
		"site/img/unused.png": {0x89, 'P', 'N', 'G'},
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestMCP_Assets(t *testing.T) {
	root := assetRepo(t)
	res, text := callTool(t, root, "get_callers", map[string]interface{}{"symbol": "site/img/logo.png"})
	if res.IsError || !strings.Contains(text, "site/index.html") || !strings.Contains(text, "site/s.css") {
		t.Errorf("get_callers of the asset: %s", text)
	}
	res, text = callTool(t, root, "analyze_change_impact", map[string]interface{}{"symbol": "site/img/logo.png"})
	if res.IsError || !strings.Contains(text, "site/index.html") {
		t.Errorf("analyze_change_impact of the asset: %s", text)
	}
	res, text = callTool(t, root, "get_context", map[string]interface{}{"symbol": "site/img/logo.png"})
	if res.IsError || strings.Contains(text, "PNG") || !strings.Contains(text, "image asset site/img/logo.png") {
		t.Errorf("get_context of the asset: %s", text)
	}
	res, text = callTool(t, root, "get_callees", map[string]interface{}{"symbol": "site/index.html"})
	if res.IsError || !strings.Contains(text, "site/img/logo.png") {
		t.Errorf("get_callees of the page: %s", text)
	}
	// An asset is no source: get_symbols has no provider for it.
	h := &ToolsHandler{rootDir: root}
	r, err := h.CallTool("get_symbols", map[string]interface{}{"path": "site/img/logo.png"})
	if err == nil && r != nil && !r.IsError {
		t.Errorf("get_symbols of an asset: %+v", r)
	}
}

// An asset added after the index was built is seen by the next call: the
// fingerprint covers the assets (their paths), so the cached index is not
// reused.
func TestMCP_AssetAddedInvalidatesIndex(t *testing.T) {
	root := assetRepo(t)
	h := &ToolsHandler{rootDir: root}
	call := func() string {
		r, err := h.CallTool("get_callees", map[string]interface{}{"path": ".", "symbol": "site/index.html"})
		if err != nil || r.IsError {
			t.Fatalf("get_callees: %v %+v", err, r)
		}
		return r.Content[0].Text
	}
	if text := call(); strings.Contains(text, "site/img/new.png") {
		t.Fatalf("new.png before it exists: %s", text)
	}
	if err := os.WriteFile(filepath.Join(root, "site/index.html"), []byte(`<img src="img/logo.png"><img src="img/new.png">`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if text := call(); strings.Contains(text, `"to": "site/img/new.png"`) {
		t.Fatalf("new.png linked while missing: %s", text)
	}
	if err := os.WriteFile(filepath.Join(root, "site/img/new.png"), []byte{0x89}, 0o644); err != nil {
		t.Fatal(err)
	}
	if text := call(); !strings.Contains(text, "site/img/new.png") {
		t.Errorf("the added asset is not linked: %s", text)
	}
	if err := os.Remove(filepath.Join(root, "site/img/new.png")); err != nil {
		t.Fatal(err)
	}
	if text := call(); strings.Contains(text, `"to": "site/img/new.png"`) {
		t.Errorf("the removed asset is still linked: %s", text)
	}
}

// An asset nothing links to is told apart from a missing file (Reviewer A,
// hardening Phase F).
func TestMCP_UnlinkedAsset(t *testing.T) {
	root := assetRepo(t)
	res, text := callTool(t, root, "get_callers", map[string]interface{}{"symbol": "site/img/unused.png"})
	if !res.IsError || !strings.Contains(text, "nothing in the index links to") {
		t.Errorf("an unlinked asset: %s", text)
	}
	res, text = callTool(t, root, "get_callers", map[string]interface{}{"symbol": "site/img/none.png"})
	if !res.IsError || strings.Contains(text, "links to") {
		t.Errorf("a missing file: %s", text)
	}
}
