package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Targets among an HTML document's embedded declarations (Phase 8, Reviewer
// C): a name an inline script shares with other languages and with the
// document's ids.

func embeddedTargetRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range map[string]string{
		"web/a.html": "<p id=send>x</p><style>@keyframes send {}</style>\n<script>function send() { helper() }\nfunction helper() {}</script>\n<script type=module>function send() {}</script>\n",
		"j/x.js":     "function send() {}\nfunction helper() {}\n",
		"g/g.go":     "package g\n\nfunc helper() {}\n",
		"go.mod":     "module example.com/x\n\ngo 1.22\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// C8-1: a filePattern selects among the document's declarations of the name,
// although a JavaScript file declares one whose Qualified is the name.
func TestEmbeddedTarget_FilePattern(t *testing.T) {
	root := embeddedTargetRepo(t)
	res, text := callTool(t, root, "get_callers", map[string]interface{}{"symbol": "helper", "filePattern": "web/a.html"})
	if res.IsError || !strings.Contains(text, "web/a.html send") {
		t.Errorf("get_callers helper in web/a.html: %s", text)
	}
	res, text = callTool(t, root, "get_callers", map[string]interface{}{"symbol": "send", "filePattern": "web/a.html"})
	if !res.IsError || !strings.Contains(text, "Ambiguous") {
		t.Fatalf("get_callers send in web/a.html: want an ambiguity, got %s", text)
	}
	for _, q := range []string{"web/a.html#send", "web/a.html @keyframes send", "web/a.html send", "web/a.html module#1 send"} {
		if !strings.Contains(text, q) {
			t.Errorf("the ambiguity lists no %q:\n%s", q, text)
		}
	}
	// A JavaScript file's send is found by its Qualified, as before.
	if res, text = callTool(t, root, "get_callers", map[string]interface{}{"symbol": "send", "filePattern": "j/x.js"}); res.IsError {
		t.Errorf("get_callers send in j/x.js: %s", text)
	}
}

// C8-2: get_symbol's retry names select one declaration each.
func TestEmbeddedTarget_GetSymbolRetry(t *testing.T) {
	h := &ToolsHandler{rootDir: embeddedTargetRepo(t)}
	res, err := h.CallTool("get_symbol", map[string]interface{}{"path": "web/a.html", "name": "send"})
	if err != nil || !res.IsError {
		t.Fatalf("get_symbol send: want an ambiguity, got %v %+v", err, res)
	}
	text := res.Content[0].Text
	var retries []string
	for _, line := range strings.Split(text, "\n")[1:] {
		if f := strings.Split(strings.TrimSpace(line), "  ("); len(f) == 2 {
			retries = append(retries, f[0])
		}
	}
	if len(retries) != 4 {
		t.Fatalf("retry names %q in\n%s", retries, text)
	}
	seen := map[string]bool{}
	for _, r := range retries {
		if seen[r] {
			t.Errorf("retry name %q listed twice:\n%s", r, text)
		}
		seen[r] = true
		res, err := h.CallTool("get_symbol", map[string]interface{}{"path": "web/a.html", "name": r})
		if err != nil || res.IsError {
			t.Errorf("get_symbol %q: %v %+v", r, err, res)
		}
	}
	res, err = h.CallTool("get_symbol", map[string]interface{}{"path": "web/a.html", "name": "web/a.html module#1 send"})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].Text, `"startLine": 4`) {
		t.Errorf("get_symbol of the module's send: %v %+v", err, res)
	}
}

// C8-8: a qualified identity two declarations share still narrows the
// choice when the owner's name selects more.
func TestEmbeddedTarget_RetryNarrows(t *testing.T) {
	root := t.TempDir()
	src := "<p id=f>a</p><p id=f>b</p><script>function f() {}</script><script>function f() {}</script>\n"
	if err := os.WriteFile(filepath.Join(root, "d.html"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &ToolsHandler{rootDir: root}
	res, err := h.CallTool("get_symbol", map[string]interface{}{"path": "d.html", "name": "f"})
	if err != nil || !res.IsError {
		t.Fatalf("get_symbol f: %v %+v", err, res)
	}
	if text := res.Content[0].Text; !strings.Contains(text, "  d.html f  (function)") {
		t.Errorf("the functions' retry name is not their qualified identity:\n%s", text)
	}
	res, err = h.CallTool("get_symbol", map[string]interface{}{"path": "d.html", "name": "d.html f"})
	if err != nil || !res.IsError || strings.Count(res.Content[0].Text, "(function)") != 2 || strings.Contains(res.Content[0].Text, "(element)") {
		t.Errorf("get_symbol d.html f: %v %+v", err, res)
	}
}
