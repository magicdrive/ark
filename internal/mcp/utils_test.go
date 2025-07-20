package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func createTestOption() *commandline.Option {
	_, opt, err := commandline.GeneralOptParse([]string{"."})
	if err != nil {
		panic(err)
	}
	return opt
}

func TestGenerateDirectoryTreeJSON(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir, err := os.MkdirTemp("", "test_tree_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files and directories
	testDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	testFile1 := filepath.Join(tmpDir, "file1.txt")
	testFile2 := filepath.Join(testDir, "file2.go")

	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create test file 1: %v", err)
	}

	if err := os.WriteFile(testFile2, []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to create test file 2: %v", err)
	}

	// Test GenerateDirectoryTreeJSON
	jsonStr, err := GenerateDirectoryTreeJSON(tmpDir)
	if err != nil {
		t.Fatalf("GenerateDirectoryTreeJSON failed: %v", err)
	}

	// Validate JSON structure
	var tree interface{}
	if err := json.Unmarshal([]byte(jsonStr), &tree); err != nil {
		t.Fatalf("Result is not valid JSON: %v", err)
	}

	// Check that it contains expected structure
	if !strings.Contains(jsonStr, "file1.txt") {
		t.Error("JSON doesn't contain file1.txt")
	}

	if !strings.Contains(jsonStr, "subdir") {
		t.Error("JSON doesn't contain subdir")
	}
}

func TestDetectLanguage(t *testing.T) {
	testCases := []struct {
		filename string
		expected string
	}{
		{"test.go", "go"},
		{"test.js", "javascript"},
		{"test.py", "python"},
		{"test.rs", "rust"},
		{"test.html", "html"},
		{"test.css", "css"},
		{"test.json", "json"},
		{"test.md", "markdown"},
		{"test.txt", "text"},
		{"Dockerfile", "dockerfile"},
		{"Makefile", "makefile"},
		{"test.unknown", ""},
		{"no_extension", ""},
	}

	for _, tc := range testCases {
		result := DetectLanguage(tc.filename)
		if result != tc.expected {
			t.Errorf("DetectLanguage(%s): expected '%s', got '%s'", tc.filename, tc.expected, result)
		}
	}
}

func TestReadAndProcessFile(t *testing.T) {
	// Create a temporary file with test content
	tmpFile, err := os.CreateTemp("", "test_process_*.go")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	testContent := `package main

import "fmt"

// This is a comment
func main() {
	fmt.Println("Hello, world!")
}`

	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	opt := createTestOption()

	// Test basic file reading
	content, err := ReadAndProcessFile(tmpFile.Name(), opt)
	if err != nil {
		t.Fatalf("ReadAndProcessFile failed: %v", err)
	}

	if !strings.Contains(content, "package main") {
		t.Error("Content doesn't contain expected text")
	}

	// Test with line numbers
	opt.WithLineNumberFlagValue = "on"
	opt.Normalize()

	content, err = ReadAndProcessFile(tmpFile.Name(), opt)
	if err != nil {
		t.Fatalf("ReadAndProcessFile with line numbers failed: %v", err)
	}

	if !strings.Contains(content, "1: ") {
		t.Error("Content doesn't contain line numbers")
	}

	// Test with comment deletion
	opt.DeleteCommentsFlag = true
	opt.WithLineNumberFlagValue = "off"
	opt.Normalize()

	content, err = ReadAndProcessFile(tmpFile.Name(), opt)
	if err != nil {
		t.Fatalf("ReadAndProcessFile with comment deletion failed: %v", err)
	}

	if strings.Contains(content, "// This is a comment") {
		t.Error("Comments were not deleted")
	}
}

