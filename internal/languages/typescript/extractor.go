package typescript

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// extractor holds the per-file extraction state. Traversal context (the
// enclosing container, `this` class, proven receiver types, type parameters)
// is threaded explicitly through scope values — there is no mutable global
// state and no traversal-order side channel.
type extractor struct {
	lang *ts.Language
	src  []byte
	file source.FileID

	symbols  []language.SymbolDraft
	refs     []language.ReferenceDraft
	imports  []language.ImportDraft
	bindings []language.BindingDraft
	exports  []language.ExportDraft

	seen     map[string]bool // kind\x00qualified: one symbol per identity
	isModule bool            // the file has an import or export (ES module)
}

func newExtractor(lang *ts.Language, src []byte, file source.FileID) *extractor {
	return &extractor{lang: lang, src: src, file: file, seen: make(map[string]bool)}
}

// --- small AST helpers (mechanics) ---

func (e *extractor) typ(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Type(e.lang)
}

func (e *extractor) text(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(e.src)
}

func (e *extractor) field(n *ts.Node, name string) *ts.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name, e.lang)
}

// hasToken reports whether n has a direct (anonymous or named) child of type t.
func (e *extractor) hasToken(n *ts.Node, t string) bool {
	for i := range n.ChildCount() {
		if e.typ(n.Child(i)) == t {
			return true
		}
	}
	return false
}

// stringValue returns the unquoted text of a string node, or "".
func (e *extractor) stringValue(n *ts.Node) string {
	if n == nil || e.typ(n) != "string" {
		return ""
	}
	raw := n.Text(e.src)
	if len(raw) >= 2 {
		return raw[1 : len(raw)-1]
	}
	return ""
}

// --- symbols ---

func (e *extractor) addSymbol(d language.SymbolDraft) bool {
	key := string(d.Kind) + "\x00" + d.Qualified
	if d.Name == "" || e.seen[key] {
		return false
	}
	e.seen[key] = true
	e.symbols = append(e.symbols, d)
	return true
}

func (e *extractor) draft(n *ts.Node, name, qualified string, kind symbol.SymbolKind, exported bool) language.SymbolDraft {
	return language.SymbolDraft{
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Location:  nodeLocation(n, e.file),
		StartByte: n.StartByte(),
		EndByte:   n.EndByte(),
		Exported:  exported,
	}
}

func (e *extractor) memberDraft(n *ts.Node, class, name string, kind symbol.SymbolKind, exported bool) language.SymbolDraft {
	d := e.draft(n, name, class+"."+name, kind, exported)
	d.Parent = class
	d.Receiver = class
	return d
}

// --- program / declarations ---

// program dispatches the top-level statements of a module or script.
func (e *extractor) program(root *ts.Node) {
	for i := range root.ChildCount() {
		n := root.Child(i)
		switch e.typ(n) {
		case "comment":
		case "import_statement":
			e.isModule = true
			e.importStatement(n)
		case "export_statement":
			e.isModule = true
			e.exportStatement(n)
		default:
			if _, ok := e.declaration(n, false); !ok {
				e.walk(n, scope{})
			}
		}
	}
}

// declInfo describes the names a top-level declaration introduced.
type declInfo struct {
	names    []string
	typeOnly bool // interface / type alias: usable only in type positions
}

// declaration extracts a top-level declaration. ok is false when n is not a
// declaration form (the caller then walks it for references).
func (e *extractor) declaration(n *ts.Node, exported bool) (info declInfo, ok bool) {
	switch e.typ(n) {
	case "function_declaration", "generator_function_declaration":
		return e.functionDecl(n, exported), true
	case "class_declaration", "abstract_class_declaration":
		return e.classDecl(n, exported), true
	case "interface_declaration":
		return e.interfaceDecl(n, exported), true
	case "type_alias_declaration":
		return e.typeAliasDecl(n, exported), true
	case "enum_declaration":
		return e.enumDecl(n, exported), true
	case "lexical_declaration", "variable_declaration":
		return e.variableDecl(n, exported), true
	case "ambient_declaration", "function_signature":
		return declInfo{}, true // `declare ...` / overload signatures: no identity
	}
	return declInfo{}, false
}

