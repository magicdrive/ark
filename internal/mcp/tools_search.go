package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/magicdrive/ark/internal/search"
	"github.com/magicdrive/ark/internal/symbol"
)

// SearchToolDefinitions returns the search_code tool definition.
func SearchToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "search_code",
			Description: "Search for symbols using structural predicates: kind, name pattern, call relationships, type usage, and file filters. Predicates are combined with AND semantics. Results list at most maxResults matches; the first line always states the total. With groupBy, return counts instead of matches: e.g. kind=resource with groupBy=resourceType counts Terraform resources by type, groupBy=directory by module (a Terraform module is a directory), groupBy=resourceType,directory both; namePattern=aws_rds narrows to types whose name contains it (a name filter, not a cloud-service classification). Counts come from the index; nothing is re-parsed.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Repository root or directory path",
					},
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Symbol kind filter: function, method, class, struct, interface, type, constant, variable, ...; Terraform: resource (managed and ephemeral), data_source, module (a module call), output, variable (var.* and local.*), configuration (provider, terraform, check); Rust: impl (an impl block), macro (a macro_rules! macro); HTML: document (a file), element (an element with an id); CSS: stylesheet, keyframes, font_face, property_registration, custom_property, css_class (a CSS Modules class); JavaScript: script (a file); YAML: document (a file), key (a mapping key, by its JSON Pointer path), anchor; GitHub Actions: workflow (a workflow file), trigger (an event of on), job, step, action (an action, by its directory); asset (an image, a font or a media file web documents link to)",
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
						"description": "Language filter: go, typescript, tsx, javascript, python, php, terraform, rust, java, html, css, zig, yaml (get_language_support lists them)",
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
						"description": "Maximum number of matches listed (the total is always reported; narrow with namePattern, filePattern or kind when it is larger). With groupBy: maximum number of group lines, at most 1000",
						"default":     50,
					},
					"groupBy": map[string]interface{}{
						"type":        "string",
						"description": "Count the matches instead of listing them, by one or two comma-separated keys: kind, language, directory, file, resourceType (Terraform resource type, with data sources as data.TYPE and ephemeral resources as ephemeral.TYPE; other symbols count as (none)). Groups are ordered by count, largest first",
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

	var groupKeys []search.GroupKey
	switch v := args["groupBy"].(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			keys, err := search.ParseGroupKeys(v)
			if err != nil {
				return &CallToolResult{Content: []Content{{Type: "text", Text: err.Error()}}, IsError: true}, nil
			}
			groupKeys = keys
		}
	case []interface{}:
		var names []string
		for _, k := range v {
			s, ok := k.(string)
			if !ok {
				return &CallToolResult{Content: []Content{{Type: "text", Text: "groupBy: keys must be strings"}}, IsError: true}, nil
			}
			names = append(names, s)
		}
		keys, err := search.ParseGroupKeys(strings.Join(names, ","))
		if err != nil {
			return &CallToolResult{Content: []Content{{Type: "text", Text: err.Error()}}, IsError: true}, nil
		}
		groupKeys = keys
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

	if groupKeys != nil {
		grouped, err := search.GroupCount(context.Background(), idx, q, groupKeys, maxResults)
		if err != nil {
			return &CallToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error searching: %v", err)}},
				IsError: true,
			}, nil
		}
		text := search.FormatGroups(grouped, q)
		if format == "json" {
			b, err := json.MarshalIndent(grouped, "", "  ")
			if err != nil {
				return nil, err
			}
			text = string(b)
		}
		return &CallToolResult{Content: []Content{{Type: "text", Text: text}}}, nil
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
