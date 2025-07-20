package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/secrets"
)

// GenerateDirectoryTreeJSON wraps core.GenerateTreeJSONString
func GenerateDirectoryTreeJSON(path string) (string, error) {
	// Create a temporary option with default values
	opt := &commandline.Option{
		WorkingDir:                      ".",
		TargetDirname:                   ".",
		OutputFilename:                  "temp-output.txt",
		ScanBufferValue:                 "10M",
		AllowGitignoreFlagValue:         "on",
		IgnoreDotFileFlagValue:          "off",
		MaskSecretsFlagValue:            "off",
		SkipNonUTF8Flag:                 false,
		DeleteCommentsFlag:              false,
		WithLineNumberFlagValue:         "off",
		OutputFormatValue:               "plaintext",
		PatternRegexpString:             "",
		IncludeExt:                      "",
		ExcludeExt:                      "",
		ExcludeDir:                      "",
		ExcludeDirRegexpString:          "",
		ExcludeFileRegexpString:         "",
		AdditionallyIgnoreRuleFilenames: "",
	}

	if err := opt.Normalize(); err != nil {
		return "", err
	}

	allowedFileMap := map[string]bool{}
	jsonStr, _, err := core.GenerateTreeJSONString(path, allowedFileMap, opt)
	return jsonStr, err
}

// ReadAndProcessFile reads a file and applies processing options
func ReadAndProcessFile(path string, opt *commandline.Option) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// Skip non-UTF8 if requested
	if opt.SkipNonUTF8Flag && core.IsBinary(data) {
		return "", fmt.Errorf("file is binary or non-UTF8")
	}

	// Delete comments if requested
	if opt.DeleteCommentsFlag {
		data = core.DeleteComments(data, path)
	}

	content := string(data)

	// Mask secrets if requested
	if opt.MaskSecretsFlag.Bool() {
		content = secrets.MaskAll(content)
	}

	// Add line numbers if requested
	if opt.WithLineNumberFlag.Bool() {
		lines := strings.Split(content, "\n")
		var numberedLines []string
		for i, line := range lines {
			numberedLines = append(numberedLines, fmt.Sprintf("%d: %s", i+1, line))
		}
		content = strings.Join(numberedLines, "\n")
	}

	return content, nil
}

// ListFilteredFiles lists files in a directory with filtering
func ListFilteredFiles(path string, opt *commandline.Option) ([]string, error) {
	var files []string

	err := filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip directories
		if info.IsDir() {
			if !core.CanBoaded(opt, currentPath) {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply filtering
		if !core.CanBoaded(opt, currentPath) {
			return nil
		}

		// Skip hidden files if requested
		if opt.IgnoreDotFileFlag.Bool() && core.IsHiddenFile(info.Name()) {
			return nil
		}

		// Skip non-UTF8 files if requested
		if opt.SkipNonUTF8Flag {
			data, err := os.ReadFile(currentPath)
			if err == nil && core.IsBinary(data) {
				return nil
			}
		}

		// Make path relative to the root
		relPath, err := filepath.Rel(path, currentPath)
		if err == nil {
			files = append(files, relPath)
		}

		return nil
	})

	return files, err
}

// SearchInFiles searches for text within files
func SearchInFiles(path, query string, isRegex bool, maxResults int, opt *commandline.Option) (string, error) {
	var results []string
	var pattern *regexp.Regexp
	var err error

	if isRegex {
		pattern, err = regexp.Compile(query)
		if err != nil {
			return "", fmt.Errorf("invalid regex pattern: %v", err)
		}
	}

	count := 0
	err = filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if count >= maxResults {
			return fmt.Errorf("max results reached")
		}

		// Skip directories
		if info.IsDir() {
			if !core.CanBoaded(opt, currentPath) {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply filtering
		if !core.CanBoaded(opt, currentPath) {
			return nil
		}

		// Skip hidden files if requested
		if opt.IgnoreDotFileFlag.Bool() && core.IsHiddenFile(info.Name()) {
			return nil
		}

		// Read file content
		data, err := os.ReadFile(currentPath)
		if err != nil {
			return nil
		}

		// Skip binary files
		if core.IsBinary(data) {
			return nil
		}

		content := string(data)
		lines := strings.Split(content, "\n")

		// Search in each line
		for lineNum, line := range lines {
			var match bool
			if isRegex {
				match = pattern.MatchString(line)
			} else {
				match = strings.Contains(line, query)
			}

			if match {
				relPath, _ := filepath.Rel(path, currentPath)
				result := fmt.Sprintf("%s:%d:%s", relPath, lineNum+1, line)
				results = append(results, result)
				count++
				if count >= maxResults {
					return fmt.Errorf("max results reached")
				}
			}
		}

		return nil
	})

	if err != nil && err.Error() != "max results reached" {
		return "", err
	}

	return strings.Join(results, "\n"), nil
}

