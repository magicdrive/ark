// Package ecmascript holds the ECMAScript semantics the JavaScript and the
// TypeScript providers share, over either grammar (tree-sitter-javascript,
// tree-sitter-typescript / tsx): the names a scope declares.
//
// A provider uses them to tell a name that denotes a local declaration — a
// parameter, a variable, a nested function or class, a catch or loop
// binding — from one that may denote a declaration Ark extracts (a
// top-level declaration, an import binding). A call of a local is no call of
// the same-named top-level function, whatever a name match would find.
//
// The scopes are over-approximated, never under: a name declared anywhere in
// a function (outside its nested functions) is local to the whole function,
// whatever block declares it, and stays local in the functions nested in it.
// Mistaking a name for a local only weakens what may be claimed about it;
// mistaking a local for a declaration Ark extracts is a false resolution.
package ecmascript

import (
	ts "github.com/odvcencio/gotreesitter"
)

// Names is a set of declared names, split by the name space ECMAScript and
// TypeScript give them: values (variables, functions, classes, enums,
// parameters) and types (type aliases, interfaces, classes, enums).
type Names struct {
	Values map[string]bool
	Types  map[string]bool
}

// Empty reports whether n declares nothing.
func (n Names) Empty() bool { return len(n.Values) == 0 && len(n.Types) == 0 }

// Union returns the names of n and o (n and o are not modified).
func (n Names) Union(o Names) Names {
	if o.Empty() {
		return n
	}
	if n.Empty() {
		return o
	}
	return Names{Values: union(n.Values, o.Values), Types: union(n.Types, o.Types)}
}

func union(a, b map[string]bool) map[string]bool {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	m := make(map[string]bool, len(a)+len(b))
	for k := range a {
		m[k] = true
	}
	for k := range b {
		m[k] = true
	}
	return m
}

type collector struct {
	lang     *ts.Language
	src      []byte
	names    Names
	assigned map[string]bool
}

func (c *collector) typ(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Type(c.lang)
}

func (c *collector) field(n *ts.Node, name string) *ts.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name, c.lang)
}

func (c *collector) value(n *ts.Node) {
	if n == nil {
		return
	}
	if c.names.Values == nil {
		c.names.Values = map[string]bool{}
	}
	c.names.Values[n.Text(c.src)] = true
}

func (c *collector) typeName(n *ts.Node) {
	if n == nil {
		return
	}
	if c.names.Types == nil {
		c.names.Types = map[string]bool{}
	}
	c.names.Types[n.Text(c.src)] = true
}

// FunctionLocals returns the names fn — a function declaration or
// expression, an arrow function, a method — declares for its own body: its
// parameters, its own name when it is a named function expression (visible
// only inside it), and every declaration of its body outside its nested
// functions.
func FunctionLocals(fn *ts.Node, lang *ts.Language, src []byte) Names {
	c := &collector{lang: lang, src: src}
	switch c.typ(fn) {
	case "function_expression", "function", "generator_function":
		if nm := c.field(fn, "name"); c.typ(nm) == "identifier" {
			c.value(nm)
		}
	}
	if p := c.field(fn, "parameter"); c.typ(p) == "identifier" { // x => ...
		c.value(p)
	}
	c.parameters(c.field(fn, "parameters"))
	c.body(c.field(fn, "body"))
	return c.names
}

// ProgramLocals returns the names program (a module or script) declares
// other than as its own top-level declarations: a destructuring pattern at
// the top level, and every declaration in a block, loop or catch clause
// outside every function. The top-level declarations a provider extracts as
// symbols — a function or class declaration, a variable declarator naming
// one identifier, directly or under an export statement — are left out by
// position, not by name: a nested declaration of a top-level name is a local
// all the same.
func ProgramLocals(program *ts.Node, lang *ts.Language, src []byte) Names {
	c := &collector{lang: lang, src: src}
	for i := range program.NamedChildCount() {
		c.topLevel(program, program.NamedChild(i))
	}
	return c.names
}

// reassigned is Reassigned(program), computed once per collector.
func (c *collector) reassigned(program *ts.Node) map[string]bool {
	if c.assigned == nil {
		c.assigned = Reassigned(program, c.lang, c.src)
	}
	return c.assigned
}

