package php

import (
	"strings"

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
//     `$r = new Repo(...)`, where the assignment is a whole statement
//     directly in a `{ ... }` block (the function body, or a block of an if,
//     loop, try, ...), and only for uses after it within that same block.
//     Statements of a block run in order (the function uses no goto), so the
//     assignment has run before every such use, in the same pass through the
//     block: it dominates them. Uses outside the block — after an if or a
//     loop, in another branch — are not dominated and get no evidence, and an
//     assignment that is not a block statement (in a condition, a match arm,
//     an unbraced branch, a switch case, a nested expression) is no evidence
//     at all. That a use reached without the assignment would read an
//     undefined variable (null, which cannot dispatch) is not relied on.
//
// Class-string evidence (phpTypeEnv.classStringType) is the same rule for a
// variable assigned exactly once by `$c = Repo::class`: `new $c(...)` then
// constructs Repo and `$c::m()` is a static access on Repo.
//
// Every other occurrence of the variable must be a receiver position
// (`$r->m()`, `$r->p`) or, for any variable, a class position (`new $c`,
// `$c::m()`, `$c::K`) — reading a variable as a class cannot rebind it; any
// other use (argument, reassignment, foreach/list,
// by-reference, global/static, closure `use`, ...) poisons the variable,
// because it might be rebound. Variables that code outside the function, or
// the engine itself, can bind are never evidence: superglobals and
// $http_response_header / $php_errormsg. Dynamic scope features (variable
// variables, extract / parse_str / mb_parse_str / eval in any case or
// qualification, a string assert(), include/require) and goto poison the
// whole function.
// Closure bodies get no evidence at all (separate variable scope).

type phpVarType struct {
	typ   phpTypeRef
	from  uint32 // evidence applies to uses at or after this byte offset
	until uint32 // ... and before this one (0: no bound)
	// classString: the variable holds the class-string of typ (`$c =
	// T::class`), not an instance of it.
	classString bool
}

// phpTypeRef is a proven class type: the name as written (its last segment) and
// the FQN the lexical rules give that name in the scope it is written in. qual is
// "" only when those rules yield no single identity.
type phpTypeRef struct {
	name string
	qual string
	// capped marks evidence that identifies the type but cannot exclude
	// writes by code outside the class (see ctor_properties.go): references
	// through it are capped at Strong.
	capped bool
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

// phpClassPropertyTypes indexes the proven instance property types of a class
// body: typed properties, promoted constructor properties and — for an
// untyped property — constructor-proven types (ctor_properties.go). A declared
// type always wins: PHP enforces it on every write. class is the declaring
// node (class, trait, interface, enum).
func phpClassPropertyTypes(class, body *ts.Node, lang *ts.Language, src []byte, sc *nameScope) map[string]phpTypeRef {
	props := make(map[string]phpTypeRef)
	defer func() {
		for name, typ := range phpCtorPropertyTypes(class, body, lang, src, sc) {
			if _, typed := props[name]; !typed {
				props[name] = typ
			}
		}
	}()
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
	blockEnd    uint32 // end of the block the assignment is a statement of
	classString bool   // assignedTyp comes from `T::class`
	poisoned    bool
}

type phpUseScan struct {
	lang      *ts.Language
	src       []byte
	sc        *nameScope
	uses      map[string]*phpVarUse
	poisonAll bool
	// blockStmt maps the start byte of each assignment expression that is a
	// whole statement directly in a block to the end byte of that block (see
	// the header); only they can be evidence. Empty when the scan is used for
	// poisonAll only.
	blockStmt map[uint32]uint32
}

// collectBlockAssignments records in out every assignment statement of a
// `{ ... }` block under n (n included), within one variable scope: nested
// functions, methods and classes are not entered. Closures are: their blocks
// are their own, and no use outside a closure lies inside one of them.
func collectBlockAssignments(n *ts.Node, lang *ts.Language, out map[uint32]uint32) {
	switch n.Type(lang) {
	case "function_definition", "method_declaration", "anonymous_class", "class_declaration":
		return
	case "compound_statement":
		for i := 0; i < n.ChildCount(); i++ {
			if stmt := n.Child(i); stmt.Type(lang) == "expression_statement" && stmt.ChildCount() > 0 {
				if asg := stmt.Child(0); asg.Type(lang) == "assignment_expression" {
					out[asg.StartByte()] = n.EndByte()
				}
			}
		}
	}
	for i := 0; i < n.ChildCount(); i++ {
		collectBlockAssignments(n.Child(i), lang, out)
	}
}

// phpEngineBoundVars are variables that hold a value without an assignment in
// the function body: superglobals (any code may write them) and variables the
// engine creates in the local scope.
var phpEngineBoundVars = map[string]bool{
	"GLOBALS": true, "_SERVER": true, "_GET": true, "_POST": true, "_FILES": true,
	"_COOKIE": true, "_SESSION": true, "_REQUEST": true, "_ENV": true,
	"http_response_header": true, "php_errormsg": true,
}

// phpScopeFunctions read or write the caller's local variables by name.
var phpScopeFunctions = map[string]bool{"extract": true, "parse_str": true, "mb_parse_str": true, "eval": true}

// isScopeFeatureCall reports whether a function call may bind local variables
// of the calling function: a scope function under any case or namespace
// qualification (an unqualified call falls back to the global function), or
// assert() with a string, which PHP 7 evaluates as code.
func (s *phpUseScan) isScopeFeatureCall(call *ts.Node) bool {
	callee := firstChildOfTypes(call, s.lang, "name", "qualified_name", "relative_name")
	if callee == nil {
		return false
	}
	name := strings.ToLower(lastName(callee, s.lang, s.src))
	if phpScopeFunctions[name] {
		return true
	}
	if name != "assert" {
		return false
	}
	args := childByType(call, s.lang, "arguments")
	if args == nil {
		return false
	}
	arg := childByType(args, s.lang, "argument")
	if arg == nil || arg.ChildCount() == 0 {
		return false
	}
	switch arg.Child(arg.ChildCount() - 1).Type(s.lang) {
	case "name", "integer", "float", "boolean", "null", "binary_expression",
		"function_call_expression", "member_call_expression", "scoped_call_expression", "unary_op_expression":
		return false
	}
	return true // a string, or an expression that may be one
}

func (s *phpUseScan) use(name string) *phpVarUse {
	u := s.uses[name]
	if u == nil {
		u = &phpVarUse{}
		s.uses[name] = u
	}
	return u
}

// isClassPosition reports whether a child at idx of a node of type parentType
// is used as a class: `new $c(...)`, `$c::m()`, `$c::K`, `$c::$p`.
func isClassPosition(parentType string, idx int) bool {
	switch parentType {
	case "object_creation_expression":
		return idx == 1
	case "scoped_call_expression", "class_constant_access_expression", "scoped_property_access_expression":
		return idx == 0
	}
	return false
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
		"require_expression", "require_once_expression", "goto_statement":
		s.poisonAll = true
	case "function_call_expression":
		if s.isScopeFeatureCall(node) {
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
		if phpEngineBoundVars[name] {
			u.poisoned = true
		}
		switch {
		case pt == "assignment_expression" && idx == 0:
			u.assignments++
			end, ok := s.blockStmt[parent.StartByte()]
			if !ok {
				u.poisoned = true // not a block statement: a write, not evidence
				return
			}
			u.blockEnd = end
			rhs := parent.Child(parent.ChildCount() - 1)
			if rhs != nil && rhs.Type(s.lang) == "object_creation_expression" {
				// `new self/static/parent` names no class here (the
				// enclosing class is not known to the scan): no evidence.
				if cls := firstChildOfTypes(rhs, s.lang, "name", "qualified_name", "relative_name"); cls != nil && !isRelativeType(lastName(cls, s.lang, s.src)) {
					u.assignedTyp = phpTypeRef{name: lastName(cls, s.lang, s.src)}
					if s.sc != nil {
						u.assignedTyp.qual, _ = s.sc.resolveClass(cls, s.lang, s.src)
					}
					u.assignedAt = parent.EndByte()
					return
				}
			}
			if typ, ok := s.classStringLiteral(rhs); ok {
				u.assignedTyp, u.classString = typ, true
				u.assignedAt = parent.EndByte()
				return
			}
			u.poisoned = true
		case isReceiverPosition(pt) && idx == 0, isClassPosition(pt, idx):
			// read-only receiver or class use
		default:
			u.poisoned = true
		}
		return
	}
	for i := 0; i < node.ChildCount(); i++ {
		s.scan(node.Child(i), node, i)
	}
}

// classStringLiteral reports whether n is `Name::class` for a class name (not
// self/static/parent) and returns that class.
func (s *phpUseScan) classStringLiteral(n *ts.Node) (phpTypeRef, bool) {
	if n == nil || n.Type(s.lang) != "class_constant_access_expression" || n.ChildCount() != 3 {
		return phpTypeRef{}, false
	}
	cls, member := n.Child(0), n.Child(2)
	switch cls.Type(s.lang) {
	case "name", "qualified_name":
	default:
		return phpTypeRef{}, false
	}
	if member.Type(s.lang) != "name" || member.Text(s.src) != "class" {
		return phpTypeRef{}, false
	}
	typ := phpTypeRef{name: lastName(cls, s.lang, s.src)}
	if isRelativeType(typ.name) {
		return phpTypeRef{}, false
	}
	if s.sc != nil {
		typ.qual, _ = s.sc.resolveClass(cls, s.lang, s.src)
	}
	return typ, true
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
	scan := &phpUseScan{lang: lang, src: src, sc: sc, uses: make(map[string]*phpVarUse), blockStmt: make(map[uint32]uint32)}
	collectBlockAssignments(body, lang, scan.blockStmt)
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
			if u := scan.uses[name]; typ.name != "" && !phpEngineBoundVars[name] && (u == nil || (u.assignments == 0 && !u.poisoned)) {
				env.vars[name] = phpVarType{typ: typ}
			}
		}
	}

	// Single `$x = new T()` / `$c = T::class` assignment.
	for name, u := range scan.uses {
		if params[name] || u.poisoned || u.assignments != 1 || u.assignedTyp.name == "" {
			continue
		}
		env.vars[name] = phpVarType{typ: u.assignedTyp, from: u.assignedAt, until: u.blockEnd, classString: u.classString}
	}
	return env
}