func TestReadAndProcessFile_NonExistentFile(t *testing.T) {
	opt := createTestOption()

	_, err := ReadAndProcessFile("/non/existent/file.txt", opt)
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestListFilteredFiles(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "test_list_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	files := []string{"test.go", "test.js", "test.py", "README.md", ".gitignore"}
	for _, filename := range files {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	opt := createTestOption()

	// Test basic listing
	fileList, err := ListFilteredFiles(tmpDir, opt)
	if err != nil {
		t.Fatalf("ListFilteredFiles failed: %v", err)
	}

	if len(fileList) == 0 {
		t.Error("No files listed")
	}

	// Test with extension filter
	opt.IncludeExt = "go"
	fileList, err = ListFilteredFiles(tmpDir, opt)
	if err != nil {
		t.Fatalf("ListFilteredFiles with extension filter failed: %v", err)
	}

	for _, file := range fileList {
		if !strings.HasSuffix(file, ".go") {
			t.Errorf("File %s doesn't match extension filter", file)
		}
	}

	// Test with ignore dotfiles
	opt.IncludeExt = ""
	opt.IgnoreDotFileFlagValue = "on"
	opt.Normalize()

	fileList, err = ListFilteredFiles(tmpDir, opt)
	if err != nil {
		t.Fatalf("ListFilteredFiles with ignore dotfiles failed: %v", err)
	}

	for _, file := range fileList {
		if strings.HasPrefix(filepath.Base(file), ".") {
			t.Errorf("Dotfile %s was not filtered out", file)
		}
	}
}

func TestSearchInFiles(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "test_search_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files with different content
	testFiles := map[string]string{
		"file1.txt": "This is a test file\nwith multiple lines\nand some test content",
		"file2.go":  "package main\nfunc test() {\n\tfmt.Println(\"test\")\n}",
		"file3.py":  "def test_function():\n    print('testing')\n    return True",
	}

	for filename, content := range testFiles {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	opt := createTestOption()

	// Test basic search
	results, err := SearchInFiles(tmpDir, "test", false, 100, opt)
	if err != nil {
		t.Fatalf("SearchInFiles failed: %v", err)
	}

	if results == "" {
		t.Error("No search results returned")
	}

	// Check that results contain expected files
	if !strings.Contains(results, "file1.txt") {
		t.Error("Results don't contain file1.txt")
	}

	// Test regex search
	results, err = SearchInFiles(tmpDir, "test.*content", true, 100, opt)
	if err != nil {
		t.Fatalf("SearchInFiles with regex failed: %v", err)
	}

	if results == "" {
		t.Error("No regex search results returned")
	}

	// Test with max results limit
	results, err = SearchInFiles(tmpDir, "test", false, 1, opt)
	if err != nil {
		t.Fatalf("SearchInFiles with max results failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(results), "\n")
	if len(lines) > 1 {
		t.Errorf("Expected max 1 result, got %d", len(lines))
	}
}

func TestGetProjectStats(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "test_stats_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFiles := map[string]string{
		"main.go":     "package main\nfunc main() {}",
		"test.py":     "print('hello')",
		"README.md":   "# Project",
		"config.json": `{"name": "test"}`,
	}

	for filename, content := range testFiles {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	opt := createTestOption()

	stats, err := GetProjectStats(tmpDir, opt)
	if err != nil {
		t.Fatalf("GetProjectStats failed: %v", err)
	}

	// Check required fields
	requiredFields := []string{"totalFiles", "totalDirectories", "totalSize", "languageStats", "extensionStats"}
	for _, field := range requiredFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("Missing required field '%s' in stats", field)
		}
	}

	// Check that file count is correct
	totalFiles := stats["totalFiles"].(int)
	if totalFiles != len(testFiles) {
		t.Errorf("Expected %d files, got %d", len(testFiles), totalFiles)
	}

	// Check language stats
	langStats := stats["languageStats"].(map[string]int)
	if langStats["go"] == 0 {
		t.Error("Go language not detected")
	}
	if langStats["python"] == 0 {
		t.Error("Python language not detected")
	}

	// Check extension stats
	extStats := stats["extensionStats"].(map[string]int)
	if extStats[".go"] == 0 {
		t.Error(".go extension not counted")
	}
	if extStats[".py"] == 0 {
		t.Error(".py extension not counted")
	}
}

func TestGenerateArkliteForFiles(t *testing.T) {
	// Create temporary files for testing
	tmpDir, err := os.MkdirTemp("", "test_arklite_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFiles := map[string]string{
		"file1.txt": "Line 1\nLine 2\nLine 3",
		"file2.go":  "package main\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
	}

	var filePaths []string
	for filename, content := range testFiles {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
		filePaths = append(filePaths, path)
	}

	opt := createTestOption()
	opt.OutputFormatValue = "arklite"

	result, err := GenerateArkliteForFiles(filePaths, opt)
	if err != nil {
		t.Fatalf("GenerateArkliteForFiles failed: %v", err)
	}

	// Check arklite format
	if !strings.Contains(result, "# Arklite Format:") {
		t.Error("Result doesn't contain arklite header")
	}

	if !strings.Contains(result, "## File Dump") {
		t.Error("Result doesn't contain file dump section")
	}

	// Check that files are included
	for range testFiles {
		if !strings.Contains(result, "@") {
			t.Error("Result doesn't contain file markers")
		}
	}

	// Check that newlines are converted to ␤
	if !strings.Contains(result, "␤") {
		t.Error("Result doesn't contain arklite newline markers")
	}
}
