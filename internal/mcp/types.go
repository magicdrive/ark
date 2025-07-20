package mcp

// MCP JSON-RPC 2.0 message types

type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP Error Codes
const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
)

// MCP Protocol Types

type InitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    ClientCapabilities     `json:"capabilities"`
	ClientInfo      ClientInfo             `json:"clientInfo"`
	Meta            map[string]interface{} `json:"meta,omitempty"`
}

type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tools

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Resources

type ListResourcesResult struct {
	Resources []Resource `json:"resources"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ReadResourceParams struct {
	URI string `json:"uri"`
}

type ReadResourceResult struct {
	Contents []ResourceContent `json:"contents"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// Tool-specific parameter types

type GetDirectoryTreeParams struct {
	Path string `json:"path"`
}

type GetFileContentParams struct {
	Path            string `json:"path"`
	MaskSecrets     bool   `json:"maskSecrets,omitempty"`
	DeleteComments  bool   `json:"deleteComments,omitempty"`
	WithLineNumbers bool   `json:"withLineNumbers,omitempty"`
}

type ListFilesParams struct {
	Path             string `json:"path"`
	IncludeExt       string `json:"includeExt,omitempty"`
	ExcludeExt       string `json:"excludeExt,omitempty"`
	ExcludeDir       string `json:"excludeDir,omitempty"`
	PatternRegex     string `json:"patternRegex,omitempty"`
	ExcludeFileRegex string `json:"excludeFileRegex,omitempty"`
	ExcludeDirRegex  string `json:"excludeDirRegex,omitempty"`
	IgnoreDotfiles   bool   `json:"ignoreDotfiles,omitempty"`
	AllowGitignore   bool   `json:"allowGitignore,omitempty"`
	SkipNonUTF8      bool   `json:"skipNonUTF8,omitempty"`
}

type SearchInFilesParams struct {
	Path           string `json:"path"`
	Query          string `json:"query"`
	IsRegex        bool   `json:"isRegex,omitempty"`
	IncludeExt     string `json:"includeExt,omitempty"`
	ExcludeExt     string `json:"excludeExt,omitempty"`
	ExcludeDir     string `json:"excludeDir,omitempty"`
	IgnoreDotfiles bool   `json:"ignoreDotfiles,omitempty"`
	AllowGitignore bool   `json:"allowGitignore,omitempty"`
	MaxResults     int    `json:"maxResults,omitempty"`
}

type GetFileInfoParams struct {
	Path string `json:"path"`
}

type GetProjectStatsParams struct {
	Path           string `json:"path"`
	IgnoreDotfiles bool   `json:"ignoreDotfiles,omitempty"`
	AllowGitignore bool   `json:"allowGitignore,omitempty"`
}

type GetFilesArkliteParams struct {
	Paths          []string `json:"paths"`
	MaskSecrets    bool     `json:"maskSecrets,omitempty"`
	DeleteComments bool     `json:"deleteComments,omitempty"`
	MaxFiles       int      `json:"maxFiles,omitempty"`
}