// receiverType returns the proven type of a member-call receiver node used at
// byte offset at — the type name as written, its FQN and whether the evidence
// is capped — or the zero value.
func (env *phpTypeEnv) receiverType(recv *ts.Node, at uint32, lang *ts.Language, src []byte) phpTypeRef {
	if env == nil || recv == nil {
		return phpTypeRef{}
	}
	switch recv.Type(lang) {
	case "variable_name":
		if v, ok := env.vars[phpVarName(recv, lang, src)]; ok && v.covers(at) && !v.classString {
			return v.typ
		}
	case "member_access_expression":
		// $this->prop (one step only).
		if recv.ChildCount() > 0 && phpVarName(recv.Child(0), lang, src) == "this" {
			if prop := childByType(recv, lang, "name"); prop != nil {
				return env.props[prop.Text(src)]
			}
		}
	}
	return phpTypeRef{}
}

// classStringType returns the class a variable used in a class position (`new
// $c`, `$c::m()`) at byte offset at is proven to name — the variable holds
// exactly one `T::class` (see the header) — or the zero value.
func (env *phpTypeEnv) classStringType(n *ts.Node, at uint32, lang *ts.Language, src []byte) phpTypeRef {
	if env == nil || n == nil || n.Type(lang) != "variable_name" {
		return phpTypeRef{}
	}
	if v, ok := env.vars[phpVarName(n, lang, src)]; ok && v.covers(at) && v.classString {
		return v.typ
	}
	return phpTypeRef{}
}

// covers reports whether the evidence applies to a use at byte offset at.
func (v phpVarType) covers(at uint32) bool {
	return at >= v.from && (v.until == 0 || at < v.until)
}
