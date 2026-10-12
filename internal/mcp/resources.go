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
	// policies is the masking policy cache, the server's tools' (NewMCPServer).
	policies *policyCache
}

// NewResourcesHandler creates a new resources handler
func NewResourcesHandler(rootDir string, opt *commandline.Option) *ResourcesHandler {
	return &ResourcesHandler{
		rootDir:  rootDir,
		opt:      opt,
		policies: newPolicyCache(),
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

// ReadResource reads a specific resource by URI, with the secrets in its
// text masked (sanitize.go) by the repository's policy (secret_policy.go):
// one request, whose file access and policy end with it
// (request_access.go).
func (h *ResourcesHandler) ReadResource(uri string) (*ReadResourceResult, error) {
	th := NewToolsHandler(h.rootDir, h.opt)
	th.policies = h.policies
	th, end := th.beginRequest()
	defer end()
	var result *ReadResourceResult
	var err error
	switch {
	case strings.HasPrefix(uri, "file://"):
		result, err = h.readFileResource(th, uri)
	case strings.HasPrefix(uri, "directory://"):
		result, err = h.readDirectoryResource(th, uri)
	default:
		return nil, fmt.Errorf("unsupported resource URI scheme: %s", uri)
	}
	if !masksByDefault(h.opt) {
		return result, err
	}
	p, perr := th.maskPolicy()
	if perr != nil {
		if result == nil {
			return nil, err
		}
		return nil, fmt.Errorf("secret masking configuration error: %v; nothing was returned: fix the configuration or remove it", perr)
	}
	return sanitizeResourceResultWith(result, p), err
}

func (h *ResourcesHandler) readFileResource(toolsHandler *ToolsHandler, uri string) (*ReadResourceResult, error) {
	// Extract path from file:// URI
	path := strings.TrimPrefix(uri, "file://")
	if path == "" {
		return nil, fmt.Errorf("file path is required")
	}

	// The tools handler of the resource's request reads the file. The path
	// is the URI's text after "file://", as is: nothing is percent-decoded.
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

func (h *ResourcesHandler) readDirectoryResource(toolsHandler *ToolsHandler, uri string) (*ReadResourceResult, error) {
	// Extract path from directory:// URI
	path := strings.TrimPrefix(uri, "directory://")
	if path == "" {
		path = "."
	}

	// The tools handler of the resource's request lists the directory.
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
