package golang

import (
	"context"
	"strings"
	"unicode"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/reference"
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
func (p *Provider) CacheVersion() string        { return "go-9" }

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
	refs, imports, facts := extractGoReferences(root, lang, src, file)
	return language.Extraction{
		Symbols: drafts, References: refs, Imports: imports, Diagnostics: treediag.ParseErrors(root, lang, file),
		// Declared-type facts (typefacts.go): what receiver chains and
		// promoted members are evaluated against.
		TypeFacts: facts,
		// Go's package scoping: an unqualified name is a declaration of this
		// package (or of a dot import); pkg.Name is the imported package's.
		Package:       goPackageName(root, lang, src),
		PackageScoped: true,
	}, nil
}

// goPackageName is the file's package clause name.
func goPackageName(root *ts.Node, lang *ts.Language, src []byte) string {
	for i := 0; i < root.ChildCount(); i++ {
		if c := root.Child(i); c.Type(lang) == "package_clause" {
			for j := 0; j < c.ChildCount(); j++ {
				if id := c.Child(j); id.Type(lang) == "package_identifier" {
					return id.Text(src)
				}
			}
		}
	}
	return ""
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
		// A type alias (type A = B) declares a type name like a type
		// definition: a KindType symbol, which owns no methods of its own.
		if t := child.Type(lang); t == "type_spec" || t == "type_alias" {
			if d := goTypeSpec(child, lang, src, file); d != nil {
				drafts = append(drafts, *d)
				drafts = append(drafts, goInterfaceMethods(child, d.Name, lang, src, file)...)
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
	return goValueDecl(node, lang, src, file, symbol.KindConstant)
}

func goVarDecl(node *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	return goValueDecl(node, lang, src, file, symbol.KindVariable)
}

// goValueDecl returns a symbol for every name a var or const declaration
// declares — of a single spec, of a parenthesized group (var specs inside a
// var_spec_list), and every name of a spec declaring several (var a, b int);
// each symbol spans its spec. The blank identifier declares nothing.
func goValueDecl(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, kind symbol.SymbolKind) []language.SymbolDraft {
	var drafts []language.SymbolDraft
	for _, spec := range goValueSpecs(node, lang) {
		for _, id := range namedChildren(spec, lang, "identifier") {
			name := id.Text(src)
			if name == "_" {
				continue
			}
			drafts = append(drafts, language.SymbolDraft{
				Name:      name,
				Qualified: name,
				Kind:      kind,
				Location:  nodeLocation(spec, file),
				StartByte: spec.StartByte(),
				EndByte:   spec.EndByte(),
				Exported:  isExported(name),
			})
		}
	}
	return drafts
}

// goValueSpecs returns the var_spec / const_spec nodes of a declaration.
func goValueSpecs(decl *ts.Node, lang *ts.Language) []*ts.Node {
	var specs []*ts.Node
	for j := 0; j < decl.ChildCount(); j++ {
		switch ch := decl.Child(j); ch.Type(lang) {
		case "var_spec", "const_spec":
			specs = append(specs, ch)
		case "var_spec_list":
			specs = append(specs, namedChildren(ch, lang, "var_spec")...)
		}
	}
	return specs
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
func extractGoReferences(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) ([]language.ReferenceDraft, []language.ImportDraft, []language.TypeFact) {
	c := &goRefCollector{lang: lang, src: src, file: file, fields: collectGoStructFields(root, lang, src),
		values: goFileValues(root, lang, src), types: goFileTypes(root, lang, src), tparams: goTypeParamScopes(root, lang, src)}
	// Imports first: whether a selector's operand is an import name decides
	// what the selector is (refs_values.go), wherever the import is written.
	for i := 0; i < root.ChildCount(); i++ {
		if ch := root.Child(i); ch.Type(lang) == "import_declaration" {
			c.collectImports(ch)
		}
	}
	facts := c.typeFacts(root)
	c.ownFields = map[string]map[string]bool{}
	for _, f := range facts {
		if f.Struct && f.Complete {
			names := map[string]bool{}
			for _, fd := range f.Fields {
				if fd.Name != "" {
					names[fd.Name] = true
				}
			}
			c.ownFields[f.Decl] = names
		}
	}
	c.walk(root, "", nil)
	return c.refs, c.imports, facts
}

type goRefCollector struct {
	lang    *ts.Language
	src     []byte
	file    source.FileID
	refs    []language.ReferenceDraft
	imports []language.ImportDraft
	fields  goStructFields          // same-file struct field types (receiver evidence)
	locals  map[string]int          // goDeclCounts of the enclosing function; nil outside
	scopes  map[string][]goScope    // local declarations of the enclosing function by scope
	values  map[string]bool         // package-level var / const names of this file
	types   map[string]bool         // package-level type names of this file
	tparams map[string][]goScope    // type parameters of the file's declarations by scope
	handled map[uint64]bool         // spans of call / construction names already recorded
	inError int                     // depth of enclosing ERROR nodes
	ltypes  map[string][]goScope    // local type declarations of the enclosing function by scope
	defs    map[string][]goLocalDef // local declarations of the enclosing function, with what they state (chains.go)
	// ownFields: the field names of the structs this file declares whose
	// fields were all read (TypeFact.Complete), by struct name.
	ownFields map[string]map[string]bool
}

// goFileTypes collects the file's package-level type names.
func goFileTypes(root *ts.Node, lang *ts.Language, src []byte) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < root.ChildCount(); i++ {
		if d := root.Child(i); d.Type(lang) == "type_declaration" {
			for _, spec := range goNamed(d) {
				if ids := namedChildren(spec, lang, "type_identifier"); len(ids) > 0 {
					out[ids[0].Text(src)] = true
				}
			}
		}
	}
	return out
}

// goNamed returns the named children of n.
func goNamed(n *ts.Node) []*ts.Node {
	var out []*ts.Node
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsNamed() {
			out = append(out, ch)
		}
	}
	return out
}

// goFileValues collects the file's package-level var and const names.
func goFileValues(root *ts.Node, lang *ts.Language, src []byte) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < root.ChildCount(); i++ {
		d := root.Child(i)
		if t := d.Type(lang); t != "var_declaration" && t != "const_declaration" {
			continue
		}
		var specs []*ts.Node
		for j := 0; j < d.ChildCount(); j++ {
			switch ch := d.Child(j); ch.Type(lang) {
			case "var_spec", "const_spec":
				specs = append(specs, ch)
			case "var_spec_list":
				specs = append(specs, namedChildren(ch, lang, "var_spec")...)
			}
		}
		for _, spec := range specs {
			for _, id := range namedChildren(spec, lang, "identifier") {
				out[id.Text(src)] = true
			}
		}
	}
	return out
}

