package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func createTestServerOption() *commandline.ServeOption {
	opt := &commandline.Option{
		WorkingDir:                      ".",
		TargetDirname:                   ".",
		OutputFilename:                  "test-output.txt",
		ScanBufferValue:                 "10M",
		AllowGitignoreFlagValue:         "on",
		IgnoreDotFileFlagValue:          "off",
		MaskSecretsFlagValue:            "on",
		SkipNonUTF8Flag:                 false,
		DeleteCommentsFlag:              false,
		WithLineNumberFlagValue:         "on",
		OutputFormatValue:               "plaintext",
		PatternRegexpString:             "",
		IncludeExt:                      "",
		ExcludeExt:                      "",
		ExcludeDir:                      "",
		ExcludeDirRegexpString:          "",
		ExcludeFileRegexpString:         "",
		AdditionallyIgnoreRuleFilenames: "",
	}
	opt.Normalize()

	return &commandline.ServeOption{
		RootDir:            ".",
		HttpPort:           "8522",
		McpServerTypeValue: "stdio",
		GeneralOption:      opt,
	}
}

func TestNewMCPServer(t *testing.T) {
	serverOpt := createTestServerOption()
	server := NewMCPServer(serverOpt.RootDir, serverOpt)

	if server == nil {
		t.Fatal("NewMCPServer returned nil")
	}

	if server.rootDir != "." {
		t.Errorf("Expected rootDir '.', got '%s'", server.rootDir)
	}

	if server.serverOpt != serverOpt {
		t.Error("ServerOpt was not set correctly")
	}

	if server.tools == nil {
		t.Error("Tools handler was not initialized")
	}

	if server.resources == nil {
		t.Error("Resources handler was not initialized")
	}
}

func TestHandleInitialize(t *testing.T) {
	serverOpt := createTestServerOption()
	server := NewMCPServer(serverOpt.RootDir, serverOpt)

	request := &MCPRequest{
		JSONRPC: "2.0",
		ID:      "test-id",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}

	response := server.handleInitialize(request)

	if response == nil {
		t.Fatal("handleInitialize returned nil")
	}

	if response.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC '2.0', got '%s'", response.JSONRPC)
	}

	if response.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got %v", response.ID)
	}

	if response.Error != nil {
		t.Errorf("Expected no error, got %v", response.Error)
	}

	// Check if result is InitializeResult
	result, ok := response.Result.(InitializeResult)
	if !ok {
		t.Fatal("Result is not InitializeResult")
	}

	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("Expected protocol version '2024-11-05', got '%s'", result.ProtocolVersion)
	}

	if result.ServerInfo.Name != "ark-mcp-server" {
		t.Errorf("Expected server name 'ark-mcp-server', got '%s'", result.ServerInfo.Name)
	}

	if result.ServerInfo.Version != "0.1.0" {
		t.Errorf("Expected server version '0.1.0', got '%s'", result.ServerInfo.Version)
	}
}

func TestHandleListTools(t *testing.T) {
	serverOpt := createTestServerOption()
	server := NewMCPServer(serverOpt.RootDir, serverOpt)

	request := &MCPRequest{
		JSONRPC: "2.0",
		ID:      "test-id",
		Method:  "tools/list",
	}

	response := server.handleListTools(request)

	if response == nil {
		t.Fatal("handleListTools returned nil")
	}

	if response.Error != nil {
		t.Errorf("Expected no error, got %v", response.Error)
	}

	// Check if result is ListToolsResult
	result, ok := response.Result.(ListToolsResult)
	if !ok {
		t.Fatal("Result is not ListToolsResult")
	}

	expectedTools := []string{
		"get_directory_tree",
		"get_file_content",
		"list_files",
		"search_in_files",
		"get_file_info",
		"get_project_stats",
		"get_files_arklite",
	}

	if len(result.Tools) != len(expectedTools) {
		t.Errorf("Expected %d tools, got %d", len(expectedTools), len(result.Tools))
	}

	for i, tool := range result.Tools {
		if tool.Name != expectedTools[i] {
			t.Errorf("Expected tool name '%s', got '%s'", expectedTools[i], tool.Name)
		}

		if tool.Description == "" {
			t.Errorf("Tool '%s' has empty description", tool.Name)
		}

		if tool.InputSchema == nil {
			t.Errorf("Tool '%s' has nil InputSchema", tool.Name)
		}
	}
}

func TestHandleRequest_InvalidJSON(t *testing.T) {
	serverOpt := createTestServerOption()
	server := NewMCPServer(serverOpt.RootDir, serverOpt)

	// Test invalid JSON through processRequest with a malformed request
	request := &MCPRequest{
		JSONRPC: "2.0",
		ID:      "test",
		Method:  "invalid",
	}

	response := server.processRequest(request)

	if response == nil {
		t.Fatal("processRequest returned nil")
	}

	if response.Error == nil {
		t.Error("Expected error for invalid method")
	}

	if response.Error.Code != ErrorCodeMethodNotFound {
		t.Errorf("Expected error code %d, got %d", ErrorCodeMethodNotFound, response.Error.Code)
	}
}

func TestHandleRequest_UnknownMethod(t *testing.T) {
	serverOpt := createTestServerOption()
	server := NewMCPServer(serverOpt.RootDir, serverOpt)

	request := &MCPRequest{
		JSONRPC: "2.0",
		ID:      "test-id",
		Method:  "unknown/method",
	}

	response := server.processRequest(request)

	if response == nil {
		t.Fatal("processRequest returned nil")
	}

	if response.Error == nil {
		t.Error("Expected error for unknown method")
	}

	if response.Error.Code != ErrorCodeMethodNotFound {
		t.Errorf("Expected error code %d, got %d", ErrorCodeMethodNotFound, response.Error.Code)
	}

	if !strings.Contains(response.Error.Message, "Method not found") {
		t.Errorf("Expected 'Method not found' in error message, got '%s'", response.Error.Message)
	}
}

func TestJSONSerialization(t *testing.T) {
	// Test MCPRequest serialization
	request := MCPRequest{
		JSONRPC: "2.0",
		ID:      "test-id",
		Method:  "test/method",
		Params: map[string]interface{}{
			"param1": "value1",
			"param2": 42,
		},
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal MCPRequest: %v", err)
	}

	var unmarshaled MCPRequest
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal MCPRequest: %v", err)
	}

	if unmarshaled.JSONRPC != request.JSONRPC {
		t.Errorf("JSONRPC mismatch: expected '%s', got '%s'", request.JSONRPC, unmarshaled.JSONRPC)
	}

	if unmarshaled.Method != request.Method {
		t.Errorf("Method mismatch: expected '%s', got '%s'", request.Method, unmarshaled.Method)
	}

	// Test MCPResponse serialization
	response := MCPResponse{
		JSONRPC: "2.0",
		ID:      "test-id",
		Result:  "test-result",
	}

	data, err = json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal MCPResponse: %v", err)
	}

	var unmarshaledResponse MCPResponse
	if err := json.Unmarshal(data, &unmarshaledResponse); err != nil {
		t.Fatalf("Failed to unmarshal MCPResponse: %v", err)
	}

	if unmarshaledResponse.JSONRPC != response.JSONRPC {
		t.Errorf("JSONRPC mismatch: expected '%s', got '%s'", response.JSONRPC, unmarshaledResponse.JSONRPC)
	}
}
