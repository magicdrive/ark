package core

import (
	"path/filepath"
	"strings"
)

// detectLanguageTag returns the appropriate GitHub Markdown code block language tag.
// If unknown, it returns an empty string ("") for fallback.
func detectLanguageTag(filename string) string {
	base := strings.ToLower(filepath.Base(filename))

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

	ext := strings.ToLower(filepath.Ext(filename))
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