// walkFunction walks a function or method declaration as container; its
// own name declares, it is no use.
func (c *goRefCollector) walkFunction(node *ts.Node, container string) {
	counts := make(map[string]int)
	goDeclCounts(node, c.lang, c.src, counts)
	env := goFunctionTypeEnv(node, c.lang, c.src, counts)
	c.locals = counts
	c.scopes = goLocalScopes(node, c.lang, c.src)
	c.ltypes = goLocalTypeScopes(node, c.lang, c.src)
	c.defs = goLocalDefs(node, c.lang, c.src)
	named := false
	for i := 0; i < node.ChildCount(); i++ {
		ch := node.Child(i)
		if t := ch.Type(c.lang); !named && (t == "identifier" || t == "field_identifier") {
			named = true
			continue
		}
		c.walk(ch, container, env)
	}
	c.locals, c.scopes, c.ltypes, c.defs = nil, nil, nil, nil
}

// walkDeclaring walks n but its direct children of type declared (the
// names it declares).
func (c *goRefCollector) walkDeclaring(n *ts.Node, declared string, container string, env goTypeEnv) {
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.Type(c.lang) != declared {
			c.walk(ch, container, env)
		}
	}
}

// walkLeftRight walks a statement whose first child is its left side:
// declared names (:=, skipped), or written targets (=, op=).
func (c *goRefCollector) walkLeftRight(n *ts.Node, container string, env goTypeEnv) {
	if n.ChildCount() == 0 {
		return
	}
	left := n.Child(0)
	declares := false
	for i := 1; i < n.ChildCount(); i++ {
		if n.Child(i).Type(c.lang) == ":=" {
			declares = true
		}
	}
	if !declares {
		if left.Type(c.lang) == "expression_list" {
			for _, t := range goNamed(left) {
				c.writeTarget(t, container, env)
			}
		} else {
			c.writeTarget(left, container, env)
		}
	}
	for i := 1; i < n.ChildCount(); i++ {
		c.walk(n.Child(i), container, env)
	}
}

