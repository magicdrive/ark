package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SearchResult struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// HandleSearch allows simple plain-text search over allowed files
func HandleSearch(root string, allowedFiles []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "Missing 'q' query parameter")
			return
		}

		if strings.ContainsAny(query, `^$.*+?[]()|\\`) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "Only plain text (non-regex) search supported")
			return
		}

		results := []SearchResult{}
		sort.Strings(allowedFiles)
		for _, rel := range allowedFiles {
			abs := filepath.Join(root, rel)
			data, err := os.ReadFile(abs)
			if err != nil {
				continue
			}

			if !bytes.Contains(data, []byte(query)) {
				continue // fast skip
			}

			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if strings.Contains(line, query) {
					results = append(results, SearchResult{
						Path:    rel,
						Line:    i + 1,
						Snippet: line,
					})
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}
