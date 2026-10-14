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

// CSS end to end through the MCP surfaces, on the site fixture of the CSS
// provider (internal/languages/css/testdata/site).

func cssFixture() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "languages", "css", "testdata", "site")
}

func cssEdges(t *testing.T, tool, symbol string) callersResult {
	t.Helper()
	_, text := callTool(t, cssFixture(), tool, map[string]interface{}{"symbol": symbol})
	var out callersResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s %s: %v\n%s", tool, symbol, err, text)
	}
	return out
}

func TestOneRegistrationInvariant_CSS(t *testing.T) {
	if syntax.DetectLanguage("a/b.css") != "css" || syntax.DetectLanguage("a/b.module.css") != "css" {
		t.Error("syntax detection does not route .css to css")
	}
	if _, ok := refProviderRegistry[".css"]; !ok {
		t.Error("find_references registry misses .css")
	}
	if lvl := languages.Registry().SupportLevelFor("css"); lvl.String() != "graph" {
		t.Errorf("css support level %s", lvl)
	}
	_, text := callTool(t, cssFixture(), "get_language_support", map[string]interface{}{})
	var entries []languageSupportEntry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, e := range entries {
		ok = ok || e.Language == "css" && e.Level == "graph" && containsExt(e.Extensions, ".css")
	}
	if !ok {
		t.Errorf("get_language_support does not report css at graph with .css: %s", text)
	}
}

func TestMCP_CSS_SymbolsAndReferences(t *testing.T) {
	h := &ToolsHandler{rootDir: cssFixture()}
	res, err := h.CallTool("get_symbols", map[string]interface{}{"path": "components/card.module.css"})
	if err != nil || res.IsError {
		t.Fatalf("get_symbols: %v %+v", err, res)
	}
	var fs syntax.FileSymbols
	if err := json.Unmarshal([]byte(res.Content[0].Text), &fs); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range fs.Symbols {
		got = append(got, string(s.Kind)+" "+s.Name)
	}
	if strings.Join(got, ",") != "stylesheet card.module.css,css_class card,css_class title" {
		t.Errorf("get_symbols = %v", got)
	}
	res, err = h.CallTool("find_references", map[string]interface{}{"path": "styles/main.css", "name": "spin"})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].Text, `"count": 2`) {
		t.Errorf("find_references spin: %v %+v", err, res)
	}
}

func TestMCP_CSS_Graph(t *testing.T) {
	out := cssEdges(t, "get_callers", "styles/main.css @keyframes spin")
	if len(out.Edges) != 1 || out.Edges[0].To != "styles/main.css" || out.Edges[0].Kind != "referenced_by" || out.Edges[0].Confidence != "strong" {
		t.Errorf("callers of spin: %+v", out.Edges)
	}
	out = cssEdges(t, "get_callers", "styles/base.css")
	if len(out.Edges) != 1 || out.Edges[0].To != "styles/main.css" || out.Edges[0].Kind != "linked_from" {
		t.Errorf("callers of base.css: %+v", out.Edges)
	}
	out = cssEdges(t, "get_callees", "components/card.module.css")
	var got []string
	for _, e := range out.Edges {
		got = append(got, e.To+" "+e.Kind+" "+e.Confidence)
	}
	want := "components/base.module.css .surface references strong,components/card.module.css .card references exact"
	if strings.Join(got, ",") != want {
		t.Errorf("callees of card.module.css: %v", got)
	}
	// var() is never an edge: the custom property is a candidate
	out = cssEdges(t, "get_callers", "styles/main.css --brand")
	if len(out.Edges) != 0 {
		t.Errorf("callers of --brand: %+v", out.Edges)
	}
}
