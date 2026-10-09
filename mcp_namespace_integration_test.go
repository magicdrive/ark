package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Name spaces through the real binary: a name another language happens to
// declare is no relation — not a caller, a callee, an impact or context —
// while a language's own resolutions (TSX importing TypeScript) stay.
func TestMCPNameSpaces_NoCrossLanguageRelations(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	for p, c := range map[string]string{
		"go.mod":       "module example.com/mix\n\ngo 1.22\n",
		"g/g.go":       "package g\n\nfunc GoOnly() {}\n",
		"p/app.py":     "def run():\n    GoOnly()\n",
		"j/app.js":     "function jsRun() { GoOnly() }\n",
		"h/app.php":    "<?php\nfunction phpRun(): void { GoOnly(); }\n",
		"web/app.ts":   "export function main(): void {}\n",
		"web/view.tsx": "import { main } from './app'\nexport function View() { main(); return <div /> }\n",
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

	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"get_callers", map[string]any{"path": ".", "symbol": "GoOnly"}},
		{"get_relations", map[string]any{"path": ".", "symbol": "GoOnly"}},
		{"get_context", map[string]any{"path": ".", "symbol": "GoOnly"}},
		{"analyze_change_impact", map[string]any{"path": ".", "symbol": "GoOnly", "format": "json"}},
		{"get_callees", map[string]any{"path": ".", "symbol": "run"}},
		{"get_callees", map[string]any{"path": ".", "symbol": "jsRun"}},
		{"get_callees", map[string]any{"path": ".", "symbol": "phpRun"}},
		{"get_context", map[string]any{"path": ".", "symbol": "run"}},
	} {
		text, isErr := c.tool(call.tool, call.args)
		if isErr {
			t.Fatalf("%s %v: %s", call.tool, call.args, text)
		}
		if call.args["symbol"] == "GoOnly" {
			for _, other := range []string{"p/app.py", "j/app.js", "h/app.php"} {
				if strings.Contains(text, other) {
					t.Errorf("%s of GoOnly mentions %s:\n%s", call.tool, other, text)
				}
			}
		} else if strings.Contains(text, "g/g.go") || strings.Contains(text, `"to": "GoOnly"`) {
			t.Errorf("%s %v reaches the Go declaration:\n%s", call.tool, call.args, text)
		}
	}
	// TSX importing TypeScript is a relation.
	text, _ := c.tool("get_callees", map[string]any{"path": ".", "symbol": "View"})
	if !strings.Contains(text, `"to": "main"`) || !strings.Contains(text, `"confidence": "exact"`) {
		t.Errorf("TSX → TypeScript import lost:\n%s", text)
	}
}
