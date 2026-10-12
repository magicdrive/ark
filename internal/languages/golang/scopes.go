package golang

import (
	ts "github.com/odvcencio/gotreesitter"
)

// Local scopes of a function, for shadowing (language.ReferenceDraft.
// ConfidenceCap).
//
// A local declaration's scope follows Go's rules: a parameter (or receiver,
// or named result) is in scope in the whole function body; a name declared
// by := , var, const or a local type is in scope from the end of its
// declaration to the end of the innermost enclosing block, case clause, or
// statement whose header declares it (if / for / switch / select init); a
// range variable throughout its for statement; a local type from its name
// on (its own declaration may refer to it: type node struct{ next *node }).
// Labels are another namespace and shadow nothing.
//
// Type parameters are scoped declarations too, also outside functions
// (goTypeParamScopes): a function's and a method's (declared by its
// receiver, func (l *List[T]) ...) in the whole declaration, a generic
// type's in its type specification.

// goScope is the byte range a local declaration is in scope.
type goScope struct{ from, to uint32 }

// goLocalScopes returns the local declarations of fn (a function or method
// declaration, or a function literal) by name.
func goLocalScopes(fn *ts.Node, lang *ts.Language, src []byte) map[string][]goScope {
	out := map[string][]goScope{}
	add := func(id *ts.Node, from, to uint32) {
		if name := id.Text(src); name != "_" {
			out[name] = append(out[name], goScope{from, to})
		}
	}
	var walk func(n *ts.Node, enclosing *ts.Node)
	walk = func(n *ts.Node, enclosing *ts.Node) {
		switch n.Type(lang) {
		case "function_declaration", "method_declaration", "func_literal":
			body := goFunctionBody(n, lang)
			if body != nil {
				for _, pl := range namedChildren(n, lang, "parameter_list") {
					for _, pd := range goNamed(pl) {
						for _, id := range namedChildren(pd, lang, "identifier") {
							add(id, body.StartByte(), body.EndByte())
						}
					}
				}
			}
			// Parameters of a nested literal are not this function's
			// identifiers until its body; the body is walked below.
		case "short_var_declaration", "receive_statement":
			if n.ChildCount() > 0 {
				for _, id := range goDeclaredIdents(n.Child(0), lang) {
					add(id, n.EndByte(), enclosing.EndByte())
				}
			}
		case "range_clause":
			if n.ChildCount() > 0 {
				for _, id := range goDeclaredIdents(n.Child(0), lang) {
					add(id, n.EndByte(), enclosing.EndByte())
				}
			}
		case "var_spec", "const_spec":
			// The declared names; a type_identifier here is their type.
			for _, id := range namedChildren(n, lang, "identifier") {
				add(id, n.EndByte(), enclosing.EndByte())
			}
		case "type_spec", "type_alias":
			// The declared type's name is the first type_identifier; its
			// scope starts at the name.
			if ids := namedChildren(n, lang, "type_identifier"); len(ids) > 0 {
				add(ids[0], ids[0].StartByte(), enclosing.EndByte())
			}
		case "type_switch_statement":
			// switch v := x.(type): v in every clause.
			for i := 0; i < n.ChildCount(); i++ {
				c := n.Child(i)
				if c.Type(lang) == "type_switch_header" || c.Type(lang) == "expression_list" {
					for _, id := range namedChildren(c, lang, "identifier") {
						add(id, c.EndByte(), n.EndByte())
					}
				}
			}
		}
		if goOpensScope(n.Type(lang)) {
			enclosing = n
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(fn, fn)
	return out
}

// goLocalTypeScopes returns the local type declarations of fn by name, each
// in scope from its name to the end of the enclosing block: the subset of
// goLocalScopes that can shadow a type name.
func goLocalTypeScopes(fn *ts.Node, lang *ts.Language, src []byte) map[string][]goScope {
	out := map[string][]goScope{}
	var walk func(n *ts.Node, enclosing *ts.Node)
	walk = func(n *ts.Node, enclosing *ts.Node) {
		switch n.Type(lang) {
		case "type_spec", "type_alias":
			if ids := namedChildren(n, lang, "type_identifier"); len(ids) > 0 {
				if name := ids[0].Text(src); name != "_" {
					out[name] = append(out[name], goScope{ids[0].StartByte(), enclosing.EndByte()})
				}
			}
		}
		if goOpensScope(n.Type(lang)) {
			enclosing = n
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(fn, fn)
	return out
}

// goTypeParamScopes returns the type parameters of the file's declarations
// by name, each in scope over its declaration: a function's or a type's
// type_parameter_list, a method's receiver type arguments.
func goTypeParamScopes(root *ts.Node, lang *ts.Language, src []byte) map[string][]goScope {
	out := map[string][]goScope{}
	add := func(id *ts.Node, scope *ts.Node) {
		if name := id.Text(src); name != "_" {
			out[name] = append(out[name], goScope{scope.StartByte(), scope.EndByte()})
		}
	}
	var walk func(n *ts.Node, depth int)
	walk = func(n *ts.Node, depth int) {
		if depth > maxGoDepth {
			return
		}
		switch n.Type(lang) {
		case "function_declaration", "type_spec", "type_alias":
			for _, tpl := range namedChildren(n, lang, "type_parameter_list") {
				for _, decl := range namedChildren(tpl, lang, "type_parameter_declaration") {
					for _, id := range namedChildren(decl, lang, "identifier") {
						add(id, n)
					}
				}
			}
		case "method_declaration":
			if recv := namedChildren(n, lang, "parameter_list"); len(recv) > 0 {
				for _, id := range goReceiverTypeParams(recv[0], lang) {
					add(id, n)
				}
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), depth+1)
		}
	}
	walk(root, 0)
	return out
}

// goReceiverTypeParams returns the type parameters a method receiver
// declares: the type arguments of its generic type, (l *List[K, V]).
func goReceiverTypeParams(recv *ts.Node, lang *ts.Language) []*ts.Node {
	var out []*ts.Node
	var find func(n *ts.Node)
	find = func(n *ts.Node) {
		switch n.Type(lang) {
		case "generic_type":
			for _, ta := range namedChildren(n, lang, "type_arguments") {
				for _, el := range goNamed(ta) {
					if el.Type(lang) == "type_elem" && len(goNamed(el)) == 1 {
						el = goNamed(el)[0]
					}
					if t := el.Type(lang); t == "type_identifier" || t == "identifier" {
						out = append(out, el)
					}
				}
			}
			return
		case "parameter_declaration", "pointer_type", "parameter_list":
			for _, c := range goNamed(n) {
				find(c)
			}
		}
	}
	find(recv)
	return out
}

// maxGoDepth bounds recursion over the syntax tree.
const maxGoDepth = 2000

// goOpensScope reports whether a node bounds the scope of declarations made
// directly inside it.
func goOpensScope(t string) bool {
	switch t {
	case "block", "if_statement", "for_statement", "expression_switch_statement",
		"type_switch_statement", "select_statement", "expression_case", "default_case",
		"type_case", "communication_case", "func_literal":
		return true
	}
	return false
}

func goFunctionBody(fn *ts.Node, lang *ts.Language) *ts.Node {
	for i := 0; i < fn.ChildCount(); i++ {
		if c := fn.Child(i); c.Type(lang) == "block" {
			return c
		}
	}
	return nil
}

// goDeclaredIdents returns the identifiers a declaration's left side names.
func goDeclaredIdents(left *ts.Node, lang *ts.Language) []*ts.Node {
	switch left.Type(lang) {
	case "identifier":
		return []*ts.Node{left}
	case "expression_list":
		return namedChildren(left, lang, "identifier")
	}
	return nil
}

// shadowed reports whether a local declaration of name is in scope at byte
// offset at.
func goShadowed(scopes map[string][]goScope, name string, at uint32) bool {
	for _, s := range scopes[name] {
		if s.from <= at && at < s.to {
			return true
		}
	}
	return false
}
