package typescript

import (
	ts "github.com/odvcencio/gotreesitter"
)

// Receiver type evidence (language.ReferenceDraft.ReceiverType).
//
// Only the declared type written in source is used — never inference:
//   - `this` → the enclosing class (not inside non-arrow function expressions
//     or object-literal methods, where `this` is rebound);
//   - a parameter / variable with an explicit simple type annotation;
//   - `const x = new T()` (the declared type of a const initialised by a
//     construction is T);
//   - `this.field` where the field of the enclosing class has an explicit
//     annotation, a `new T()` initialiser, or is a constructor parameter
//     property.
//
// Only simple type names are recorded (T, T<...>); unions, intersections,
// qualified, literal and builtin types, and the type parameters in scope,
// yield no evidence. A name declared more than once inside the function
// (shadowing, closure parameters, catch/for variables, destructuring) is
// poisoned. Narrowing, control flow and return types are never consulted.

// varType is one proven local variable type.
type varType struct {
	typ  string
	from uint32 // evidence applies to uses at or after this byte offset
}

// scope is the explicit traversal context of the reference collector.
type scope struct {
	container string             // qualified container symbol, "" at top level
	thisClass string             // enclosing class for `this`, "" when unproven
	props     map[string]string  // typed instance properties of thisClass
	vars      map[string]varType // proven local variable types
	tparams   map[string]bool    // type parameters in scope
}

