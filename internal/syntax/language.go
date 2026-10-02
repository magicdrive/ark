package syntax

import (
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// SupportedLanguage represents a language supported by syntax analysis
type SupportedLanguage string

const (
	LangGo         SupportedLanguage = "go"
	LangTypeScript SupportedLanguage = "typescript"
	LangTSX        SupportedLanguage = "tsx"
	LangJavaScript SupportedLanguage = "javascript"
	LangPython     SupportedLanguage = "python"
	LangUnknown    SupportedLanguage = ""
)

// languageRegistry maps language names to Language getters
var languageRegistry = map[SupportedLanguage]func() *ts.Language{
	LangGo:         grammars.GoLanguage,
	LangTypeScript: grammars.TypescriptLanguage,
	LangTSX:        grammars.TsxLanguage,
	LangJavaScript: grammars.JavascriptLanguage,
	LangPython:     grammars.PythonLanguage,
}

// extensionToLanguage maps file extensions to languages
var extensionToLanguage = map[string]SupportedLanguage{
	".go":  LangGo,
	".ts":  LangTypeScript,
	".tsx": LangTSX,
	".js":  LangJavaScript,
	".jsx": LangJavaScript, // JSX uses JavaScript parser with some extensions
	".mjs": LangJavaScript,
	".cjs": LangJavaScript,
	".py":  LangPython,
	".pyw": LangPython,
}

// DetectLanguage detects the syntax language from a filename
func DetectLanguage(filename string) SupportedLanguage {
	ext := strings.ToLower(filepath.Ext(filename))
	if lang, ok := extensionToLanguage[ext]; ok {
		return lang
	}
	return LangUnknown
}

// GetLanguage returns the tree-sitter Language for parsing
func GetLanguage(lang SupportedLanguage) *ts.Language {
	if getter, ok := languageRegistry[lang]; ok {
		return getter()
	}
	return nil
}

// IsSupported returns true if the language is supported for syntax analysis
func IsSupported(lang SupportedLanguage) bool {
	_, ok := languageRegistry[lang]
	return ok
}

// IsSupportedFile returns true if the file can be analyzed
func IsSupportedFile(filename string) bool {
	lang := DetectLanguage(filename)
	return IsSupported(lang)
}

// SupportedLanguages returns a list of all supported languages
func SupportedLanguages() []SupportedLanguage {
	result := make([]SupportedLanguage, 0, len(languageRegistry))
	for lang := range languageRegistry {
		result = append(result, lang)
	}
	return result
}

// SupportedExtensions returns a list of all supported file extensions
func SupportedExtensions() []string {
	result := make([]string, 0, len(extensionToLanguage))
	for ext := range extensionToLanguage {
		result = append(result, ext)
	}
	return result
}

// ExtensionsFor returns all file extensions mapped to the given language, sorted.
func ExtensionsFor(lang SupportedLanguage) []string {
	var result []string
	for ext, l := range extensionToLanguage {
		if l == lang {
			result = append(result, ext)
		}
	}
	// Sort for deterministic output.
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j] < result[j-1]; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}
