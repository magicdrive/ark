package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/magicdrive/ark/internal/repomap"
)

// RepomapToolDefinitions returns the get_repository_map tool definition.
func RepomapToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_repository_map",
			Description: "Get a compact logical representation of the repository structure for LLM orientation",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Repository root path",
					},
					"detail": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"minimal", "normal", "verbose"},
						"description": "Level of detail: minimal (packages only), normal (top symbols), verbose (all exported symbols)",
						"default":     "normal",
					},
					"maxSymbols": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum symbols per package",
						"default":     10,
					},
					"maxPackages": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of packages to include",
						"default":     50,
					},
					"format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"text", "json"},
						"description": "Output format",
						"default":     "text",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (h *ToolsHandler) getRepositoryMap(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath := filepath.Join(h.rootDir, path)

	detail := repomap.DetailNormal
	if v, ok := args["detail"].(string); ok {
		switch v {
		case "minimal":
			detail = repomap.DetailMinimal
		case "verbose":
			detail = repomap.DetailVerbose
		}
	}

	maxSymbols := 10
	if v, ok := args["maxSymbols"].(float64); ok && v > 0 {
		maxSymbols = int(v)
	}
	maxPackages := 50
	if v, ok := args["maxPackages"].(float64); ok && v > 0 {
		maxPackages = int(v)
	}

	outputJSON := false
	if v, ok := args["format"].(string); ok && v == "json" {
		outputJSON = true
	}

	idx, err := h.buildIndex(context.Background(), fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error building index: %v", err)}},
			IsError: true,
		}, nil
	}

	opts := repomap.Options{
		DetailLevel: detail,
		MaxSymbols:  maxSymbols,
		MaxPackages: maxPackages,
	}
	rm := repomap.Build(idx, fullPath, opts)

	if outputJSON {
		b, err := json.MarshalIndent(rm, "", "  ")
		if err != nil {
			return nil, err
		}
		return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}}, nil
	}

	return &CallToolResult{Content: []Content{{Type: "text", Text: rm.Format()}}}, nil
}