// walk collects references. env carries the proven receiver types of the
// enclosing function (see receiver_types.go); nil outside functions.
func (c *goRefCollector) walk(node *ts.Node, container string, env goTypeEnv) {
	if node.IsError() {
		// Inside an ERROR the parser recovered by guessing: its structure is
		// no evidence of what a name is (a declaration's name may sit where
		// a use would). Calls keep being collected as before; type uses,
		// values and writes are not (refs_values.go).
		c.inError++
		defer func() { c.inError-- }()
	}
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
		return // collected before the walk
	case "type_spec", "type_alias":
		// The first type_identifier is the declared name; the rest are uses
		// by the declaration (a field's type, an embedded type, ...).
		name := ""
		for i := 0; i < node.ChildCount(); i++ {
			ch := node.Child(i)
			if name == "" && ch.Type(c.lang) == "type_identifier" {
				name = ch.Text(c.src)
				continue
			}
			inner := container
			if inner == "" {
				inner = name
			}
			c.walk(ch, inner, env)
		}
		return
	case "var_spec", "const_spec":
		// A package-level spec declaring one name contains its uses.
		inner := container
		if inner == "" && c.locals == nil {
			if ids := namedChildren(node, c.lang, "identifier"); len(ids) == 1 {
				inner = ids[0].Text(c.src)
			}
		}
		c.walkDeclaring(node, "identifier", inner, env)
		return
	case "parameter_declaration", "variadic_parameter_declaration", "type_parameter_declaration":
		c.walkDeclaring(node, "identifier", container, env)
		return
	case "short_var_declaration", "range_clause", "receive_statement", "assignment_statement":
		c.walkLeftRight(node, container, env)
		return
	case "inc_statement", "dec_statement":
		for _, t := range goNamed(node) {
			c.writeTarget(t, container, env)
		}
		return
	case "type_switch_statement":
		// switch v := x.(type): v is declared.
		for i := 0; i < node.ChildCount(); i++ {
			ch := node.Child(i)
			if ch.Type(c.lang) == "expression_list" && i+1 < node.ChildCount() && node.Child(i+1).Type(c.lang) == ":=" {
				continue
			}
			c.walk(ch, container, env)
		}
		return
	case "type_identifier", "qualified_type":
		c.typeUse(node, container)
		return
	case "identifier":
		c.valueUse(node, container, reference.KindValueReference)
		return
	case "selector_expression":
		c.selectorUse(node, container, env, false)
		return
	case "func_literal":
		if c.locals == nil {
			// A function literal outside any function (a package-level
			// initializer) declares its own locals.
			c.locals = make(map[string]int)
			goDeclCounts(node, c.lang, c.src, c.locals)
			c.scopes = goLocalScopes(node, c.lang, c.src)
			c.ltypes = goLocalTypeScopes(node, c.lang, c.src)
			c.defs = goLocalDefs(node, c.lang, c.src)
			defer func() { c.locals, c.scopes, c.ltypes, c.defs = nil, nil, nil, nil }()
		}
	case "call_expression":
		c.collectCall(node, container, env)
		// fall through to recurse for nested calls
	case "type_conversion_expression":
		c.collectGenericConversion(node, container, env)
	case "composite_literal":
		c.collectComposite(node, container)
		for i := 0; i < node.ChildCount(); i++ {
			ch := node.Child(i)
			if ch.Type(c.lang) == "literal_value" {
				c.literalValue(ch, container, env, node.ChildCount() > 0 && keysAreValues(node.Child(0), c.lang))
			} else {
				c.walk(ch, container, env)
			}
		}
		return
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
				case ".", "dot":
					// The grammar names the dot-import token "dot".
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
	var indices []*ts.Node
	for i := 1; i < node.ChildCount(); i++ {
		if ta := node.Child(i); ta.Type(c.lang) == "type_arguments" {
			indices = append(indices, goNamed(ta)...)
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
		indices = append(indices, index)
		funcNode = operand
	}
	name, recv := c.callNameFromExpr(funcNode)
	if name == "" {
		return
	}
	c.addCall(name, recv, funcNode, indices, container, env)
}

// collectGenericConversion collects T[X](v) and pkg.T(v) read as a
// conversion: the call of a function or the conversion to a type — the
// syntax cannot tell them apart, and Ark records a conversion T(v) written
// as a call the same way.
func (c *goRefCollector) collectGenericConversion(node *ts.Node, container string, env goTypeEnv) {
	if node.ChildCount() == 0 {
		return
	}
	base := node.Child(0)
	var indices []*ts.Node
	if base.Type(c.lang) == "generic_type" {
		if base.ChildCount() == 0 {
			return
		}
		for _, ta := range namedChildren(base, c.lang, "type_arguments") {
			indices = append(indices, goNamed(ta)...)
		}
		if len(indices) == 0 {
			return
		}
		base = base.Child(0)
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
	c.addCall(name, recv, base, indices, container, env)
}

// addCall records a call of name (on recv) at funcNode; indices are the
// subscripts of a subscripted callee f[x](...), nil for a plain call.
//
// f[x](...) is a generic instantiation (or conversion) only if f denotes a
// generic function, method or type; Go syntax cannot tell it from calling an
// element of a slice, array or map of functions, and the parser's choice
// between the two readings is no evidence. So:
//   - a subscript that is a value (a local, a literal, a var / const of the
//     file), or a callee that is one (a local, a var / const of the file):
//     the call of an element, which names no declaration — not recorded,
//     like any element call (fns[0](...));
//   - otherwise the reference can denote only a function, method or type
//     (ReferenceDraft.TargetKinds): resolved to one, it is that generic
//     call; resolved to a variable or constant — an element call after all —
//     it is Unresolved.
func (c *goRefCollector) addCall(name, recv string, funcNode *ts.Node, indices []*ts.Node, container string, env goTypeEnv) {
	var kinds string
	if len(indices) > 0 {
		for _, ix := range indices {
			if c.isValue(ix) {
				return
			}
		}
		if recv == "" && (c.locals[name] > 0 || c.values[name]) {
			return
		}
		kinds = goGenericKinds
	}
	recvType := c.receiverType(recv, funcNode.StartByte(), env)
	capped := c.localCap(name, recv, recvType, funcNode.StartByte())
	chain := ""
	if recv != "" && recvType == "" {
		// Without a proven type, how the receiver is built (chains.go):
		// at most Strong, as derived from other declarations' types.
		if chain = c.receiverChain(funcNode); chain != "" {
			capped = "strong"
		}
	}
	c.markHandled(funcNode)
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:          name,
		Kind:          "call",
		Container:     container,
		Location:      nodeLocation(funcNode, c.file),
		ReceiverExpr:  recv,
		ReceiverType:  recvType,
		ReceiverChain: chain,
		IsCall:        true,
		TargetKinds:   kinds,
		ConfidenceCap: capped,
	})
}

