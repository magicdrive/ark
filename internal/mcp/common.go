package mcp

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
)

func GenerateDirectoryStructure(path string, allowedFileList []string, opt *commandline.Option) ([]string, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("Error reading directory %s: %v", path, err)
	}

	for _, file := range files {
		if opt.IgnoreDotFileFlag.Bool() && core.IsHiddenFile(file.Name()) {
			continue
		}

		fullPath := filepath.Join(path, file.Name())

		if core.IsUnderGitDir(file.Name()) {
			continue
		}

		if !core.CanBoaded(opt, fullPath) {
			continue
		}

		if file.IsDir() {
			fl, _ := GenerateDirectoryStructure(fullPath, allowedFileList, opt)
			allowedFileList = append(allowedFileList, fl...)
		} else {
			allowedFileList = append(allowedFileList, fullPath)
		}

	}

	return allowedFileList, nil
}
