package typescript

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/symbol"
)

// Members of values: the members a top-level value states in the forms whose
// meaning the syntax fixes.
//
//	const X = { m() {}, n: function () {}, p: () => {}, q: 1, r }
//	                                    X.m, X.n, X.p (methods), X.q, X.r (properties);
//	                                    not when X has a declared type (TypeScript)
//	X.m = function () {}                X.m, for a top-level const object X
//	F.prototype.m = function () {}      F.m, for a top-level function F
//	F.prototype = { m() {} }            F.m, likewise (without a spread)
//
// Each is a member symbol (Parent and Receiver X) the resolver finds under a
// receiver of type X (resolver: a value with members, Strong at most — code
// may reassign a member). Only a const object is an owner: a let or var may
// hold another object by the time it is used. A function's own properties
// (F.m = …, a "static" member) are not stated: a member symbol cannot tell
// them from its prototype's.

// valueOwners returns the top-level names whose members the program states
// (owners) — const objects and functions and classes with prototype members
// — and the const objects among them. It is computed before the program is
// walked, so a reference written before the member's statement sees it.
func (e *extractor) valueOwners(root *ts.Node) (owners, objects map[string]bool) {
	objects = map[string]bool{}
	constructors := map[string]bool{}
	for i := range root.NamedChildCount() {
		n := root.NamedChild(i)
		if i > 0 && e.jsdocType(root.NamedChild(i-1)) {
			continue // a JSDoc @type declares the object's type, as an annotation does
		}
		if e.typ(n) == "export_statement" {
			if d := e.field(n, "declaration"); d != nil {
				n = d
			}
		}
		switch e.typ(n) {
		case "lexical_declaration":
			if !e.hasToken(n, "const") {
				continue
			}
			for j := range n.NamedChildCount() {
				d := n.NamedChild(j)
				// A declared type (TypeScript) is the object's type: its
				// members are the type's, not the literal's.
				// A spread ({ m() {}, ...base }) may replace any member:
				// such an object states none.
				if nm, v := e.field(d, "name"), e.field(d, "value"); e.typ(d) == "variable_declarator" && e.typ(nm) == "identifier" &&
					e.typ(v) == "object" && e.field(d, "type") == nil && !e.hasNamed(v, "spread_element") {
					objects[e.text(nm)] = true
				}
			}
		case "function_declaration":
			// A class's prototype members are its own declarations; an
			// assignment to its prototype would be one a class-name
			// receiver (K.b(), a static call) finds as a member.
			// TypeScript does not read a function with prototype
			// assignments as a constructor: JavaScript only.
			if e.dialect == dialectJavaScript {
				constructors[e.text(e.field(n, "name"))] = true
			}
		}
	}
	owners = map[string]bool{}
	for k := range objects {
		owners[k] = true
	}
	for i := range root.NamedChildCount() {
		if owner, _, proto := e.memberAssignment(root.NamedChild(i)); owner != "" && proto && constructors[owner] {
			owners[owner] = true
		}
	}
	return owners, objects
}

// memberAssignment reads a top-level expression statement assigning a
// member: owner.name = value (proto false) or owner.prototype.name = value /
// owner.prototype = value (proto true; name "" for the whole prototype).
func (e *extractor) memberAssignment(stmt *ts.Node) (owner, name string, proto bool) {
	if e.typ(stmt) != "expression_statement" || stmt.NamedChildCount() != 1 {
		return "", "", false
	}
	asg := stmt.NamedChild(0)
	if e.typ(asg) != "assignment_expression" {
		return "", "", false
	}
	left := e.field(asg, "left")
	if e.typ(left) != "member_expression" {
		return "", "", false
	}
	obj, prop := e.field(left, "object"), e.field(left, "property")
	if e.typ(prop) != "property_identifier" {
		return "", "", false
	}
	switch {
	case e.typ(obj) == "identifier" && e.text(prop) == "prototype":
		return e.text(obj), "", true
	case e.typ(obj) == "identifier":
		return e.text(obj), e.text(prop), false
	case e.typ(obj) == "member_expression":
		o, p := e.field(obj, "object"), e.field(obj, "property")
		if e.typ(o) == "identifier" && e.text(p) == "prototype" {
			return e.text(o), e.text(prop), true
		}
	}
	return "", "", false
}

