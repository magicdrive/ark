package typescript

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

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
func (p *Provider) CacheVersion() string { return "1" }

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
	refs, imports := extractReferences(root, tsLang, src, file)
	return language.Extraction{Symbols: drafts, References: refs, Imports: imports}, nil
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
			drafts = append(drafts, tsLexical(inner, lang, src, file, true)...)
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

// extractReferences walks the AST for call, construction, type-use, and import references.
func extractReferences(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) ([]language.ReferenceDraft, []language.ImportDraft) {
	c := &tsRefCollector{lang: lang, src: src, file: file}
	c.walk(root, "")
	return c.refs, c.imports
}

type tsRefCollector struct {
	lang    *ts.Language
	src     []byte
	file    source.FileID
	refs    []language.ReferenceDraft
	imports []language.ImportDraft
}

func (c *tsRefCollector) walk(node *ts.Node, container string) {
	t := node.Type(c.lang)
	switch t {
	case "function_declaration":
		name := childText(node, c.lang, c.src, "identifier")
		for i := 0; i < node.ChildCount(); i++ {
			c.walk(node.Child(i), name)
		}
		return
	case "method_definition":
		name := childText(node, c.lang, c.src, "property_identifier")
		if name == "" {
			name = childText(node, c.lang, c.src, "identifier")
		}
		for i := 0; i < node.ChildCount(); i++ {
			c.walk(node.Child(i), name)
		}
		return
	case "import_statement":
		c.collectImport(node)
		return
	case "call_expression":
		c.collectCall(node, container)
		// recurse for nested
	case "new_expression":
		c.collectNew(node, container)
		// recurse
	case "type_reference":
		c.collectTypeRef(node, container)
		return // leaf
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.walk(node.Child(i), container)
	}
}

func (c *tsRefCollector) collectImport(node *ts.Node) {
	// import_statement: "import" import_clause "from" string
	// or: import "module" (side-effect)
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

func (c *tsRefCollector) collectCall(node *ts.Node, container string) {
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

func (c *tsRefCollector) collectNew(node *ts.Node, container string) {
	// new_expression: "new" constructor arguments
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

func (c *tsRefCollector) collectTypeRef(node *ts.Node, container string) {
	// type_reference: a type name used in annotations
	name := node.Text(c.src)
	if name == "" {
		return
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:      name,
		Kind:      "type_use",
		Container: container,
		Location:  nodeLocation(node, c.file),
	})
}

func (c *tsRefCollector) nameFromExpr(node *ts.Node) (name, recv string) {
	switch node.Type(c.lang) {
	case "identifier":
		return node.Text(c.src), ""
	case "member_expression":
		// object.property
		var obj, prop string
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			switch child.Type(c.lang) {
			case "property_identifier":
				prop = child.Text(c.src)
			case ".":
				// skip
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
