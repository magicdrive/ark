package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Go package scoping through the real binary: calls resolve by Go's rules
// (imports, aliases, package scope), and nothing that only shares a name —
// a local function value, an external package, a builtin — becomes an edge
// or context.
func TestMCPGoResolution_PackageScoping(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	for p, c := range map[string]string{
		"go.mod": "module example.com/syn\n\ngo 1.22\n",
		"a/a.go": "package a\n\nfunc Helper() {}\n\n// cancel shares only its name with the local below.\ntype cancel struct{}\n\nfunc append() {}\n",
		"b/b.go": "package b\n\nfunc Helper() {}\n",
		"c/c.go": `package c

import (
	"strings"

	"example.com/syn/a"
	al "example.com/syn/b"
)

func Use() {
	a.Helper()
	al.Helper()
	cancel := func() {}
	cancel()
	_ = strings.ToUpper("x")
	_ = append([]int{}, 1)
}
`,
	} {
		full := filepath.Join(proj, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()

	type edge struct{ To, Confidence, Evidence string }
	callees := func(sym string) []edge {
		text, isErr := c.tool("get_callees", map[string]any{"path": ".", "symbol": sym})
		if isErr {
			t.Fatalf("get_callees %s: %s", sym, text)
		}
		var out struct{ Edges []edge }
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatal(err)
		}
		return out.Edges
	}
	for _, e := range callees("Use") {
		if e.To != "Helper" || e.Confidence != "exact" || !strings.Contains(e.Evidence, "imported package") {
			t.Errorf("unexpected callee of Use: %+v", e)
		}
	}
	if n := len(callees("Use")); n != 2 {
		t.Errorf("Use has %d callees, want a.Helper and b.Helper", n)
	}
	// The unrelated cancel type and append function have no caller.
	for _, sym := range []string{"cancel", "append"} {
		text, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": sym})
		if strings.Contains(text, `"to": "Use"`) {
			t.Errorf("%s gained Use as a caller: %s", sym, text)
		}
	}
	// Each Helper's caller is Use, found through its own import.
	for _, file := range []string{"a/a.go", "b/b.go"} {
		text, _ := c.tool("get_callers", map[string]any{"path": ".", "symbol": "Helper", "filePattern": file})
		if !strings.Contains(text, `"to": "Use"`) {
			t.Errorf("Helper in %s lost its caller: %s", file, text)
		}
	}
	// Context and impact are built from these edges only.
	ctx, _ := c.tool("get_context", map[string]any{"path": ".", "symbol": "Use"})
	if strings.Contains(ctx, "type cancel struct") || strings.Contains(ctx, "func append()") {
		t.Errorf("get_context mixes in name-only matches:\n%s", ctx)
	}
	impact, _ := c.tool("analyze_change_impact", map[string]any{"path": ".", "symbol": "cancel", "format": "json"})
	if strings.Contains(impact, `"Use"`) {
		t.Errorf("impact of the unrelated type includes Use:\n%s", impact)
	}
	// Deterministic.
	first, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "Use"})
	for range 3 {
		if again, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "Use"}); again != first {
			t.Fatal("get_callees output varies")
		}
	}
}
