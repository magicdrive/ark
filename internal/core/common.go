package core

import (
	"path/filepath"
	"slices"
	"strings"
)

func IsHiddenFile(name string) bool {
	return strings.HasPrefix(name, ".")
}

func IsUnderGitDir(path string) bool {
	absPath := filepath.Clean(path)
	parts := strings.Split(absPath, string(filepath.Separator))
	if slices.Contains(parts, ".git") {
		return true
	}
	return false
}
