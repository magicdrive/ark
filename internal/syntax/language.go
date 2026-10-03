package syntax

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
)

// SupportedLanguage is a language identity for syntax analysis. It is an alias
// of language.Language: there is no separate language type system. The canonical
// set of supported languages lives in the language Registry assembled by
// internal/languages, not here.
type SupportedLanguage = language.Language

// LangUnknown is the sentinel for "no supported language detected".
const LangUnknown SupportedLanguage = ""

// DetectLanguage detects the syntax language from a filename, or LangUnknown.
func DetectLanguage(filename string) SupportedLanguage {
	if d, ok := languages.Registry().DetectByFilename(filename); ok {
		return d.Language
	}
	return LangUnknown
}

// grammarFor returns the tree-sitter Language for parsing, or nil.
func grammarFor(lang SupportedLanguage) *ts.Language {
	if getter, ok := languages.Grammar(lang); ok {
		return getter()
	}
	return nil
}

// IsSupported reports whether the language has a registered provider.
func IsSupported(lang SupportedLanguage) bool {
	_, ok := languages.Registry().Lookup(lang)
	return ok
}

// IsSupportedFile reports whether the file can be analyzed.
func IsSupportedFile(filename string) bool {
	return languages.Registry().IsSupportedFilename(filename)
}