func (c *collector) topLevel(program, n *ts.Node) {
	switch c.typ(n) {
	case "export_statement":
		if d := c.field(n, "declaration"); d != nil {
			c.topLevel(program, d)
		} else {
			c.body(n)
		}
	case "lexical_declaration", "variable_declaration":
		for i := range n.NamedChildCount() {
			d := n.NamedChild(i)
			if c.typ(d) != "variable_declarator" {
				continue
			}
			nm, val := c.field(d, "name"), c.field(d, "value")
			if IsRequire(val, c.lang, c.src) {
				// A CommonJS import: its names are module bindings when
				// RequireBindings can state them, locals otherwise.
				if _, ok := RequireBindings(d, c.lang, c.src, c.reassigned(program)); !ok {
					c.pattern(nm)
				}
			} else if c.typ(nm) != "identifier" {
				c.pattern(nm) // a pattern has no single identity
			}
			c.body(val)
		}
	case "function_declaration", "generator_function_declaration",
		"interface_declaration", "type_alias_declaration", "enum_declaration":
		// A symbol of its own; its body is a scope of its own.
	case "class_declaration", "abstract_class_declaration":
		// A symbol; its field initialisers and static blocks may declare.
		if b := c.field(n, "body"); b != nil {
			c.body(b)
		}
	default:
		c.body(n)
	}
}

// parameters adds the names a formal parameter list binds, in either
// grammar: TypeScript wraps each parameter (required_parameter /
// optional_parameter / rest_parameter with a pattern), JavaScript writes the
// patterns directly (identifier, assignment_pattern, object / array / rest
// pattern).
func (c *collector) parameters(params *ts.Node) {
	if params == nil {
		return
	}
	for i := range params.NamedChildCount() {
		p := params.NamedChild(i)
		switch c.typ(p) {
		case "required_parameter", "optional_parameter", "rest_parameter":
			if pat := c.field(p, "pattern"); pat != nil {
				c.pattern(pat)
			} else {
				c.pattern(p)
			}
		default:
			c.pattern(p)
		}
	}
}

// pattern adds the names a binding pattern binds. Default values and
// computed keys are expressions, not bindings.
func (c *collector) pattern(n *ts.Node) {
	if n == nil {
		return
	}
	switch c.typ(n) {
	case "identifier", "shorthand_property_identifier_pattern":
		c.value(n)
	case "assignment_pattern", "object_assignment_pattern":
		c.pattern(c.field(n, "left"))
	case "pair_pattern":
		c.pattern(c.field(n, "value"))
	case "rest_pattern":
		for i := range n.NamedChildCount() {
			c.pattern(n.NamedChild(i))
		}
	case "object_pattern", "array_pattern":
		for i := range n.NamedChildCount() {
			c.pattern(n.NamedChild(i))
		}
	case "required_parameter", "optional_parameter":
		c.pattern(c.field(n, "pattern"))
	}
}

// body adds every declaration under n outside nested functions.
func (c *collector) body(n *ts.Node) {
	if n == nil {
		return
	}
	switch c.typ(n) {
	case "variable_declarator":
		c.pattern(c.field(n, "name"))
		c.body(c.field(n, "value"))
		return
	case "function_declaration", "generator_function_declaration":
		c.value(c.field(n, "name"))
		return // its parameters and body are its own scope
	case "function_expression", "function", "generator_function", "arrow_function", "method_definition":
		return
	case "class_declaration", "abstract_class_declaration", "class":
		// A class expression's name is visible only inside the class;
		// taking it local to the enclosing scope over-approximates.
		if nm := c.field(n, "name"); nm != nil {
			c.value(nm)
			c.typeName(nm)
		}
		// The class body's methods are functions of their own; field
		// initialisers and static blocks are walked for declarations.
	case "enum_declaration":
		nm := c.field(n, "name")
		c.value(nm)
		c.typeName(nm)
		return
	case "interface_declaration", "type_alias_declaration":
		c.typeName(c.field(n, "name"))
		return
	case "catch_clause":
		c.pattern(c.field(n, "parameter"))
	case "for_in_statement":
		// for (const k of ks) / for (k in o): the left side binds (or
		// assigns) k; an assignment target is not a declaration, but
		// counting it only over-approximates.
		c.pattern(c.field(n, "left"))
	case "import_statement", "export_statement":
		// Module-level forms: their names are bindings and exports the
		// provider records itself. An export statement's declaration is
		// walked by the caller.
		if d := c.field(n, "declaration"); d != nil {
			c.body(d)
		}
		if v := c.field(n, "value"); v != nil {
			c.body(v) // export default <expression>: a class with a static block
		}
		return
	}
	for i := range n.NamedChildCount() {
		c.body(n.NamedChild(i))
	}
}

// hasConst reports whether a lexical declaration is a const one.
func (c *collector) hasConst(decl *ts.Node) bool {
	for i := range decl.ChildCount() {
		if c.typ(decl.Child(i)) == "const" {
			return true
		}
	}
	return false
}

