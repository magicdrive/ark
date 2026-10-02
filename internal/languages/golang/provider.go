package golang

import (
	"context"
	"unicode"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Provider extracts symbols from Go source files.
type Provider struct{}

// NewProvider returns a ready-to-use Go Provider.
func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "go" }
func (p *Provider) Extensions() []string        { return []string{".go"} }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.GoLanguage()
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
	drafts := extractGoSymbols(root, lang, src, file)
	return language.Extraction{Symbols: drafts}, nil
}

func extractGoSymbols(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Type(lang) {
		case "function_declaration":
			if d := goFunction(child, lang, src, file); d != nil {
				drafts = append(drafts, *d)
			}
		case "method_declaration":
			if d := goMethod(child, lang, src, file); d != nil {
				drafts = append(drafts, *d)
			}
		case "type_declaration":
			drafts = append(drafts, goTypeDecl(child, lang, src, file)...)
		case "const_declaration":
			drafts = append(drafts, goConstDecl(child, lang, src, file)...)
		case "var_declaration":
			drafts = append(drafts, goVarDecl(child, lang, src, file)...)
		}
	}
	return drafts
}

func goFunction(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) *language.SymbolDraft {
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
		Exported:  isExported(name),
	}
}

func goMethod(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) *language.SymbolDraft {
	var name, receiver string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		t := child.Type(lang)
		if t == "parameter_list" && receiver == "" {
			receiver = goReceiverType(child, lang, src)
		} else if (t == "field_identifier" || t == "identifier") && name == "" {
			name = child.Text(src)
		}
	}
	if name == "" {
		return nil
	}
	qualified := name
	if receiver != "" {
		qualified = receiver + "." + name
	}
	return &language.SymbolDraft{
		Name:      name,
		Qualified: qualified,
		Kind:      symbol.KindMethod,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Receiver:  receiver,
		Exported:  isExported(name),
	}
}

func goReceiverType(node *ts.Node, lang *ts.Language, src []byte) string {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "parameter_declaration" {
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				switch gc.Type(lang) {
				case "type_identifier":
					return gc.Text(src)
				case "pointer_type":
					for k := 0; k < gc.ChildCount(); k++ {
						ggc := gc.Child(k)
						if ggc.Type(lang) == "type_identifier" {
							return ggc.Text(src)
						}
					}
				}
			}
		}
	}
	return ""
}

func goTypeDecl(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "type_spec" {
			if d := goTypeSpec(child, lang, src, file); d != nil {
				drafts = append(drafts, *d)
			}
		}
	}
	return drafts
}

func goTypeSpec(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) *language.SymbolDraft {
	var name string
	kind := symbol.KindType
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(lang) {
		case "type_identifier":
			if name == "" {
				name = child.Text(src)
			}
		case "struct_type":
			kind = symbol.KindStruct
		case "interface_type":
			kind = symbol.KindInterface
		}
	}
	if name == "" {
		return nil
	}
	return &language.SymbolDraft{
		Name:      name,
		Qualified: name,
		Kind:      kind,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  isExported(name),
	}
}

func goConstDecl(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "const_spec" {
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				if gc.Type(lang) == "identifier" {
					name := gc.Text(src)
					drafts = append(drafts, language.SymbolDraft{
						Name:      name,
						Qualified: name,
						Kind:      symbol.KindConstant,
						Location:  nodeLocation(child, file),
						StartByte: child.StartByte(),
						EndByte:   child.EndByte(),
						Exported:  isExported(name),
					})
					break
				}
			}
		}
	}
	return drafts
}

func goVarDecl(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "var_spec" {
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				if gc.Type(lang) == "identifier" {
					name := gc.Text(src)
					drafts = append(drafts, language.SymbolDraft{
						Name:      name,
						Qualified: name,
						Kind:      symbol.KindVariable,
						Location:  nodeLocation(child, file),
						StartByte: child.StartByte(),
						EndByte:   child.EndByte(),
						Exported:  isExported(name),
					})
					break
				}
			}
		}
	}
	return drafts
}

// childText returns the text of the first child node with the given type.
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

func isExported(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsUpper([]rune(name)[0])
}
