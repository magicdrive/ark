package mcp

import (
	"context"
	"fmt"

	arkctx "github.com/magicdrive/ark/internal/context"
)

// ContextToolDefinitions returns the get_context tool definition.
func ContextToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_context",
			Description: "Get token-budgeted, relevance-ranked source context for a symbol. Prefer this over reading files directly — it returns the code an LLM needs to understand or safely modify a symbol.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Repository root or directory path to index",
					},
					"symbolId": symbolIDProperty,
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name or qualified name to get context for (e.g. 'Create' or 'UserService.Create')",
					},
					"maxTokens": map[string]interface{}{
						"type":        "integer",
						"description": "Estimated token budget (len(text)/4 approximation); target is always included",
						"default":     8000,
					},
					"maxDepth": map[string]interface{}{
						"type":        "integer",
						"description": "Graph traversal depth for collecting related symbols",
						"default":     2,
					},
					"includeTests": map[string]interface{}{
						"type":        "boolean",
						"description": "Include test symbols in context",
						"default":     false,
					},
					"format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"text", "json"},
						"description": "Output format",
						"default":     "text",
					},
					"filePattern": map[string]interface{}{
						"type":        "string",
						"description": "Narrow an ambiguous symbol name by file-path substring",
					},
				},
				"required": []string{"path", "symbol"},
			},
		},
	}
}

func (h *ToolsHandler) getContext(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	symName, ok := args["symbol"].(string)
	if !ok {
		return nil, fmt.Errorf("symbol parameter is required")
	}

	maxTokens := 8000
	if v, ok := args["maxTokens"].(float64); ok {
		maxTokens = int(v)
	}
	maxDepth := 2
	if v, ok := args["maxDepth"].(float64); ok {
		maxDepth = int(v)
	}
	includeTests := false
	if v, ok := args["includeTests"].(bool); ok {
		includeTests = v
	}
	format := "text"
	if v, ok := args["format"].(string); ok {
		format = v
	}
	filePattern := ""
	if v, ok := args["filePattern"].(string); ok {
		filePattern = v
	}

	fullPath, rel, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	idx, err := h.buildIndex(context.Background(), fullPath)
	hookIndex()
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error building index: %v", err)}},
			IsError: true,
		}, nil
	}

	// Resolve the target symbol. Never silently pick among multiple viable
	// targets — ambiguity returns candidate evidence instead.
	tl, errRes := lookupToolTarget(idx, args, symName, filePattern, path)
	if errRes != nil {
		return errRes, nil
	}

	// Snippets are read through the request's pinned tree and policy
	// snapshot, not reopened by path (request_access.go).
	eng := arkctx.New(idx, fullPath).WithSourceReader(h.sourceReader(rel))
	result, err := eng.Build(context.Background(), arkctx.Request{
		Target:       tl.Symbol.ID,
		MaxTokens:    maxTokens,
		MaxDepth:     maxDepth,
		IncludeTests: includeTests,
	})
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error building context: %v", err)}},
			IsError: true,
		}, nil
	}

	var text string
	if format == "json" {
		b, err := arkctx.FormatJSON(result)
		if err != nil {
			return nil, err
		}
		text = string(b)
	} else {
		text = arkctx.Format(result)
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: text}},
	}, nil
}
