package typescript

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/ecmascript"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// extractor holds the per-file extraction state. Traversal context (the
// enclosing container, `this` class, proven receiver types, type parameters)
// is threaded explicitly through scope values — there is no mutable global
// state and no traversal-order side channel.
type extractor struct {
	lang    *ts.Language
	src     []byte
	file    source.FileID
	dialect dialect

	symbols  []language.SymbolDraft
	refs     []language.ReferenceDraft
	imports  []language.ImportDraft
	bindings []language.BindingDraft
	exports  []language.ExportDraft

	seen       map[string]bool // kind\x00qualified: one symbol per identity
	isModule   bool            // the file has an import or export (ES module)
	lineStarts []uint32        // byte offsets of line starts, built on demand

	// moduleLocals are the names the program declares other than as its
	// top-level declarations (ecmascript.ProgramLocals): a destructuring
	// pattern, a declaration in a top-level block, loop or catch clause.
	moduleLocals ecmascript.Names

	// CommonJS (commonjs.go): the names the program reassigns, whether it
	// replaces module.exports, and the indexes in exports of its property
	// exports (exports.a = f).
	reassigned  map[string]bool
	cjsReplaced bool
	cjsProperty []int
	// cjsAssigned counts the property assignments (exports.a = …) of each
	// name, whatever their value.
	cjsAssigned map[string]int
	// cjsExports are the indexes in exports of every CommonJS export, and
	// cjsWrites the program's other writes to the exports object.
	cjsExports []int
	cjsWrites  commonJSWrites
	// cjsReplacements counts the top-level replacements of module.exports;
	// cjsReplacement is the ordinal of the one each export of a replacement
	// object comes from (1, 2, …; absent for a property export).
	cjsReplacements int
	cjsReplacement  map[int]int

	// owners are the top-level values whose members the program states,
	// objects the const objects among them (values.go).
	owners, objects map[string]bool

	// cssModules are the local names a default or namespace import binds
	// to a CSS Modules file, and that file (cssmodules.go).
	cssModules map[string]cssModule
	// cssClasses are the local names a named import binds to a class of a
	// CSS Modules file: that file and the class.
	cssClasses map[string][2]string

	// reads are the value reads kept aside until the program's top-level
	// names are known (reads.go).
	reads []pendingRead
}

// dialect is the grammar an extractor reads: TypeScript (and TSX) or
// JavaScript. They share the ECMAScript semantics; only TypeScript has
// types, and only its grammar needs the generic-call correction.
type dialect uint8

const (
	dialectTypeScript dialect = iota
	dialectJavaScript
)

func newExtractor(lang *ts.Language, src []byte, file source.FileID, d dialect) *extractor {
	return &extractor{lang: lang, src: src, file: file, dialect: d, seen: make(map[string]bool)}
}

// top is the scope of a top-level declaration named container ("" outside
// one): the program's own locals are in scope everywhere in it.
func (e *extractor) top(container string) scope {
	return scope{container: container, locals: e.moduleLocals}
}

// topWith is top with the type parameters of the declaration in scope.
func (e *extractor) topWith(container string, tparams map[string]bool) scope {
	sc := e.top(container)
	sc.tparams = tparams
	return sc
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

// addSymbol records a symbol. TypeScript states one identity once (an
// overload's signatures and its implementation are one declaration);
// JavaScript may declare a function twice (the later one wins at run time),
// and each declaration is its own symbol — the index tells them apart.
func (e *extractor) addSymbol(d language.SymbolDraft) bool {
	key := string(d.Kind) + "\x00" + d.Qualified
	if d.Name == "" || (e.seen[key] && e.dialect == dialectTypeScript) {
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
	e.moduleLocals = ecmascript.ProgramLocals(root, e.lang, e.src)
	e.reassigned = ecmascript.Reassigned(root, e.lang, e.src)
	e.owners, e.objects = e.valueOwners(root)
	e.cssModuleImports(root)
	e.scanCommonJSWrites(root)
	defer e.finishCommonJS()
	defer e.finishReads() // once every top-level name and binding is known
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
		case "expression_statement":
			e.commonJSExports(n)
			if !e.assignedMember(n) {
				e.walk(n, e.top(""))
			}
		default:
			if _, ok := e.declaration(n, false); !ok {
				e.walk(n, e.top(""))
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
		e.walk(n, e.top(""))
		return declInfo{}
	}
	e.addSymbol(e.draft(n, name, name, symbol.KindFunction, exported))
	if e.owners[name] && !e.objects[name] && !e.moduleLocals.Values[name] {
		// A constructor function with prototype members (values.go): in
		// its body `this` is the instance it constructs (Strong at most:
		// the resolver caps a value's members).
		sc := e.top(name)
		sc.thisClass = name
		e.walkChildren(n, e.enterFunction(n, sc, true), "name")
		return declInfo{names: []string{name}}
	}
	e.walk(n, e.top(name))
	return declInfo{names: []string{name}}
}

func (e *extractor) interfaceDecl(n *ts.Node, exported bool) declInfo {
	name := e.text(e.field(n, "name"))
	if name == "" {
		return declInfo{}
	}
	e.addSymbol(e.draft(n, name, name, symbol.KindInterface, exported))
	sc := e.topWith(name, setOf(e.typeParamNames(n)))
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
	sc := e.topWith(name, setOf(e.typeParamNames(n)))
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
	sc := e.top(name)
	sc.enumBody = true // a member's initializer names the enum's members first
	e.walk(e.field(n, "body"), sc)
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
		if ecmascript.IsRequire(e.field(d, "value"), e.lang, e.src) {
			// A CommonJS import is a binding (commonjs.go), not a
			// declaration; a form RequireBindings cannot state binds
			// locals (ecmascript.ProgramLocals).
			e.requireBindings(d)
			e.walk(d, e.top(""))
			continue
		}
		if e.typ(nameNode) != "identifier" {
			e.walk(d, e.top("")) // a pattern has no single identity
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
		if v := e.field(d, "value"); e.objects[name] {
			e.walk(e.field(d, "type"), e.top(name))
			e.objectMembers(v, name, exported)
			continue
		}
		e.walkChildren(d, e.top(name), "name")
	}
	return info
}
