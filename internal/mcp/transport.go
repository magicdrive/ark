package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// Transport represents the communication layer for MCP
type Transport interface {
	Start(handler RequestHandler) error
	Stop() error
}

// RequestHandler processes MCP requests and returns responses
type RequestHandler func(request *MCPRequest) *MCPResponse

// StdioTransport handles stdin/stdout communication
type StdioTransport struct{}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport() *StdioTransport {
	return &StdioTransport{}
}

// Start begins listening for requests on stdin
func (t *StdioTransport) Start(handler RequestHandler) error {
	log.Println("Starting MCP Server on stdin/stdout")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var request MCPRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			response := &MCPResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &MCPError{
					Code:    ErrorCodeParseError,
					Message: "Parse error",
					Data:    err.Error(),
				},
			}
			t.sendResponse(response)
			continue
		}

		response := handler(&request)
		if response != nil {
			t.sendResponse(response)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading from stdin: %v", err)
	}

	return nil
}

// Stop stops the stdio transport (no-op for stdio)
func (t *StdioTransport) Stop() error {
	return nil
}

// sendResponse sends a response to stdout
func (t *StdioTransport) sendResponse(response *MCPResponse) {
	responseBytes, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
		return
	}
	fmt.Println(string(responseBytes))
}

// HttpTransport handles HTTP communication
type HttpTransport struct {
	host   string
	port   string
	server *http.Server
}

// NewHttpTransport creates a new HTTP transport
func NewHttpTransport(host, port string) *HttpTransport {
	return &HttpTransport{
		host: host,
		port: port,
	}
}

// Start begins listening for HTTP requests
func (t *HttpTransport) Start(handler RequestHandler) error {
	mux := http.NewServeMux()

	// MCP JSON-RPC endpoint
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		t.handleMCPRequest(w, r, handler)
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","server":"ark-mcp-server"}`)
	})

	// API documentation endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		t.handleDocumentation(w, r)
	})

	addr := fmt.Sprintf("%s:%s", t.host, t.port)
	t.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Printf("Starting MCP Server on HTTP %s", addr)
	return t.server.ListenAndServe()
}

// Stop stops the HTTP server
func (t *HttpTransport) Stop() error {
	if t.server != nil {
		return t.server.Close()
	}
	return nil
}

// handleMCPRequest processes MCP requests over HTTP
func (t *HttpTransport) handleMCPRequest(w http.ResponseWriter, r *http.Request, handler RequestHandler) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response := &MCPResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &MCPError{
				Code:    ErrorCodeParseError,
				Message: "Parse error",
				Data:    err.Error(),
			},
		}
		t.sendHTTPResponse(w, response)
		return
	}

	response := handler(&request)
	t.sendHTTPResponse(w, response)
}

// sendHTTPResponse sends a JSON response over HTTP
func (t *HttpTransport) sendHTTPResponse(w http.ResponseWriter, response *MCPResponse) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleDocumentation serves API documentation
func (t *HttpTransport) handleDocumentation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Ark MCP Server</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .endpoint { background: #f5f5f5; padding: 10px; margin: 10px 0; border-radius: 4px; }
        .method { font-weight: bold; color: #0066cc; }
    </style>
</head>
<body>
    <h1>Ark MCP Server</h1>
    <p>Model Context Protocol (MCP) server for file and directory analysis.</p>
    
    <h2>Available Endpoints</h2>
    
    <div class="endpoint">
        <div class="method">POST /mcp</div>
        <p>Main MCP JSON-RPC endpoint. Send MCP requests here.</p>
        <p>Content-Type: application/json</p>
    </div>
    
    <div class="endpoint">
        <div class="method">GET /health</div>
        <p>Health check endpoint.</p>
    </div>
    
    <div class="endpoint">
        <div class="method">GET /</div>
        <p>This documentation page.</p>
    </div>
    
    <h2>Example MCP Request</h2>
    <pre>
{
  "jsonrpc": "2.0",
  "id": "1",
  "method": "tools/list",
  "params": {}
}
    </pre>
    
    <h2>Available Tools</h2>
    <ul>
        <li>get_directory_tree - Get directory structure as JSON</li>
        <li>get_file_content - Get content of a single file</li>
        <li>list_files - List files with filtering options</li>
        <li>search_in_files - Search for text within files</li>
        <li>get_file_info - Get file metadata</li>
        <li>get_project_stats - Get project statistics</li>
        <li>get_files_arklite - Get multiple files in arklite format</li>
    </ul>
</body>
</html>
`
	fmt.Fprint(w, html)
}