// receiverType returns the proven type of receiver expression recv used at
// at (receiver_types.go), unless a type name it relies on — the variable's
// type, and for a field step the struct whose field it reads — is a type
// parameter or a local type there, not the package's type of that name. (A
// variable named like its type does not shadow it: state := &state{} is of
// the package's type state.)
func (c *goRefCollector) receiverType(recv string, at uint32, env goTypeEnv) string {
	t := env.receiverType(recv, at, c.fields)
	if t == "" {
		return ""
	}
	shadowed := func(name string) bool {
		return name != "" && !strings.Contains(name, ".") && (goShadowed(c.tparams, name, at) || goShadowed(c.ltypes, name, at))
	}
	if shadowed(t) {
		return ""
	}
	if root, _, step := strings.Cut(recv, "."); step {
		if v, ok := env[root]; ok && shadowed(v.typ) {
			return ""
		}
	}
	return t
}

// localCap caps a reference that a local declaration in scope at its
// position shadows (scopes.go): a called name, or the root of a receiver
// without a proven type. The call is then of a local function value, or the
// receiver a variable hiding an import or a type of the same name: never an
// edge to a package-level declaration, though a same-named one may be listed
// as a Candidate.
func (c *goRefCollector) localCap(name, recv, recvType string, at uint32) string {
	if recv == "" {
		if c.shadowedAt(name, at) {
			return "candidate"
		}
		return ""
	}
	root, _, _ := strings.Cut(recv, ".")
	if recvType == "" && (c.shadowedAt(root, at) || c.values[root]) {
		// A local, or a package variable of this file named like an import's
		// guessed name (var v2 = …; v2.Do() beside "math/rand/v2"): a value,
		// never the import.
		return "candidate"
	}
	return ""
}

