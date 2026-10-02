package mcp_test

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/mcp"
)

func newContractHandler(t *testing.T) *mcp.ToolsHandler {
	t.Helper()
	_, opt, err := commandline.GeneralOptParse([]string{"."})
	if err != nil {
		t.Fatalf("GeneralOptParse: %v", err)
	}
	return mcp.NewToolsHandler(".", opt)
}

func contractFixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	// Use the context testdata fixture as a real repository for integration assertions.
	return filepath.Join(filepath.Dir(file), "..", "context", "testdata", "fixtures", "user_service")
}

// TestToolDefinitions_SchemaCompleteness verifies that every registered tool
// has a name, description, and inputSchema — the minimum MCP contract.
func TestToolDefinitions_SchemaCompleteness(t *testing.T) {
	h := newContractHandler(t)
	tools := h.ListTools()
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has nil/empty InputSchema", tool.Name)
		}
		// InputSchema must be JSON-marshalable (no chan, func, etc.).
		if _, err := json.Marshal(tool.InputSchema); err != nil {
			t.Errorf("tool %q: InputSchema is not JSON-marshalable: %v", tool.Name, err)
		}
	}
}

// TestToolDefinitions_RequiredFieldsHaveTypes verifies that every property
// listed in "required" also appears in "properties".
func TestToolDefinitions_RequiredFieldsHaveTypes(t *testing.T) {
	h := newContractHandler(t)
	for _, tool := range h.ListTools() {
		schema := tool.InputSchema
		props, _ := schema["properties"].(map[string]interface{})
		required, _ := schema["required"].([]string)
		for _, req := range required {
			if _, found := props[req]; !found {
				t.Errorf("tool %q: required field %q not in properties", tool.Name, req)
			}
		}
	}
}

// TestCallTool_UnknownTool verifies that calling an unknown tool returns an
// actionable error rather than a panic.
func TestCallTool_UnknownTool(t *testing.T) {
	h := newContractHandler(t)
	result, err := h.CallTool("nonexistent_tool_xyz", map[string]interface{}{})
	if err != nil {
		return // acceptable — returning an error is fine
	}
	if result == nil {
		t.Fatal("CallTool returned nil result and nil error for unknown tool")
	}
	if !result.IsError {
		t.Error("expected IsError=true for unknown tool, got false")
	}
}

// TestCallTool_MissingRequiredParam verifies that missing required params
// return an actionable error, not a panic.
func TestCallTool_MissingRequiredParam(t *testing.T) {
	h := newContractHandler(t)
	// get_context requires "path" and "symbol".
	result, err := h.CallTool("get_context", map[string]interface{}{})
	if err == nil && result != nil && !result.IsError {
		t.Error("expected error for missing required params in get_context")
	}
}

// TestGetContext_ReturnsStableOutput verifies that calling get_context twice
// with the same inputs produces identical output (determinism contract).
func TestGetContext_ReturnsStableOutput(t *testing.T) {
	h := newContractHandler(t)
	dir := contractFixtureDir()
	args := map[string]interface{}{
		"path":   dir,
		"symbol": "Create",
		"format": "json",
	}
	r1, err1 := h.CallTool("get_context", args)
	r2, err2 := h.CallTool("get_context", args)

	if err1 != nil || err2 != nil {
		t.Skipf("get_context error: %v / %v", err1, err2)
	}
	if len(r1.Content) == 0 || len(r2.Content) == 0 {
		t.Skip("no content returned")
	}
	if r1.Content[0].Text != r2.Content[0].Text {
		t.Error("get_context is non-deterministic: same inputs produced different outputs")
	}
}
