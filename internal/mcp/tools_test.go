package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func createTestToolsHandler(t *testing.T) *ToolsHandler {
	// Use the same approach as in the working server
	_, opt, err := commandline.GeneralOptParse([]string{"."})
	if err != nil {
		t.Fatalf("Failed to parse options: %v", err)
	}

	return NewToolsHandler(".", opt)
}

func TestNewToolsHandler(t *testing.T) {
	_, opt, err := commandline.GeneralOptParse([]string{"."})
	if err != nil {
		t.Fatalf("Failed to parse options: %v", err)
	}

	handler := NewToolsHandler("/test/path", opt)

	if handler == nil {
		t.Fatal("NewToolsHandler returned nil")
	}

	if handler.rootDir != "/test/path" {
		t.Errorf("Expected rootDir '/test/path', got '%s'", handler.rootDir)
	}

	if handler.opt != opt {
		t.Error("Option was not set correctly")
	}
}

func TestListTools(t *testing.T) {
	handler := createTestToolsHandler(t)
	tools := handler.ListTools()

	expectedTools := []string{
		"get_directory_tree",
		"get_file_content",
		"list_files",
		"search_in_files",
		"get_file_info",
		"get_project_stats",
		"get_files_arklite",
	}

	if len(tools) != len(expectedTools) {
		t.Errorf("Expected %d tools, got %d", len(expectedTools), len(tools))
	}

	for i, tool := range tools {
		if tool.Name != expectedTools[i] {
			t.Errorf("Expected tool name '%s', got '%s'", expectedTools[i], tool.Name)
		}

		if tool.Description == "" {
			t.Errorf("Tool '%s' has empty description", tool.Name)
		}

		if tool.InputSchema == nil {
			t.Errorf("Tool '%s' has nil InputSchema", tool.Name)
		}

		// Check that InputSchema has required properties
		schema := tool.InputSchema
		if schema["type"] != "object" {
			t.Errorf("Tool '%s' schema type is not 'object'", tool.Name)
		}

		properties, ok := schema["properties"]
		if !ok {
			t.Errorf("Tool '%s' schema missing 'properties'", tool.Name)
		}

		if properties == nil {
			t.Errorf("Tool '%s' schema 'properties' is nil", tool.Name)
		}
	}
}

func TestCallTool_UnknownTool(t *testing.T) {
	handler := createTestToolsHandler(t)

	result, err := handler.CallTool("unknown_tool", map[string]interface{}{})

	if err == nil {
		t.Error("Expected error for unknown tool")
	}

	if result != nil {
		t.Error("Expected nil result for unknown tool")
	}

	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("Expected 'unknown tool' in error message, got '%s'", err.Error())
	}
}

func TestGetDirectoryTree(t *testing.T) {
	handler := createTestToolsHandler(t)

	// Test with valid path
	args := map[string]interface{}{
		"path": ".",
	}

	result, err := handler.CallTool("get_directory_tree", args)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	if result.IsError {
		t.Errorf("Result indicates error: %s", result.Content[0].Text)
	}

	if len(result.Content) == 0 {
		t.Error("Result content is empty")
	}

	// Validate JSON structure
	var jsonData interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &jsonData); err != nil {
		t.Errorf("Result is not valid JSON: %v", err)
	}
}

func TestGetFileInfo(t *testing.T) {
	handler := createTestToolsHandler(t)

	// Create a test file in current directory
	testFile := "test_file_for_info.txt"
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	args := map[string]interface{}{
		"path": testFile,
	}

	result, err := handler.CallTool("get_file_info", args)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	if result.IsError {
		t.Errorf("Result indicates error: %s", result.Content[0].Text)
	}

	// Parse JSON result
	var fileInfo map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &fileInfo); err != nil {
		t.Errorf("Result is not valid JSON: %v", err)
	}

	// Check required fields
	requiredFields := []string{"path", "size", "modTime", "isDir", "language", "extension", "basename"}
	for _, field := range requiredFields {
		if _, exists := fileInfo[field]; !exists {
			t.Errorf("Missing required field '%s' in file info", field)
		}
	}

	if fileInfo["isDir"].(bool) {
		t.Error("Expected isDir to be false for file")
	}
}

