package mcp

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/magicdrive/ark/internal/commandline"
)

// RunMCPServe starts the MCP server with the given root directory and options
func RunMCPServe(rootDir string, serverOpt *commandline.ServeOption) {
	server := NewMCPServer(rootDir, serverOpt)

	var transport Transport

	// Choose transport based on mode
	switch serverOpt.McpServerType.String() {
	case "http":
		transport = NewHttpTransport("localhost", serverOpt.HttpPort)
	case "stdio":
		fallthrough
	default:
		transport = NewStdioTransport()
	}

	// Create request handler
	handler := func(request *MCPRequest) *MCPResponse {
		return server.processRequest(request)
	}

	// Start the transport
	if err := transport.Start(handler); err != nil {
		log.Fatalf("Transport error: %v", err)
	}
}

// MCPServer represents the main MCP server
type MCPServer struct {
	rootDir   string
	serverOpt *commandline.ServeOption
	tools     *ToolsHandler
	resources *ResourcesHandler
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(rootDir string, serverOpt *commandline.ServeOption) *MCPServer {
	return &MCPServer{
		rootDir:   rootDir,
		serverOpt: serverOpt,
		tools:     NewToolsHandler(rootDir, serverOpt.GeneralOption),
		resources: NewResourcesHandler(rootDir, serverOpt.GeneralOption),
	}
}

// processRequest routes the request to the appropriate handler
func (s *MCPServer) processRequest(request *MCPRequest) *MCPResponse {
	switch request.Method {
	case "initialize":
		return s.handleInitialize(request)
	case "tools/list":
		return s.handleListTools(request)
	case "tools/call":
		return s.handleCallTool(request)
	case "resources/list":
		return s.handleListResources(request)
	case "resources/read":
		return s.handleReadResource(request)
	default:
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeMethodNotFound,
				Message: fmt.Sprintf("Method not found: %s", request.Method),
			},
		}
	}
}

// handleInitialize handles the MCP initialize request
func (s *MCPServer) handleInitialize(request *MCPRequest) *MCPResponse {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
			Resources: &ResourcesCapability{
				Subscribe:   false,
				ListChanged: false,
			},
		},
		ServerInfo: ServerInfo{
			Name:    "ark-mcp-server",
			Version: "0.1.0",
		},
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  result,
	}
}

// handleListTools handles the tools/list request
func (s *MCPServer) handleListTools(request *MCPRequest) *MCPResponse {
	tools := s.tools.ListTools()
	result := ListToolsResult{
		Tools: tools,
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  result,
	}
}

// handleCallTool handles the tools/call request
func (s *MCPServer) handleCallTool(request *MCPRequest) *MCPResponse {
	var params CallToolParams
	paramsBytes, err := json.Marshal(request.Params)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeInvalidParams,
				Message: "Invalid parameters",
				Data:    err.Error(),
			},
		}
	}

	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeInvalidParams,
				Message: "Invalid parameters",
				Data:    err.Error(),
			},
		}
	}

	result, err := s.tools.CallTool(params.Name, params.Arguments)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeInternalError,
				Message: "Tool execution error",
				Data:    err.Error(),
			},
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  result,
	}
}

// handleListResources handles the resources/list request
func (s *MCPServer) handleListResources(request *MCPRequest) *MCPResponse {
	resources := s.resources.ListResources()
	result := ListResourcesResult{
		Resources: resources,
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  result,
	}
}

// handleReadResource handles the resources/read request
func (s *MCPServer) handleReadResource(request *MCPRequest) *MCPResponse {
	var params ReadResourceParams
	paramsBytes, err := json.Marshal(request.Params)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeInvalidParams,
				Message: "Invalid parameters",
				Data:    err.Error(),
			},
		}
	}

	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeInvalidParams,
				Message: "Invalid parameters",
				Data:    err.Error(),
			},
		}
	}

	result, err := s.resources.ReadResource(params.URI)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: &MCPError{
				Code:    ErrorCodeInternalError,
				Message: "Resource read error",
				Data:    err.Error(),
			},
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  result,
	}
}