// goGenericKinds are the declarations f in f[x](...) can be: a generic
// function, method or type.
var goGenericKinds = strings.Join([]string{string(symbol.KindFunction), string(symbol.KindMethod), string(symbol.KindType), string(symbol.KindStruct), string(symbol.KindInterface)}, ",")

// isValue reports whether a subscript is certainly a value — then f[x] is
// an element — by syntax and the file's declarations; a node type the parser
// chose (type_identifier vs identifier) is no evidence. A name it cannot
// place (a type, or a constant of another file) is not certainly a value.
func (c *goRefCollector) isValue(n *ts.Node) bool {
	named := goNamed(n)
	switch n.Type(c.lang) {
	case "type_elem", "parenthesized_type", "parenthesized_expression":
		return len(named) == 1 && c.isValue(named[0])
	case "identifier", "type_identifier":
		nm := n.Text(c.src)
		return c.locals[nm] > 0 || c.values[nm]
	case "pointer_type":
		return len(named) == 1 && c.isValue(named[0]) // *T, or *p of a value
	case "unary_expression":
		// *p, or the pointer type *T; any other operator makes a value.
		if n.ChildCount() == 0 || n.Child(0).Type(c.lang) != "*" || len(named) != 1 {
			return true
		}
		return c.isValue(named[0])
	case "qualified_type", "selector_expression":
		// pkg.T, pkg.Const, or a field of a value.
		if len(named) > 0 {
			op := named[0].Text(c.src)
			return c.locals[op] > 0 || c.values[op]
		}
		return false
	case "generic_type":
		// G[X]: an instantiated type, or an element m[k] of a value m.
		return len(named) > 0 && c.isValue(named[0])
	case "slice_type", "array_type", "map_type", "channel_type", "function_type",
		"struct_type", "interface_type", "negated_type", "implicit_length_array_type":
		return false // type syntax no value expression has
	}
	return true // literals, calls, operators, ...: values
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
	var name, recv string
	switch typeNode.Type(c.lang) {
	case "type_identifier":
		name = typeNode.Text(c.src)
	case "qualified_type":
		// pkg.Type: the package is the receiver, as in pkg.F().
		for i := 0; i < typeNode.ChildCount(); i++ {
			switch ch := typeNode.Child(i); ch.Type(c.lang) {
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
	c.markHandled(typeNode)
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:          name,
		Kind:          "construction",
		Container:     container,
		Location:      nodeLocation(typeNode, c.file),
		ReceiverExpr:  recv,
		ConfidenceCap: c.localCap(name, recv, "", typeNode.StartByte()),
	})
}
