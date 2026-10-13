package golang

import (
	"strconv"

	ts "github.com/odvcencio/gotreesitter"
)

// Receiver chains (language.ReferenceDraft.ReceiverChain).
//
// A receiver without a proven type (receiver_types.go) is described by how
// the code builds it, step by step, naming declarations only as written:
//
//	x := NewServer(cfg); x.Run()        decl:NewServer|call:0
//	a, err := pkg.Open(); a.Close()     decl:pkg.Open|call:0
//	s.repo.Find()     (s *Server)       type:*Server|member:repo
//	for _, it := range items { it.M() } (items []*Item)  type:[]*Item|elem
//	v := y.(*T); v.M()                  type:*T
//	NewServer().Run()                   decl:NewServer|call:0
//	(*T).M, T[int].M, pkg.T.M           decl:T, decl:T|elem, decl:pkg.T
//
// A local variable is described by its one declaration in scope at the use
// (goLocalScopes, Go's own scoping): a parameter or a var with a written
// type is a value of that type, a := or a var without one is its
// initializer's chain, a range variable an element of the ranged value. A
// := naming a variable its block already declares (or, at a function body's
// top, a parameter or a named result) redeclares it — an assignment that
// keeps its type — and `for k, v = range x` assigns: neither declares. A
// variable whose declaration states nothing usable (a type switch binding, a
// channel receive, the key of a range) has no chain — and, being local, is
// never read as a package-level declaration. A name no local declaration
// covers is the package-level declaration of that name (decl:), unless it is
// an import, a predeclared name or a type parameter.
//
// A chain is only a description: the resolver evaluates it against the
// declarations' own types (language.TypeFact) and proves nothing with a step
// that names no single declaration (resolver/typefacts.go) — the receiver is
// then an untyped value, never an import or a type its name spells.

// maxLocalChain bounds the derivation of a local from other locals.
const maxLocalChain = 8

// goLocalDef is how one local declaration states its variable's type.
type goLocalDef struct {
	scope goScope
	// block: the start of the node whose scope the declaration is in (a
	// block, a case clause, an if / for / switch statement, the function's
	// body for a parameter) — which block, for telling a redeclaration from
	// a declaration in an inner block that ends at the same byte.
	block uint32
	// typ: the written type (a parameter, a var with a type); value: the
	// initializer; index: which result of a multi-value call value is; elem:
	// the value is ranged over (the variable is the range's element).
	typ   *ts.Node
	value *ts.Node
	index int
	elem  bool
	// none: the declaration states nothing usable.
	none bool
}

