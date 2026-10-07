package php

import (
	ts "github.com/odvcencio/gotreesitter"
)

// Constructor property evidence.
//
// Laravel 6 / PHP 7.2 code cannot declare property types, so a dependency is
// injected as
//
//	private $policy;
//	public function __construct(LoginScreenPolicy $policy) { $this->policy = $policy; }
//
// and used as `$this->policy->method()`. The parameter type is enforced by
// PHP when the constructor is called; this file turns it into receiver type
// evidence for `$this->policy` when the class's own source proves that the
// property never holds anything else. Only source structure is used: no
// docblocks, container bindings, configuration or runtime behaviour.
//
// A property p of a class C (a class_declaration, never a trait) gets the type
// T when all of the following hold:
//
//   - the constructor body has, as a direct statement, `$this->p = $param`
//     where $param is a by-value, non-variadic parameter with a proven class
//     type T, or `$this->p = new T(...)`; several such statements must agree
//     on T. An assignment nested in a branch, loop or closure is a write, not
//     evidence (no control-flow analysis is done);
//   - $param is never rebound in the constructor: it appears only as the
//     source of such assignments or as a member receiver;
//   - no other code of C writes a property named p — through any receiver,
//     since C's code may write p on another instance of C or through an alias
//     of $this: assignment, compound or reference assignment, ++/--, unset,
//     list()/[] destructuring, foreach target, or passing it as a call
//     argument (which may be by reference);
//   - C writes no dynamic property name ($this->$n = …), and the constructor
//     uses no dynamic scope feature (variable variables, extract, eval, …);
//   - p is not declared static, and not declared with a type (a typed
//     property is already proven by its declaration).
//
// The evidence is exact only when no code outside C can write p: p is private
// or protected in a final class, and C uses no trait (trait methods become
// C's methods but are written elsewhere). Otherwise — public, protected in a
// non-final class, undeclared (dynamic or inherited), or C uses a trait — the
// target is still identified by T but the confidence is capped at Strong.

// phpCtorPropertyTypes returns the constructor-proven types of class's
// properties. body is the class's declaration_list; sc the scope the class is
// declared in.
func phpCtorPropertyTypes(class, body *ts.Node, lang *ts.Language, src []byte, sc *nameScope) map[string]phpTypeRef {
	if class.Type(lang) != "class_declaration" {
		return nil
	}
	var ctor *ts.Node
	declared := make(map[string]string) // property → visibility
	static := make(map[string]bool)
	usesTrait := false
	for i := 0; i < body.ChildCount(); i++ {
		member := body.Child(i)
		switch member.Type(lang) {
		case "method_declaration":
			if childText(member, lang, src, "name") == "__construct" {
				ctor = member
			}
		case "use_declaration":
			usesTrait = true
		case "property_declaration":
			vis := "public" // `var` and no modifier are public
			if v := childByType(member, lang, "visibility_modifier"); v != nil {
				vis = v.Text(src)
			}
			isStatic := childByType(member, lang, "static_modifier") != nil
			for j := 0; j < member.ChildCount(); j++ {
				if el := member.Child(j); el.Type(lang) == "property_element" {
					if name := phpVarName(childByType(el, lang, "variable_name"), lang, src); name != "" {
						declared[name] = vis
						static[name] = isStatic
					}
				}
			}
		}
	}
	if ctor == nil {
		return nil
	}
	cbody := childByType(ctor, lang, "compound_statement")
	if cbody == nil {
		return nil
	}

	// Dynamic scope features in the constructor make every local suspect.
	scan := &phpUseScan{lang: lang, src: src, sc: sc, uses: make(map[string]*phpVarUse)}
	for i := 0; i < cbody.ChildCount(); i++ {
		scan.scan(cbody.Child(i), cbody, i)
	}
	if scan.poisonAll {
		return nil
	}

	// Typed, by-value, non-variadic constructor parameters.
	params := make(map[string]phpTypeRef)
	if fp := childByType(ctor, lang, "formal_parameters"); fp != nil {
		for i := 0; i < fp.ChildCount(); i++ {
			p := fp.Child(i)
			if p.Type(lang) != "simple_parameter" || childByType(p, lang, "reference_modifier") != nil {
				continue
			}
			name := phpVarName(childByType(p, lang, "variable_name"), lang, src)
			if typ := phpTypeOf(p, sc, lang, src); name != "" && typ.qual != "" {
				params[name] = typ
			}
		}
	}

	// Evidence assignments: direct statements of the constructor body.
	evidence := make(map[*ts.Node]bool) // the assignment_expression nodes
	types := make(map[string]phpTypeRef)
	conflict := make(map[string]bool)
	paramSources := make(map[*ts.Node]bool) // RHS variable nodes of evidence
	for i := 0; i < cbody.ChildCount(); i++ {
		stmt := cbody.Child(i)
		if stmt.Type(lang) != "expression_statement" || stmt.ChildCount() == 0 {
			continue
		}
		asg := stmt.Child(0)
		if asg.Type(lang) != "assignment_expression" || asg.ChildCount() < 3 {
			continue
		}
		prop := thisPropertyName(asg.Child(0), lang, src)
		if prop == "" {
			continue
		}
		rhs := asg.Child(asg.ChildCount() - 1)
		var typ phpTypeRef
		switch rhs.Type(lang) {
		case "variable_name":
			typ = params[phpVarName(rhs, lang, src)]
			if typ.qual != "" {
				paramSources[rhs] = true
			}
		case "object_creation_expression":
			if cls := firstChildOfTypes(rhs, lang, "name", "qualified_name"); cls != nil {
				typ = phpTypeRef{name: lastName(cls, lang, src)}
				typ.qual, _ = sc.resolveClass(cls, lang, src)
			}
		}
		if typ.qual == "" {
			continue // not evidence; the poison scan treats it as a write
		}
		evidence[asg] = true
		if prev, ok := types[prop]; ok && prev.qual != typ.qual {
			conflict[prop] = true
		}
		types[prop] = typ
	}
	if len(types) == 0 {
		return nil
	}

	// A parameter rebound or escaping in the constructor is no evidence.
	rebound := make(map[string]bool)
	walkScope(cbody, nil, 0, lang, func(n, parent *ts.Node, idx int) {
		if n.Type(lang) != "variable_name" {
			return
		}
		name := phpVarName(n, lang, src)
		if _, ok := params[name]; !ok || paramSources[n] {
			return
		}
		if parent != nil && isReceiverPosition(parent.Type(lang)) && idx == 0 {
			return
		}
		rebound[name] = true
	})

	// Writes of a property name anywhere in the class.
	poisoned, dynamic := classPropertyWrites(body, evidence, lang, src)
	if dynamic {
		return nil
	}

	out := make(map[string]phpTypeRef)
	final := childByType(class, lang, "final_modifier") != nil
	for prop, typ := range types {
		if conflict[prop] || poisoned[prop] || static[prop] {
			continue
		}
		if param := ctorSourceParam(cbody, prop, evidence, lang, src); param != "" && rebound[param] {
			continue
		}
		vis, isDeclared := declared[prop]
		exact := isDeclared && !usesTrait && (vis == "private" || (vis == "protected" && final))
		typ.capped = !exact
		out[prop] = typ
	}
	return out
}

