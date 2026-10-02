package mcp

import (
	"context"
	"fmt"

	"github.com/magicdrive/ark/internal/search"
	"github.com/magicdrive/ark/internal/symbol"
)

// SearchToolDefinitions returns the search_code tool definition.
func SearchToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "search_code",
			Description: "Search for symbols using structural predicates: kind, name pattern, call relationships, type usage, and file filters. Predicates are combined with AND semantics.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Repository root or directory path",
					},
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Symbol kind filter (function/method/struct/interface/class/type/constant/variable/...)",
					},
					"namePattern": map[string]interface{}{
						"type":        "string",
						"description": "Name substring filter (case-insensitive)",
					},
					"exported": map[string]interface{}{
						"type":        "boolean",
						"description": "Filter by exported/public status",
					},
					"callsName": map[string]interface{}{
						"type":        "string",
						"description": "Only symbols that contain a call to a function/method whose name contains this string",
					},
					"usesType": map[string]interface{}{
						"type":        "string",
						"description": "Only symbols that reference a type whose name contains this string",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "Language filter (go/typescript/javascript/python)",
					},
					"filePattern": map[string]interface{}{
						"type":        "string",
						"description": "File path substring filter (case-insensitive)",
					},
					"excludeTest": map[string]interface{}{
						"type":        "boolean",
						"description": "Exclude test files and testdata directories (default true)",
						"default":     true,
					},
					"excludeGenerated": map[string]interface{}{
						"type":        "boolean",
						"description": "Exclude generated/vendor files",
						"default":     false,
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     50,
					},
					"format": map[string]interface{}{
						"type":    "string",
						"enum":    []string{"text", "json"},
						"default": "text",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (h *ToolsHandler) searchCode(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	q := search.Query{}

	if v, ok := args["kind"].(string); ok && v != "" {
		q.Kind = symbol.SymbolKind(v)
	}
	if v, ok := args["namePattern"].(string); ok {
		q.NamePattern = v
	}
	if v, ok := args["exported"].(bool); ok {
		q.Exported = &v
	}
	if v, ok := args["callsName"].(string); ok {
		q.CallsName = v
	}
	if v, ok := args["usesType"].(string); ok {
		q.UsesType = v
	}
	if v, ok := args["language"].(string); ok {
		q.Language = v
	}
	if v, ok := args["filePattern"].(string); ok {
		q.FilePattern = v
	}
	// Default true: LLMs exploring production code rarely want test/testdata results.
	q.ExcludeTest = true
	if v, ok := args["excludeTest"].(bool); ok {
		q.ExcludeTest = v
	}
	if v, ok := args["excludeGenerated"].(bool); ok {
		q.ExcludeGenerated = v
	}

	maxResults := 50
	if v, ok := args["maxResults"].(float64); ok {
		maxResults = int(v)
	}

	format := "text"
	if v, ok := args["format"].(string); ok {
		format = v
	}

	fullPath, _, pathErr := h.resolveToolPath(path)
	if pathErr != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: pathErr.Error()}},
			IsError: true,
		}, nil
	}

	idx, err := h.buildIndex(context.Background(), fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error building index: %v", err)}},
			IsError: true,
		}, nil
	}

	result, err := search.Search(context.Background(), idx, q, maxResults)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error searching: %v", err)}},
			IsError: true,
		}, nil
	}

	var text string
	if format == "json" {
		b, err := search.FormatJSON(result)
		if err != nil {
			return nil, err
		}
		text = string(b)
	} else {
		text = search.Format(result, q)
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: text}},
	}, nil
}
