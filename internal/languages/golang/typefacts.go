package golang

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Declared-type facts (language.TypeFact) and interface methods.
//
// What a package-level declaration's own text writes about types:
//
//   - a struct: every field with its declared type, embedded fields marked
//     (their members are promoted); Complete unless a parse ERROR sits in
//     its field list, which then lists what the parser read, not the code;
//   - an interface: its embedded interfaces (its methods are symbols:
//     goInterfaceMethods); union and approximation terms of a constraint
//     add no methods and are not listed;
//   - any other defined type: its underlying type; an alias: the type it
//     names;
//   - a function or a method: its result types (a named result list of n
//     names declares n results);
//   - a package variable: its written type, or — without one — how its
//     initializer is built (a receiver chain, chains.go).
//
// A type is written as language.TypeExpr (goTypeExpr): a named type as
// written (T, or q.T for an import q of the file), a pointer, slice, array or
// map of one; type arguments are dropped. A type the expression cannot
// state is "": a function, channel or literal struct type, a predeclared
// type, and any type naming a type parameter of the declaration — its
// members depend on the instantiation.

// goTypeExpr returns the TypeExpr of type node n, or "". blank reports a
// name n cannot denote a package-level type at its position (a type
// parameter or a local type in scope there).
func (c *goRefCollector) goTypeExpr(n *ts.Node, blank func(name string, at uint32) bool) language.TypeExpr {
	if n == nil {
		return ""
	}
	switch n.Type(c.lang) {
	case "type_identifier":
		name := n.Text(c.src)
		if name == "_" || c.predeclared(name) || (blank != nil && blank(name, n.StartByte())) {
			return ""
		}
		return name
	case "qualified_type":
		var q, name string
		for i := 0; i < n.ChildCount(); i++ {
			switch ch := n.Child(i); ch.Type(c.lang) {
			case "package_identifier":
				q = ch.Text(c.src)
			case "type_identifier":
				name = ch.Text(c.src)
			}
		}
		if q == "" || name == "" || !c.isImportName(q, n.StartByte()) {
			return ""
		}
		return q + "." + name
	case "pointer_type":
		if el := c.goTypeExpr(goLastNamed(n), blank); el != "" {
			return "*" + el
		}
	case "generic_type":
		if n.ChildCount() > 0 {
			switch base := n.Child(0); base.Type(c.lang) {
			case "type_identifier", "qualified_type":
				return c.goTypeExpr(base, blank)
			}
		}
	case "slice_type", "array_type", "implicit_length_array_type":
		if el := c.goTypeExpr(goLastNamed(n), blank); el != "" {
			return "[]" + el
		}
	case "map_type":
		named := goNamed(n)
		if len(named) == 2 {
			if v := c.goTypeExpr(named[1], blank); v != "" {
				return "map[" + c.goTypeExpr(named[0], blank) + "]" + v
			}
		}
	case "parenthesized_type":
		return c.goTypeExpr(goLastNamed(n), blank)
	}
	return ""
}

// goLastNamed returns n's last named child (a composite type's element).
func goLastNamed(n *ts.Node) *ts.Node {
	named := goNamed(n)
	if len(named) == 0 {
		return nil
	}
	return named[len(named)-1]
}

// goTypeParams returns the type parameter names a declaration node
// declares: a function's or a type specification's type_parameter_list, or
// the type arguments of a method's receiver type (func (b Box[T]) ...).
func goTypeParams(n *ts.Node, lang *ts.Language, src []byte) map[string]bool {
	out := map[string]bool{}
	for _, tpl := range namedChildren(n, lang, "type_parameter_list") {
		for _, d := range namedChildren(tpl, lang, "type_parameter_declaration") {
			for _, id := range namedChildren(d, lang, "identifier") {
				out[id.Text(src)] = true
			}
		}
	}
	if n.Type(lang) == "method_declaration" {
		if pls := namedChildren(n, lang, "parameter_list"); len(pls) > 0 {
			var walk func(x *ts.Node)
			walk = func(x *ts.Node) {
				switch x.Type(lang) {
				case "type_arguments":
					for _, a := range goNamed(x) {
						if a.Type(lang) == "type_elem" {
							for _, id := range namedChildren(a, lang, "type_identifier") {
								out[id.Text(src)] = true
							}
						}
						if a.Type(lang) == "type_identifier" || a.Type(lang) == "identifier" {
							out[a.Text(src)] = true
						}
					}
					return
				}
				for i := 0; i < x.ChildCount(); i++ {
					walk(x.Child(i))
				}
			}
			walk(pls[0])
		}
	}
	return out
}

