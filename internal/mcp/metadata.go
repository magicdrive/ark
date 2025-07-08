package mcp

import (
	"encoding/json"
	"net/http"
)

type Capability struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Method      string `json:"method"`
}

type ServerInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Metadata struct {
	ServerInfo   ServerInfo   `json:"serverInfo"`
	Capabilities []Capability `json:"capabilities"`
}

// HandleMetadata returns static info about available endpoints
func HandleMetadata() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meta := Metadata{
			ServerInfo: ServerInfo{
				Name:        "ark",
				Version:     "v1.0.0",
				Description: "Project file dumper + MCP server",
			},
			Capabilities: []Capability{
				{Path: "/mcp/metadata", Method: "GET", Description: "Show this metadata"},
				{Path: "/mcp/chunks", Method: "GET", Description: "List file chunks based on token estimate"},
				{Path: "/mcp/chunk/:id", Method: "GET", Description: "Get text for specific chunk"},
				{Path: "/mcp/file", Method: "GET", Description: "Get content of single file by relative path"},
				{Path: "/mcp/structure.json", Method: "GET", Description: "Hierarchical file structure as JSON"},
				{Path: "/mcp/search", Method: "GET", Description: "Search files with plain text query"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}
}