func (e *extractor) functionDecl(n *ts.Node, exported bool) declInfo {
	name := e.text(e.field(n, "name"))
	if name == "" {
		e.walk(n, scope{})
		return declInfo{}
	}
	e.addSymbol(e.draft(n, name, name, symbol.KindFunction, exported))
	e.walk(n, scope{container: name})
	return declInfo{names: []string{name}}
}

func (e *extractor) interfaceDecl(n *ts.Node, exported bool) declInfo {
	name := e.text(e.field(n, "name"))
	if name == "" {
		return declInfo{}
	}
	e.addSymbol(e.draft(n, name, name, symbol.KindInterface, exported))
	sc := scope{container: name, tparams: setOf(e.typeParamNames(n))}
	e.walkTypeParams(n, sc)

	for i := range n.ChildCount() {
		c := n.Child(i)
		switch e.typ(c) {
		case "extends_type_clause":
			e.heritage(c, "inheritance", name, sc)
		case "interface_body", "object_type":
			e.interfaceBody(c, name, sc)
		}
	}
	return declInfo{names: []string{name}, typeOnly: true}
}

func (e *extractor) interfaceBody(body *ts.Node, iface string, sc scope) {
	for i := range body.ChildCount() {
		m := body.Child(i)
		var kind symbol.SymbolKind
		switch e.typ(m) {
		case "method_signature":
			kind = symbol.KindMethod
		case "property_signature":
			kind = symbol.KindProperty
		default:
			continue
		}
		name := e.memberName(m)
		if name == "" {
			continue
		}
		q := iface + "." + name
		e.addSymbol(e.memberDraft(m, iface, name, kind, true))
		msc := sc
		msc.container = q
		if kind == symbol.KindMethod {
			msc = e.enterFunction(m, msc, false)
		}
		e.walkChildren(m, msc, "name")
	}
}

func (e *extractor) typeAliasDecl(n *ts.Node, exported bool) declInfo {
	name := e.text(e.field(n, "name"))
	if name == "" {
		return declInfo{}
	}
	e.addSymbol(e.draft(n, name, name, symbol.KindTypeAlias, exported))
	sc := scope{container: name, tparams: setOf(e.typeParamNames(n))}
	e.walkTypeParams(n, sc)
	e.walk(e.field(n, "value"), sc)
	return declInfo{names: []string{name}, typeOnly: true}
}

func (e *extractor) enumDecl(n *ts.Node, exported bool) declInfo {
	name := e.text(e.field(n, "name"))
	if name == "" {
		return declInfo{}
	}
	e.addSymbol(e.draft(n, name, name, symbol.KindEnum, exported))
	e.walk(e.field(n, "body"), scope{container: name})
	return declInfo{names: []string{name}}
}

// variableDecl extracts `const/let/var` declarators with a plain identifier
// name. Destructuring patterns have no single stable identity and are skipped.
// A declarator whose value is an arrow/function expression is a function.
func (e *extractor) variableDecl(n *ts.Node, exported bool) declInfo {
	isConst := false
	for i := range n.ChildCount() {
		if e.typ(n.Child(i)) == "const" {
			isConst = true
		}
	}
	var info declInfo
	for i := range n.ChildCount() {
		d := n.Child(i)
		if e.typ(d) != "variable_declarator" {
			continue
		}
		nameNode := e.field(d, "name")
		if e.typ(nameNode) != "identifier" {
			e.walk(d, scope{})
			continue
		}
		name := e.text(nameNode)
		kind := symbol.KindVariable
		if isConst {
			kind = symbol.KindConstant
		}
		switch e.typ(e.field(d, "value")) {
		case "arrow_function", "function_expression", "function", "generator_function":
			kind = symbol.KindFunction
		}
		e.addSymbol(e.draft(d, name, name, kind, exported))
		info.names = append(info.names, name)
		e.walkChildren(d, scope{container: name}, "name")
	}
	return info
}
