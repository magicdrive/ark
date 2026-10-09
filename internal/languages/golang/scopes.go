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
// range variable throughout its for statement. Labels are another
// namespace and shadow nothing.

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
			// The declared type's name is the first type_identifier.
			if ids := namedChildren(n, lang, "type_identifier"); len(ids) > 0 {
				add(ids[0], n.EndByte(), enclosing.EndByte())
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
