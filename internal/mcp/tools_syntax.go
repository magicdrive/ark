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
// repository root containment. It is the single path gate for every tool.
//
// A relative path is resolved against h.rootDir (absolute since construction,
// so never against the process CWD); an absolute path is taken as is. The
// result must be inside the root:
//
//   - lexically: filepath.Rel, so /repo-other is not inside /repo and ../ is
//     refused. String-prefix checks are intentionally not used.
//   - physically, when the path exists: its symlink-resolved form must be
//     inside the symlink-resolved root, so a symlink in the repository cannot
//     expose what lies outside it — unless the operator started the server
//     with --allow-external-symlinks on (access_policy.go), and then only
//     through a path that is itself inside the root. A symlink that stays
//     inside the root works.
//
// An absolute path that names an in-root location through another spelling of
// a symlinked root (macOS /var vs /private/var) is accepted and returned in the
// root's own spelling. The check is made when the request arrives; it is not a
// guarantee against the repository changing under a running request.
func (h *ToolsHandler) resolveToolPath(path string) (fullPath, relPath string, err error) {
	root := h.rootDir
	var candidate string
	if filepath.IsAbs(path) {
		candidate = path
	} else {
		candidate = filepath.Join(root, path)
	}
	candidate = filepath.Clean(candidate)

	rel, inside := relInside(root, candidate)
	if !inside {
		// The only way back in is an absolute path through another spelling
		// of the root, which can be seen only on the resolved forms.
		realRoot, rootErr := filepath.EvalSymlinks(root)
		real, realErr := filepath.EvalSymlinks(candidate)
		if !filepath.IsAbs(path) || rootErr != nil || realErr != nil {
			return "", "", outsideRootError(path, root)
		}
		if rel, inside = relInside(realRoot, real); !inside {
			return "", "", outsideRootError(path, root)
		}
		if policy := h.pathPolicy(rel); policy.excludesRel(rel) {
			return "", "", policy.excludedPathError(path)
		}
		canonical, err := h.canonicalCheck(path, rel)
		if err != nil {
			return "", "", err
		}
		return filepath.Join(root, canonical), canonical, nil
	}

	// The .arkignore access policy (access_policy.go): the path as named
	// and, below, the file a symlink leads to — each decided by the rule
	// files that can apply to it. The root itself is never excluded.
	if rel != "." {
		if policy := h.pathPolicy(rel); policy.excludesRel(rel) {
			return "", "", policy.excludedPathError(path)
		}
	}
	// The same, for the path as the file system names it; the tool is given
	// that path, so a walk from it names entries as the rules expect.
	canonical, err := h.canonicalCheck(path, rel)
	if err != nil {
		return "", "", err
	}
	if canonical != rel {
		candidate, rel = filepath.Join(root, canonical), canonical
	}

	// Physical containment. A path that does not resolve (missing, dangling)
	// is left to the tool, which reports it as not found; nothing is read
	// through it.
	if real, realErr := filepath.EvalSymlinks(candidate); realErr == nil {
		realRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			return "", "", outsideRootError(path, root)
		}
		realRel, ok := relInside(realRoot, real)
		if !ok {
			// Reached through a symlink in the repository (the path itself
			// is inside the root): readable only if the operator allowed it.
			if h.opt != nil && h.opt.AllowExternalSymlinks {
				return candidate, rel, nil
			}
			return "", "", fmt.Errorf("path %q resolves through a symlink to %q, outside the server root %q; use a path inside the repository", path, real, root)
		}
		if realRel != "." && realRel != rel {
			if policy := h.pathPolicy(realRel); policy.excludesRel(realRel) {
				return "", "", policy.excludedPathError(path)
			}
		}
	}
	return candidate, rel, nil
}

// canonicalCheck applies the access policy to rel (relative to the root) as
// the file system names it — on a case-insensitive file system "SECRET.TXT"
// is the file "secret.txt" — and to the symlink-free path it leads to, so
// named (access_policy.go). It returns the canonical path.
func (h *ToolsHandler) canonicalCheck(path, rel string) (string, error) {
	allowExternal := h.opt != nil && h.opt.AllowExternalSymlinks
	canonical, real, err := fsCanonicalPath(h.rootDir, rel, allowExternal)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, errNotCanonical)
	}
	for _, p := range []string{canonical, real} {
		if p == "" || p == "." || p == rel {
			continue
		}
		if policy := h.pathPolicy(p); policy.excludesRel(p) {
			return "", policy.excludedPathError(path)
		}
	}
	return canonical, nil
}

