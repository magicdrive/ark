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

func TestReadAndWriteAllFiles_PlaintextLineNumbers(t *testing.T) {
	rootDir := t.TempDir()

	code := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}`
	testFile := filepath.Join(rootDir, "main.go")
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	outFile := filepath.Join(rootDir, "output.txt")

	opt := &commandline.Option{
		OutputFormatValue:   "txt",
		IgnoreDotFileFlag:   model.OnOffSwitch("off"),
		MaskSecretsFlag:     model.OnOffSwitch("on"),
		WithLineNumberFlag:  model.OnOffSwitch("on"),
		ScanBuffer:          model.ByteString("1M"),
		SkipNonUTF8Flag:     false,
		AllowGitignoreFlag:  model.OnOffSwitch("on"),
		WorkingDir:          rootDir,
		PatternRegexpString: "",
		IncludeExtList:      []string{".go"},
		ExcludeDirList:      []string{},
		ExcludeExtList:      []string{},
	}

	var fileListMap = map[string]bool{}
	fileListMap[testFile] = true
	err := core.WriteAllFiles("mock tree string", rootDir, outFile, fileListMap, opt)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "=== "+testFile+" ===") {
		t.Errorf("Expected file header not found")
	}
	if !strings.Contains(content, "1: package main") {
		t.Errorf("Expected line number not present")
	}
}

func TestReadAndWriteAllFiles_CreatesOutputFile(t *testing.T) {
	dir := t.TempDir()
	dummyInput := filepath.Join(dir, "test.go")
	err := os.WriteFile(dummyInput, []byte("package main\nfunc main() {}\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	outputFile := filepath.Join(dir, "out.txt")
	opt := &commandline.Option{
		OutputFormatValue:  "txt",
		IgnoreDotFileFlag:  model.OnOffSwitch("on"),
		AllowGitignoreFlag: model.OnOffSwitch("on"),
		MaskSecretsFlag:    model.OnOffSwitch("on"),
		OutputFormat:       model.OutputFormat("plaintext"),
		WithLineNumberFlag: model.OnOffSwitch("off"),
		ScanBuffer:         model.ByteString("1M"),
		WorkingDir:         dir,
	}

	var fileListMap = map[string]bool{}
	fileListMap[dummyInput] = true
	err = core.WriteAllFiles("test.go", dir, outputFile, fileListMap, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("could not read output: %v", err)
	}
	if !strings.Contains(string(content), "=== "+dummyInput+" ===") {
		t.Errorf("expected file header not found in output:\n%s", content)
	}
}

func TestReadAndWriteAllFiles_MarkdownFormat(t *testing.T) {
	dir := t.TempDir()
	dummyInput := filepath.Join(dir, "test.go")
	err := os.WriteFile(dummyInput, []byte("package main\nfunc main() {}\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	outputFile := filepath.Join(dir, "out.md")
	opt := &commandline.Option{
		OutputFormatValue:  "md",
		OutputFormat:       model.OutputFormat("markdown"),
		MaskSecretsFlag:    model.OnOffSwitch("on"),
		WithLineNumberFlag: model.OnOffSwitch("off"),
		ScanBuffer:         model.ByteString("1M"),
		WorkingDir:         dir,
		IgnoreDotFileFlag:  model.OnOffSwitch("on"),
		AllowGitignoreFlag: model.OnOffSwitch("on"),
	}

	var fileListMap = map[string]bool{}
	fileListMap[dummyInput] = true
	err = core.WriteAllFiles("test.go", dir, outputFile, fileListMap, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("could not read output: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, "```go") {
		t.Errorf("expected markdown code block header not found in output:\n%s", text)
	}
	if !strings.Contains(text, "# File: "+dummyInput) {
		t.Errorf("expected markdown file header not found:\n%s", text)
	}
}

