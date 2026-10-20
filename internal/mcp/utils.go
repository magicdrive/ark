package mcp

import (
	"encoding/json"
	"fmt"
	"github.com/magicdrive/ark/internal/common"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/libgitignore"
	"github.com/magicdrive/ark/internal/secrets"
	"github.com/magicdrive/ark/internal/textenc"
)

// skipMetadata reports whether a walk entry below root is repository metadata
// (core.IsMetadataDirName) and, if so, the value the walk function returns. The
// walk root itself is never skipped: an explicitly requested path is honoured.
func skipMetadata(root, current string, info os.FileInfo) (bool, error) {
	if current == root || !core.IsMetadataDirName(info.Name()) {
		return false, nil
	}
	if info.IsDir() {
		return true, filepath.SkipDir
	}
	return true, nil
}

// GenerateDirectoryTreeJSON wraps core.GenerateTreeJSONString. ignore is the
// repository's ignore rule (rooted at the repository root, not the process
// working directory); nil applies none. exclude is the .arkignore access
// policy (Option.AccessExclude); nil excludes nothing.
func GenerateDirectoryTreeJSON(path string, ignore *libgitignore.RuleSet, exclude func(string) bool) (string, error) {
	return directoryTreeJSON(path, ignore, exclude, nil, treeLimits{})
}

// directoryTreeJSON is GenerateBoundedDirectoryTreeJSON listing through fsys
// (the request's walker) when it is set.
func directoryTreeJSON(path string, ignore *libgitignore.RuleSet, exclude func(string) bool, fsys commandline.FileAccess, limits treeLimits) (string, error) {
	if !limits.none() {
		return boundedTreeJSON(path, ignore, exclude, fsys, limits)
	}
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

	if err := opt.NormalizeFileFilters(); err != nil {
		return "", err
	}
	opt.GitIgnoreRule = ignore
	opt.AccessExclude = exclude
	opt.FS = fsys

	allowedFileMap := map[string]bool{}
	jsonStr, _, err := core.GenerateTreeJSONString(path, allowedFileMap, opt)
	return jsonStr, err
}

// treeLimits bounds a directory tree: maxDepth > 0 lists directories at that
// depth (the root's children are depth 1) without their contents, marking
// them truncated; excludeDirs names directories never listed — a bare name
// matches a path component, a name with "/" a path relative to the tree root.
type treeLimits struct {
	maxDepth    int
	excludeDirs []string
}

func (l treeLimits) none() bool { return l.maxDepth <= 0 && len(l.excludeDirs) == 0 }

func (l treeLimits) excluded(rel, name string) bool {
	for _, x := range l.excludeDirs {
		if strings.Contains(x, "/") {
			if rel == x {
				return true
			}
		} else if name == x {
			return true
		}
	}
	return false
}

