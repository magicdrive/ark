package typescript

import (
	"context"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Provider extracts symbols from TypeScript and TSX source files.
type Provider struct {
	lang   language.Language
	tsLang func() *ts.Language
}

// NewProvider returns a Provider for TypeScript (.ts) files.
func NewProvider() *Provider {
	return &Provider{lang: "typescript", tsLang: grammars.TypescriptLanguage}
}

// NewTSXProvider returns a Provider for TSX (.tsx) files.
func NewTSXProvider() *Provider {
	return &Provider{lang: "tsx", tsLang: grammars.TsxLanguage}
}

func (p *Provider) Language() language.Language { return p.lang }

func (p *Provider) Extensions() []string {
	if p.lang == "tsx" {
		return []string{".tsx"}
	}
	return []string{".ts"}
}

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	tsLang := p.tsLang()
	parser := ts.NewParser(tsLang)
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
	drafts := extractSymbols(root, tsLang, src, file)
	return language.Extraction{Symbols: drafts}, nil
}

func extractSymbols(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Type(lang) {
		case "function_declaration":
			if d := tsFunction(child, lang, src, file, false); d != nil {
				drafts = append(drafts, *d)
			}
		case "class_declaration":
			if d := tsClass(child, lang, src, file, false); d != nil {
				drafts = append(drafts, *d)
			}
		case "interface_declaration":
			if d := tsInterface(child, lang, src, file, false); d != nil {
				drafts = append(drafts, *d)
			}
		case "type_alias_declaration":
			if d := tsTypeAlias(child, lang, src, file, false); d != nil {
				drafts = append(drafts, *d)
			}
		case "lexical_declaration":
			drafts = append(drafts, tsLexical(child, lang, src, file, false)...)
		case "export_statement":
			drafts = append(drafts, exportedStatements(child, lang, src, file)...)
		}
	}
	return drafts
}

func exportedStatements(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for j := 0; j < node.ChildCount(); j++ {
		inner := node.Child(j)
		switch inner.Type(lang) {
		case "function_declaration":
			if d := tsFunction(inner, lang, src, file, true); d != nil {
				drafts = append(drafts, *d)
			}
		case "class_declaration":
			if d := tsClass(inner, lang, src, file, true); d != nil {
				drafts = append(drafts, *d)
			}
		case "interface_declaration":
			if d := tsInterface(inner, lang, src, file, true); d != nil {
				drafts = append(drafts, *d)
			}
		case "type_alias_declaration":
			if d := tsTypeAlias(inner, lang, src, file, true); d != nil {
				drafts = append(drafts, *d)
			}
		case "lexical_declaration":
			for _, d := range tsLexical(inner, lang, src, file, true) {
				drafts = append(drafts, d)
			}
		}
	}
	return drafts
}

func tsFunction(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) *language.SymbolDraft {
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

func tsClass(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) *language.SymbolDraft {
	name := childText(node, lang, src, "type_identifier")
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

func tsInterface(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) *language.SymbolDraft {
	name := childText(node, lang, src, "type_identifier")
	if name == "" {
		return nil
	}
	return &language.SymbolDraft{
		Name:      name,
		Qualified: name,
		Kind:      symbol.KindInterface,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  exported,
	}
}

func tsTypeAlias(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) *language.SymbolDraft {
	name := childText(node, lang, src, "type_identifier")
	if name == "" {
		return nil
	}
	return &language.SymbolDraft{
		Name:      name,
		Qualified: name,
		Kind:      symbol.KindTypeAlias,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  exported,
	}
}

func tsLexical(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, exported bool) []language.SymbolDraft {
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
