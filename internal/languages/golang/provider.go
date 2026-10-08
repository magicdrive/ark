package golang

import (
	"context"
	"strings"
	"unicode"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Provider extracts symbols from Go source files.
type Provider struct{}

// NewProvider returns a ready-to-use Go Provider.
func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "go" }
func (p *Provider) Extensions() []string        { return []string{".go"} }
func (p *Provider) CacheVersion() string        { return "go-5" }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.GoLanguage()
	tree, err := tsparse.Parse(lang, src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{treediag.ParseFailed(file, err)},
		}, nil
	}
	defer tree.Release()

	root := tree.RootNode()
	drafts := extractGoSymbols(root, lang, src, file)
	refs, imports := extractGoReferences(root, lang, src, file)
	return language.Extraction{Symbols: drafts, References: refs, Imports: imports, Diagnostics: treediag.ParseErrors(root, lang, file)}, nil
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
	seenReceiver := false
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		t := child.Type(lang)
		if t == "parameter_list" && !seenReceiver {
			// Only the first parameter list is the receiver; the next one
			// holds the method's own parameters.
			seenReceiver = true
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
				if name := goReceiverTypeName(child.Child(j), lang, src); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// goReceiverTypeName is the base type name of a receiver type: T, *T, T[P]
// or *T[P].
func goReceiverTypeName(n *ts.Node, lang *ts.Language, src []byte) string {
	switch n.Type(lang) {
	case "type_identifier":
		return n.Text(src)
	case "pointer_type", "generic_type":
		for k := 0; k < n.ChildCount(); k++ {
			c := n.Child(k)
			switch c.Type(lang) {
			case "type_identifier":
				return c.Text(src)
			case "generic_type":
				return goReceiverTypeName(c, lang, src)
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
	c := &goRefCollector{lang: lang, src: src, file: file, fields: collectGoStructFields(root, lang, src)}
	c.walk(root, "", nil)
	return c.refs, c.imports
}

type goRefCollector struct {
	lang    *ts.Language
	src     []byte
	file    source.FileID
	refs    []language.ReferenceDraft
	imports []language.ImportDraft
	fields  goStructFields // same-file struct field types (receiver evidence)
	locals  map[string]int // goDeclCounts of the enclosing function; nil outside
}

// walkFunction walks a function or method declaration as container.
func (c *goRefCollector) walkFunction(node *ts.Node, container string) {
	counts := make(map[string]int)
	goDeclCounts(node, c.lang, c.src, counts)
	env := goFunctionTypeEnv(node, c.lang, c.src, counts)
	c.locals = counts
	for i := 0; i < node.ChildCount(); i++ {
		c.walk(node.Child(i), container, env)
	}
	c.locals = nil
}

// walk collects references. env carries the proven receiver types of the
// enclosing function (see receiver_types.go); nil outside functions.
func (c *goRefCollector) walk(node *ts.Node, container string, env goTypeEnv) {
	t := node.Type(c.lang)
	switch t {
	case "function_declaration":
		name := childText(node, c.lang, c.src, "identifier")
		c.walkFunction(node, name)
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
		c.walkFunction(node, qualified)
		return
	case "import_declaration":
		c.collectImports(node)
		return // don't recurse into imports
	case "func_literal":
		if c.locals == nil {
			// A function literal outside any function (a package-level
			// initializer) declares its own locals.
			c.locals = make(map[string]int)
			goDeclCounts(node, c.lang, c.src, c.locals)
			defer func() { c.locals = nil }()
		}
	case "call_expression":
		c.collectCall(node, container, env)
		// fall through to recurse for nested calls
	case "type_conversion_expression":
		c.collectGenericConversion(node, container, env)
	case "composite_literal":
		c.collectComposite(node, container)
		// fall through to recurse
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.walk(node.Child(i), container, env)
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

func (c *goRefCollector) collectCall(node *ts.Node, container string, env goTypeEnv) {
	// call_expression: function expr, optional type_arguments, argument_list
	if node.ChildCount() == 0 {
		return
	}
	funcNode := node.Child(0)
	subscripted := false
	for i := 1; i < node.ChildCount(); i++ {
		if node.Child(i).Type(c.lang) == "type_arguments" {
			subscripted = true
		}
	}
	if funcNode.Type(c.lang) == "index_expression" {
		// f[x](...): the index may be a type argument.
		var operand, index *ts.Node
		for i := 0; i < funcNode.ChildCount(); i++ {
			if ch := funcNode.Child(i); ch.IsNamed() {
				if operand == nil {
					operand = ch
				} else {
					index = ch
				}
			}
		}
		if operand == nil || index == nil || !goMayBeTypeArgument(index, c.lang) {
			return
		}
		subscripted = true
		funcNode = operand
	}
	name, recv := c.callNameFromExpr(funcNode)
	if name == "" {
		return
	}
	c.addCall(name, recv, funcNode, subscripted, container, env)
}

// collectGenericConversion collects T[X](v) and pkg.T(v) read as a
// conversion: the call of a function or the conversion to a type — the
// syntax cannot tell them apart, and Ark records a conversion T(v) written
// as a call the same way.
func (c *goRefCollector) collectGenericConversion(node *ts.Node, container string, env goTypeEnv) {
	if node.ChildCount() == 0 {
		return
	}
	base, subscripted := node.Child(0), false
	if base.Type(c.lang) == "generic_type" {
		if base.ChildCount() == 0 {
			return
		}
		base, subscripted = base.Child(0), true
	}
	var name, recv string
	switch base.Type(c.lang) {
	case "type_identifier":
		name = base.Text(c.src)
	case "qualified_type":
		// pkg.T, or a value's member v.f the grammar read as a type.
		for i := 0; i < base.ChildCount(); i++ {
			switch ch := base.Child(i); ch.Type(c.lang) {
			case "package_identifier":
				recv = ch.Text(c.src)
			case "type_identifier":
				name = ch.Text(c.src)
			}
		}
	}
	if name == "" {
		return
	}
	c.addCall(name, recv, base, subscripted, container, env)
}

// addCall records a call of name (on recv) at funcNode. A subscripted call
// f[x](...) is a generic instantiation only if f denotes a generic function
// or type; Go syntax cannot tell it from calling an element of a value:
//   - f, or the receiver of f, declared in the enclosing function: a value
//     (functions and types declared in a function are never generic), so
//     f[x] is an element of that value and the call names nothing — not
//     recorded, like the call of any element (fns[0](...));
//   - a member r.f of a value r (not a package): a generic method or an
//     element of a field — the name is right only in the first case, so the
//     reference is never claimed above Candidate;
//   - otherwise a package-level name or pkg.Name: recorded as a call.
func (c *goRefCollector) addCall(name, recv string, funcNode *ts.Node, subscripted bool, container string, env goTypeEnv) {
	capConf := ""
	if subscripted {
		root := recv
		if i := strings.IndexByte(recv, '.'); i >= 0 {
			root = recv[:i]
		}
		switch {
		case recv == "" && c.locals[name] > 0:
			return
		case recv != "" && (c.locals[root] > 0 || !isGoIdent(recv)):
			capConf = "candidate"
		}
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:          name,
		Kind:          "call",
		Container:     container,
		Location:      nodeLocation(funcNode, c.file),
		ReceiverExpr:  recv,
		ReceiverType:  env.receiverType(recv, funcNode.StartByte(), c.fields),
		IsCall:        true,
		ConfidenceCap: capConf,
	})
}

// goMayBeTypeArgument reports whether an index expression's index could be
// a type argument (T, pkg.T, *T) rather than only a value.
func goMayBeTypeArgument(n *ts.Node, lang *ts.Language) bool {
	switch n.Type(lang) {
	case "identifier", "type_identifier", "selector_expression", "qualified_type", "generic_type":
		return true
	case "unary_expression":
		return n.ChildCount() > 0 && n.Child(0).Type(lang) == "*"
	}
	return false
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
	if typeNode.Type(c.lang) == "generic_type" && typeNode.ChildCount() > 0 {
		// T[X]{...} constructs T.
		typeNode = typeNode.Child(0)
	}
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