func TestGetFileContent_MissingPath(t *testing.T) {
	handler := createTestToolsHandler(t)

	args := map[string]interface{}{}

	result, err := handler.CallTool("get_file_content", args)

	if err == nil {
		t.Error("Expected error for missing path parameter")
	}

	if result != nil {
		t.Error("Expected nil result for missing path parameter")
	}

	if !strings.Contains(err.Error(), "path parameter is required") {
		t.Errorf("Expected 'path parameter is required' in error message, got '%s'", err.Error())
	}
}

func TestSearchInFiles_MissingQuery(t *testing.T) {
	handler := createTestToolsHandler(t)

	args := map[string]interface{}{
		"path": ".",
	}

	result, err := handler.CallTool("search_in_files", args)

	if err == nil {
		t.Error("Expected error for missing query parameter")
	}

	if result != nil {
		t.Error("Expected nil result for missing query parameter")
	}

	if !strings.Contains(err.Error(), "query parameter is required") {
		t.Errorf("Expected 'query parameter is required' in error message, got '%s'", err.Error())
	}
}

func TestGetFilesArklite(t *testing.T) {
	handler := createTestToolsHandler(t)

	// Create temporary files for testing
	tmpDir, err := os.MkdirTemp("", "test_arklite_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	file1 := filepath.Join(tmpDir, "test1.txt")
	file2 := filepath.Join(tmpDir, "test2.txt")

	if err := os.WriteFile(file1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create test file 1: %v", err)
	}

	if err := os.WriteFile(file2, []byte("content2"), 0644); err != nil {
		t.Fatalf("Failed to create test file 2: %v", err)
	}

	// Get relative paths
	relPath1, _ := filepath.Rel(".", file1)
	relPath2, _ := filepath.Rel(".", file2)

	args := map[string]interface{}{
		"paths": []interface{}{relPath1, relPath2},
	}

	result, err := handler.CallTool("get_files_arklite", args)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	if result.IsError {
		t.Errorf("Result indicates error: %s", result.Content[0].Text)
	}

	if len(result.Content) == 0 {
		t.Error("Result content is empty")
	}

	// Check arklite format
	content := result.Content[0].Text
	if !strings.Contains(content, "# Arklite Format:") {
		t.Error("Result doesn't contain arklite header")
	}

	if !strings.Contains(content, "## File Dump") {
		t.Error("Result doesn't contain file dump section")
	}
}

func TestGetFilesArklite_InvalidPaths(t *testing.T) {
	handler := createTestToolsHandler(t)

	args := map[string]interface{}{
		"paths": "not_an_array",
	}

	result, err := handler.CallTool("get_files_arklite", args)

	if err == nil {
		t.Error("Expected error for invalid paths parameter")
	}

	if result != nil {
		t.Error("Expected nil result for invalid paths parameter")
	}

	if !strings.Contains(err.Error(), "paths must be an array") {
		t.Errorf("Expected 'paths must be an array' in error message, got '%s'", err.Error())
	}
}

func TestGetProjectStatsViaTool(t *testing.T) {
	handler := createTestToolsHandler(t)

	args := map[string]interface{}{
		"path": ".",
	}

	result, err := handler.CallTool("get_project_stats", args)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	if result.IsError {
		t.Errorf("Result indicates error: %s", result.Content[0].Text)
	}

	// Parse JSON result
	var stats map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &stats); err != nil {
		t.Errorf("Result is not valid JSON: %v", err)
	}

	// Check required fields
	requiredFields := []string{"totalFiles", "totalDirectories", "totalSize", "languageStats", "extensionStats"}
	for _, field := range requiredFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("Missing required field '%s' in project stats", field)
		}
	}
}