// ctorSourceParam returns the parameter assigned to $this->prop by an evidence
// assignment, or "" when the evidence is a construction.
func ctorSourceParam(cbody *ts.Node, prop string, evidence map[*ts.Node]bool, lang *ts.Language, src []byte) string {
	for i := 0; i < cbody.ChildCount(); i++ {
		stmt := cbody.Child(i)
		if stmt.ChildCount() == 0 || !evidence[stmt.Child(0)] {
			continue
		}
		asg := stmt.Child(0)
		if thisPropertyName(asg.Child(0), lang, src) != prop {
			continue
		}
		if rhs := asg.Child(asg.ChildCount() - 1); rhs.Type(lang) == "variable_name" {
			return phpVarName(rhs, lang, src)
		}
	}
	return ""
}

// thisPropertyName returns p for a `$this->p` member access with a static
// member name, or "".
func thisPropertyName(n *ts.Node, lang *ts.Language, src []byte) string {
	if n == nil || n.Type(lang) != "member_access_expression" || n.ChildCount() < 3 {
		return ""
	}
	if phpVarName(n.Child(0), lang, src) != "this" {
		return ""
	}
	if m := n.Child(n.ChildCount() - 1); m.Type(lang) == "name" {
		return m.Text(src)
	}
	return ""
}

// classPropertyWrites reports the property names written anywhere under body
// (through any receiver), excluding the evidence assignments, and whether a
// dynamic property name is written (which may be any property).
func classPropertyWrites(body *ts.Node, evidence map[*ts.Node]bool, lang *ts.Language, src []byte) (map[string]bool, bool) {
	written := make(map[string]bool)
	dynamic := false
	var walk func(n, parent *ts.Node, idx int, writeCtx bool)
	walk = func(n, parent *ts.Node, idx int, writeCtx bool) {
		t := n.Type(lang)
		if t == "member_access_expression" && writeCtx {
			if m := n.Child(n.ChildCount() - 1); m != nil && m.Type(lang) == "name" {
				written[m.Text(src)] = true
			} else {
				dynamic = true
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), n, i, writePosition(n, i, writeCtx, evidence, lang))
		}
	}
	walk(body, nil, 0, false)
	return written, dynamic
}

// writePosition reports whether the i-th child of n is written to (or may be,
// through a reference) by n. writeCtx is whether n itself is written.
func writePosition(n *ts.Node, i int, writeCtx bool, evidence map[*ts.Node]bool, lang *ts.Language) bool {
	switch n.Type(lang) {
	case "assignment_expression":
		return i == 0 && !evidence[n]
	case "augmented_assignment_expression", "update_expression":
		return i == 0
	case "reference_assignment_expression", "unset_statement", "argument", "by_ref", "list_literal":
		// Reference assignment aliases both sides; unset destroys; an argument
		// may be passed by reference; list()/[...] destructuring writes its
		// elements (the grammar only produces list_literal for destructuring).
		return true
	case "array_element_initializer", "pair":
		// Elements of a destructuring pattern are written; elements of an
		// ordinary array literal are only read.
		return writeCtx
	case "foreach_statement":
		// Everything after `as` is a loop target.
		for j := 0; j < i; j++ {
			if n.Child(j).Type(lang) == "as" {
				return true
			}
		}
	}
	return false
}

// walkScope visits n's subtree within one variable scope: nested functions,
// methods and classes are separate scopes and are not entered.
func walkScope(n, parent *ts.Node, idx int, lang *ts.Language, visit func(n, parent *ts.Node, idx int)) {
	switch n.Type(lang) {
	case "function_definition", "method_declaration", "anonymous_class", "class_declaration":
		if parent != nil {
			return
		}
	}
	visit(n, parent, idx)
	for i := 0; i < n.ChildCount(); i++ {
		walkScope(n.Child(i), n, i, lang, visit)
	}
}