// relInside reports p relative to root and whether it is root or below it.
func relInside(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func outsideRootError(path, root string) error {
	return fmt.Errorf("path %q is outside the server root %q; use a path inside the repository", path, root)
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
						"description": "Language override (go, typescript, tsx, javascript, python, php, terraform). Auto-detected if not specified.",
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
						"description": "Maximum number of results. When more symbols match, the first maxResults (in path order) are returned with truncated: true",
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

	// Matches are collected in walk order (filepath.Walk visits entries in
	// lexical order) and symbol order within a file, so results are
	// deterministic. The walk stops at the first match beyond maxResults: that
	// match is not returned, but proves the result was truncated. Without it
	// the walk covered every file and the result is complete.
	var matches []syntax.SymbolMatch
	resultCount := 0
	truncated := false
	errDone := fmt.Errorf("done")

	var statsScanned, statsSkipped, statsParseErr int

	policy := h.accessPolicy()
	err = h.walkFrom(fullPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if truncated {
			return errDone
		}
		if skip, err := skipMetadata(fullPath, filePath, info); skip {
			return err
		}
		if filePath != fullPath && policy.excludesEntry(filePath, info.Mode()&os.ModeSymlink != 0) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
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
			// Kind filter
			if kindFilter != "" && string(sym.Kind) != kindFilter {
				continue
			}

			// Pattern match
			if regex.MatchString(sym.Name) {
				if resultCount >= maxResults {
					truncated = true
					break
				}
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
		// Truncated reports that more matches exist beyond Matches: the search
		// stopped at maxResults. False means Matches is every match.
		Truncated bool   `json:"truncated"`
		Message   string `json:"message,omitempty"`
	}

	result := searchResult{
		Query:     pattern,
		Matches:   matches,
		Truncated: truncated,
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

	// Find the symbol. A name that denotes several symbols of the file (e.g. two
	// classes that both define `save`) is ambiguity, never "the first one".
	picked, ambiguous := pickFileSymbol(symbols, name)
	if len(ambiguous) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "Ambiguous symbol %q in %s — %d matches found. Retry with one of the qualified names below:\n", name, path, len(ambiguous))
		for _, a := range ambiguous {
			fmt.Fprintf(&b, "  %s  (%s)  line %d\n", fileSymbolQualified(a), a.Kind, a.StartLine)
		}
		return &CallToolResult{Content: []Content{{Type: "text", Text: b.String()}}, IsError: true}, nil
	}
	if picked == nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Symbol not found: %s", name)}},
			IsError: true,
		}, nil
	}
	foundSymbol := picked

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

// fileSymbolQualified returns Owner.name for members and name otherwise.
func fileSymbolQualified(s syntax.Symbol) string {
	owner := s.Parent
	if owner == "" {
		owner = s.Receiver
	}
	if owner == "" {
		return s.Name
	}
	return owner + "." + s.Name
}

// pickFileSymbol selects the one symbol a (possibly qualified) name denotes in
// a file. A qualified match ("UserService.create") wins over a bare-name
// match; when the best tier has several symbols they are returned as
// ambiguous and nothing is picked.
func pickFileSymbol(symbols []syntax.Symbol, name string) (picked *syntax.Symbol, ambiguous []syntax.Symbol) {
	var byQualified, byName []syntax.Symbol
	for _, s := range symbols {
		if fileSymbolQualified(s) == name {
			byQualified = append(byQualified, s)
		}
		if s.Name == name {
			byName = append(byName, s)
		}
	}
	for _, tier := range [][]syntax.Symbol{byQualified, byName} {
		switch len(tier) {
		case 0:
			continue
		case 1:
			return &tier[0], nil
		default:
			return nil, tier
		}
	}
	return nil, nil
}
