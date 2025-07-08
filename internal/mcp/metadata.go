package mcp

import (
	"encoding/json"
	"net/http"
)

type EndpointMeta struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Method      string `json:"method"`
}

type Metadata struct {
	Name          string         `json:"name"`
	InterfaceType string         `json:"interfaceType"`
	Description   string         `json:"description"`
	Endpoints     []EndpointMeta `json:"endpoints"`
}

// HandleMetadata returns static info about available endpoints
func HandleMetadata() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meta := Metadata{
			Name:          "ark",
			InterfaceType: "rest",
			Description:   "Project file dumper + MCP server",
			Endpoints: []EndpointMeta{
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