// DetectLanguage detects the language of a file based on its extension and name
func DetectLanguage(path string) string {
	base := strings.ToLower(filepath.Base(path))

	switch {
	case base == "dockerfile":
		return "dockerfile"
	case base == "makefile" || strings.HasPrefix(base, "makefile"):
		return "makefile"
	case base == "cmakelists.txt":
		return "cmake"
	case base == "build.gradle":
		return "groovy"
	case base == "vagrantfile":
		return "ruby"
	}

	ext := strings.ToLower(filepath.Ext(path))
	if tag, ok := knownLanguageTags[ext]; ok {
		return tag
	}
	return ""
}

// knownLanguageTags maps file extensions to GitHub-compatible language identifiers
var knownLanguageTags = map[string]string{
	".abap":        "abap",
	".ada":         "ada",
	".ahk":         "autohotkey",
	".apacheconf":  "apache",
	".applescript": "applescript",
	".as":          "actionscript",
	".bash":        "bash",
	".bat":         "bat",
	".bf":          "brainfuck",
	".c":           "c",
	".h":           "c",
	".cc":          "cpp",
	".cpp":         "cpp",
	".cxx":         "cpp",
	".cs":          "csharp",
	".clj":         "clojure",
	".cljs":        "clojure",
	".cmake":       "cmake",
	".coffee":      "coffeescript",
	".css":         "css",
	".dart":        "dart",
	".diff":        "diff",
	".dockerfile":  "dockerfile",
	".el":          "emacs-lisp",
	".erl":         "erlang",
	".go":          "go",
	".groovy":      "groovy",
	".hs":          "haskell",
	".html":        "html",
	".ini":         "ini",
	".java":        "java",
	".js":          "javascript",
	".jsx":         "jsx",
	".json":        "json",
	".kt":          "kotlin",
	".kts":         "kotlin",
	".less":        "less",
	".lisp":        "lisp",
	".lua":         "lua",
	".md":          "markdown",
	".markdown":    "markdown",
	".mkd":         "markdown",
	".m":           "objectivec",
	".mm":          "objectivec",
	".php":         "php",
	".pl":          "perl",
	".ps1":         "powershell",
	".py":          "python",
	".r":           "r",
	".rb":          "ruby",
	".rs":          "rust",
	".scala":       "scala",
	".scss":        "scss",
	".sh":          "bash",
	".zsh":         "bash",
	".sql":         "sql",
	".swift":       "swift",
	".tex":         "latex",
	".toml":        "toml",
	".ts":          "typescript",
	".tsx":         "tsx",
	".vue":         "vue",
	".vim":         "vim",
	".xml":         "xml",
	".yml":         "yaml",
	".yaml":        "yaml",
	".txt":         "text",
}

// GetProjectStats generates statistics about a project directory
func GetProjectStats(path string, opt *commandline.Option) (map[string]interface{}, error) {
	stats := map[string]interface{}{
		"totalFiles":       0,
		"totalDirectories": 0,
		"totalSize":        int64(0),
		"languageStats":    map[string]int{},
		"extensionStats":   map[string]int{},
	}

	err := filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			if !core.CanBoaded(opt, currentPath) {
				return filepath.SkipDir
			}
			if currentPath != path { // Don't count root directory
				stats["totalDirectories"] = stats["totalDirectories"].(int) + 1
			}
			return nil
		}

		// Apply filtering
		if !core.CanBoaded(opt, currentPath) {
			return nil
		}

		// Skip hidden files if requested
		if opt.IgnoreDotFileFlag.Bool() && core.IsHiddenFile(info.Name()) {
			return nil
		}

		stats["totalFiles"] = stats["totalFiles"].(int) + 1
		stats["totalSize"] = stats["totalSize"].(int64) + info.Size()

		// Language detection
		language := DetectLanguage(currentPath)
		if language != "" {
			langStats := stats["languageStats"].(map[string]int)
			langStats[language]++
		}

		// Extension stats
		ext := filepath.Ext(currentPath)
		if ext != "" {
			extStats := stats["extensionStats"].(map[string]int)
			extStats[ext]++
		}

		return nil
	})

	return stats, err
}

// GenerateArkliteForFiles generates arklite format for multiple files
func GenerateArkliteForFiles(paths []string, opt *commandline.Option) (string, error) {
	var result strings.Builder

	// Write header
	projectName := "Multiple Files"
	if len(paths) > 0 {
		projectName = filepath.Dir(paths[0])
	}
	result.WriteString(fmt.Sprintf("# Arklite Format: %s\n\n", projectName))

	// Write file dump
	result.WriteString("## File Dump\n")
	for _, path := range paths {
		content, err := ReadAndProcessFile(path, opt)
		if err != nil {
			result.WriteString(fmt.Sprintf("@%s\nError: %v\n", path, err))
			continue
		}

		// Convert newlines to ␤ for arklite format
		arkliteContent := strings.ReplaceAll(content, "\n", "␤")
		result.WriteString(fmt.Sprintf("@%s\n%s\n", path, arkliteContent))
	}

	return result.String(), nil
}
