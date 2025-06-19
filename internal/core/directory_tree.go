package core

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
)

func isHiddenFile(name string) bool {
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

func GenerateTreeString(path string, indent string, opt *commandline.Option) (string, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("Error reading directory %s: %v", path, err)
	}

	ApplySort(files)

	var b strings.Builder

	for i, file := range files {
		if opt.IgnoreDotFileFlag.Bool() && isHiddenFile(file.Name()) {
			continue
		}

		fullPath := filepath.Join(path, file.Name())

		if IsUnderGitDir(file.Name()) {
			continue
		}

		if !CanBoaded(opt, fullPath) {
			continue
		}

		isLastItem := i == len(files)-1

		if file.IsDir() {
			if isLastItem {
				b.WriteString(indent)
				b.WriteString("└── ")
				b.WriteString(file.Name())
				b.WriteString("/\n")
				treeStr, _ := GenerateTreeString(fullPath, indent+"    ", opt)
				b.WriteString(treeStr)
			} else {
				b.WriteString(indent)
				b.WriteString("├── ")
				b.WriteString(file.Name())
				b.WriteString("/\n")
				treeStr, _ := GenerateTreeString(fullPath, indent+"│   ", opt)
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
