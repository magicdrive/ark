package mcp

import (
	"fmt"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
)

// ResourcesHandler handles MCP resources
type ResourcesHandler struct {
	rootDir string
	opt     *commandline.Option
}

// NewResourcesHandler creates a new resources handler
func NewResourcesHandler(rootDir string, opt *commandline.Option) *ResourcesHandler {
	return &ResourcesHandler{
		rootDir: rootDir,
		opt:     opt,
	}
}

// ListResources returns all available resources
func (h *ResourcesHandler) ListResources() []Resource {
	return []Resource{
		{
			URI:         "file://",
			Name:        "File Access",
			Description: "Access individual files using file:// scheme",
			MimeType:    "text/plain",
		},
		{
			URI:         "directory://",
			Name:        "Directory Access",
			Description: "Access directory information using directory:// scheme",
			MimeType:    "application/json",
		},
	}
}

// ReadResource reads a specific resource by URI
func (h *ResourcesHandler) ReadResource(uri string) (*ReadResourceResult, error) {
	if strings.HasPrefix(uri, "file://") {
		return h.readFileResource(uri)
	} else if strings.HasPrefix(uri, "directory://") {
		return h.readDirectoryResource(uri)
	}

	return nil, fmt.Errorf("unsupported resource URI scheme: %s", uri)
}

func (h *ResourcesHandler) readFileResource(uri string) (*ReadResourceResult, error) {
	// Extract path from file:// URI
	path := strings.TrimPrefix(uri, "file://")
	if path == "" {
		return nil, fmt.Errorf("file path is required")
	}

	// Use tools handler to get file content
	toolsHandler := NewToolsHandler(h.rootDir, h.opt)
	args := map[string]interface{}{
		"path": path,
	}

	result, err := toolsHandler.getFileContent(args)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("error reading file: %s", result.Content[0].Text)
	}

	return &ReadResourceResult{
		Contents: []ResourceContent{
			{
				URI:      uri,
				MimeType: "text/plain",
				Text:     result.Content[0].Text,
			},
		},
	}, nil
}

func (h *ResourcesHandler) readDirectoryResource(uri string) (*ReadResourceResult, error) {
	// Extract path from directory:// URI
	path := strings.TrimPrefix(uri, "directory://")
	if path == "" {
		path = "."
	}

	// Use tools handler to get directory tree
	toolsHandler := NewToolsHandler(h.rootDir, h.opt)
	args := map[string]interface{}{
		"path": path,
	}

	result, err := toolsHandler.getDirectoryTree(args)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("error reading directory: %s", result.Content[0].Text)
	}

	return &ReadResourceResult{
		Contents: []ResourceContent{
			{
				URI:      uri,
				MimeType: "application/json",
				Text:     result.Content[0].Text,
			},
		},
	}, nil
}
