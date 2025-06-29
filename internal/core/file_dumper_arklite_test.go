package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/model"
)

func TestWriteAllFilesAsArklite(t *testing.T) {
	// Set up temp directory
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), `
package main

// hello world
func main() {
    println("Hi")
}`)

	// Set up tree string and allowed map
	treeStr := `{"name":"test","type":"directory","children":[{"name":"main.go","type":"file"}]}`
	allowed := map[string]bool{
		filepath.Join(root, "main.go"): true,
	}

	outputFile := filepath.Join(root, "output.arklite")

	opt := &commandline.Option{
		IgnoreDotFileFlag: model.OnOffSwitch("off"),
		MaskSecretsFlag:   model.OnOffSwitch("on"),
	}

	err := core.WriteAllFilesAsArklite(treeStr, root, outputFile, allowed, opt)
	if err != nil {
		t.Fatalf("WriteAllFilesAsArklite failed: %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}
	text := string(data)

	//t.Logf("Arklite output:\n%s", text)

	if !strings.Contains(text, "# Arklite Format Overview") {
		t.Error("Missing Arklite header")
	}
	if !strings.Contains(text, "## Directory Tree (JSON)") {
		t.Error("Missing Directory Tree section")
	}
	if !strings.Contains(text, "## File Dump") {
		t.Error("Missing File Dump section")
	}
	if !strings.Contains(text, "@main.go") {
		t.Error("Missing file dump entry for main.go")
	}
	if !strings.Contains(text, "package main") {
		t.Error("Expected compressed content to include 'package main'")
	}
	if !strings.Contains(text, "func main()") {
		t.Error("Expected compressed content to include 'func main()'")
	}
	if !strings.Contains(text, "println(\"Hi\")") {
		t.Error("Expected compressed content to include 'println(\"Hi\")'")
	}
	if !strings.Contains(text, "␤") {
		t.Error("Expected ␤ as newline token in compressed content")
	}
}

// helper
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
