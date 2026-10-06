package php

import (
	ts "github.com/odvcencio/gotreesitter"
)

// Receiver type evidence (language.ReferenceDraft.ReceiverType).
//
// Only local, structural facts are used — never inference:
//   - `$this->prop` where prop is a typed property (or promoted constructor
//     property) of the enclosing class, declared in the same file (PHP
//     enforces typed-property assignments at runtime);
//   - a typed parameter (`Repo $r`, `?Repo $r`) that is never written in the
//     function body;
//   - a variable assigned exactly once in the function body, by
//     `$r = new Repo(...)`, used after that assignment.
//
// Every other occurrence of the variable must be a receiver position
// (`$r->m()`, `$r->p`); any other use (argument, reassignment, foreach/list,
// by-reference, global/static, closure `use`, ...) poisons the variable,
// because it might be rebound. Dynamic scope features (variable variables,
// extract/parse_str/eval, include/require) poison the whole function.
// Closure bodies get no evidence at all (separate variable scope).

type phpVarType struct {
	typ  phpTypeRef
	from uint32 // evidence applies to uses at or after this byte offset
}

// phpTypeRef is a proven class type: the name as written (its last segment) and
// the FQN the lexical rules give that name in the scope it is written in. qual is
// "" only when those rules yield no single identity.
type phpTypeRef struct {
	name string
	qual string
}

// phpTypeEnv is the proven receiver-type environment of one function body.
type phpTypeEnv struct {
	vars  map[string]phpVarType // variable name (without '$') → type
	props map[string]phpTypeRef // enclosing class property name → declared type
}

// phpTypeName returns the class type of a named_type / optional_type(named_type)
// node, or the zero value for unions, intersections, builtins and relative
// types. sc is the lexical scope the type is written in.
func phpTypeName(n *ts.Node, sc *nameScope, lang *ts.Language, src []byte) phpTypeRef {
	switch n.Type(lang) {
	case "named_type":
		cls := firstChildOfTypes(n, lang, "name", "qualified_name", "relative_name")
		if cls == nil {
			return phpTypeRef{}
		}
		name := lastName(cls, lang, src)
		if isRelativeType(name) {
			return phpTypeRef{}
		}
		ref := phpTypeRef{name: name}
		if sc != nil {
			ref.qual, _ = sc.resolveClass(cls, lang, src)
		}
		return ref
	case "optional_type":
		if inner := childByType(n, lang, "named_type"); inner != nil {
			return phpTypeName(inner, sc, lang, src)
		}
	}
	return phpTypeRef{}
}

// phpTypeOf returns the declared class type among node's direct children.
func phpTypeOf(n *ts.Node, sc *nameScope, lang *ts.Language, src []byte) phpTypeRef {
	for i := 0; i < n.ChildCount(); i++ {
		if t := phpTypeName(n.Child(i), sc, lang, src); t.name != "" {
			return t
		}
	}
	return phpTypeRef{}
}

func phpVarName(n *ts.Node, lang *ts.Language, src []byte) string {
	if n == nil || n.Type(lang) != "variable_name" {
		return ""
	}
	return childText(n, lang, src, "name")
}

// phpClassPropertyTypes indexes typed instance properties of a class body,
// including promoted constructor properties.
func phpClassPropertyTypes(body *ts.Node, lang *ts.Language, src []byte, sc *nameScope) map[string]phpTypeRef {
	props := make(map[string]phpTypeRef)
	for i := 0; i < body.ChildCount(); i++ {
		member := body.Child(i)
		switch member.Type(lang) {
		case "property_declaration":
			if childByType(member, lang, "static_modifier") != nil {
				continue
			}
			typ := phpTypeOf(member, sc, lang, src)
			if typ.name == "" {
				continue
			}
			for j := 0; j < member.ChildCount(); j++ {
				if el := member.Child(j); el.Type(lang) == "property_element" {
					if name := phpVarName(childByType(el, lang, "variable_name"), lang, src); name != "" {
						props[name] = typ
					}
				}
			}
		case "method_declaration":
			if childText(member, lang, src, "name") != "__construct" {
				continue
			}
			fp := childByType(member, lang, "formal_parameters")
			if fp == nil {
				continue
			}
			for j := 0; j < fp.ChildCount(); j++ {
				p := fp.Child(j)
				if p.Type(lang) != "property_promotion_parameter" {
					continue
				}
				if typ := phpTypeOf(p, sc, lang, src); typ.name != "" {
					if name := phpVarName(childByType(p, lang, "variable_name"), lang, src); name != "" {
						props[name] = typ
					}
				}
			}
		}
	}
	return props
}

// phpVarUse accumulates how one variable is used in a function body.
type phpVarUse struct {
	assignments int
	assignedTyp phpTypeRef
	assignedAt  uint32
	poisoned    bool
}

type phpUseScan struct {
	lang      *ts.Language
	src       []byte
	sc        *nameScope
	uses      map[string]*phpVarUse
	poisonAll bool
}

