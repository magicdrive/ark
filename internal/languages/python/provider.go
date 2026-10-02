package python

import (
	"context"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Provider extracts symbols from Python source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "python" }
func (p *Provider) Extensions() []string        { return []string{".py", ".pyw"} }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.PythonLanguage()
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

func extractSymbols(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Type(lang) {
		case "function_definition":
			if d := pyFunction(child, lang, src, file); d != nil {
				drafts = append(drafts, *d)
			}
		case "class_definition":
			if d := pyClass(child, lang, src, file); d != nil {
				drafts = append(drafts, *d)
			}
		case "decorated_definition":
			for j := 0; j < child.ChildCount(); j++ {
				inner := child.Child(j)
				switch inner.Type(lang) {
				case "function_definition":
					if d := pyFunction(inner, lang, src, file); d != nil {
						drafts = append(drafts, *d)
					}
				case "class_definition":
					if d := pyClass(inner, lang, src, file); d != nil {
						drafts = append(drafts, *d)
					}
				}
			}
		}
	}
	return drafts
}

func pyFunction(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) *language.SymbolDraft {
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
		Exported:  !isPrivate(name),
	}
}

func pyClass(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) *language.SymbolDraft {
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
		Exported:  !isPrivate(name),
	}
}

// isPrivate returns true for names starting with underscore (Python convention).
func isPrivate(name string) bool {
	return len(name) > 0 && name[0] == '_'
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