func TestReadAndWriteAllFiles_WithLineNumber(t *testing.T) {
	dir := t.TempDir()
	dummyInput := filepath.Join(dir, "main.txt")
	content := "line one\nline two\nline three\n"
	if err := os.WriteFile(dummyInput, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	outputFile := filepath.Join(dir, "output.txt")
	opt := &commandline.Option{
		OutputFormatValue:  "txt",
		MaskSecretsFlag:    model.OnOffSwitch("on"),
		OutputFormat:       model.OutputFormat("plaintext"),
		WithLineNumberFlag: model.OnOffSwitch("on"),
		ScanBuffer:         model.ByteString("1M"),
		WorkingDir:         dir,
		IgnoreDotFileFlag:  model.OnOffSwitch("on"),
		AllowGitignoreFlag: model.OnOffSwitch("on"),
	}

	var fileListMap = map[string]bool{}
	fileListMap[dummyInput] = true
	err := core.WriteAllFiles("main.txt", dir, outputFile, fileListMap, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("could not read output: %v", err)
	}
	outStr := string(outData)

	if !strings.Contains(outStr, "     1: line one") ||
		!strings.Contains(outStr, "     2: line two") ||
		!strings.Contains(outStr, "     3: line three") {
		t.Errorf("line numbers not correctly formatted:\n%s", outStr)
	}
}

func TestReadAndWriteAllFiles_MarkdownOutput(t *testing.T) {
	dir := t.TempDir()
	dummyFile := filepath.Join(dir, "test.go")
	content := "package main\nfunc main() {}\n"
	if err := os.WriteFile(dummyFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	outputFile := filepath.Join(dir, "output.md")
	opt := &commandline.Option{
		OutputFormatValue:  "markdown",
		OutputFormat:       model.OutputFormat("markdown"),
		MaskSecretsFlag:    model.OnOffSwitch("on"),
		WithLineNumberFlag: model.OnOffSwitch("off"),
		ScanBuffer:         model.ByteString("1M"),
		WorkingDir:         dir,
		IgnoreDotFileFlag:  model.OnOffSwitch("on"),
		AllowGitignoreFlag: model.OnOffSwitch("on"),
	}

	var fileListMap = map[string]bool{}
	fileListMap[dummyFile] = true
	err := core.WriteAllFiles("test.go", dir, outputFile, fileListMap, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("could not read markdown output: %v", err)
	}
	outStr := string(outData)

	if !strings.Contains(outStr, "# File:") || !strings.Contains(outStr, "```go") || !strings.Contains(outStr, "package main") {
		t.Errorf("markdown formatting not applied:\n%s", outStr)
	}
}

func TestReadAndWriteAllFiles_SkipNonUTF8File(t *testing.T) {
	dir := t.TempDir()
	// ISO-8859-1 encoded data
	data := []byte{0xff, 0xfe, 0xfd, 0x00, 0x41, 0x42, 0x43}
	nonUtf8File := filepath.Join(dir, "weird.txt")
	if err := os.WriteFile(nonUtf8File, data, 0644); err != nil {
		t.Fatal(err)
	}

	outputFile := filepath.Join(dir, "output.txt")
	opt := &commandline.Option{
		OutputFormatValue:  "txt",
		OutputFormat:       model.OutputFormat("plaintext"),
		SkipNonUTF8Flag:    true,
		MaskSecretsFlag:    model.OnOffSwitch("on"),
		WithLineNumberFlag: model.OnOffSwitch("off"),
		ScanBuffer:         model.ByteString("1M"),
		WorkingDir:         dir,
		IgnoreDotFileFlag:  model.OnOffSwitch("on"),
		AllowGitignoreFlag: model.OnOffSwitch("on"),
	}

	var fileListMap = map[string]bool{}
	fileListMap[nonUtf8File] = true
	err := core.WriteAllFiles("weird.txt", dir, outputFile, fileListMap, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("could not read output: %v", err)
	}
	if strings.Contains(string(outData), "ABC") {
		t.Errorf("Expected non-UTF8 file content to be skipped")
	}
}

func createTempDirWithTree(t *testing.T) (string, string, map[string]bool) {
	t.Helper()
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	os.Mkdir(subDir, 0755)

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}"), 0644)
	os.WriteFile(filepath.Join(subDir, "sub.txt"), []byte("hello world"), 0644)
	var fileListMap = map[string]bool{}
	fileListMap[filepath.Join(dir, "main.go")] = true
	fileListMap[filepath.Join(subDir, "sub.txt")] = true

	return dir, "example project tree", fileListMap
}

func TestReadAndWriteAllFiles_OutputFormatPlainText(t *testing.T) {
	dir, treeStr, fileListMap := createTempDirWithTree(t)
	outputFile := filepath.Join(t.TempDir(), "output.txt")

	opt := &commandline.Option{
		OutputFormat:       model.OutputFormat("plaintext"),
		WithLineNumberFlag: model.OnOffSwitch("off"),
		MaskSecretsFlag:    model.OnOffSwitch("on"),
		ScanBuffer:         model.ByteString("10M"),
		IgnoreDotFileFlag:  model.OnOffSwitch("on"),
		AllowGitignoreFlag: model.OnOffSwitch("on"),
	}

	err := core.WriteAllFiles(treeStr, dir, outputFile, fileListMap, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bytes, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	content := string(bytes)
	if !strings.Contains(content, "example project tree") {
		t.Errorf("expected tree string to be included")
	}
	if !strings.Contains(content, "=== ") {
		t.Errorf("expected file header to be included")
	}
	if !strings.Contains(content, "hello world") {
		t.Errorf("expected file content to be included")
	}
}
