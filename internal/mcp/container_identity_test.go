package mcp

import (
	"strings"
	"testing"
)

// TestMCP_GetRelations_ContainerIsFileScoped is the STOP-2 regression for the
// MCP relations pipeline: references are attributed to the target only when
// they sit in the target's own file. a/main.go:main must not report the call
// to helperB that lives in b/main.go:main.
func TestMCP_GetRelations_ContainerIsFileScoped(t *testing.T) {
	dir := ambigDir("container_go")
	res, text := callTool(t, dir, "get_relations", map[string]interface{}{"symbol": "main", "filePattern": "a/"})
	if res.IsError {
		t.Fatalf("get_relations a/main: %s", text)
	}
	if !strings.Contains(text, "helperA") {
		t.Errorf("a/main.go:main must report its call to helperA:\n%s", text)
	}
	if strings.Contains(text, "helperB") {
		t.Errorf("fabricated relation: a/main.go:main reported helperB from b/main.go:\n%s", text)
	}
}
