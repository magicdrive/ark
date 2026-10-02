package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
)

// ReferenceToolDefinitions returns tool definitions for reference-related tools.
func ReferenceToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "find_references",
			Description: "Find all syntactic references to a name within a file or directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File or directory path to search in",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name to search for (exact match)",
					},
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Filter by reference kind: call, type_use, import, construction, read, write, unknown",
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     100,
					},
				},
				"required": []string{"path", "name"},
			},
		},
	}
}

// referenceResult is the JSON-serialisable form of a single reference.
type referenceResult struct {
	ID           reference.ReferenceID `json:"id"`
	Name         string                `json:"name"`
	Kind         string                `json:"kind"`
	Language     string                `json:"language"`
	File         string                `json:"file"`
	StartLine    uint32                `json:"startLine"`
	StartColumn  uint32                `json:"startColumn"`
	Container    string                `json:"container,omitempty"`
	ReceiverExpr string                `json:"receiverExpr,omitempty"`
	IsCall       bool                  `json:"isCall,omitempty"`
}

// languageRegistry maps extensions to providers for reference extraction.
var refProviderRegistry = buildRefProviderRegistry()

func buildRefProviderRegistry() map[string]language.Provider {
	m := map[string]language.Provider{}
	for _, p := range []language.Provider{
		golang.NewProvider(),
		typescript.NewProvider(),
		typescript.NewTSXProvider(),
		javascript.NewProvider(),
		python.NewProvider(),
	} {
		for _, ext := range p.Extensions() {
			m[ext] = p
		}
	}
	return m
}

func (h *ToolsHandler) findReferences(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	name, ok := args["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	kindFilter := ""
	if k, ok := args["kind"].(string); ok {
		kindFilter = k
	}

	maxResults := 100
	if v, ok := args["maxResults"].(float64); ok {
		maxResults = int(v)
	}

	fullPath, relBase, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	var results []referenceResult

	info, err := os.Stat(fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	collect := func(filePath string) {
		if len(results) >= maxResults {
			return
		}
		ext := filepath.Ext(filePath)
		provider, ok := refProviderRegistry[ext]
		if !ok {
			return
		}
		src, err := os.ReadFile(filePath)
		if err != nil {
			return
		}
		relPath, _ := filepath.Rel(h.rootDir, filePath)
		fileID := source.FileID(relPath)
		ext2, err := provider.Extract(context.Background(), fileID, src)
		if err != nil {
			return
		}
		lang := string(provider.Language())
		for _, ref := range ext2.References {
			if len(results) >= maxResults {
				break
			}
			if ref.Name != name {
				continue
			}
			if kindFilter != "" && ref.Kind != kindFilter {
				continue
			}
			rid := reference.NewReferenceID(lang, fileID,
				reference.ReferenceKind(ref.Kind), ref.Name, ref.Location)
			results = append(results, referenceResult{
				ID:           rid,
				Name:         ref.Name,
				Kind:         ref.Kind,
				Language:     lang,
				File:         relPath,
				StartLine:    ref.Location.Range.Start.Line,
				StartColumn:  ref.Location.Range.Start.Column,
				Container:    ref.Container,
				ReceiverExpr: ref.ReceiverExpr,
				IsCall:       ref.IsCall,
			})
		}
	}

	if info.IsDir() {
		_ = filepath.Walk(fullPath, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			collect(p)
			return nil
		})
	} else {
		collect(fullPath)
	}

	type findRefsOutput struct {
		Path    string            `json:"path"`
		Name    string            `json:"name"`
		Results []referenceResult `json:"results"`
		Count   int               `json:"count"`
	}
	out := findRefsOutput{
		Path:    relBase,
		Name:    name,
		Results: results,
		Count:   len(results),
	}
	if out.Results == nil {
		out.Results = []referenceResult{}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("JSON error: %v", err)}},
			IsError: true,
		}, nil
	}
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(data)}},
	}, nil
}
