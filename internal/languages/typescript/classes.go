package typescript

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/symbol"
)

// classDecl extracts a top-level class and its members. Members are
// first-class symbols with stable containment:
//
//	Qualified = Class + "." + member, Parent = Class, Receiver = Class
//
// Constructor parameter properties become property members. A get/set pair
// with the same name is ONE KindProperty symbol: when the accessors are
// adjacent the symbol spans both, otherwise the first accessor is the
// representative span (documented limitation).
func (e *extractor) classDecl(n *ts.Node, exported bool) declInfo {
	name := e.text(e.field(n, "name"))
	body := e.field(n, "body")
	if name == "" || body == nil {
		e.walk(n, scope{})
		return declInfo{}
	}
	e.addSymbol(e.draft(n, name, name, symbol.KindClass, exported))

	csc := scope{container: name, tparams: setOf(e.typeParamNames(n))}
	e.walkTypeParams(n, csc)
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch e.typ(c) {
		case "class_heritage":
			e.classHeritage(c, name, csc)
		case "decorator":
			e.walk(c, csc)
		}
	}

	csc.thisClass = name
	csc.props = e.classProps(body, csc.tparams)

	accessorIdx := map[string]int{} // accessor name → index into e.symbols
	prevAccessor := ""              // accessor name of the previous member, if any
	for i := range body.ChildCount() {
		m := body.Child(i)
		cur := ""
		switch e.typ(m) {
		case "method_definition", "method_signature", "abstract_method_signature":
			cur = e.method(m, name, csc, accessorIdx, prevAccessor)
		case "public_field_definition":
			e.field_(m, name, csc)
		}
		prevAccessor = cur
	}
	return declInfo{names: []string{name}}
}

// classHeritage emits extends / implements references. `extends` (a value
// expression) and `implements` (types) each get one reference per named type.
func (e *extractor) classHeritage(h *ts.Node, class string, sc scope) {
	for i := range h.ChildCount() {
		c := h.Child(i)
		switch e.typ(c) {
		case "extends_clause":
			e.heritage(c, "inheritance", class, sc)
		case "implements_clause":
			e.heritage(c, "implementation", class, sc)
		}
	}
}

// heritage emits one reference of the given kind for each type named in an
// extends / implements clause. Type arguments become ordinary type uses.
func (e *extractor) heritage(clause *ts.Node, kind, container string, sc scope) {
	hsc := sc
	hsc.container = container
	for i := range clause.ChildCount() {
		c := clause.Child(i)
		switch e.typ(c) {
		case "identifier", "type_identifier":
			e.addRef(c, e.text(c), kind, container, "", "", false)
		case "member_expression", "nested_type_identifier", "nested_identifier":
			if name, recv := e.splitQualified(c); name != "" {
				e.addRef(c, name, kind, container, recv, "", false)
			}
		case "generic_type":
			if nm := e.field(c, "name"); nm != nil {
				if name, recv := e.splitQualified(nm); name != "" {
					e.addRef(nm, name, kind, container, recv, "", false)
				}
			}
			e.walk(e.field(c, "type_arguments"), hsc)
		case "type_arguments":
			e.walk(c, hsc)
		}
	}
}

// memberName returns the declared name of a member, or "" when it is not a
// stable identifier (computed or string-literal names).
func (e *extractor) memberName(m *ts.Node) string {
	nameNode := e.field(m, "name")
	// `static async *gen()` is parsed with `async` in the name field; the real
	// name is the last name token before the parameters.
	if e.hasToken(m, "*") {
		for i := range m.ChildCount() {
			c := m.Child(i)
			switch e.typ(c) {
			case "property_identifier", "private_property_identifier":
				nameNode = c
			case "formal_parameters":
				goto done
			}
		}
	}
done:
	switch e.typ(nameNode) {
	case "property_identifier", "private_property_identifier":
		return e.text(nameNode)
	}
	return ""
}

// exportedMember reports whether a class member is visible outside the class.
func (e *extractor) exportedMember(m *ts.Node, name string) bool {
	if strings.HasPrefix(name, "#") {
		return false
	}
	for i := range m.ChildCount() {
		c := m.Child(i)
		if e.typ(c) == "accessibility_modifier" {
			t := e.text(c)
			return t != "private" && t != "protected"
		}
	}
	return true
}

// method extracts a method / constructor / accessor / abstract signature and
// walks its references. It returns the accessor name (for adjacency tracking)
// or "" for non-accessors.
func (e *extractor) method(m *ts.Node, class string, csc scope, accessorIdx map[string]int, prevAccessor string) string {
	name := e.memberName(m)
	if name == "" {
		return ""
	}
	q := class + "." + name
	exp := e.exportedMember(m, name)

	accessor := ""
	kind := symbol.KindMethod
	switch {
	case name == "constructor" && e.typ(m) == "method_definition":
		kind = symbol.KindConstructor
	case e.typ(m) == "method_definition" && (e.hasToken(m, "get") || e.hasToken(m, "set")):
		kind = symbol.KindProperty
		accessor = name
	}

	if accessor != "" {
		if idx, dup := accessorIdx[accessor]; dup {
			if prevAccessor == accessor { // adjacent: span both accessors
				e.symbols[idx].EndByte = m.EndByte()
				e.symbols[idx].Location.Range.End = nodeLocation(m, e.file).Range.End
			}
		} else {
			accessorIdx[accessor] = len(e.symbols)
			e.addSymbol(e.memberDraft(m, class, name, kind, exp))
		}
	} else {
		e.addSymbol(e.memberDraft(m, class, name, kind, exp))
	}

	msc := csc
	msc.container = q
	msc = e.enterFunction(m, msc, true) // methods keep the class `this`
	e.walkChildren(m, msc, "name")

	if kind == symbol.KindConstructor {
		e.parameterProperties(m, class, csc)
	}
	return accessor
}

// parameterProperties emits `constructor(private repo: Repo)` parameters as
// property members of the class.
func (e *extractor) parameterProperties(ctor *ts.Node, class string, csc scope) {
	params := e.field(ctor, "parameters")
	if params == nil {
		return
	}
	for i := range params.ChildCount() {
		p := params.Child(i)
		if t := e.typ(p); t != "required_parameter" && t != "optional_parameter" {
			continue
		}
		if !e.hasToken(p, "accessibility_modifier") && !e.hasToken(p, "readonly") {
			continue
		}
		pat := e.field(p, "pattern")
		if e.typ(pat) != "identifier" {
			continue
		}
		name := e.text(pat)
		e.addSymbol(e.memberDraft(p, class, name, symbol.KindProperty, e.exportedMember(p, name)))
	}
}

// field_ extracts a class field. A field initialised with an arrow/function
// expression is a method (callable member); anything else is a property.
func (e *extractor) field_(m *ts.Node, class string, csc scope) {
	name := e.memberName(m)
	if name == "" {
		return
	}
	kind := symbol.KindProperty
	switch e.typ(e.field(m, "value")) {
	case "arrow_function", "function_expression", "function":
		kind = symbol.KindMethod
	}
	e.addSymbol(e.memberDraft(m, class, name, kind, e.exportedMember(m, name)))
	fsc := csc
	fsc.container = class + "." + name
	e.walkChildren(m, fsc, "name")
}
