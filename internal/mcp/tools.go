package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicdrive/ark/internal/commandline"
)

// ToolsHandler handles all MCP tools
type ToolsHandler struct {
	rootDir string
	opt     *commandline.Option
}

// NewToolsHandler creates a new tools handler
func NewToolsHandler(rootDir string, opt *commandline.Option) *ToolsHandler {
	return &ToolsHandler{
		rootDir: rootDir,
		opt:     opt,
	}
}

// ListTools returns all available tools
func (h *ToolsHandler) ListTools() []Tool {
	return []Tool{
		{
			Name:        "get_directory_tree",
			Description: "Get directory tree structure as JSON",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path to scan",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "get_file_content",
			Description: "Get content of a single file with optional filtering",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path to read",
					},
					"maskSecrets": map[string]interface{}{
						"type":        "boolean",
						"description": "Mask secrets in output",
						"default":     true,
					},
					"deleteComments": map[string]interface{}{
						"type":        "boolean",
						"description": "Remove code comments",
						"default":     false,
					},
					"withLineNumbers": map[string]interface{}{
						"type":        "boolean",
						"description": "Include line numbers",
						"default":     true,
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files in directory with filtering options",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path to scan",
					},
					"includeExt": map[string]interface{}{
						"type":        "string",
						"description": "Include only these extensions (comma-separated)",
					},
					"excludeExt": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these extensions (comma-separated)",
					},
					"excludeDir": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these directories (comma-separated)",
					},
					"patternRegex": map[string]interface{}{
						"type":        "string",
						"description": "Include files matching this regex pattern",
					},
					"excludeFileRegex": map[string]interface{}{
						"type":        "string",
						"description": "Exclude files matching this regex pattern",
					},
					"excludeDirRegex": map[string]interface{}{
						"type":        "string",
						"description": "Exclude directories matching this regex pattern",
					},
					"ignoreDotfiles": map[string]interface{}{
						"type":        "boolean",
						"description": "Ignore dotfiles",
						"default":     false,
					},
					"allowGitignore": map[string]interface{}{
						"type":        "boolean",
						"description": "Respect .gitignore rules",
						"default":     true,
					},
					"skipNonUTF8": map[string]interface{}{
						"type":        "boolean",
						"description": "Skip non-UTF8 files",
						"default":     false,
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "search_in_files",
			Description: "Search for text within files",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path to search in",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
					"isRegex": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat query as regex",
						"default":     false,
					},
					"includeExt": map[string]interface{}{
						"type":        "string",
						"description": "Include only these extensions (comma-separated)",
					},
					"excludeExt": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these extensions (comma-separated)",
					},
					"excludeDir": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these directories (comma-separated)",
					},
					"ignoreDotfiles": map[string]interface{}{
						"type":        "boolean",
						"description": "Ignore dotfiles",
						"default":     false,
					},
					"allowGitignore": map[string]interface{}{
						"type":        "boolean",
						"description": "Respect .gitignore rules",
						"default":     true,
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     100,
					},
				},
				"required": []string{"path", "query"},
			},
		},
		{
			Name:        "get_file_info",
			Description: "Get metadata information about a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path to analyze",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "get_project_stats",
			Description: "Get statistics about a project directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Project directory path",
					},
					"ignoreDotfiles": map[string]interface{}{
						"type":        "boolean",
						"description": "Ignore dotfiles",
						"default":     false,
					},
					"allowGitignore": map[string]interface{}{
						"type":        "boolean",
						"description": "Respect .gitignore rules",
						"default":     true,
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "get_files_arklite",
			Description: "Get multiple files in arklite format",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"paths": map[string]interface{}{
						"type":        "array",
						"description": "Array of file paths to include",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"maskSecrets": map[string]interface{}{
						"type":        "boolean",
						"description": "Mask secrets in output",
						"default":     true,
					},
					"deleteComments": map[string]interface{}{
						"type":        "boolean",
						"description": "Remove code comments",
						"default":     false,
					},
					"maxFiles": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of files to process",
						"default":     10,
					},
				},
				"required": []string{"paths"},
			},
		},
	}
}

