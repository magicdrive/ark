package common

import (
	"encoding/json"
	"os"
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

func ToJSON[T any](v T) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func FromJSON[T any](s string) (T, error) {
	var out T
	err := json.Unmarshal([]byte(s), &out)
	return out, err
}
