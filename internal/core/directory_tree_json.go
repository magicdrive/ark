package core

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/common"
)

type TreeEntry struct {
	Name     string       `json:"name"`
	Type     string       `json:"type"` // "file" or "directory"
	Children []*TreeEntry `json:"children,omitempty"`
}

func GenerateTreeJSONString(rootPath string, allowedFileMap map[string]bool, opt *commandline.Option) (string, map[string]bool, *TreeEntry, error) {
	tree, allowedFileMap, err := generateTreeJSON(rootPath, allowedFileMap, opt)
	if err != nil {
		return "", nil, nil, err
	}
	jsonBytes, _ := json.Marshal(tree)
	return string(jsonBytes), allowedFileMap, tree, nil

}

// GenerateTreeJSON generates a filtered directory tree in JSON format,
// along with the allowed file map used for output filtering.
func generateTreeJSON(path string, allowedFileMap map[string]bool, opt *commandline.Option) (*TreeEntry, map[string]bool, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, nil, err
	}

	// sort files in consistent order
	ApplySort(files)

	node := &TreeEntry{
		Name: filepath.Base(path),
		Type: "directory",
	}

	for _, file := range files {
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

		if file.IsDir() {
			childNode, childMap, err := generateTreeJSON(fullPath, allowedFileMap, opt)
			if err != nil {
				continue
			}
			node.Children = append(node.Children, childNode)
			allowedFileMap = common.MergeAllowFileList(childMap, allowedFileMap)
			allowedFileMap[fullPath] = true
		} else {
			node.Children = append(node.Children, &TreeEntry{
				Name: file.Name(),
				Type: "file",
			})
			allowedFileMap[fullPath] = true
		}
	}

	return node, allowedFileMap, nil
}
