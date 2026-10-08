package javascript

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Provider extracts symbols from JavaScript source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "javascript" }
func (p *Provider) Extensions() []string {
	return []string{".js", ".mjs", ".cjs", ".jsx"}
}
func (p *Provider) CacheVersion() string { return "3" }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.JavascriptLanguage()
	tree, err := tsparse.Parse(lang, src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{treediag.ParseFailed(file, err)},
		}, nil
	}
	defer tree.Release()

	root := tree.RootNode()
	drafts := extractSymbols(root, lang, src, file)
	refs, imports := extractReferences(root, lang, src, file)
	return language.Extraction{Symbols: drafts, References: refs, Imports: imports, Diagnostics: treediag.ParseErrors(root, lang, file)}, nil
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
			drafts = append(drafts, jsLexical(inner, lang, src, file, true)...)
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

// extractReferences walks the AST for call, construction, and import references.
func extractReferences(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) ([]language.ReferenceDraft, []language.ImportDraft) {
	c := &jsRefCollector{lang: lang, src: src, file: file}
	c.walk(root, "")
	return c.refs, c.imports
}

type jsRefCollector struct {
	lang    *ts.Language
	src     []byte
	file    source.FileID
	refs    []language.ReferenceDraft
	imports []language.ImportDraft
}

func (c *jsRefCollector) walk(node *ts.Node, container string) {
	t := node.Type(c.lang)
	switch t {
	case "function_declaration":
		name := childText(node, c.lang, c.src, "identifier")
		for i := 0; i < node.ChildCount(); i++ {
			c.walk(node.Child(i), name)
		}
		return
	case "import_statement":
		c.collectImport(node)
		return
	case "call_expression":
		c.collectCall(node, container)
	case "new_expression":
		c.collectNew(node, container)
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.walk(node.Child(i), container)
	}
}

func (c *jsRefCollector) collectImport(node *ts.Node) {
	var path string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(c.lang) == "string" {
			raw := child.Text(c.src)
			if len(raw) >= 2 {
				path = raw[1 : len(raw)-1]
			}
		}
	}
	if path != "" {
		c.imports = append(c.imports, language.ImportDraft{
			Path:     path,
			Location: nodeLocation(node, c.file),
		})
	}
}

func (c *jsRefCollector) collectCall(node *ts.Node, container string) {
	if node.ChildCount() == 0 {
		return
	}
	funcNode := node.Child(0)
	name, recv := c.nameFromExpr(funcNode)
	if name == "" {
		return
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:         name,
		Kind:         "call",
		Container:    container,
		Location:     nodeLocation(funcNode, c.file),
		ReceiverExpr: recv,
		IsCall:       true,
	})
}

func (c *jsRefCollector) collectNew(node *ts.Node, container string) {
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		t := child.Type(c.lang)
		if t == "identifier" || t == "member_expression" {
			name = child.Text(c.src)
			break
		}
	}
	if name == "" {
		return
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:      name,
		Kind:      "construction",
		Container: container,
		Location:  nodeLocation(node, c.file),
	})
}

func (c *jsRefCollector) nameFromExpr(node *ts.Node) (name, recv string) {
	switch node.Type(c.lang) {
	case "identifier":
		return node.Text(c.src), ""
	case "member_expression":
		var obj, prop string
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			switch child.Type(c.lang) {
			case "property_identifier":
				prop = child.Text(c.src)
			case ".":
			default:
				if obj == "" {
					obj = child.Text(c.src)
				}
			}
		}
		return prop, obj
	}
	return "", ""
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