// IsRequire reports whether n is a CommonJS import: require("m") with one
// string argument, or a member of one (require("m").x).
func IsRequire(n *ts.Node, lang *ts.Language, src []byte) bool {
	c := &collector{lang: lang, src: src}
	for c.typ(n) == "member_expression" {
		n = c.field(n, "object")
	}
	if c.typ(n) != "call_expression" {
		return false
	}
	fn := c.field(n, "function")
	if c.typ(fn) != "identifier" || fn.Text(src) != "require" {
		return false
	}
	args := c.field(n, "arguments")
	return args != nil && args.NamedChildCount() == 1 && c.typ(args.NamedChild(0)) == "string"
}

// RequireBinding is one name a CommonJS import binds: Local, bound to the
// export Imported ("module.exports" for the whole module.exports) of the
// module Specifier. At is the node naming Local.
type RequireBinding struct {
	Local, Imported, Specifier string
	At                         *ts.Node
}

// Reassigned returns the names an assignment anywhere in program writes
// (x = …, x += …, x++, for (x of …)): a require bound to one of them through
// var or let may denote another value by the time it is used.
func Reassigned(program *ts.Node, lang *ts.Language, src []byte) map[string]bool {
	c := &collector{lang: lang, src: src}
	out := map[string]bool{}
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		var target *ts.Node
		switch c.typ(n) {
		case "assignment_expression", "augmented_assignment_expression":
			target = c.field(n, "left")
		case "update_expression":
			target = c.field(n, "argument")
		case "for_in_statement":
			if !c.declares(n) {
				target = c.field(n, "left")
			}
		}
		if c.typ(target) == "identifier" {
			out[target.Text(src)] = true
		}
		for i := range n.NamedChildCount() {
			walk(n.NamedChild(i))
		}
	}
	walk(program)
	return out
}

// declares reports whether a for-in / for-of statement declares its left
// side (var / let / const) rather than assigning it.
func (c *collector) declares(n *ts.Node) bool {
	for i := range n.ChildCount() {
		switch c.typ(n.Child(i)) {
		case "var", "let", "const":
			return true
		}
	}
	return false
}

// RequireBindings returns the bindings of a variable declarator whose value
// is a CommonJS import, in the forms whose meaning the syntax fixes:
//
//	x = require("m")            x ← module.exports
//	{ a, b: c } = require("m")  a ← a, c ← b
//	y = require("m").y          y ← y
//
// ok is false for any other declarator (a nested or defaulted pattern, a
// deeper member chain, no require), and for a var or let declarator one of
// whose names the program reassigns (reassigned: Reassigned).
func RequireBindings(d *ts.Node, lang *ts.Language, src []byte, reassigned map[string]bool) (out []RequireBinding, ok bool) {
	c := &collector{lang: lang, src: src}
	if c.typ(d) != "variable_declarator" {
		return nil, false
	}
	// var and let may be reassigned; const may not.
	mutable := d.Parent() == nil || !c.hasConst(d.Parent())
	defer func() {
		if !mutable {
			return
		}
		for _, b := range out {
			if reassigned[b.Local] {
				out, ok = nil, false
				return
			}
		}
	}()
	name, val := c.field(d, "name"), c.field(d, "value")
	member := ""
	if c.typ(val) == "member_expression" {
		prop := c.field(val, "property")
		if c.typ(prop) != "property_identifier" {
			return nil, false
		}
		member = prop.Text(src)
		val = c.field(val, "object")
	}
	if !IsRequire(val, lang, src) || c.typ(val) != "call_expression" {
		return nil, false
	}
	arg := c.field(val, "arguments").NamedChild(0)
	raw := arg.Text(src)
	if len(raw) < 2 {
		return nil, false
	}
	spec := raw[1 : len(raw)-1]
	switch c.typ(name) {
	case "identifier":
		imported := "module.exports"
		if member != "" {
			imported = member
		}
		return []RequireBinding{{Local: name.Text(src), Imported: imported, Specifier: spec, At: name}}, true
	case "object_pattern":
		if member != "" {
			return nil, false
		}
		for i := range name.NamedChildCount() {
			p := name.NamedChild(i)
			switch c.typ(p) {
			case "shorthand_property_identifier_pattern":
				out = append(out, RequireBinding{Local: p.Text(src), Imported: p.Text(src), Specifier: spec, At: p})
			case "pair_pattern":
				k, v := c.field(p, "key"), c.field(p, "value")
				if c.typ(k) != "property_identifier" || c.typ(v) != "identifier" {
					return nil, false
				}
				out = append(out, RequireBinding{Local: v.Text(src), Imported: k.Text(src), Specifier: spec, At: v})
			default:
				return nil, false
			}
		}
		return out, len(out) > 0
	}
	return nil, false
}
