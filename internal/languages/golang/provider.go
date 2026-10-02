package golang

import (
	"context"
	"unicode"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

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
func (p *Provider) CacheVersion() string        { return "1" }

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
	refs, imports := extractGoReferences(root, lang, src, file)
	return language.Extraction{Symbols: drafts, References: refs, Imports: imports}, nil
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

// extractGoReferences walks the AST and collects syntactic references and imports.
func extractGoReferences(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) ([]language.ReferenceDraft, []language.ImportDraft) {
	c := &goRefCollector{lang: lang, src: src, file: file}
	c.walk(root, "")
	return c.refs, c.imports
}

type goRefCollector struct {
	lang    *ts.Language
	src     []byte
	file    source.FileID
	refs    []language.ReferenceDraft
	imports []language.ImportDraft
}

func (c *goRefCollector) walk(node *ts.Node, container string) {
	t := node.Type(c.lang)
	switch t {
	case "function_declaration":
		name := childText(node, c.lang, c.src, "identifier")
		for i := 0; i < node.ChildCount(); i++ {
			c.walk(node.Child(i), name)
		}
		return
	case "method_declaration":
		var name, recv string
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			ct := child.Type(c.lang)
			if ct == "parameter_list" && recv == "" {
				recv = goReceiverType(child, c.lang, c.src)
			} else if (ct == "field_identifier" || ct == "identifier") && name == "" {
				name = child.Text(c.src)
			}
		}
		qualified := name
		if recv != "" {
			qualified = recv + "." + name
		}
		for i := 0; i < node.ChildCount(); i++ {
			c.walk(node.Child(i), qualified)
		}
		return
	case "import_declaration":
		c.collectImports(node)
		return // don't recurse into imports
	case "call_expression":
		c.collectCall(node, container)
		// fall through to recurse for nested calls
	case "composite_literal":
		c.collectComposite(node, container)
		// fall through to recurse
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.walk(node.Child(i), container)
	}
}

func (c *goRefCollector) collectImports(node *ts.Node) {
	c.walkImportNode(node)
}

// walkImportNode recurses through import_declaration and import_spec_list to
// find import_spec nodes and single interpreted_string_literal imports.
func (c *goRefCollector) walkImportNode(node *ts.Node) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(c.lang) {
		case "import_spec_list":
			c.walkImportNode(child)
		case "import_spec":
			var path, alias string
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				switch gc.Type(c.lang) {
				case "interpreted_string_literal":
					path = goStringLiteralContent(gc, c.lang, c.src)
				case "package_identifier":
					alias = gc.Text(c.src)
				case ".":
					alias = "."
				case "blank_identifier":
					alias = "_"
				}
			}
			if path != "" {
				c.imports = append(c.imports, language.ImportDraft{
					Path:     path,
					Alias:    alias,
					Location: nodeLocation(child, c.file),
				})
			}
		case "interpreted_string_literal":
			// single-line: import "fmt"
			path := goStringLiteralContent(child, c.lang, c.src)
			if path != "" {
				c.imports = append(c.imports, language.ImportDraft{
					Path:     path,
					Location: nodeLocation(child, c.file),
				})
			}
		}
	}
}

// goStringLiteralContent extracts the unquoted content from an interpreted_string_literal node.
// It prefers the interpreted_string_literal_content child if present, otherwise strips quotes.
func goStringLiteralContent(node *ts.Node, lang *ts.Language, src []byte) string {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "interpreted_string_literal_content" {
			return child.Text(src)
		}
	}
	// fallback: strip surrounding quotes
	raw := node.Text(src)
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}

func (c *goRefCollector) collectCall(node *ts.Node, container string) {
	// call_expression: first child = function expr, last child = argument_list
	if node.ChildCount() == 0 {
		return
	}
	funcNode := node.Child(0)
	name, recv := c.callNameFromExpr(funcNode)
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

func (c *goRefCollector) callNameFromExpr(node *ts.Node) (name, recv string) {
	switch node.Type(c.lang) {
	case "identifier":
		return node.Text(c.src), ""
	case "selector_expression":
		// children: operand, ".", field_identifier
		var operand, field string
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			switch child.Type(c.lang) {
			case "field_identifier":
				field = child.Text(c.src)
			case ".":
				// skip
			default:
				if operand == "" {
					operand = child.Text(c.src)
				}
			}
		}
		return field, operand
	default:
		// complex expression (chained calls, etc.): skip
		return "", ""
	}
}

func (c *goRefCollector) collectComposite(node *ts.Node, container string) {
	// composite_literal: first child is the type (type_identifier or qualified_type)
	if node.ChildCount() == 0 {
		return
	}
	typeNode := node.Child(0)
	var name string
	switch typeNode.Type(c.lang) {
	case "type_identifier":
		name = typeNode.Text(c.src)
	case "qualified_type":
		// pkg.Type → use full text
		name = typeNode.Text(c.src)
	}
	if name == "" {
		return
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:      name,
		Kind:      "construction",
		Container: container,
		Location:  nodeLocation(typeNode, c.file),
	})
}