func (s *phpUseScan) use(name string) *phpVarUse {
	u := s.uses[name]
	if u == nil {
		u = &phpVarUse{}
		s.uses[name] = u
	}
	return u
}

func isReceiverPosition(parentType string) bool {
	switch parentType {
	case "member_call_expression", "nullsafe_member_call_expression",
		"member_access_expression", "nullsafe_member_access_expression":
		return true
	}
	return false
}

func (s *phpUseScan) scan(node, parent *ts.Node, idx int) {
	t := node.Type(s.lang)
	switch t {
	case "function_definition", "method_declaration", "anonymous_class", "class_declaration":
		return // separate scopes
	case "dynamic_variable_name", "include_expression", "include_once_expression",
		"require_expression", "require_once_expression":
		s.poisonAll = true
	case "function_call_expression":
		switch childText(node, s.lang, s.src, "name") {
		case "extract", "parse_str", "eval":
			s.poisonAll = true
		}
	case "variable_name":
		name := phpVarName(node, s.lang, s.src)
		if name == "" || name == "this" {
			return
		}
		pt := ""
		if parent != nil {
			pt = parent.Type(s.lang)
		}
		u := s.use(name)
		switch {
		case pt == "assignment_expression" && idx == 0:
			u.assignments++
			rhs := parent.Child(parent.ChildCount() - 1)
			if rhs != nil && rhs.Type(s.lang) == "object_creation_expression" {
				if cls := firstChildOfTypes(rhs, s.lang, "name", "qualified_name", "relative_name"); cls != nil {
					u.assignedTyp = phpTypeRef{name: lastName(cls, s.lang, s.src)}
					if s.sc != nil {
						u.assignedTyp.qual, _ = s.sc.resolveClass(cls, s.lang, s.src)
					}
					u.assignedAt = parent.EndByte()
					return
				}
			}
			u.poisoned = true
		case isReceiverPosition(pt) && idx == 0:
			// read-only receiver use
		default:
			u.poisoned = true
		}
		return
	}
	for i := 0; i < node.ChildCount(); i++ {
		s.scan(node.Child(i), node, i)
	}
}

// phpFunctionTypeEnv builds the receiver-type environment for a function or
// method declaration. sc is the lexical scope the function is declared in; props
// are the enclosing class's typed properties (nil for free functions).
func phpFunctionTypeEnv(fn *ts.Node, lang *ts.Language, src []byte, sc *nameScope, props map[string]phpTypeRef) *phpTypeEnv {
	env := &phpTypeEnv{vars: make(map[string]phpVarType), props: props}
	body := childByType(fn, lang, "compound_statement")
	if body == nil {
		return env
	}
	scan := &phpUseScan{lang: lang, src: src, sc: sc, uses: make(map[string]*phpVarUse)}
	for i := 0; i < body.ChildCount(); i++ {
		scan.scan(body.Child(i), body, i)
	}
	if scan.poisonAll {
		return &phpTypeEnv{props: props}
	}

	// Typed, never-written parameters.
	params := make(map[string]bool)
	if fp := childByType(fn, lang, "formal_parameters"); fp != nil {
		for i := 0; i < fp.ChildCount(); i++ {
			p := fp.Child(i)
			if p.Type(lang) != "simple_parameter" && p.Type(lang) != "property_promotion_parameter" &&
				p.Type(lang) != "variadic_parameter" {
				continue
			}
			name := phpVarName(childByType(p, lang, "variable_name"), lang, src)
			if name == "" {
				continue
			}
			params[name] = true
			if p.Type(lang) != "simple_parameter" || childByType(p, lang, "reference_modifier") != nil {
				continue
			}
			typ := phpTypeOf(p, sc, lang, src)
			if u := scan.uses[name]; typ.name != "" && (u == nil || (u.assignments == 0 && !u.poisoned)) {
				env.vars[name] = phpVarType{typ: typ}
			}
		}
	}

	// Single `$x = new T()` assignment.
	for name, u := range scan.uses {
		if params[name] || u.poisoned || u.assignments != 1 || u.assignedTyp.name == "" {
			continue
		}
		env.vars[name] = phpVarType{typ: u.assignedTyp, from: u.assignedAt}
	}
	return env
}

// receiverType returns the proven declared type of a member-call receiver
// node used at byte offset at — the type name as written and its FQN — or "", "".
func (env *phpTypeEnv) receiverType(recv *ts.Node, at uint32, lang *ts.Language, src []byte) (name, qual string) {
	if env == nil || recv == nil {
		return "", ""
	}
	switch recv.Type(lang) {
	case "variable_name":
		if v, ok := env.vars[phpVarName(recv, lang, src)]; ok && at >= v.from {
			return v.typ.name, v.typ.qual
		}
	case "member_access_expression":
		// $this->prop (one step only).
		if recv.ChildCount() > 0 && phpVarName(recv.Child(0), lang, src) == "this" {
			if prop := childByType(recv, lang, "name"); prop != nil {
				t := env.props[prop.Text(src)]
				return t.name, t.qual
			}
		}
	}
	return "", ""
}