// goHasError reports whether n contains an ERROR node.
func goHasError(n *ts.Node) bool {
	if n.IsError() {
		return true
	}
	for i := 0; i < n.ChildCount(); i++ {
		if goHasError(n.Child(i)) {
			return true
		}
	}
	return false
}

// typeFacts returns the facts of the file's package-level declarations.
func (c *goRefCollector) typeFacts(root *ts.Node) []language.TypeFact {
	var out []language.TypeFact
	at := func(n *ts.Node) (uint32, uint32) {
		st := nodeLocation(n, c.file).Range.Start
		return st.Line, st.Column
	}
	for i := 0; i < root.ChildCount(); i++ {
		d := root.Child(i)
		switch d.Type(c.lang) {
		case "type_declaration":
			for j := 0; j < d.ChildCount(); j++ {
				spec := d.Child(j)
				if t := spec.Type(c.lang); t != "type_spec" && t != "type_alias" {
					continue
				}
				if f, ok := c.typeSpecFact(spec); ok {
					f.Line, f.Column = at(spec)
					out = append(out, f)
				}
			}
		case "function_declaration", "method_declaration":
			name := ""
			if d.Type(c.lang) == "function_declaration" {
				name = childText(d, c.lang, c.src, "identifier")
			} else if s := goMethod(d, c.lang, c.src, c.file); s != nil {
				name = s.Qualified
			}
			if name == "" {
				continue
			}
			f := language.TypeFact{Decl: name, Results: c.resultTypes(d, goTypeParams(d, c.lang, c.src))}
			f.Line, f.Column = at(d)
			out = append(out, f)
		case "var_declaration":
			for _, spec := range goValueSpecs(d, c.lang) {
				out = append(out, c.varFacts(spec, at)...)
			}
		}
	}
	return out
}

// typeSpecFact states a type specification's fields, embedded types,
// underlying or aliased type.
func (c *goRefCollector) typeSpecFact(spec *ts.Node) (language.TypeFact, bool) {
	ids := namedChildren(spec, c.lang, "type_identifier")
	if len(ids) == 0 {
		return language.TypeFact{}, false
	}
	f := language.TypeFact{Decl: ids[0].Text(c.src)}
	tparams := goTypeParams(spec, c.lang, c.src)
	blank := func(name string, _ uint32) bool { return tparams[name] }
	var body *ts.Node
	for i := 0; i < spec.ChildCount(); i++ {
		ch := spec.Child(i)
		if ch == ids[0] || !ch.IsNamed() {
			continue
		}
		switch ch.Type(c.lang) {
		case "type_parameter_list":
			continue
		}
		body = ch
	}
	if body == nil {
		return language.TypeFact{}, false
	}
	switch body.Type(c.lang) {
	case "struct_type":
		f.Struct, f.Complete = true, !goHasError(body)
		for _, list := range namedChildren(body, c.lang, "field_declaration_list") {
			for _, fd := range namedChildren(list, c.lang, "field_declaration") {
				names := namedChildren(fd, c.lang, "field_identifier")
				var typ *ts.Node
				for k := 0; k < fd.ChildCount(); k++ {
					ch := fd.Child(k)
					switch ch.Type(c.lang) {
					case "field_identifier", ",", "*", "raw_string_literal", "interpreted_string_literal":
					default:
						if ch.IsNamed() && typ == nil {
							typ = ch
						}
					}
				}
				if len(names) == 0 {
					// An embedded field: T, *T, q.T, T[X] — its name is the
					// type's name.
					embedded := c.goTypeExpr(typ, blank)
					if embedded != "" && fd.ChildCount() > 0 && fd.Child(0).Type(c.lang) == "*" {
						embedded = "*" + embedded
					}
					f.Fields = append(f.Fields, language.FieldFact{Type: embedded, Embedded: true})
					if embedded == "" {
						// The field's name is unknown too: no member
						// lookup can exclude it.
						f.Complete = false
					}
					continue
				}
				te := c.goTypeExpr(typ, blank)
				for _, n := range names {
					f.Fields = append(f.Fields, language.FieldFact{Name: n.Text(c.src), Type: te})
				}
			}
		}
	case "interface_type":
		f.Interface, f.Complete = true, !goHasError(body)
		for _, el := range namedChildren(body, c.lang, "type_elem") {
			named := goNamed(el)
			if len(named) != 1 {
				continue // a union: a type set, no methods
			}
			switch named[0].Type(c.lang) {
			case "type_identifier", "qualified_type", "generic_type":
				te := c.goTypeExpr(named[0], blank)
				f.Fields = append(f.Fields, language.FieldFact{Type: te, Embedded: true})
				if te == "" {
					f.Complete = false
				}
			}
		}
	default:
		te := c.goTypeExpr(body, blank)
		if spec.Type(c.lang) == "type_alias" {
			if _, _, named := language.TypeNamed(language.TypeDeref(te)); named && te == language.TypeDeref(te) {
				f.Alias = te
			} else if te != "" {
				f.Underlying = te
			} else {
				return f, true // aliases an unstated type: no fact beyond the declaration
			}
		} else {
			f.Underlying = te
		}
	}
	return f, true
}

