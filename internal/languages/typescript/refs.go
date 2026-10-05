package typescript

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
)

// addRef appends one reference. Extraction records syntax and structural
// evidence only; resolution interprets it.
func (e *extractor) addRef(at *ts.Node, name, kind, container, receiver, receiverType string, isCall bool) {
	if name == "" || at == nil {
		return
	}
	e.refs = append(e.refs, language.ReferenceDraft{
		Name:         name,
		Kind:         kind,
		Container:    container,
		Location:     nodeLocation(at, e.file),
		ReceiverExpr: receiver,
		ReceiverType: receiverType,
		IsCall:       isCall,
	})
}

// splitQualified splits `a.b.C` (member_expression / nested_type_identifier)
// into the final name and the verbatim receiver text before the last dot. A
// plain `C` yields (C, ""); a malformed `a.` yields no name.
func (e *extractor) splitQualified(n *ts.Node) (name, receiver string) {
	txt := strings.TrimSpace(e.text(n))
	i := strings.LastIndex(txt, ".")
	switch {
	case i < 0:
		return txt, ""
	case i == 0 || i == len(txt)-1:
		return "", ""
	}
	return strings.TrimSpace(txt[i+1:]), strings.TrimSpace(txt[:i])
}

// walkChildren walks the children of n, skipping the child in field skip.
func (e *extractor) walkChildren(n *ts.Node, sc scope, skip string) {
	for i := range n.ChildCount() {
		if skip != "" && n.FieldNameForChild(i, e.lang) == skip {
			continue
		}
		e.walk(n.Child(i), sc)
	}
}

// walkTypeParams walks the constraints / defaults of a declaration's type
// parameters (their names are declarations, not references).
func (e *extractor) walkTypeParams(n *ts.Node, sc scope) {
	e.walk(e.field(n, "type_parameters"), sc)
}

// walk collects references under n. Nested functions keep the enclosing
// container (only top-level declarations and class members are symbols).
func (e *extractor) walk(n *ts.Node, sc scope) {
	if n == nil {
		return
	}
	switch e.typ(n) {
	case "call_expression":
		e.call(n, sc)
	case "new_expression":
		e.construction(n, sc)
	case "type_identifier":
		if name := e.text(n); !sc.tparams[name] { // type parameters are declarations, not uses
			e.addRef(n, name, string(reference.KindTypeUse), sc.container, "", "", false)
		}
		return
	case "nested_type_identifier":
		if name, recv := e.splitQualified(n); name != "" {
			e.addRef(n, name, string(reference.KindTypeUse), sc.container, recv, "", false)
		}
		return
	case "jsx_opening_element", "jsx_self_closing_element":
		e.jsx(n, sc)
	case "arrow_function":
		sc = e.enterFunction(n, sc, true)
	case "function_expression", "function", "generator_function", "function_declaration",
		"generator_function_declaration", "method_definition":
		sc = e.enterFunction(n, sc, false)
	case "class_declaration", "class", "abstract_class_declaration":
		// A nested / expression class has no symbol: no `this` evidence.
		sc.thisClass, sc.props = "", nil
		e.walkChildren(n, sc, "name")
		return
	case "interface_declaration", "type_alias_declaration", "type_parameter", "enum_declaration":
		e.walkChildren(n, sc, "name")
		return
	case "ambient_declaration", "function_signature", "import_statement", "export_statement":
		return
	}
	for i := range n.ChildCount() {
		e.walk(n.Child(i), sc)
	}
}

// unwrap strips syntax that does not change which value is the receiver.
func (e *extractor) unwrap(n *ts.Node) *ts.Node {
	for n != nil {
		switch e.typ(n) {
		case "non_null_expression":
			if n.NamedChildCount() == 0 {
				return n
			}
			n = n.NamedChild(0)
		case "parenthesized_expression":
			if n.NamedChildCount() != 1 {
				return n
			}
			n = n.NamedChild(0)
		default:
			return n
		}
	}
	return n
}

// call records `f()`, `recv.m()` and `recv?.m()`. The receiver expression is
// kept verbatim; ReceiverType is set only when proven (types.go). Computed
// access, `super` and other callee shapes are not guessed at.
func (e *extractor) call(n *ts.Node, sc scope) {
	fn := e.field(n, "function")
	switch e.typ(fn) {
	case "identifier":
		e.addRef(fn, e.text(fn), string(reference.KindCall), sc.container, "", "", true)
	case "member_expression":
		prop := e.field(fn, "property")
		obj := e.unwrap(e.field(fn, "object"))
		if obj == nil || e.typ(obj) == "super" {
			return
		}
		switch e.typ(prop) {
		case "property_identifier", "private_property_identifier":
		default:
			return
		}
		e.addRef(prop, e.text(prop), string(reference.KindCall), sc.container,
			e.text(obj), e.receiverType(obj, prop.StartByte(), sc), true)
	}
}

// construction records `new T()` and `new ns.T()` (receiver = ns).
func (e *extractor) construction(n *ts.Node, sc scope) {
	c := e.field(n, "constructor")
	switch e.typ(c) {
	case "identifier":
		e.addRef(c, e.text(c), string(reference.KindConstruction), sc.container, "", "", false)
	case "member_expression":
		if name, recv := e.splitQualified(c); name != "" {
			e.addRef(c, name, string(reference.KindConstruction), sc.container, recv, "", false)
		}
	}
}

// jsx records component references. Intrinsic elements (lowercase tags such
// as <div />) are never repository references; capitalised identifiers and
// member tags (<Layout.Header />) are structural evidence only — resolution
// still requires local or import evidence.
func (e *extractor) jsx(n *ts.Node, sc scope) {
	var name *ts.Node
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch e.typ(c) {
		case "identifier", "member_expression", "nested_identifier", "jsx_namespace_name":
			name = c
		}
		if name != nil {
			break
		}
	}
	if name == nil {
		return
	}
	switch e.typ(name) {
	case "identifier":
		txt := e.text(name)
		if txt != "" && txt[0] >= 'A' && txt[0] <= 'Z' {
			e.addRef(name, txt, string(reference.KindCall), sc.container, "", "", true)
		}
	case "member_expression", "nested_identifier":
		if nm, recv := e.splitQualified(name); nm != "" {
			e.addRef(name, nm, string(reference.KindCall), sc.container, recv, "", true)
		}
	}
}