// CallTool executes a specific tool
func (h *ToolsHandler) CallTool(name string, arguments map[string]interface{}) (*CallToolResult, error) {
	switch name {
	case "get_directory_tree":
		return h.getDirectoryTree(arguments)
	case "get_file_content":
		return h.getFileContent(arguments)
	case "list_files":
		return h.listFiles(arguments)
	case "search_in_files":
		return h.searchInFiles(arguments)
	case "get_file_info":
		return h.getFileInfo(arguments)
	case "get_project_stats":
		return h.getProjectStats(arguments)
	case "get_files_arklite":
		return h.getFilesArklite(arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *ToolsHandler) getDirectoryTree(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath := filepath.Join(h.rootDir, path)
	tree, err := GenerateDirectoryTreeJSON(fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: tree}},
	}, nil
}

func (h *ToolsHandler) getFileContent(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath := filepath.Join(h.rootDir, path)

	// Create option based on parameters
	opt := *h.opt // Copy base options
	if maskSecrets, ok := args["maskSecrets"].(bool); ok {
		if maskSecrets {
			opt.MaskSecretsFlagValue = "on"
		} else {
			opt.MaskSecretsFlagValue = "off"
		}
	}
	if deleteComments, ok := args["deleteComments"].(bool); ok {
		opt.DeleteCommentsFlag = deleteComments
	}
	if withLineNumbers, ok := args["withLineNumbers"].(bool); ok {
		if withLineNumbers {
			opt.WithLineNumberFlagValue = "on"
		} else {
			opt.WithLineNumberFlagValue = "off"
		}
	}

	content, err := ReadAndProcessFile(fullPath, &opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: content}},
	}, nil
}

func (h *ToolsHandler) listFiles(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath := filepath.Join(h.rootDir, path)

	// Create option based on parameters
	opt := *h.opt // Copy base options
	if includeExt, ok := args["includeExt"].(string); ok {
		opt.IncludeExt = includeExt
	}
	if excludeExt, ok := args["excludeExt"].(string); ok {
		opt.ExcludeExt = excludeExt
	}
	if excludeDir, ok := args["excludeDir"].(string); ok {
		opt.ExcludeDir = excludeDir
	}
	if patternRegex, ok := args["patternRegex"].(string); ok {
		opt.PatternRegexpString = patternRegex
	}
	if excludeFileRegex, ok := args["excludeFileRegex"].(string); ok {
		opt.ExcludeFileRegexpString = excludeFileRegex
	}
	if excludeDirRegex, ok := args["excludeDirRegex"].(string); ok {
		opt.ExcludeDirRegexpString = excludeDirRegex
	}
	if ignoreDotfiles, ok := args["ignoreDotfiles"].(bool); ok {
		if ignoreDotfiles {
			opt.IgnoreDotFileFlagValue = "on"
		} else {
			opt.IgnoreDotFileFlagValue = "off"
		}
	}
	if allowGitignore, ok := args["allowGitignore"].(bool); ok {
		if allowGitignore {
			opt.AllowGitignoreFlagValue = "on"
		} else {
			opt.AllowGitignoreFlagValue = "off"
		}
	}
	if skipNonUTF8, ok := args["skipNonUTF8"].(bool); ok {
		opt.SkipNonUTF8Flag = skipNonUTF8
	}

	files, err := ListFilteredFiles(fullPath, &opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	filesList := strings.Join(files, "\n")
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: filesList}},
	}, nil
}