// resultTypes returns the declared result types of a function or method.
func (c *goRefCollector) resultTypes(d *ts.Node, tparams map[string]bool) []language.TypeExpr {
	blank := func(name string, _ uint32) bool { return tparams[name] }
	// The result follows the (last) parameter list: a type, or a list.
	var result *ts.Node
	seenParams := 0
	wantParams := 1
	if d.Type(c.lang) == "method_declaration" {
		wantParams = 2 // the receiver, then the parameters
	}
	for i := 0; i < d.ChildCount(); i++ {
		ch := d.Child(i)
		switch ch.Type(c.lang) {
		case "parameter_list":
			seenParams++
			if seenParams > wantParams {
				result = ch
			}
		case "block", "identifier", "field_identifier", "type_parameter_list", "func":
		default:
			if ch.IsNamed() && seenParams >= wantParams {
				result = ch
			}
		}
	}
	if result == nil || goHasError(result) {
		// No result, or one the parser could not read as written: no
		// result types are stated (a miscounted list would misnumber them).
		return nil
	}
	if result.Type(c.lang) != "parameter_list" {
		return []language.TypeExpr{c.goTypeExpr(result, blank)}
	}
	var out []language.TypeExpr
	for _, pd := range namedChildren(result, c.lang, "parameter_declaration") {
		te := c.goTypeExpr(goLastNamed(pd), blank)
		n := len(namedChildren(pd, c.lang, "identifier"))
		if n == 0 {
			n = 1
		}
		for k := 0; k < n; k++ {
			out = append(out, te)
		}
	}
	return out
}

// varFacts states each name of a package-level var spec: its written type,
// or how its initializer is built.
func (c *goRefCollector) varFacts(spec *ts.Node, at func(*ts.Node) (uint32, uint32)) []language.TypeFact {
	ids := namedChildren(spec, c.lang, "identifier")
	var typ, values *ts.Node
	for i := 0; i < spec.ChildCount(); i++ {
		ch := spec.Child(i)
		switch ch.Type(c.lang) {
		case "identifier", ",", "=":
		case "expression_list":
			values = ch
		default:
			if ch.IsNamed() {
				typ = ch
			}
		}
	}
	line, col := at(spec)
	if goHasError(spec) {
		return nil // what the parser recovered is no statement of a type
	}
	var out []language.TypeFact
	for i, id := range ids {
		name := id.Text(c.src)
		if name == "_" {
			continue
		}
		f := language.TypeFact{Decl: name, Line: line, Column: col}
		switch {
		case typ != nil:
			f.Type = c.goTypeExpr(typ, nil)
		case values != nil:
			vals := goNamed(values)
			switch {
			case len(vals) == len(ids):
				f.Chain = c.exprChain(vals[i], 0)
			case len(vals) == 1:
				f.Chain = c.callResultChain(vals[0], i, 0)
			}
		}
		if f.Type != "" || f.Chain != "" {
			out = append(out, f)
		}
	}
	return out
}

// goInterfaceMethods returns a symbol for every method an interface type
// declares (method_elem): a member of the interface, like a method of a
// type (Qualified I.M, Receiver I), contained in it (Parent I).
func goInterfaceMethods(spec *ts.Node, iface string, lang *ts.Language, src []byte, file source.FileID) []language.SymbolDraft {
	var out []language.SymbolDraft
	for _, it := range namedChildren(spec, lang, "interface_type") {
		for _, m := range namedChildren(it, lang, "method_elem") {
			ids := namedChildren(m, lang, "field_identifier")
			if len(ids) == 0 {
				continue
			}
			name := ids[0].Text(src)
			out = append(out, language.SymbolDraft{
				Name:      name,
				Qualified: iface + "." + name,
				Kind:      symbol.KindMethod,
				Location:  nodeLocation(m, file),
				StartByte: m.StartByte(),
				EndByte:   m.EndByte(),
				Parent:    iface,
				Receiver:  iface,
				Exported:  isExported(name),
			})
		}
	}
	return out
}
