package common

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func MergeAllowFileList(a, b map[string]bool) map[string]bool {
	merged := make(map[string]bool)
	for k, v := range a {
		if v {
			merged[k] = true
		}
	}
	for k, v := range b {
		if v {
			merged[k] = true
		}
	}
	return merged
}

func CommaSeparated2StringList(s string) []string {
	if s == "" {
		return nil
	}

	seen := make(map[string]struct{}, 16)
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			if _, exists := seen[trimmed]; !exists {
				seen[trimmed] = struct{}{}
				result = append(result, trimmed)
			}
		}
	}

	return result
}

func FindGitignore() (string, error) {

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		gitignorePath := filepath.Join(dir, ".gitignore")
		if _, err := os.Stat(gitignorePath); err == nil {
			return gitignorePath, nil
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			break
		}
		dir = parentDir
	}

	return "", errors.New("gitignore not found")
}

func FindArkignore() (string, error) {

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		arkignorePath := filepath.Join(dir, ".arkignore")
		if _, err := os.Stat(arkignorePath); err == nil {
			return arkignorePath, nil
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			break
		}
		dir = parentDir
	}

	return "", errors.New("arkignore not found")
}

func GetCurrentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func TrimDotSlash(path string) string {
	return strings.TrimPrefix(path, "./")
}