// objectMembers states the members of an object literal value as members of
// owner, and walks their values with the member as container.
func (e *extractor) objectMembers(obj *ts.Node, owner string, exported bool) {
	for i := range obj.NamedChildCount() {
		m := obj.NamedChild(i)
		switch e.typ(m) {
		case "method_definition":
			name := e.memberName(m)
			if name == "" {
				e.walk(m, e.top(owner))
				continue
			}
			e.addSymbol(e.memberDraft(m, owner, name, symbol.KindMethod, exported))
			// Its parameters and locals are in scope in its body.
			e.walkChildren(m, e.enterFunction(m, e.top(owner+"."+name), true), "name")
		case "pair":
			k, v := e.field(m, "key"), e.field(m, "value")
			if e.typ(k) != "property_identifier" {
				e.walk(m, e.top(owner))
				continue
			}
			name := e.text(k)
			e.addSymbol(e.memberDraft(m, owner, name, valueKind(e.typ(v)), exported))
			e.walk(v, e.top(owner+"."+name))
		case "shorthand_property_identifier":
			e.addSymbol(e.memberDraft(m, owner, e.text(m), symbol.KindProperty, exported))
		default:
			e.walk(m, e.top(owner))
		}
	}
}

// valueKind is the member kind of a property whose value is a node of type t.
func valueKind(t string) symbol.SymbolKind {
	switch t {
	case "function_expression", "function", "arrow_function", "generator_function":
		return symbol.KindMethod
	}
	return symbol.KindProperty
}

// assignedMember states the member a top-level assignment declares
// (memberAssignment) when its owner is one valueOwners found, and walks the
// value with the member as container; inside a prototype method `this` is
// an instance of the owner. It reports whether it did.
func (e *extractor) assignedMember(stmt *ts.Node) bool {
	owner, name, proto := e.memberAssignment(stmt)
	if owner == "" || !e.owners[owner] || e.moduleLocals.Values[owner] {
		return false
	}
	if !proto && !e.objects[owner] {
		return false // a function's own property: not stated
	}
	value := e.field(stmt.NamedChild(0), "right")
	if name == "" { // owner.prototype = { ... }
		if e.typ(value) != "object" || e.hasNamed(value, "spread_element") {
			return false
		}
		from := len(e.symbols)
		e.objectMembers(value, owner, true)
		for i := from; i < len(e.symbols); i++ {
			e.symbols[i].Placement = symbol.PlacementInstance // the prototype's
		}
		return true
	}
	d := e.memberDraft(stmt, owner, name, valueKind(e.typ(value)), true)
	if proto {
		d.Placement = symbol.PlacementInstance
	}
	e.addSymbol(d)
	sc := e.top(owner + "." + name)
	switch e.typ(value) {
	case "function_expression", "function", "generator_function":
		// A prototype method's `this` is an instance of the owner: walk its
		// body keeping it (enterFunction would drop it). An arrow function
		// keeps the module's `this` instead: walked by the default case.
		if proto {
			sc.thisClass = owner
		}
		fsc := e.enterFunction(value, sc, true)
		e.walkChildren(value, fsc, "name")
	default:
		e.walk(value, sc)
	}
	return true
}

// hasNamed reports whether n has a direct named child of type t.
func (e *extractor) hasNamed(n *ts.Node, t string) bool {
	for i := range n.NamedChildCount() {
		if e.typ(n.NamedChild(i)) == t {
			return true
		}
	}
	return false
}

// jsdocType reports whether n is a JSDoc comment stating a type (@type):
// the declaration after it has a declared type.
func (e *extractor) jsdocType(n *ts.Node) bool {
	if e.typ(n) != "comment" {
		return false
	}
	t := e.text(n)
	return strings.HasPrefix(t, "/**") && strings.Contains(t, "@type")
}
