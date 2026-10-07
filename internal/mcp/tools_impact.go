package mcp

import (
	"context"
	"fmt"

	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/impact"
)

// ImpactToolDefinitions returns the analyze_change_impact tool definition.
func ImpactToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "analyze_change_impact",
			Description: "Analyze the likely impact of changing a symbol. Returns dependents categorized by confidence level. Heuristic results are labelled 'possible_dependent' and are NOT guaranteed impacts.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Repository root or directory path",
					},
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name or qualified name to analyze (e.g. 'Save' or 'Repository.Save')",
					},
					"maxDepth": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum transitive depth to follow",
						"default":     3,
					},
					"format": map[string]interface{}{
						"type":    "string",
						"enum":    []string{"text", "json"},
						"default": "text",
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

func (h *ToolsHandler) analyzeChangeImpact(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	symName, ok := args["symbol"].(string)
	if !ok {
		return nil, fmt.Errorf("symbol parameter is required")
	}

	maxDepth := 3
	if v, ok := args["maxDepth"].(float64); ok {
		maxDepth = int(v)
	}
	format := "text"
	if v, ok := args["format"].(string); ok {
		format = v
	}
	filePattern := ""
	if v, ok := args["filePattern"].(string); ok {
		filePattern = v
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

	// Resolve the target symbol. Impact analysis MUST NOT run on an arbitrary
	// first candidate: a false target produces a plausible but wrong report.
	tl := resolveTarget(targetCandidatesFromIndex(idx, symName), filePattern)
	if !tl.Found {
		return targetNotFoundResult(symName, path), nil
	}
	if tl.Ambiguous {
		return ambiguousTargetResult(symName, tl.Candidates), nil
	}

	g := graph.New(idx)
	result, err := impact.Analyze(context.Background(), idx, g, tl.Symbol.ID, maxDepth)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error analyzing impact: %v", err)}},
			IsError: true,
		}, nil
	}
	if result == nil {
		return targetNotFoundResult(symName, path), nil
	}

	var text string
	if format == "json" {
		b, err := impact.FormatJSON(result)
		if err != nil {
			return nil, err
		}
		text = string(b)
	} else {
		text = impact.Format(result)
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: text}},
	}, nil
}