func setOf(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func unionSet(a map[string]bool, names []string) map[string]bool {
	if len(names) == 0 {
		return a
	}
	m := make(map[string]bool, len(a)+len(names))
	for k := range a {
		m[k] = true
	}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// typeParamNames returns the type parameters declared by a node
// (function / class / interface / alias / arrow function).
func (e *extractor) typeParamNames(n *ts.Node) []string {
	tp := e.field(n, "type_parameters")
	if tp == nil {
		return nil
	}
	var out []string
	for i := range tp.ChildCount() {
		c := tp.Child(i)
		if e.typ(c) == "type_parameter" {
			if nm := e.field(c, "name"); nm != nil {
				out = append(out, e.text(nm))
			}
		}
	}
	return out
}

// simpleType returns the class-like type name of a type annotation (or bare
// type node), or "" when it is not a plain named type.
func (e *extractor) simpleType(n *ts.Node, tparams map[string]bool) string {
	if n == nil {
		return ""
	}
	if e.typ(n) == "type_annotation" {
		for i := range n.ChildCount() {
			if c := n.Child(i); c.IsNamed() {
				return e.simpleType(c, tparams)
			}
		}
		return ""
	}
	name := ""
	switch e.typ(n) {
	case "type_identifier":
		name = e.text(n)
	case "generic_type":
		if nm := e.field(n, "name"); e.typ(nm) == "type_identifier" {
			name = e.text(nm)
		}
	}
	if name == "" || tparams[name] {
		return ""
	}
	return name
}

// constructedType returns T for `new T(...)`, or "".
func (e *extractor) constructedType(n *ts.Node) string {
	if e.typ(n) != "new_expression" {
		return ""
	}
	if c := e.field(n, "constructor"); e.typ(c) == "identifier" {
		return e.text(c)
	}
	return ""
}

// classProps indexes the typed instance properties of a class body: annotated
// fields, `new T()`-initialised fields and constructor parameter properties.
func (e *extractor) classProps(body *ts.Node, tparams map[string]bool) map[string]string {
	props := map[string]string{}
	for i := range body.ChildCount() {
		m := body.Child(i)
		switch e.typ(m) {
		case "public_field_definition":
			if e.hasToken(m, "static") {
				continue
			}
			name := e.memberName(m)
			typ := e.simpleType(e.field(m, "type"), tparams)
			if typ == "" {
				typ = e.constructedType(e.field(m, "value"))
			}
			if name != "" && typ != "" {
				props[name] = typ
			}
		case "method_definition":
			if e.memberName(m) != "constructor" {
				continue
			}
			params := e.field(m, "parameters")
			if params == nil {
				continue
			}
			for j := range params.ChildCount() {
				p := params.Child(j)
				if t := e.typ(p); t != "required_parameter" && t != "optional_parameter" {
					continue
				}
				if !e.hasToken(p, "accessibility_modifier") && !e.hasToken(p, "readonly") {
					continue
				}
				pat := e.field(p, "pattern")
				if typ := e.simpleType(e.field(p, "type"), tparams); typ != "" && e.typ(pat) == "identifier" {
					props[e.text(pat)] = typ
				}
			}
		}
	}
	if len(props) == 0 {
		return nil
	}
	return props
}

// patternIdents counts every identifier bound by a (possibly destructuring)
// pattern.
func (e *extractor) patternIdents(n *ts.Node, counts map[string]int) {
	if n == nil {
		return
	}
	switch e.typ(n) {
	case "identifier", "shorthand_property_identifier_pattern":
		counts[e.text(n)]++
		return
	}
	for i := range n.ChildCount() {
		e.patternIdents(n.Child(i), counts)
	}
}

// declCounts counts identifier declarations of every form under n.
func (e *extractor) declCounts(n *ts.Node, counts map[string]int) {
	switch e.typ(n) {
	case "variable_declarator":
		e.patternIdents(e.field(n, "name"), counts)
	case "required_parameter", "optional_parameter", "rest_parameter":
		e.patternIdents(e.field(n, "pattern"), counts)
	case "catch_clause":
		e.patternIdents(e.field(n, "parameter"), counts)
	case "for_in_statement":
		e.patternIdents(e.field(n, "left"), counts)
	case "function_declaration", "generator_function_declaration":
		e.patternIdents(e.field(n, "name"), counts)
	case "class_declaration":
		counts[e.text(e.field(n, "name"))]++
	case "arrow_function":
		if p := e.field(n, "parameter"); e.typ(p) == "identifier" {
			counts[e.text(p)]++
		}
	}
	for i := range n.ChildCount() {
		e.declCounts(n.Child(i), counts)
	}
}

// functionEnv computes the proven receiver types of one function-like node:
// annotated parameters (whole function) and top-level declarations of its
// body (from the end of the declaration).
func (e *extractor) functionEnv(fn *ts.Node, tparams map[string]bool) map[string]varType {
	counts := map[string]int{}
	e.declCounts(fn, counts)
	env := map[string]varType{}
	set := func(name, typ string, from uint32) {
		if name != "" && typ != "" && counts[name] == 1 {
			env[name] = varType{typ: typ, from: from}
		}
	}

	if params := e.field(fn, "parameters"); params != nil {
		for i := range params.ChildCount() {
			p := params.Child(i)
			if t := e.typ(p); t != "required_parameter" && t != "optional_parameter" {
				continue
			}
			if pat := e.field(p, "pattern"); e.typ(pat) == "identifier" {
				set(e.text(pat), e.simpleType(e.field(p, "type"), tparams), 0)
			}
		}
	}

	if body := e.field(fn, "body"); e.typ(body) == "statement_block" {
		for i := range body.ChildCount() {
			st := body.Child(i)
			if t := e.typ(st); t != "lexical_declaration" && t != "variable_declaration" {
				continue
			}
			isConst := e.hasToken(st, "const")
			for j := range st.ChildCount() {
				d := st.Child(j)
				nm := e.field(d, "name")
				if e.typ(d) != "variable_declarator" || e.typ(nm) != "identifier" {
					continue
				}
				typ := e.simpleType(e.field(d, "type"), tparams)
				if typ == "" && isConst {
					typ = e.constructedType(e.field(d, "value"))
				}
				set(e.text(nm), typ, d.EndByte())
			}
		}
	}
	return env
}

// enterFunction returns the scope for a function-like node. keepThis is true
// for methods and arrow functions (which keep the class `this`).
func (e *extractor) enterFunction(fn *ts.Node, sc scope, keepThis bool) scope {
	ns := sc
	if !keepThis {
		ns.thisClass, ns.props = "", nil
	}
	ns.tparams = unionSet(sc.tparams, e.typeParamNames(fn))
	if env := e.functionEnv(fn, ns.tparams); len(env) > 0 {
		vars := make(map[string]varType, len(sc.vars)+len(env))
		for k, v := range sc.vars {
			vars[k] = v
		}
		for k, v := range env {
			vars[k] = v
		}
		ns.vars = vars
	}
	return ns
}

// receiverType returns the proven declared type of a member-access object
// used at byte offset at, or "".
func (e *extractor) receiverType(obj *ts.Node, at uint32, sc scope) string {
	switch e.typ(obj) {
	case "this":
		return sc.thisClass
	case "identifier":
		if v, ok := sc.vars[e.text(obj)]; ok && at >= v.from {
			return v.typ
		}
	case "member_expression": // this.field (one step)
		o, p := e.field(obj, "object"), e.field(obj, "property")
		if e.typ(o) == "this" && sc.thisClass != "" && e.typ(p) != "" {
			return sc.props[e.text(p)]
		}
	}
	return ""
}