func (h *ToolsHandler) searchInFiles(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	query, ok := args["query"].(string)
	if !ok {
		return nil, fmt.Errorf("query parameter is required")
	}

	fullPath := filepath.Join(h.rootDir, path)

	isRegex := false
	if val, ok := args["isRegex"].(bool); ok {
		isRegex = val
	}

	maxResults := 100
	if val, ok := args["maxResults"].(float64); ok {
		maxResults = int(val)
	}

	// Create option based on parameters
	opt := *h.opt // Copy base options
	if includeExt, ok := args["includeExt"].(string); ok {
		opt.IncludeExt = includeExt
	}
	if excludeExt, ok := args["excludeExt"].(string); ok {
		opt.ExcludeExt = excludeExt
	}
	if excludeDir, ok := args["excludeDir"].(string); ok {
		opt.ExcludeDir = excludeDir
	}
	if ignoreDotfiles, ok := args["ignoreDotfiles"].(bool); ok {
		if ignoreDotfiles {
			opt.IgnoreDotFileFlagValue = "on"
		} else {
			opt.IgnoreDotFileFlagValue = "off"
		}
	}
	if allowGitignore, ok := args["allowGitignore"].(bool); ok {
		if allowGitignore {
			opt.AllowGitignoreFlagValue = "on"
		} else {
			opt.AllowGitignoreFlagValue = "off"
		}
	}

	results, err := SearchInFiles(fullPath, query, isRegex, maxResults, &opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: results}},
	}, nil
}

func (h *ToolsHandler) getFileInfo(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath := filepath.Join(h.rootDir, path)

	info, err := os.Stat(fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	language := DetectLanguage(fullPath)

	fileInfo := map[string]interface{}{
		"path":      path,
		"size":      info.Size(),
		"modTime":   info.ModTime().Format(time.RFC3339),
		"isDir":     info.IsDir(),
		"language":  language,
		"extension": filepath.Ext(fullPath),
		"basename":  filepath.Base(fullPath),
	}

	infoJSON, err := json.MarshalIndent(fileInfo, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(infoJSON)}},
	}, nil
}

func (h *ToolsHandler) getProjectStats(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath := filepath.Join(h.rootDir, path)

	// Create option based on parameters
	opt := *h.opt // Copy base options
	if ignoreDotfiles, ok := args["ignoreDotfiles"].(bool); ok {
		if ignoreDotfiles {
			opt.IgnoreDotFileFlagValue = "on"
		} else {
			opt.IgnoreDotFileFlagValue = "off"
		}
	}
	if allowGitignore, ok := args["allowGitignore"].(bool); ok {
		if allowGitignore {
			opt.AllowGitignoreFlagValue = "on"
		} else {
			opt.AllowGitignoreFlagValue = "off"
		}
	}

	stats, err := GetProjectStats(fullPath, &opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	statsJSON, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(statsJSON)}},
	}, nil
}

func (h *ToolsHandler) getFilesArklite(args map[string]interface{}) (*CallToolResult, error) {
	pathsInterface, ok := args["paths"]
	if !ok {
		return nil, fmt.Errorf("paths parameter is required")
	}

	pathsSlice, ok := pathsInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("paths must be an array")
	}

	var paths []string
	for _, p := range pathsSlice {
		if pathStr, ok := p.(string); ok {
			paths = append(paths, pathStr)
		}
	}

	maxFiles := 10
	if val, ok := args["maxFiles"].(float64); ok {
		maxFiles = int(val)
	}

	if len(paths) > maxFiles {
		paths = paths[:maxFiles]
	}

	// Create option based on parameters
	opt := *h.opt // Copy base options
	opt.OutputFormatValue = "arklite"

	if maskSecrets, ok := args["maskSecrets"].(bool); ok {
		if maskSecrets {
			opt.MaskSecretsFlagValue = "on"
		} else {
			opt.MaskSecretsFlagValue = "off"
		}
	}
	if deleteComments, ok := args["deleteComments"].(bool); ok {
		opt.DeleteCommentsFlag = deleteComments
	}

	// Convert relative paths to absolute
	fullPaths := make([]string, len(paths))
	for i, path := range paths {
		fullPaths[i] = filepath.Join(h.rootDir, path)
	}

	content, err := GenerateArkliteForFiles(fullPaths, &opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: content}},
	}, nil
}
