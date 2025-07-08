package mcp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/secrets"
)

// HandleFile serves the content of a single file specified via query param "path"
func HandleFile(root string, allowedFiles []string, opt *commandline.ServeOption) http.HandlerFunc {
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

		// is binary check.
		if core.IsBinary(data) {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			fmt.Fprintf(w, "Binary file not supported: %s", relPath)
			return
		}

		// utf8 flags
		if opt.GeneralOption.SkipNonUTF8Flag {
			converted, err := core.ConvertToUTF8(bytes.NewReader(data))
			if err != nil {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				fmt.Fprintf(w, "Non-UTF8 file skipped: %s", relPath)
				return
			}
			data, err = io.ReadAll(converted)
			if err != nil {
				fmt.Fprintf(w, "failed to read %s: %s", relPath, err.Error())
				return
			}
		}

		// --delete-comments
		if opt.GeneralOption.DeleteCommentsFlag {
			data = core.DeleteComments(data, relPath)
		}

		// --mask-secrets
		if opt.GeneralOption.MaskSecretsFlagValue == "on" {
			d := secrets.MaskAll(string(data))
			data = []byte(d)
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, fmt.Sprintf("@%s\n", relPath))
		w.Write(data)
	}
}
