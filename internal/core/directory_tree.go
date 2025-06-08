package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func isHiddenFile(name string) bool {
	return strings.HasPrefix(name, ".")
}

func GenerateTreeString(path string, indent string) (string, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("Error reading directory %s: %v", path, err)
	}

	ApplySort(files)
	var b strings.Builder

	for i, file := range files {
		if isHiddenFile(file.Name()) {
			continue
		}

		fullPath := filepath.Join(path, file.Name())
		isLastItem := i == len(files)-1

		if file.IsDir() {
			if isLastItem {
				b.WriteString(indent)
				b.WriteString("└── ")
				b.WriteString(file.Name())
				b.WriteString("/\n")
				treeStr, _ := GenerateTreeString(fullPath, indent+"    ")
				b.WriteString(treeStr)
			} else {
				b.WriteString(indent)
				b.WriteString("├── ")
				b.WriteString(file.Name())
				b.WriteString("/\n")
				treeStr, _ := GenerateTreeString(fullPath, indent+"│   ")
				b.WriteString(treeStr)
			}
		} else {
			b.WriteString(indent)
			if isLastItem {
				b.WriteString("└── ")
			} else {
				b.WriteString("├── ")
			}
			b.WriteString(file.Name())
			b.WriteString("\n")
		}
	}

	return b.String(), nil
}
