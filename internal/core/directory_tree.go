package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/common"
)

func GenerateTreeString(path string, indent string, allowedFileListMap map[string]bool, opt *commandline.Option) (string, map[string]bool, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return "", nil, fmt.Errorf("Error reading directory %s: %v", path, err)
	}

	ApplySort(files)

	var b strings.Builder

	for i, file := range files {
		if opt.IgnoreDotFileFlag.Bool() && IsHiddenFile(file.Name()) {
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
				treeStr, fl, _ := GenerateTreeString(fullPath, indent+"    ", allowedFileListMap, opt)
				allowedFileListMap = common.MergeAllowFileList(fl, allowedFileListMap)
				b.WriteString(treeStr)
			} else {
				b.WriteString(indent)
				b.WriteString("├── ")
				b.WriteString(file.Name())
				b.WriteString("/\n")
				treeStr, fl, _ := GenerateTreeString(fullPath, indent+"│   ", allowedFileListMap, opt)
				allowedFileListMap = common.MergeAllowFileList(fl, allowedFileListMap)
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
			allowedFileListMap[fullPath] = true
		}

	}

	return b.String(), allowedFileListMap, nil
}
