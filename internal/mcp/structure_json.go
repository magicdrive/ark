package mcp

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

type FileNode struct {
	Name     string     `json:"name"`
	Type     string     `json:"type"` // "file" or "dir"
	Children []FileNode `json:"children,omitempty"`
}

// buildTree builds a nested file tree from a flat list of paths
func buildTree(paths []string) FileNode {
	sort.Strings(paths)
	root := FileNode{Name: "root", Type: "dir"}
	nodes := map[string]*FileNode{"": &root}

	for _, rel := range paths {
		parts := strings.Split(rel, string(filepath.Separator))
		parentPath := ""
		for i := 0; i < len(parts); i++ {
			seg := parts[i]
			curPath := filepath.Join(parentPath, seg)
			if _, exists := nodes[curPath]; !exists {
				n := &FileNode{Name: seg}
				if i == len(parts)-1 {
					n.Type = "file"
				} else {
					n.Type = "dir"
					n.Children = []FileNode{}
				}
				nodes[curPath] = n
				nodes[parentPath].Children = append(nodes[parentPath].Children, *n)
			}
			parentPath = curPath
		}
	}
	return root
}

// HandleStructureJSON serves the project structure as a hierarchical JSON tree
func HandleStructureJSON(allowedFiles []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := buildTree(allowedFiles)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tree)
	}
}
