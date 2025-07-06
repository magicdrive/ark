package mcp

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// HandleFile serves the content of a single file specified via query param "path"
func HandleFile(root string, allowedFiles []string) http.HandlerFunc {
	// build a map for faster lookup
	allowedMap := make(map[string]bool)
	for _, f := range allowedFiles {
		allowedMap[f] = true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		relPath := r.URL.Query().Get("path")
		if relPath == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "Missing 'path' query parameter")
			return
		}

		if !allowedMap[relPath] {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, "Access denied to file: %s", relPath)
			return
		}

		absPath := filepath.Join(root, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "File not found: %s", relPath)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, fmt.Sprintf("@%s\n", relPath))
		w.Write(data)
	}
}
