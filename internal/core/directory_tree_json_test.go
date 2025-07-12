package core_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/model"
)

func TestGenerateTreeJSONString(t *testing.T) {
	// Setup temporary directory structure:
	root := t.TempDir()

	// Create structure:
	// root/
	// ├── main.go
	// └── utils/
	//     └── helper.py

	mustWriteFile(t, filepath.Join(root, "main.go"), `package main`)
	utilsDir := filepath.Join(root, "utils")
	mustMkdir(t, utilsDir)
	mustWriteFile(t, filepath.Join(utilsDir, "helper.py"), `print("hi")`)

	opt := &commandline.Option{
		IgnoreDotFileFlag: model.OnOffSwitch("off"),
	}
	allowed := make(map[string]bool)

	jsonStr, fileMap, _, err := core.GenerateTreeJSONString(root, allowed, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if jsonStr == "" {
		t.Fatalf("expected non-empty JSON output")
	}

	// check that JSON can be parsed
	var rootNode core.TreeEntry
	if err := json.Unmarshal([]byte(jsonStr), &rootNode); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// basic structure checks
	if rootNode.Type != "directory" {
		t.Errorf("expected root type to be directory, got %q", rootNode.Type)
	}

	childNames := collectNames(rootNode.Children)
	if !contains(childNames, "main.go") || !contains(childNames, "utils") {
		t.Errorf("expected children to include main.go and utils, got %v", childNames)
	}

	// allowedFileMap should include full paths
	mainPath := filepath.Join(root, "main.go")
	helperPath := filepath.Join(root, "utils", "helper.py")
	if !fileMap[mainPath] {
		t.Errorf("main.go not found in allowedFileMap")
	}
	if !fileMap[helperPath] {
		t.Errorf("helper.py not found in allowedFileMap")
	}
}

// Helpers
func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", path, err)
	}
}

func collectNames(children []*core.TreeEntry) []string {
	var names []string
	for _, c := range children {
		names = append(names, c.Name)
	}
	return names
}

func contains(list []string, target string) bool {
	if slices.Contains(list, target) {
		return true
	}
	return false
}
