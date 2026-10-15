package mcp

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/syntax"
)

// HTML end to end through the MCP surfaces, on the site fixture of the HTML
// provider (internal/languages/html/testdata/site).

func htmlFixture() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "languages", "html", "testdata", "site")
}

func htmlCall(t *testing.T, tool string, args map[string]interface{}) (string, bool) {
	t.Helper()
	res, text := callTool(t, htmlFixture(), tool, args)
	return text, res.IsError
}

// htmlFileTool calls a tool on one file of the fixture (callTool sets the
// path to the root).
func htmlFileTool(t *testing.T, tool string, args map[string]interface{}) (string, bool) {
	t.Helper()
	h := &ToolsHandler{rootDir: htmlFixture()}
	res, err := h.CallTool(tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var text string
	for _, c := range res.Content {
		text += c.Text
	}
	return text, res.IsError
}

func htmlEdges(t *testing.T, tool, symbol string) callersResult {
	t.Helper()
	text, isErr := htmlCall(t, tool, map[string]interface{}{"symbol": symbol})
	if isErr {
		t.Fatalf("%s %s: %s", tool, symbol, text)
	}
	var out callersResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return out
}

func TestOneRegistrationInvariant_HTML(t *testing.T) {
	for _, f := range []string{"index.html", "a/b.htm"} {
		if syntax.DetectLanguage(f) != "html" {
			t.Errorf("syntax detection does not route %s to html", f)
		}
	}
	found := false
	for _, p := range defaultProviders() {
		found = found || p.Language() == "html"
	}
	if !found {
		t.Error("defaultProviders() misses html")
	}
	for _, ext := range []string{".html", ".htm"} {
		if _, ok := refProviderRegistry[ext]; !ok {
			t.Errorf("find_references registry misses %s", ext)
		}
	}
	if lvl := languages.Registry().SupportLevelFor("html"); lvl.String() != "graph" {
		t.Errorf("html support level %s", lvl)
	}
	text, _ := htmlCall(t, "get_language_support", map[string]interface{}{})
	var entries []languageSupportEntry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, e := range entries {
		if e.Language == "html" {
			ok = e.Level == "graph" && containsExt(e.Extensions, ".html") && containsExt(e.Extensions, ".htm")
		}
	}
	if !ok {
		t.Errorf("get_language_support does not report html at graph with .html and .htm: %s", text)
	}
}

func TestMCP_HTML_Symbols(t *testing.T) {
	text, isErr := htmlFileTool(t, "get_symbols", map[string]interface{}{"path": "docs/form.html"})
	if isErr {
		t.Fatal(text)
	}
	var fs syntax.FileSymbols
	if err := json.Unmarshal([]byte(text), &fs); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range fs.Symbols {
		got = append(got, string(s.Kind)+" "+s.Name)
	}
	// The function of the inline script (Phase 8) is the document's too.
	want := "document form.html,element signup,element email,element email-help,element domains,function send"
	if strings.Join(got, ",") != want {
		t.Errorf("get_symbols = %v, want %s", got, want)
	}
	text, isErr = htmlFileTool(t, "get_symbol", map[string]interface{}{"path": "docs/form.html", "name": "send"})
	if isErr || !strings.Contains(text, "function send() {}") {
		t.Errorf("get_symbol send: %s", text)
	}

	text, isErr = htmlFileTool(t, "get_symbol", map[string]interface{}{"path": "docs/guide.html", "name": "install"})
	if isErr || !strings.Contains(text, `h2 id=\"install\"`) || !strings.Contains(text, `"kind": "element"`) {
		t.Errorf("get_symbol install: %s", text)
	}
	text, isErr = htmlCall(t, "find_symbol", map[string]interface{}{"pattern": "^card"})
	if isErr || !strings.Contains(text, "card-title") || !strings.Contains(text, "guide.html") {
		t.Errorf("find_symbol ^card: %s", text)
	}
	text, isErr = htmlFileTool(t, "find_references", map[string]interface{}{"path": "docs/form.html", "name": "email", "kind": "id_reference"})
	if isErr || !strings.Contains(text, `"count": 1`) {
		t.Errorf("find_references email: %s", text)
	}
}

func TestMCP_HTML_Graph(t *testing.T) {
	// The heading is referenced by the paragraph that it labels.
	out := htmlEdges(t, "get_callers", "index.html#title")
	if len(out.Edges) != 1 || out.Edges[0].To != "index.html" || out.Edges[0].Kind != "referenced_by" || out.Edges[0].Confidence != "exact" {
		t.Errorf("callers of index.html#title: %+v", out.Edges)
	}
	// A document's links and id references.
	out = htmlEdges(t, "get_callees", "index.html")
	var got []string
	for _, e := range out.Edges {
		got = append(got, e.To+" "+e.Kind+" "+e.Confidence)
	}
	want := []string{
		"docs/form.html links strong",
		"docs/guide.html links strong",
		"docs/guide.html#install references strong",
		"index.html#g references exact",
		"index.html#main references exact",
		"index.html#title references exact",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("callees of index.html\n got %v\nwant %v", got, want)
	}
	out = htmlEdges(t, "get_callers", "docs/guide.html")
	if len(out.Edges) != 1 || out.Edges[0].To != "index.html" || out.Edges[0].Kind != "linked_from" {
		t.Errorf("callers of docs/guide.html: %+v", out.Edges)
	}
	// A dynamic document: its id references are Strong.
	out = htmlEdges(t, "get_callees", "docs/form.html")
	for _, e := range out.Edges {
		if e.Confidence != "strong" {
			t.Errorf("callee of the dynamic form.html %s is %s", e.To, e.Confidence)
		}
	}
	if len(out.Edges) != 4 {
		t.Errorf("callees of docs/form.html: %+v", out.Edges)
	}
	// Inside a template: a candidate, never an edge.
	if out := htmlEdges(t, "get_callers", "docs/guide.html#1 card-title"); len(out.Edges) != 0 {
		t.Errorf("callers of the template's card-title: %+v", out.Edges)
	}
}
