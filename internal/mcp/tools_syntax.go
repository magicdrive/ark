package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/magicdrive/ark/internal/syntax"
)

// resolveToolPath normalises a path argument from an MCP tool call and enforces
// repository root containment.  Both relative and absolute inputs are accepted;
// both must resolve to a path that is contained within h.rootDir.
//
// The containment check uses filepath.Rel so that prefix collisions like
// /repo vs /repo-other are handled correctly.  String-prefix checks are
// intentionally not used.
func (h *ToolsHandler) resolveToolPath(path string) (fullPath, relPath string, err error) {
	var candidate string
	if filepath.IsAbs(path) {
		candidate = path
	} else {
		candidate = filepath.Join(h.rootDir, path)
	}
	candidate = filepath.Clean(candidate)

	rel, relErr := filepath.Rel(h.rootDir, candidate)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path %q is outside the server root %q; use a path inside the repository", path, h.rootDir)
	}
	return candidate, rel, nil
}

// SyntaxToolDefinitions returns tool definitions for syntax-related tools
func SyntaxToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_symbols",
			Description: "Extract code symbols (functions, classes, types, etc.) from a file using Tree-sitter parsing",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path to analyze",
					},
					"lang": map[string]interface{}{
						"type":        "string",
						"description": "Language override (go, typescript, tsx, javascript, python, php). Auto-detected if not specified.",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "find_symbol",
			Description: "Search for symbols matching a pattern across files in a directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name pattern to search for (supports regex)",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path to search in (default: current directory)",
					},
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Filter by symbol kind (function, method, class, interface, type, struct, constant, variable)",
					},
					"includeExt": map[string]interface{}{
						"type":        "string",
						"description": "Include only these extensions (comma-separated, e.g., 'go,ts,py')",
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     50,
					},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "get_symbol",
			Description: "Get detailed information about a specific symbol including its source code",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path containing the symbol",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name to find",
					},
					"includeSource": map[string]interface{}{
						"type":        "boolean",
						"description": "Include the source code of the symbol",
						"default":     true,
					},
				},
				"required": []string{"path", "name"},
			},
		},
	}
}

// getSymbols extracts symbols from a file
func (h *ToolsHandler) getSymbols(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath, path, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	// Read file
	source, err := os.ReadFile(fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error reading file: %v", err)}},
			IsError: true,
		}, nil
	}

	// Determine language
	var lang syntax.SupportedLanguage
	if langStr, ok := args["lang"].(string); ok && langStr != "" {
		lang = syntax.SupportedLanguage(langStr)
		if !syntax.IsSupported(lang) {
			return &CallToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Unsupported language: %s", langStr)}},
				IsError: true,
			}, nil
		}
	} else {
		lang = syntax.DetectLanguage(fullPath)
		if lang == syntax.LangUnknown {
			return &CallToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Could not detect language for: %s", path)}},
				IsError: true,
			}, nil
		}
	}

	// Parse and extract symbols
	result, err := syntax.Parse(lang, source)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Parse error: %v", err)}},
			IsError: true,
		}, nil
	}
	defer result.Release()

	symbols := syntax.ExtractSymbols(result)

	fileSymbols := &syntax.FileSymbols{
		Path:     path,
		Language: string(lang),
		Symbols:  symbols,
	}

	output, err := json.MarshalIndent(fileSymbols, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("JSON error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(output)}},
	}, nil
}

