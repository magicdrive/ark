package mcp

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/syntax"
)

// The symbol tools extract a file as its repository-relative name (a
// provider may name symbols after their file: an HTML document, a Rust
// crate path, a Terraform override file): never as an absolute path nor
// as a synthetic name.

func fileTool(t *testing.T, root, tool string, args map[string]interface{}) string {
	t.Helper()
	h := &ToolsHandler{rootDir: root}
	res, err := h.CallTool(tool, args)
	if err != nil || res.IsError {
		t.Fatalf("%s %v: %v %+v", tool, args, err, res)
	}
	var text string
	for _, c := range res.Content {
		text += c.Text
	}
	return text
}

func TestSymbolTools_RepositoryRelativeNames(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	golden := filepath.Join(filepath.Dir(here), "..", "golden", "testdata")
	for _, root := range []string{htmlFixture(), filepath.Join(golden, "rust"), filepath.Join(golden, "terraform")} {
		abs, _ := filepath.Abs(root)
		text := fileTool(t, root, "find_symbol", map[string]interface{}{"path": ".", "pattern": ".", "maxResults": 1000})
		if strings.Contains(text, abs) || strings.Contains(text, `"_.`) || strings.Contains(text, "{_.") {
			t.Errorf("%s: find_symbol output holds an absolute or synthetic file name:\n%s", root, text)
		}
	}
	text := fileTool(t, htmlFixture(), "find_symbol", map[string]interface{}{"path": ".", "pattern": "^card-title$"})
	if !strings.Contains(text, `"parent": "docs/guide.html"`) {
		t.Errorf("find_symbol card-title: %s", text)
	}
	// Rust: a member's parent is its crate path.
	text = fileTool(t, filepath.Join(golden, "rust"), "get_symbols", map[string]interface{}{"path": "src/lib.rs"})
	if strings.Contains(text, "_.rust") {
		t.Errorf("get_symbols src/lib.rs names a synthetic file:\n%s", text)
	}
	// Terraform: an override file declares nothing.
	var fs syntax.FileSymbols
	text = fileTool(t, filepath.Join(golden, "terraform"), "get_symbols", map[string]interface{}{"path": "override.tf"})
	if err := json.Unmarshal([]byte(text), &fs); err != nil {
		t.Fatal(err)
	}
	if len(fs.Symbols) != 0 {
		t.Errorf("get_symbols override.tf: %d symbols, want none (an override file declares nothing)", len(fs.Symbols))
	}
}
