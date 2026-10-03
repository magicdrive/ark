package mcp

import (
	"encoding/json"

	"github.com/magicdrive/ark/internal/languages"
)

// LanguageSupportToolDefinitions returns the get_language_support tool definition.
func LanguageSupportToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_language_support",
			Description: "List all supported languages and their tested support levels (parse/symbols/references/resolution/graph/context_quality_certified).",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
	}
}

type languageSupportEntry struct {
	Language   string   `json:"language"`
	Level      string   `json:"level"`
	Extensions []string `json:"extensions"`
}

func (h *ToolsHandler) getLanguageSupport(_ map[string]interface{}) (*CallToolResult, error) {
	reg := languages.Registry()
	var entries []languageSupportEntry
	for _, d := range reg.Descriptors() {
		entries = append(entries, languageSupportEntry{
			Language:   string(d.Language),
			Level:      d.SupportLevel.String(),
			Extensions: reg.ExtensionsFor(d.Language),
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: "error: " + err.Error()}},
			IsError: true,
		}, nil
	}
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(data)}},
	}, nil
}