// boundedTreeEntry is core.TreeEntry plus the truncation mark.
type boundedTreeEntry struct {
	Name      string              `json:"name"`
	Type      string              `json:"type"`
	Children  []*boundedTreeEntry `json:"children,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

// GenerateBoundedDirectoryTreeJSON is GenerateDirectoryTreeJSON with limits.
// Without limits it is exactly GenerateDirectoryTreeJSON; with them it walks
// the same entries in the same order under the same filters, stopping at the
// limits instead of filtering afterwards.
func GenerateBoundedDirectoryTreeJSON(path string, ignore *libgitignore.RuleSet, exclude func(string) bool, limits treeLimits) (string, error) {
	return directoryTreeJSON(path, ignore, exclude, nil, limits)
}

func boundedTreeJSON(path string, ignore *libgitignore.RuleSet, exclude func(string) bool, fsys commandline.FileAccess, limits treeLimits) (string, error) {
	opt := &commandline.Option{AllowGitignoreFlagValue: "on", IgnoreDotFileFlagValue: "off"}
	if err := opt.NormalizeFileFilters(); err != nil {
		return "", err
	}
	opt.GitIgnoreRule = ignore
	opt.AccessExclude = exclude
	opt.FS = fsys
	tree, err := boundedTree(path, "", 0, opt, limits)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(tree)
	return string(b), err
}

func boundedTree(path, rel string, depth int, opt *commandline.Option, limits treeLimits) (*boundedTreeEntry, error) {
	files, err := opt.ReadDir(path)
	if err != nil {
		return nil, err
	}
	core.ApplySort(files)
	node := &boundedTreeEntry{Name: filepath.Base(path), Type: "directory"}
	for _, file := range files {
		if opt.IgnoreDotFileFlag.Bool() && core.IsHiddenFile(file.Name()) {
			continue
		}
		fullPath := filepath.Join(path, file.Name())
		if core.IsMetadataDirName(file.Name()) || !core.CanBoaded(opt, fullPath) {
			continue
		}
		childRel := file.Name()
		if rel != "" {
			childRel = rel + "/" + file.Name()
		}
		if !file.IsDir() {
			node.Children = append(node.Children, &boundedTreeEntry{Name: file.Name(), Type: "file"})
			continue
		}
		if limits.excluded(childRel, file.Name()) {
			continue
		}
		if limits.maxDepth > 0 && depth+1 >= limits.maxDepth {
			entries, _ := opt.ReadDir(fullPath)
			node.Children = append(node.Children, &boundedTreeEntry{Name: file.Name(), Type: "directory", Truncated: len(entries) > 0})
			continue
		}
		child, err := boundedTree(fullPath, childRel, depth+1, opt, limits)
		if err != nil {
			continue
		}
		node.Children = append(node.Children, child)
	}
	return node, nil
}

// ReadAndProcessFile reads a file and applies processing options
func ReadAndProcessFile(path string, opt *commandline.Option) (string, error) {
	data, err := os.ReadFile(path)
	return processFileContent(data, err, path, opt)
}

// processFileContent applies the processing options to a file's content
// read (data) or the error reading it (readErr).
func processFileContent(data []byte, readErr error, path string, opt *commandline.Option) (string, error) {
	return processFileContentIn(data, readErr, path, opt, secrets.SyntaxUnknown)
}

// processFileContentIn is processFileContent for a file in syntax (masking:
// secrets.MaskAllIn).
func processFileContentIn(data []byte, readErr error, path string, opt *commandline.Option, syntax secrets.Syntax) (string, error) {
	if readErr != nil {
		return "", readErr
	}

	// Skip non-UTF8 if requested
	if opt.SkipNonUTF8Flag && core.IsBinary(data) {
		return "", fmt.Errorf("file is binary or non-UTF8")
	}

	// Text in another encoding (or with a byte order mark) is shown decoded,
	// as the repository dump shows it (textenc). A file not in the encoding
	// --file-encoding names for it is refused, not shown with characters
	// replaced; bytes whose encoding cannot be told are passed on as before.
	switch r := textenc.Decode(data, opt.FileEncodingFor(path)); {
	case r.Kind == textenc.Invalid:
		return "", fmt.Errorf("%s (--file-encoding)", r.Reason)
	case r.Kind == textenc.Text && (r.Encoding != "utf-8" || len(r.Text) != len(data)):
		data = []byte(r.Text)
	}

	// Delete comments if requested
	if opt.DeleteCommentsFlag {
		data = core.DeleteComments(data, path)
	}

	content := string(data)

	// Mask secrets if requested
	if opt.MaskSecretsFlag.Bool() {
		content = opt.MaskPolicy().Mask(content, secrets.Mode{Syntax: syntax})
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

	err := walkFrom(opt, path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if skip, err := skipMetadata(path, currentPath, info); skip {
			return err
		}

		// Skip directories
		if info.IsDir() {
			if !core.CanEnterDir(opt, currentPath) {
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
			data, err := opt.ReadFile(currentPath)
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
	err = walkFrom(opt, path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if count >= maxResults {
			return fmt.Errorf("max results reached")
		}
		if skip, err := skipMetadata(path, currentPath, info); skip {
			return err
		}

		// Skip directories
		if info.IsDir() {
			if !core.CanEnterDir(opt, currentPath) {
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

		// Read file content: the file the walk decided (Option.FS).
		data, err := opt.ReadFile(currentPath)
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

// DetectLanguage returns the language of a file for get_file_info and
// get_project_stats: the name of the language Ark analyzes it as
// (SupportedLanguage) when there is one — the name get_language_support and
// the repository map use — otherwise a highlighting tag from the file name
// (knownLanguageTags), or "" when there is none.
func DetectLanguage(path string) string {
	if lang := SupportedLanguage(path); lang != "" {
		return lang
	}
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

// SupportedLanguage returns the language Ark's language providers analyze a
// file as, from its extension (languages.Registry), or "" for a file of no
// supported language. Terraform's JSON syntax (.tf.json, .tfvars.json) is not
// one: its extension is .json.
func SupportedLanguage(path string) string {
	if d, ok := languages.Registry().DetectByFilename(path); ok {
		return string(d.Language)
	}
	return ""
}

// knownLanguageTags are highlighting tags for files of no supported language
// (DetectLanguage).
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
		// supportedLanguageStats counts the files of the languages Ark
		// analyzes (get_language_support), by that language; languageStats
		// counts every file with a language or highlighting tag.
		"supportedLanguageStats": map[string]int{},
		"extensionStats":         map[string]int{},
	}

	err := walkFrom(opt, path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if skip, err := skipMetadata(path, currentPath, info); skip {
			return err
		}

		if info.IsDir() {
			if !core.CanEnterDir(opt, currentPath) {
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
		if supported := SupportedLanguage(currentPath); supported != "" {
			stats["supportedLanguageStats"].(map[string]int)[supported]++
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
	files := make([]arkliteFile, 0, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		files = append(files, arkliteFile{path: p, data: data, err: err})
	}
	return generateArklite(files, opt)
}

// arkliteFile is one file of an arklite dump: its path and content, or the
// error reading it.
type arkliteFile struct {
	path string
	data []byte
	err  error
}

func generateArklite(files []arkliteFile, opt *commandline.Option) (string, error) {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	var result strings.Builder

	// Write header
	projectName := "Multiple Files"
	if len(paths) > 0 {
		projectName = filepath.Dir(paths[0])
	}
	result.WriteString(fmt.Sprintf("# Arklite Format: %s\n\n", projectName))

	// Write file dump
	result.WriteString("## File Dump\n")
	for _, f := range files {
		path := f.path
		content, err := processFileContent(f.data, f.err, path, opt)
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

// walkFrom walks start like filepath.Walk, entering it when it is the server
// root given through a symlink (common.WalkRoot). A directory link named as
// any other start is not entered: below it, paths would not be the paths the
// .arkignore rules name.
func walkFrom(opt *commandline.Option, start string, fn filepath.WalkFunc) error {
	if opt != nil && opt.FS != nil {
		return opt.FS.Walk(start, fn)
	}
	if opt != nil && common.SamePath(start, opt.IgnoreRoot()) {
		return common.WalkRoot(start, fn)
	}
	return filepath.Walk(start, fn)
}