// findSymbol searches for symbols across files
func (h *ToolsHandler) findSymbol(args map[string]interface{}) (*CallToolResult, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("pattern parameter is required")
	}

	searchPath := "."
	if p, ok := args["path"].(string); ok && p != "" {
		searchPath = p
	}

	fullPath, _, err := h.resolveToolPath(searchPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	kindFilter := ""
	if k, ok := args["kind"].(string); ok {
		kindFilter = k
	}

	includeExt := ""
	if ext, ok := args["includeExt"].(string); ok {
		includeExt = ext
	}

	maxResults := 50
	if val, ok := args["maxResults"].(float64); ok {
		maxResults = int(val)
	}

	// Compile regex pattern
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Invalid regex pattern: %v", err)}},
			IsError: true,
		}, nil
	}

	// Parse include extensions
	var extFilter map[string]bool
	if includeExt != "" {
		extFilter = make(map[string]bool)
		for _, ext := range strings.Split(includeExt, ",") {
			ext = strings.TrimSpace(ext)
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extFilter[ext] = true
		}
	}

	var matches []syntax.SymbolMatch
	resultCount := 0
	errDone := fmt.Errorf("done")

	var statsScanned, statsSkipped, statsParseErr int

	err = filepath.Walk(fullPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if resultCount >= maxResults {
			return errDone
		}
		if info.IsDir() {
			return nil
		}

		ext := filepath.Ext(filePath)

		// Extension filter
		if extFilter != nil && !extFilter[ext] {
			statsSkipped++
			return nil
		}

		// Check if file is supported
		lang := syntax.DetectLanguage(filePath)
		if lang == syntax.LangUnknown {
			statsSkipped++
			return nil
		}

		// Read and parse file
		source, err := os.ReadFile(filePath)
		if err != nil {
			statsParseErr++
			return nil
		}

		parseResult, err := syntax.Parse(lang, source)
		if err != nil {
			statsParseErr++
			return nil
		}
		defer parseResult.Release()

		statsScanned++
		symbols := syntax.ExtractSymbols(parseResult)

		relPath, _ := filepath.Rel(h.rootDir, filePath)

		for _, sym := range symbols {
			if resultCount >= maxResults {
				break
			}

			// Kind filter
			if kindFilter != "" && string(sym.Kind) != kindFilter {
				continue
			}

			// Pattern match
			if regex.MatchString(sym.Name) {
				matches = append(matches, syntax.SymbolMatch{
					Path:   relPath,
					Symbol: sym,
				})
				resultCount++
			}
		}

		return nil
	})

	if err != nil && err != errDone {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Search error: %v", err)}},
			IsError: true,
		}, nil
	}

	type searchStats struct {
		FilesScanned int `json:"filesScanned"`
		FilesSkipped int `json:"filesSkipped"`
		ParseErrors  int `json:"parseErrors"`
	}
	type searchResult struct {
		Query   string               `json:"query"`
		Matches []syntax.SymbolMatch `json:"matches"`
		Stats   searchStats          `json:"stats"`
		Message string               `json:"message,omitempty"`
	}

	result := searchResult{
		Query:   pattern,
		Matches: matches,
		Stats: searchStats{
			FilesScanned: statsScanned,
			FilesSkipped: statsSkipped,
			ParseErrors:  statsParseErr,
		},
	}
	if len(matches) == 0 {
		msg := fmt.Sprintf("No symbols matching %q found", pattern)
		if statsScanned > 0 {
			msg += fmt.Sprintf(" in %d scanned file(s)", statsScanned)
		}
		if statsSkipped > 0 {
			msg += fmt.Sprintf(" (%d skipped: unsupported language or extension)", statsSkipped)
		}
		if statsParseErr > 0 {
			msg += fmt.Sprintf(", %d file(s) had parse errors", statsParseErr)
		}
		if statsScanned == 0 && statsParseErr == 0 {
			msg += fmt.Sprintf("; no supported files found under %q", searchPath)
		}
		result.Message = msg
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("JSON error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(output)}},
	}, nil
}

// getSymbol gets detailed information about a specific symbol
func (h *ToolsHandler) getSymbol(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	name, ok := args["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	includeSource := true
	if val, ok := args["includeSource"].(bool); ok {
		includeSource = val
	}

	fullPath, path, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	// Read file
	source, err := os.ReadFile(fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error reading file: %v", err)}},
			IsError: true,
		}, nil
	}

	// Detect language
	lang := syntax.DetectLanguage(fullPath)
	if lang == syntax.LangUnknown {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Could not detect language for: %s", path)}},
			IsError: true,
		}, nil
	}

	// Parse and extract symbols
	parseResult, err := syntax.Parse(lang, source)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Parse error: %v", err)}},
			IsError: true,
		}, nil
	}
	defer parseResult.Release()

	symbols := syntax.ExtractSymbols(parseResult)

	// Find the symbol
	var foundSymbol *syntax.Symbol
	for _, sym := range symbols {
		if sym.Name == name {
			foundSymbol = &sym
			break
		}
	}

	if foundSymbol == nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Symbol not found: %s", name)}},
			IsError: true,
		}, nil
	}

	// Build response
	response := map[string]interface{}{
		"path":      path,
		"language":  string(lang),
		"name":      foundSymbol.Name,
		"kind":      string(foundSymbol.Kind),
		"startLine": foundSymbol.StartLine,
		"endLine":   foundSymbol.EndLine,
		"startCol":  foundSymbol.StartCol,
		"endCol":    foundSymbol.EndCol,
		"exported":  foundSymbol.Exported,
	}

	if foundSymbol.Receiver != "" {
		response["receiver"] = foundSymbol.Receiver
	}
	if foundSymbol.Parent != "" {
		response["parent"] = foundSymbol.Parent
	}

	if includeSource {
		// Extract source code for the symbol
		startByte := int(foundSymbol.StartByte)
		endByte := int(foundSymbol.EndByte)
		if startByte >= 0 && endByte <= len(source) && startByte < endByte {
			response["source"] = string(source[startByte:endByte])
		}
	}

	output, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("JSON error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(output)}},
	}, nil
}
