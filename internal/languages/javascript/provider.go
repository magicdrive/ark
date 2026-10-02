package javascript

import (
	"context"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Provider extracts symbols from JavaScript source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "javascript" }
func (p *Provider) Extensions() []string {
	return []string{".js", ".mjs", ".cjs", ".jsx"}
}

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.JavascriptLanguage()
	parser := ts.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{{
				Severity: language.SeverityError,
				Message:  "parse failed: " + err.Error(),
				Location: source.Location{File: file},
			}},
		}, nil
	}
	defer tree.Release()

	root := tree.RootNode()
	drafts := extractSymbols(root, lang, src, file)
	return language.Extraction{Symbols: drafts}, nil
}

// JavaScript shares its top-level declaration shapes with TypeScript.
func extractSymbols(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Type(lang) {
		case "function_declaration":
			if d := jsFunction(child, lang, src, file, false); d != nil {
				drafts = append(drafts, *d)
			}
		case "class_declaration":
			if d := jsClass(child, lang, src, file, false); d != nil {
				drafts = append(drafts, *d)
			}
		case "lexical_declaration":
			drafts = append(drafts, jsLexical(child, lang, src, file, false)...)
		case "export_statement":
			drafts = append(drafts, jsExported(child, lang, src, file)...)
		}
	}
	return drafts
}

func jsExported(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for j := 0; j < node.ChildCount(); j++ {
		inner := node.Child(j)
		switch inner.Type(lang) {
		case "function_declaration":
			if d := jsFunction(inner, lang, src, file, true); d != nil {
				drafts = append(drafts, *d)
			}
		case "class_declaration":
			if d := jsClass(inner, lang, src, file, true); d != nil {
				drafts = append(drafts, *d)
			}
		case "lexical_declaration":
			for _, d := range jsLexical(inner, lang, src, file, true) {
				drafts = append(drafts, d)
			}
		}
	}
	return drafts
}

func jsFunction(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) *language.SymbolDraft {
	name := childText(node, lang, src, "identifier")
	if name == "" {
		return nil
	}
	return &language.SymbolDraft{
		Name:      name,
		Qualified: name,
		Kind:      symbol.KindFunction,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  exported,
	}
}

func jsClass(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) *language.SymbolDraft {
	name := childText(node, lang, src, "identifier")
	if name == "" {
		return nil
	}
	return &language.SymbolDraft{
		Name:      name,
		Qualified: name,
		Kind:      symbol.KindClass,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  exported,
	}
}

func jsLexical(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	isConst := false
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(lang) {
		case "const":
			isConst = true
		case "variable_declarator":
			name := childText(child, lang, src, "identifier")
			if name == "" {
				continue
			}
			kind := symbol.KindVariable
			if isConst {
				kind = symbol.KindConstant
			}
			drafts = append(drafts, language.SymbolDraft{
				Name:      name,
				Qualified: name,
				Kind:      kind,
				Location:  nodeLocation(child, file),
				StartByte: child.StartByte(),
				EndByte:   child.EndByte(),
				Exported:  exported,
			})
		}
	}
	return drafts
}

func childText(node *ts.Node, lang *ts.Language, src []byte, nodeType string) string {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == nodeType {
			return child.Text(src)
		}
	}
	return ""
}

func nodeLocation(node *ts.Node, file source.FileID) source.Location {
	return source.Location{
		File: file,
		Range: source.Range{
			Start: source.Position{
				Line:   node.StartPoint().Row + 1,
				Column: node.StartPoint().Column + 1,
			},
			End: source.Position{
				Line:   node.EndPoint().Row + 1,
				Column: node.EndPoint().Column + 1,
			},
		},
	}
}
