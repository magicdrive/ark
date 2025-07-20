package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStdioTransport_NewStdioTransport(t *testing.T) {
	transport := NewStdioTransport()

	if transport == nil {
		t.Fatal("NewStdioTransport returned nil")
	}
}

func TestStdioTransport_Stop(t *testing.T) {
	transport := NewStdioTransport()

	err := transport.Stop()
	if err != nil {
		t.Errorf("Stop() returned error: %v", err)
	}
}

func TestHttpTransport_NewHttpTransport(t *testing.T) {
	transport := NewHttpTransport("localhost", "8080")

	if transport == nil {
		t.Fatal("NewHttpTransport returned nil")
	}

	if transport.host != "localhost" {
		t.Errorf("Expected host 'localhost', got '%s'", transport.host)
	}

	if transport.port != "8080" {
		t.Errorf("Expected port '8080', got '%s'", transport.port)
	}
}

func TestHttpTransport_Stop(t *testing.T) {
	transport := NewHttpTransport("localhost", "0") // port 0 for auto-assignment

	// Start server in background
	go func() {
		transport.Start(func(request *MCPRequest) *MCPResponse {
			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      request.ID,
				Result:  "test",
			}
		})
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	err := transport.Stop()
	if err != nil {
		t.Errorf("Stop() returned error: %v", err)
	}
}

func TestHttpTransport_HealthEndpoint(t *testing.T) {
	transport := NewHttpTransport("localhost", "8080")

	// Create a test handler
	handler := func(request *MCPRequest) *MCPResponse {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  "test",
		}
	}

	// Create test server using httptest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ok","server":"ark-mcp-server"}`)
		} else {
			transport.handleMCPRequest(w, r, handler)
		}
	}))
	defer server.Close()

	// Test health endpoint
	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expectedBody := `{"status":"ok","server":"ark-mcp-server"}`
	if strings.TrimSpace(string(body)) != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, string(body))
	}
}

func TestHttpTransport_MCPEndpoint(t *testing.T) {
	transport := NewHttpTransport("localhost", "8080")

	// Create a test handler
	handler := func(request *MCPRequest) *MCPResponse {
		if request.Method == "tools/list" {
			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      request.ID,
				Result: ListToolsResult{
					Tools: []Tool{
						{Name: "test_tool", Description: "Test tool"},
					},
				},
			}
		}
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeMethodNotFound,
				Message: "Method not found",
			},
		}
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transport.handleMCPRequest(w, r, handler)
	}))
	defer server.Close()

	// Test MCP request
	mcpRequest := MCPRequest{
		JSONRPC: "2.0",
		ID:      "test-id",
		Method:  "tools/list",
		Params:  map[string]interface{}{},
	}

	requestBody, err := json.Marshal(mcpRequest)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(server.URL, "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		t.Fatalf("MCP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var mcpResponse MCPResponse
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if mcpResponse.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC '2.0', got '%s'", mcpResponse.JSONRPC)
	}

	if mcpResponse.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got %v", mcpResponse.ID)
	}

	if mcpResponse.Error != nil {
		t.Errorf("Expected no error, got %v", mcpResponse.Error)
	}
}

func TestHttpTransport_InvalidJSONRequest(t *testing.T) {
	transport := NewHttpTransport("localhost", "8080")

	// Create a test handler
	handler := func(request *MCPRequest) *MCPResponse {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  "test",
		}
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transport.handleMCPRequest(w, r, handler)
	}))
	defer server.Close()

	// Send invalid JSON
	invalidJSON := `{"jsonrpc": "2.0", "id": "test", "method": incomplete`

	resp, err := http.Post(server.URL, "application/json", strings.NewReader(invalidJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	var mcpResponse MCPResponse
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if mcpResponse.Error == nil {
		t.Error("Expected error for invalid JSON")
	}

	if mcpResponse.Error.Code != ErrorCodeParseError {
		t.Errorf("Expected error code %d, got %d", ErrorCodeParseError, mcpResponse.Error.Code)
	}
}

func TestHttpTransport_MethodNotAllowed(t *testing.T) {
	transport := NewHttpTransport("localhost", "8080")

	// Create a test handler
	handler := func(request *MCPRequest) *MCPResponse {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  "test",
		}
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transport.handleMCPRequest(w, r, handler)
	}))
	defer server.Close()

	// Send GET request to MCP endpoint (should be POST only)
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHttpTransport_CORSHeaders(t *testing.T) {
	transport := NewHttpTransport("localhost", "8080")

	// Create a test handler
	handler := func(request *MCPRequest) *MCPResponse {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  "test",
		}
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transport.handleMCPRequest(w, r, handler)
	}))
	defer server.Close()

	// Send OPTIONS request
	req, err := http.NewRequest("OPTIONS", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", resp.StatusCode)
	}

	// Check CORS headers
	corsOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if corsOrigin != "*" {
		t.Errorf("Expected CORS origin '*', got '%s'", corsOrigin)
	}

	corsMethods := resp.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(corsMethods, "POST") {
		t.Errorf("Expected CORS methods to contain 'POST', got '%s'", corsMethods)
	}
}

func TestHttpTransport_DocumentationEndpoint(t *testing.T) {
	transport := NewHttpTransport("localhost", "8080")

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			transport.handleDocumentation(w, r)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Test documentation endpoint
	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("Documentation request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("Expected content type 'text/html', got '%s'", contentType)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Check for key elements in the documentation
	expectedElements := []string{
		"<title>Ark MCP Server</title>",
		"Model Context Protocol",
		"POST /mcp",
		"GET /health",
		"Available Tools",
	}

	for _, element := range expectedElements {
		if !strings.Contains(bodyStr, element) {
			t.Errorf("Documentation missing expected element: %s", element)
		}
	}
}

// Note: Direct testing of StdioTransport.Start() is complex due to stdin dependency,
// so we focus on testing the sendResponse method and interface compliance.

func TestStdioTransport_SendResponse(t *testing.T) {
	transport := NewStdioTransport()

	// Capture stdout
	originalStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	response := &MCPResponse{
		JSONRPC: "2.0",
		ID:      "test-id",
		Result:  "test-result",
	}

	// Send response
	transport.sendResponse(response)

	// Restore stdout and read captured output
	w.Close()
	os.Stdout = originalStdout

	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Failed to read captured output: %v", err)
	}

	// Parse the output as JSON
	var capturedResponse MCPResponse
	if err := json.Unmarshal(output, &capturedResponse); err != nil {
		t.Fatalf("Failed to parse captured output as JSON: %v", err)
	}

	if capturedResponse.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC '2.0', got '%s'", capturedResponse.JSONRPC)
	}

	if capturedResponse.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got %v", capturedResponse.ID)
	}

	if capturedResponse.Result != "test-result" {
		t.Errorf("Expected result 'test-result', got %v", capturedResponse.Result)
	}
}

func TestTransportInterface(t *testing.T) {
	// Test that both transports implement the Transport interface
	var _ Transport = &StdioTransport{}
	var _ Transport = &HttpTransport{}
}
