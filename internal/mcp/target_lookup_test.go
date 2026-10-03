package mcp

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func ambigDir(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func callTool(t *testing.T, dir, tool string, args map[string]interface{}) (*CallToolResult, string) {
	t.Helper()
	h := &ToolsHandler{rootDir: dir}
	args["path"] = "."
	res, err := h.CallTool(tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var text string
	for _, c := range res.Content {
		text += c.Text
	}
	return res, text
}

// --- Ambiguity must be rejected, never silently resolved to candidate 0 ---

func TestMCP_GetContext_AmbiguousRejected(t *testing.T) {
	// Go: two packages each define func Create (qualified "Create" collides).
	res, text := callTool(t, ambigDir("ambig_go"), "get_context", map[string]interface{}{"symbol": "Create"})
	if !res.IsError {
		t.Fatalf("ambiguous get_context must be IsError; got:\n%s", text)
	}
	if !strings.Contains(text, "Ambiguous") {
		t.Errorf("missing ambiguity message:\n%s", text)
	}
	for _, want := range []string{"a/svc.go", "b/svc.go"} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate %q not reported:\n%s", want, text)
		}
	}
}

func TestMCP_GetContext_UniqueWorks(t *testing.T) {
	res, text := callTool(t, ambigDir("ambig_go"), "get_context", map[string]interface{}{"symbol": "UniqueAlpha"})
	if res.IsError {
		t.Fatalf("unique target must succeed; got error:\n%s", text)
	}
	if strings.Contains(text, "Ambiguous") {
		t.Errorf("unique target wrongly flagged ambiguous:\n%s", text)
	}
}

func TestMCP_GetContext_PHPQualifiedDisambiguates(t *testing.T) {
	dir := ambigDir("ambig_php")
	// Short name is ambiguous across namespaces.
	res, text := callTool(t, dir, "get_context", map[string]interface{}{"symbol": "create"})
	if !res.IsError {
		t.Fatalf("ambiguous php create must be IsError:\n%s", text)
	}
	for _, want := range []string{"A\\User.create", "B\\User.create"} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate %q not reported:\n%s", want, text)
		}
	}
	// Fully-qualified name selects exactly one.
	res2, text2 := callTool(t, dir, "get_context", map[string]interface{}{"symbol": "A\\User.create"})
	if res2.IsError {
		t.Fatalf("qualified php target must succeed:\n%s", text2)
	}
	if strings.Contains(text2, "Ambiguous") {
		t.Errorf("qualified php target wrongly ambiguous:\n%s", text2)
	}
}

func TestMCP_Impact_AmbiguousRejected(t *testing.T) {
	// analyze_change_impact MUST NOT produce an impact report for an arbitrary
	// first candidate.
	res, text := callTool(t, ambigDir("ambig_go"), "analyze_change_impact", map[string]interface{}{"symbol": "Create"})
	if !res.IsError {
		t.Fatalf("ambiguous impact must be IsError (no report for candidate 0):\n%s", text)
	}
	if !strings.Contains(text, "Ambiguous") {
		t.Errorf("missing ambiguity message:\n%s", text)
	}
}

func TestMCP_Impact_UniqueWorks(t *testing.T) {
	res, text := callTool(t, ambigDir("ambig_go"), "analyze_change_impact", map[string]interface{}{"symbol": "UniqueAlpha"})
	if res.IsError {
		t.Fatalf("unique impact target must succeed:\n%s", text)
	}
}

func TestMCP_Relations_AmbiguousNotMerged(t *testing.T) {
	// get_relations must not merge relations of multiple same-name symbols.
	res, text := callTool(t, ambigDir("ambig_php"), "get_relations", map[string]interface{}{"symbol": "create"})
	if !res.IsError {
		t.Fatalf("ambiguous get_relations must be IsError (not merged):\n%s", text)
	}
	for _, want := range []string{"A\\User.create", "B\\User.create"} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate %q not reported:\n%s", want, text)
		}
	}
}

func TestMCP_Callers_AmbiguityPreserved(t *testing.T) {
	// get_callers already rejected ambiguity; migrating to the shared helper
	// must preserve that.
	res, text := callTool(t, ambigDir("ambig_go"), "get_callers", map[string]interface{}{"symbol": "Create"})
	if !res.IsError {
		t.Fatalf("ambiguous get_callers must stay IsError:\n%s", text)
	}
	res2, text2 := callTool(t, ambigDir("ambig_go"), "get_callees", map[string]interface{}{"symbol": "Create"})
	if !res2.IsError {
		t.Fatalf("ambiguous get_callees must stay IsError:\n%s", text2)
	}
}

func TestMCP_Callers_FilePatternNarrows(t *testing.T) {
	// filePattern narrows an otherwise-ambiguous name to a single target.
	res, text := callTool(t, ambigDir("ambig_go"), "get_callers", map[string]interface{}{
		"symbol": "Create", "filePattern": "a/svc.go",
	})
	if res.IsError {
		t.Fatalf("filePattern should disambiguate get_callers:\n%s", text)
	}
}

// --- Determinism: ambiguity output is byte-identical across runs ---

func TestMCP_Ambiguity_Deterministic(t *testing.T) {
	dir := ambigDir("ambig_php")
	_, first := callTool(t, dir, "get_context", map[string]interface{}{"symbol": "create"})
	for i := range 100 {
		_, got := callTool(t, dir, "get_context", map[string]interface{}{"symbol": "create"})
		if got != first {
			t.Fatalf("run %d: ambiguity output not deterministic:\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
}
