package syntax

import (
	"context"
	"errors"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/source"
)

var (
	ErrUnsupportedLanguage = errors.New("unsupported syntax language")
	ErrParseFailed         = errors.New("failed to parse source")
)

// providers maps each SupportedLanguage to its extraction Provider.
// Tree-sitter usage is fully contained inside each provider.
var providers = map[SupportedLanguage]language.Provider{
	LangGo:         golang.NewProvider(),
	LangTypeScript: typescript.NewProvider(),
	LangTSX:        typescript.NewTSXProvider(),
	LangJavaScript: javascript.NewProvider(),
	LangPython:     python.NewProvider(),
}

// ParseResult holds the result of parsing a source file.
// It is used by RunSyntaxCommand for raw AST output; symbol extraction now
// goes through the language Provider instead of this struct.
type ParseResult struct {
	Tree     *ts.Tree
	Language *ts.Language
	Source   []byte
	Lang     SupportedLanguage
}

// Release releases resources associated with the parse result.
func (r *ParseResult) Release() {
	if r.Tree != nil {
		r.Tree.Release()
	}
}

// ParseFile parses source code and returns a parse result.
func ParseFile(filename string, source []byte) (*ParseResult, error) {
	lang := DetectLanguage(filename)
	if lang == LangUnknown {
		return nil, ErrUnsupportedLanguage
	}
	return Parse(lang, source)
}

// Parse parses source code with a specific language.
func Parse(lang SupportedLanguage, src []byte) (*ParseResult, error) {
	tsLang := GetLanguage(lang)
	if tsLang == nil {
		return nil, ErrUnsupportedLanguage
	}

	parser := ts.NewParser(tsLang)
	tree, err := parser.Parse(src)
	if err != nil {
		return nil, ErrParseFailed
	}

	return &ParseResult{
		Tree:     tree,
		Language: tsLang,
		Source:   src,
		Lang:     lang,
	}, nil
}

// ExtractSymbolsFromFile parses src and extracts symbols using the registered
// language Provider. Tree-sitter does not leak past this call.
func ExtractSymbolsFromFile(filename string, src []byte) (*FileSymbols, error) {
	lang := DetectLanguage(filename)
	if lang == LangUnknown {
		return nil, ErrUnsupportedLanguage
	}

	p, ok := providers[lang]
	if !ok {
		return nil, ErrUnsupportedLanguage
	}

	ext, err := p.Extract(context.Background(), source.FileID(filename), src)
	if err != nil {
		return nil, err
	}

	symbols := draftsToSymbols(ext.Symbols)
	return &FileSymbols{
		Path:     filename,
		Language: string(lang),
		Symbols:  symbols,
	}, nil
}

// ExtractSymbols extracts symbols from a ParseResult.
// Internally it re-routes through the language Provider so extraction logic
// lives in exactly one place.
func ExtractSymbols(result *ParseResult) []Symbol {
	if result == nil {
		return nil
	}

	p, ok := providers[result.Lang]
	if !ok {
		return nil
	}

	// Use a synthetic filename to satisfy the Provider API.
	fakeFile := source.FileID("_." + string(result.Lang))
	ext, err := p.Extract(context.Background(), fakeFile, result.Source)
	if err != nil || ext.Symbols == nil {
		return nil
	}

	return draftsToSymbols(ext.Symbols)
}

// draftsToSymbols converts Provider output to the legacy Symbol type used by
// the MCP/CLI adapters. The JSON schema of Symbol must not change.
func draftsToSymbols(drafts []language.SymbolDraft) []Symbol {
	symbols := make([]Symbol, 0, len(drafts))
	for _, d := range drafts {
		symbols = append(symbols, Symbol{
			Name:      d.Name,
			Kind:      SymbolKind(d.Kind),
			StartLine: d.Location.Range.Start.Line,
			EndLine:   d.Location.Range.End.Line,
			StartCol:  d.Location.Range.Start.Column,
			EndCol:    d.Location.Range.End.Column,
			StartByte: d.StartByte,
			EndByte:   d.EndByte,
			Receiver:  d.Receiver,
			Parent:    d.Parent,
			Exported:  d.Exported,
		})
	}
	return symbols
}