// goLocalDefs returns fn's local declarations by name with their scopes
// (as goLocalScopes) and what each states about its variable's type.
func goLocalDefs(fn *ts.Node, lang *ts.Language, src []byte) map[string][]goLocalDef {
	out := map[string][]goLocalDef{}
	add := func(id *ts.Node, d goLocalDef) {
		if name := id.Text(src); name != "_" {
			out[name] = append(out[name], d)
		}
	}
	var walk func(n *ts.Node, enclosing *ts.Node)
	walk = func(n *ts.Node, enclosing *ts.Node) {
		switch n.Type(lang) {
		case "function_declaration", "method_declaration", "func_literal":
			if body := goFunctionBody(n, lang); body != nil {
				for _, pl := range namedChildren(n, lang, "parameter_list") {
					for _, pd := range goNamed(pl) {
						typ := goLastNamed(pd)
						variadic := pd.Type(lang) == "variadic_parameter_declaration"
						for _, id := range namedChildren(pd, lang, "identifier") {
							d := goLocalDef{scope: goScope{body.StartByte(), body.EndByte()}, block: body.StartByte(), typ: typ}
							if variadic {
								d.none = true // ...T is a []T: no written node states it
							}
							add(id, d)
						}
					}
				}
			}
		case "short_var_declaration":
			lists := namedChildren(n, lang, "expression_list")
			if len(lists) == 2 {
				lhs, rhs := goNamed(lists[0]), goNamed(lists[1])
				for i, id := range lhs {
					if id.Type(lang) != "identifier" {
						continue
					}
					// A name already declared in this block (or, at a
					// function body's top, a parameter or a named result) is
					// redeclared: assigned, its type unchanged — no new
					// declaration.
					redeclared := false
					for _, prev := range out[id.Text(src)] {
						if prev.block == enclosing.StartByte() && prev.scope.to == enclosing.EndByte() && prev.scope.from <= n.StartByte() {
							redeclared = true
						}
					}
					if redeclared {
						continue
					}
					d := goLocalDef{scope: goScope{n.EndByte(), enclosing.EndByte()}, block: enclosing.StartByte()}
					switch {
					case len(rhs) == len(lhs):
						d.value = rhs[i]
					case len(rhs) == 1:
						d.value, d.index = rhs[0], i
					default:
						d.none = true
					}
					add(id, d)
				}
			}
		case "receive_statement":
			if n.ChildCount() > 0 {
				for _, id := range goDeclaredIdents(n.Child(0), lang) {
					add(id, goLocalDef{scope: goScope{n.EndByte(), enclosing.EndByte()}, block: enclosing.StartByte(), none: true})
				}
			}
		case "range_clause":
			// Only `for k, v := range x` declares; with = it assigns.
			declares := false
			for i := 0; i < n.ChildCount(); i++ {
				if n.Child(i).Type(lang) == ":=" {
					declares = true
				}
			}
			if declares && n.ChildCount() > 0 {
				ids := goDeclaredIdents(n.Child(0), lang)
				ranged := goLastNamed(n)
				for i, id := range ids {
					d := goLocalDef{scope: goScope{n.EndByte(), enclosing.EndByte()}, block: enclosing.StartByte()}
					if i == 1 && ranged != nil {
						d.value, d.elem = ranged, true
					} else {
						d.none = true // a key, an index — or the elements of a channel or an iterator
					}
					add(id, d)
				}
			}
		case "var_spec", "const_spec":
			ids := namedChildren(n, lang, "identifier")
			var typ, values *ts.Node
			for i := 0; i < n.ChildCount(); i++ {
				ch := n.Child(i)
				switch ch.Type(lang) {
				case "identifier", ",", "=":
				case "expression_list":
					values = ch
				default:
					if ch.IsNamed() {
						typ = ch
					}
				}
			}
			for i, id := range ids {
				d := goLocalDef{scope: goScope{n.EndByte(), enclosing.EndByte()}, block: enclosing.StartByte()}
				switch {
				case n.Type(lang) == "const_spec":
					d.none = true
				case typ != nil:
					d.typ = typ
				case values != nil && len(goNamed(values)) == len(ids):
					d.value = goNamed(values)[i]
				case values != nil && len(goNamed(values)) == 1:
					d.value, d.index = goNamed(values)[0], i
				default:
					d.none = true
				}
				add(id, d)
			}
		case "type_switch_statement":
			// switch v := x.(type): v is declared in each clause's implicit
			// block (a := of v there redeclares it), of a type no syntax
			// here states.
			var guards []*ts.Node
			for i := 0; i < n.ChildCount(); i++ {
				ch := n.Child(i)
				if ch.Type(lang) == "type_switch_header" || ch.Type(lang) == "expression_list" {
					for _, id := range namedChildren(ch, lang, "identifier") {
						guards = append(guards, id)
						add(id, goLocalDef{scope: goScope{ch.EndByte(), n.EndByte()}, none: true})
					}
				}
			}
			for i := 0; i < n.ChildCount(); i++ {
				if cl := n.Child(i); cl.Type(lang) == "type_case" || cl.Type(lang) == "default_case" {
					for _, id := range guards {
						add(id, goLocalDef{scope: goScope{cl.StartByte(), cl.EndByte()}, block: cl.StartByte(), none: true})
					}
				}
			}
		case "type_spec", "type_alias":
			// A local type: a name in scope that is no variable.
			if ids := namedChildren(n, lang, "type_identifier"); len(ids) > 0 {
				add(ids[0], goLocalDef{scope: goScope{ids[0].StartByte(), enclosing.EndByte()}, none: true})
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

// localDef returns the innermost local declaration of name in scope at at.
func (c *goRefCollector) localDef(name string, at uint32) (goLocalDef, bool) {
	var best goLocalDef
	found := false
	for _, d := range c.defs[name] {
		if at < d.scope.from || at >= d.scope.to {
			continue
		}
		if !found || d.scope.from > best.scope.from {
			best, found = d, true
		}
	}
	return best, found
}

// blankAt reports a type name a type parameter or a local type shadows at
// at: no package-level type.
func (c *goRefCollector) blankAt(name string, at uint32) bool {
	return goShadowed(c.tparams, name, at) || goShadowed(c.ltypes, name, at)
}

// identChain describes identifier name used at at.
func (c *goRefCollector) identChain(name string, at uint32, depth int) string {
	if name == "" || name == "_" || depth > maxLocalChain {
		return ""
	}
	if d, local := c.localDef(name, at); local {
		return c.localChain(d, depth+1)
	}
	if c.shadowedAt(name, at) || c.predeclared(name) || c.isImportName(name, at) {
		return ""
	}
	return "decl:" + name
}

// localChain describes a local variable by its declaration.
func (c *goRefCollector) localChain(d goLocalDef, depth int) string {
	switch {
	case d.none || depth > maxLocalChain:
		return ""
	case d.typ != nil:
		if te := c.goTypeExpr(d.typ, c.blankAt); te != "" {
			return "type:" + te
		}
		return ""
	case d.value != nil && d.elem:
		if ch := c.exprChain(d.value, depth); ch != "" {
			return ch + "|elem"
		}
		return ""
	case d.value != nil && d.index > 0:
		return c.callResultChain(d.value, d.index, depth)
	case d.value != nil:
		if ch := c.exprChain(d.value, depth); ch != "" {
			return ch
		}
		// x, ok := y.(T) / m[k]: the first of two values.
		return ""
	}
	return ""
}

// callResultChain describes result index of the multi-value expression n:
// a call's result, or the value of a type assertion or a map index (v, ok).
func (c *goRefCollector) callResultChain(n *ts.Node, index, depth int) string {
	switch n.Type(c.lang) {
	case "call_expression":
		if fn := c.calleeChain(n, depth); fn != "" {
			return fn + "|call:" + strconv.Itoa(index)
		}
	case "type_assertion_expression", "index_expression":
		if index == 0 {
			return c.exprChain(n, depth)
		}
	case "parenthesized_expression":
		if in := goLastNamed(n); in != nil {
			return c.callResultChain(in, index, depth)
		}
	}
	return ""
}

// calleeChain describes the function a call calls ("" for a builtin, a
// conversion to a written type it cannot state, ...).
func (c *goRefCollector) calleeChain(call *ts.Node, depth int) string {
	if call.ChildCount() == 0 {
		return ""
	}
	return c.exprChain(call.Child(0), depth)
}

// exprChain describes the value of expression n.
func (c *goRefCollector) exprChain(n *ts.Node, depth int) string {
	if n == nil || depth > maxLocalChain {
		return ""
	}
	switch n.Type(c.lang) {
	case "identifier":
		return c.identChain(n.Text(c.src), n.StartByte(), depth)
	case "selector_expression":
		operand, field := goSelectorParts(n, c.lang)
		if operand == nil || field == nil {
			return ""
		}
		if operand.Type(c.lang) == "identifier" && c.isImportName(operand.Text(c.src), operand.StartByte()) {
			return "decl:" + operand.Text(c.src) + "." + field.Text(c.src)
		}
		if ch := c.exprChain(operand, depth+1); ch != "" {
			return ch + "|member:" + field.Text(c.src)
		}
	case "call_expression":
		fn := n.Child(0)
		if fn.Type(c.lang) == "identifier" && fn.Text(c.src) == "new" && c.predeclared("new") && !c.shadowedAt("new", fn.StartByte()) {
			if args := namedChildren(n, c.lang, "argument_list"); len(args) == 1 {
				if a := goNamed(args[0]); len(a) == 1 {
					if te := c.goTypeExpr(a[0], c.blankAt); te != "" {
						return "type:*" + te
					}
				}
			}
			return ""
		}
		if fn.Type(c.lang) == "identifier" && c.predeclared(fn.Text(c.src)) && !c.shadowedAt(fn.Text(c.src), fn.StartByte()) {
			return "" // a builtin: append, make, min, ...
		}
		if ch := c.calleeChain(n, depth+1); ch != "" {
			return ch + "|call:0"
		}
	case "composite_literal":
		if n.ChildCount() > 0 {
			if te := c.goTypeExpr(n.Child(0), c.blankAt); te != "" {
				return "type:" + te
			}
		}
	case "unary_expression":
		// &x is a pointer to x, *p what p points to: the same members.
		if n.ChildCount() == 2 {
			switch n.Child(0).Type(c.lang) {
			case "&", "*":
				return c.exprChain(n.Child(1), depth+1)
			}
		}
	case "parenthesized_expression":
		return c.exprChain(goLastNamed(n), depth+1)
	case "index_expression":
		// x[i]: an element — or, for a generic function or type, an
		// instantiation (the resolver tells them apart by what x names).
		if named := goNamed(n); len(named) > 0 {
			if ch := c.exprChain(named[0], depth+1); ch != "" {
				return ch + "|elem"
			}
		}
	case "type_assertion_expression":
		if t := goLastNamed(n); t != nil {
			if te := c.goTypeExpr(t, c.blankAt); te != "" {
				return "type:" + te
			}
		}
	}
	return ""
}

// goSelectorParts returns a selector expression's operand and field.
func goSelectorParts(n *ts.Node, lang *ts.Language) (operand, field *ts.Node) {
	for i := 0; i < n.ChildCount(); i++ {
		switch ch := n.Child(i); ch.Type(lang) {
		case "field_identifier":
			field = ch
		case ".":
		default:
			if operand == nil {
				operand = ch
			}
		}
	}
	return operand, field
}

// receiverChain describes the receiver of a call or selector node (a
// selector_expression, or a qualified_type the parser read for v.f(x)), or
// "": an import's member, a receiver without a usable description.
func (c *goRefCollector) receiverChain(node *ts.Node) string {
	var operand *ts.Node
	switch node.Type(c.lang) {
	case "selector_expression":
		operand, _ = goSelectorParts(node, c.lang)
	case "qualified_type":
		for i := 0; i < node.ChildCount(); i++ {
			if ch := node.Child(i); ch.Type(c.lang) == "package_identifier" {
				operand = ch
			}
		}
		if operand == nil {
			return ""
		}
		name := operand.Text(c.src)
		if c.isImportName(name, operand.StartByte()) {
			return ""
		}
		return c.identChain(name, operand.StartByte(), 0)
	}
	if operand == nil {
		return ""
	}
	if operand.Type(c.lang) == "identifier" && c.isImportName(operand.Text(c.src), operand.StartByte()) {
		return ""
	}
	return c.exprChain(operand, 0)
}
