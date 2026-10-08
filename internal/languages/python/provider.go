package python

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Provider extracts symbols from Python source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "python" }
func (p *Provider) Extensions() []string        { return []string{".py", ".pyw"} }
func (p *Provider) CacheVersion() string        { return "2" }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.PythonLanguage()
	parser := ts.NewParser(lang)
	tree, err := parser.Parse(src)
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

// extractReferences walks the AST for call and import references.
func extractReferences(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) ([]language.ReferenceDraft, []language.ImportDraft) {
	c := &pyRefCollector{lang: lang, src: src, file: file}
	c.walk(root, "")
	return c.refs, c.imports
}

type pyRefCollector struct {
	lang    *ts.Language
	src     []byte
	file    source.FileID
	refs    []language.ReferenceDraft
	imports []language.ImportDraft
}

func (c *pyRefCollector) walk(node *ts.Node, container string) {
	t := node.Type(c.lang)
	switch t {
	case "function_definition":
		name := childText(node, c.lang, c.src, "identifier")
		for i := 0; i < node.ChildCount(); i++ {
			c.walk(node.Child(i), name)
		}
		return
	case "import_statement":
		c.collectImport(node)
		return
	case "import_from_statement":
		c.collectFromImport(node)
		return
	case "call":
		c.collectCall(node, container)
		// recurse for nested calls
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.walk(node.Child(i), container)
	}
}

func (c *pyRefCollector) collectImport(node *ts.Node) {
	// import foo, bar  or  import foo as bar
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(c.lang) {
		case "dotted_name":
			c.imports = append(c.imports, language.ImportDraft{
				Path:     child.Text(c.src),
				Location: nodeLocation(child, c.file),
			})
		case "aliased_import":
			// name as alias
			var path, alias string
			for j := 0; j < child.ChildCount(); j++ {
				gc := child.Child(j)
				switch gc.Type(c.lang) {
				case "dotted_name":
					if path == "" {
						path = gc.Text(c.src)
					}
				case "identifier":
					alias = gc.Text(c.src)
				}
			}
			if path != "" {
				c.imports = append(c.imports, language.ImportDraft{
					Path:     path,
					Alias:    alias,
					Location: nodeLocation(child, c.file),
				})
			}
		}
	}
}

func (c *pyRefCollector) collectFromImport(node *ts.Node) {
	// from foo import bar, baz
	var module string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(c.lang) == "dotted_name" && module == "" {
			module = child.Text(c.src)
		}
	}
	if module != "" {
		c.imports = append(c.imports, language.ImportDraft{
			Path:     module,
			Location: nodeLocation(node, c.file),
		})
	}
}

func (c *pyRefCollector) collectCall(node *ts.Node, container string) {
	// call: function arguments
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

func (c *pyRefCollector) nameFromExpr(node *ts.Node) (name, recv string) {
	switch node.Type(c.lang) {
	case "identifier":
		return node.Text(c.src), ""
	case "attribute":
		// object.attribute
		var obj, attr string
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			switch child.Type(c.lang) {
			case "identifier":
				if attr == "" && i == node.ChildCount()-1 {
					attr = child.Text(c.src)
				} else if obj == "" {
					obj = child.Text(c.src)
				}
			case ".":
			}
		}
		if attr == "" && node.ChildCount() >= 3 {
			attr = node.Child(2).Text(c.src)
			obj = node.Child(0).Text(c.src)
		}
		return attr, obj
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
