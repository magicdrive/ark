package mcp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// maxMessageBytes bounds one inbound JSON-RPC message: a stdio line or an HTTP
// request body.
const maxMessageBytes = 8 << 20

// Transport represents the communication layer for MCP
type Transport interface {
	Start(handler RequestHandler) error
	Stop() error
}

// RequestHandler processes MCP requests and returns responses. It is called
// only for messages classified as requests (see message.go); notifications and
// client responses never reach it.
type RequestHandler func(request *MCPRequest) *MCPResponse

// StdioTransport handles stdin/stdout communication
type StdioTransport struct {
	onNotification NotificationHandler

	// in / out default to os.Stdin / os.Stdout (resolved when used). They exist
	// so the real read-classify-write loop can be driven from tests.
	in  io.Reader
	out io.Writer
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport() *StdioTransport {
	return &StdioTransport{}
}

// WithNotificationHandler sets the receiver of JSON-RPC notifications. Without
// one, notifications are accepted and ignored. In either case a notification
// never produces a response.
func (t *StdioTransport) WithNotificationHandler(h NotificationHandler) *StdioTransport {
	t.onNotification = h
	return t
}

// Start begins listening for requests on stdin
func (t *StdioTransport) Start(handler RequestHandler) error {
	log.Println("Starting MCP Server on stdin/stdout")

	in := t.in
	if in == nil {
		in = os.Stdin
	}
	rt := router{request: handler, notification: t.onNotification}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if response := rt.route([]byte(line)); response != nil {
			if err := t.sendResponse(response); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("error reading from stdin: message exceeds %d bytes", maxMessageBytes)
		}
		return fmt.Errorf("error reading from stdin: %v", err)
	}

	return nil
}

// Stop stops the stdio transport (no-op for stdio)
func (t *StdioTransport) Stop() error {
	return nil
}

// sendResponse sends a response to stdout. It fails only when not even a
// -32603 fallback response can be produced; the transport then stops.
func (t *StdioTransport) sendResponse(response *MCPResponse) error {
	responseBytes, err := encodeResponse(response)
	if err != nil {
		return err
	}
	out := t.out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintln(out, string(responseBytes))
	return nil
}

// HttpTransport handles HTTP communication
type HttpTransport struct {
	host   string
	port   string
	mu     sync.Mutex
	server *http.Server

	onNotification NotificationHandler
}

// NewHttpTransport creates a new HTTP transport
func NewHttpTransport(host, port string) *HttpTransport {
	return &HttpTransport{
		host: host,
		port: port,
	}
}

// WithNotificationHandler sets the receiver of JSON-RPC notifications. Without
// one, notifications are accepted and ignored. In either case a notification
// never produces a JSON-RPC response (the HTTP reply is 202 with no body).
func (t *HttpTransport) WithNotificationHandler(h NotificationHandler) *HttpTransport {
	t.onNotification = h
	return t
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
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	t.mu.Lock()
	t.server = srv
	t.mu.Unlock()

	log.Printf("Starting MCP Server on HTTP %s", addr)
	return srv.ListenAndServe()
}

// Stop stops the HTTP server
func (t *HttpTransport) Stop() error {
	t.mu.Lock()
	srv := t.server
	t.mu.Unlock()
	if srv != nil {
		return srv.Close()
	}
	return nil
}

// handleMCPRequest processes MCP requests over HTTP
func (t *HttpTransport) handleMCPRequest(w http.ResponseWriter, r *http.Request, handler RequestHandler) {
	// The endpoint is for local clients. Host and Origin are checked first so a
	// web page cannot reach it through DNS rebinding or a cross-origin fetch. No
	// Access-Control-Allow-Origin is sent: browsers get no cross-origin access.
	if !isLoopbackHost(r.Host) {
		http.Error(w, "Forbidden: invalid Host", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin) {
		http.Error(w, "Forbidden: invalid Origin", http.StatusForbidden)
		return
	}

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

	// A simple cross-origin form post cannot carry application/json.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	// The same classification as stdio. A message that must not be answered
	// (notification, client response) is accepted with 202 and no body.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMessageBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Request entity too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	response := router{request: handler, notification: t.onNotification}.route(body)
	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	t.sendHTTPResponse(w, response)
}

// sendHTTPResponse sends a JSON response over HTTP
func (t *HttpTransport) sendHTTPResponse(w http.ResponseWriter, response *MCPResponse) {
	body, err := encodeResponse(response)
	if err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(body, '\n'))
}

// isLoopbackName reports whether a bare host name is localhost or a loopback
// IP literal.
func isLoopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackHost validates a Host header value ("host" or "host:port").
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return isLoopbackName(host)
}

// isLoopbackOrigin validates an Origin header value: an http(s) origin whose
// host is loopback. "null" and anything unparsable are rejected.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return false
	}
	return isLoopbackName(u.Hostname())
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
