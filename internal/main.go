package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func isHiddenFile(name string) bool {
	return strings.HasPrefix(name, ".")
}

func printTree(path string, indent string) error {
	files, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("Error reading directory %s: %v", path, err)
	}

	ApplySort(files)

	for i, file := range files {
		if isHiddenFile(file.Name()) {
			continue
		}

		fullPath := filepath.Join(path, file.Name())
		isLastItem := i == len(files)-1

		if file.IsDir() {
			if isLastItem {
				fmt.Println(indent + "└── " + file.Name() + "/")
				printTree(fullPath, indent+"    ")
			} else {
				fmt.Println(indent + "├── " + file.Name() + "/")
				printTree(fullPath, indent+"│   ")
			}
		} else {
			if isLastItem {
				fmt.Println(indent + "└── " + file.Name())
			} else {
				fmt.Println(indent + "├── " + file.Name())
			}
		}
	}

	return nil
}

func main() {
	rootDir := "hoge"

	fmt.Println(rootDir + "/")

	err := printTree(rootDir, "")
	if err != nil {
		fmt.Println("Error:", err)
	}
}
