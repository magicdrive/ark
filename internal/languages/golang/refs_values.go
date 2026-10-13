package golang

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Type uses, value references and writes.
//
// Besides calls and constructions, a Go file names declarations as types
// (a parameter's type, a field's, a conversion target in a type
// assertion, an embedded field, a type argument), as values (a constant, a
// package variable, a function passed as a value, pkg.Var) and as the target
// of a write (x = v, x += v, x++, a range assignment). Each becomes one
// reference:
//
//	type_identifier T, qualified_type pkg.T   type_use        (edge: uses_type)
//	identifier x, pkg.X                       value_reference (edge: references)
//	x = …, pkg.X = …                          write           (no edge)
//	v.f = …                                   write of the field f (no edge;
//	                                          TargetKinds field: Go fields are
//	                                          no symbols, so it names nothing)
//
// Only names that can denote a package-level declaration are recorded; what
// cannot, by the language's own rules visible in the file, is not:
//
//   - a name a local declaration or a type parameter shadows at its position
//     (scopes.go) — a local's use is not a reference to a declaration;
//   - a predeclared name (int, error, any, nil, true, iota, ...) the file
//     does not declare itself;
//   - a declaring position: the names of a var / const spec, a parameter, a
//     := or range := left side, a type switch binding, a type parameter, a
//     method receiver's type arguments, a function's, method's or type's own
//     name;
//   - the key of a composite literal element whose type is not a map, slice
//     or array written in the literal: a struct literal's keys are field
//     names, and a literal whose type is a name (or elided) may be a struct;
//   - anything inside an ERROR node: the parser's recovery is no evidence;
//   - a selector x.f whose operand is neither one of the file's imports nor
//     a type the file declares (T.M, a method expression) is a field or a
//     method of a value: recorded only when the operand has type evidence
//     (a proven ReceiverType or a ReceiverChain, chains.go), and only
//     Speculatively — a field is no symbol, so the reference is kept only
//     when it resolves to a method (a method value, or a method expression
//     (*T).M, pkg.T.M, T[X].M). Its operand is a use too.
//
// A call's or construction's own name was recorded as that (collectCall,
// collectComposite, collectGenericConversion) and is not recorded again.

// goPredeclared are Go's predeclared identifiers: types, constants, zero
// value and functions (the universe block).
var goPredeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "rune": true, "string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true,
	"true": true, "false": true, "iota": true, "nil": true,
	"append": true, "cap": true, "clear": true, "close": true, "complex": true, "copy": true, "delete": true,
	"imag": true, "len": true, "make": true, "max": true, "min": true, "new": true, "panic": true,
	"print": true, "println": true, "real": true, "recover": true,
}

// span keys a node by its byte range.
func span(n *ts.Node) uint64 { return uint64(n.StartByte())<<32 | uint64(n.EndByte()) }

// markHandled records that n is a call's or construction's name, already a
// reference.
func (c *goRefCollector) markHandled(n *ts.Node) {
	if c.handled == nil {
		c.handled = map[uint64]bool{}
	}
	c.handled[span(n)] = true
}

func (c *goRefCollector) isHandled(n *ts.Node) bool { return c.handled[span(n)] }

// shadowedAt reports whether a local declaration or a type parameter named
// name is in scope at byte offset at.
func (c *goRefCollector) shadowedAt(name string, at uint32) bool {
	return goShadowed(c.scopes, name, at) || goShadowed(c.tparams, name, at)
}

// predeclared reports whether name denotes a predeclared identifier here: a
// universe name the file declares at package level neither as a type nor as
// a value. (A predeclared name another file of the package redeclares is
// missed: no reference, never a wrong one.)
func (c *goRefCollector) predeclared(name string) bool {
	return goPredeclared[name] && !c.values[name] && !c.types[name]
}

// isImportName reports whether name is one of the file's import names (an
// explicit alias, or an import path's last element: the implicit name of
// most imports) not shadowed at at. Go forbids a package-level declaration
// to share a name with an import of the file.
func (c *goRefCollector) isImportName(name string, at uint32) bool {
	if name == "" || name == "_" || name == "." || c.shadowedAt(name, at) || c.values[name] || c.types[name] {
		// A name the file declares at package level is that declaration:
		// Go forbids it to share a name with an import of the file.
		return false
	}
	for _, imp := range c.imports {
		if imp.Alias != "" {
			if imp.Alias == name {
				return true
			}
			continue
		}
		for _, n := range goImplicitImportNames(imp.Path) {
			if n == name {
				return true
			}
		}
	}
	return false
}

// goImplicitImportNames are the names an import without an alias may bind:
// the package's name, which by convention is the import path's last
// element — without a version suffix (gopkg.in/yaml.v3), a go- prefix or a
// -go suffix — or, for a major-version path (example.com/m/v2), the element
// before it. Over-approximate on purpose: a name read as an import is only
// not recorded as a use of a declaration (Go forbids a package-level
// declaration to share a name with an import of the file), and pkg.X under
// a wrong guess is resolved through the import bindings the resolver checks.
func goImplicitImportNames(path string) []string {
	elems := strings.Split(path, "/")
	last := elems[len(elems)-1]
	names := []string{last}
	if len(elems) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		names = append(names, elems[len(elems)-2])
	}
	for _, n := range names {
		if i := strings.Index(n, ".v"); i > 0 {
			names = append(names, n[:i])
		}
		if t := strings.TrimPrefix(n, "go-"); t != n {
			names = append(names, t)
		}
		if t := strings.TrimSuffix(n, "-go"); t != n {
			names = append(names, t)
		}
	}
	return names
}

// typeUse records a type name: type_identifier T or qualified_type pkg.T.
func (c *goRefCollector) typeUse(n *ts.Node, container string) {
	if c.isHandled(n) || c.inError > 0 {
		return
	}
	var name, recv string
	switch n.Type(c.lang) {
	case "type_identifier":
		name = n.Text(c.src)
		if c.shadowedAt(name, n.StartByte()) || c.predeclared(name) {
			return
		}
	case "qualified_type":
		for i := 0; i < n.ChildCount(); i++ {
			switch ch := n.Child(i); ch.Type(c.lang) {
			case "package_identifier":
				recv = ch.Text(c.src)
			case "type_identifier":
				name = ch.Text(c.src)
			}
		}
		if recv == "" || !c.isImportName(recv, n.StartByte()) {
			return
		}
	}
	if name == "" || name == "_" {
		return
	}
	kind := reference.KindTypeUse
	if recv == "" && c.values[name] && !c.types[name] {
		// The parser's type_identifier is no evidence (handlers[i](1) may be
		// read as an instantiation): the file declares name as a value.
		kind = reference.KindValueReference
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:         name,
		Kind:         string(kind),
		Container:    container,
		Location:     nodeLocation(n, c.file),
		ReceiverExpr: recv,
	})
}

// valueUse records an identifier in expression position read as a value,
// or written (kind write).
func (c *goRefCollector) valueUse(n *ts.Node, container string, kind reference.ReferenceKind) {
	if c.isHandled(n) || c.inError > 0 {
		return
	}
	name := n.Text(c.src)
	if name == "_" || c.shadowedAt(name, n.StartByte()) || c.predeclared(name) || c.isImportName(name, n.StartByte()) {
		return
	}
	if kind == reference.KindValueReference && c.types[name] && !c.values[name] {
		// An identifier the file declares as a type (a type argument the
		// parser read as an index, Select[Node]) is a type use.
		kind = reference.KindTypeUse
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:      name,
		Kind:      string(kind),
		Container: container,
		Location:  nodeLocation(n, c.file),
	})
}

// selectorUse records pkg.X (a value of an imported package, or its write)
// or, for any other operand, a write of a field v.f; it then walks the
// operand, whose own names are uses.
func (c *goRefCollector) selectorUse(n *ts.Node, container string, env goTypeEnv, write bool) {
	var operand, field *ts.Node
	for i := 0; i < n.ChildCount(); i++ {
		switch ch := n.Child(i); ch.Type(c.lang) {
		case "field_identifier":
			field = ch
		case ".":
		default:
			if operand == nil {
				operand = ch
			}
		}
	}
	if operand == nil || field == nil {
		return
	}
	if !c.isHandled(n) && c.inError == 0 {
		kind := reference.KindValueReference
		if write {
			kind = reference.KindWrite
		}
		switch {
		case operand.Type(c.lang) == "identifier" && c.isImportName(operand.Text(c.src), operand.StartByte()):
			c.refs = append(c.refs, language.ReferenceDraft{
				Name:         field.Text(c.src),
				Kind:         string(kind),
				Container:    container,
				Location:     nodeLocation(n, c.file),
				ReceiverExpr: operand.Text(c.src),
			})
			return // the operand is the package name
		case !write && operand.Type(c.lang) == "identifier" && c.types[operand.Text(c.src)] &&
			!c.shadowedAt(operand.Text(c.src), operand.StartByte()):
			// T.M with T a type the file declares: a method expression
			// (a type has no fields to select). The operand is the type.
			c.refs = append(c.refs, language.ReferenceDraft{
				Name:         field.Text(c.src),
				Kind:         string(reference.KindValueReference),
				Container:    container,
				Location:     nodeLocation(n, c.file),
				ReceiverExpr: operand.Text(c.src),
			})
		case !write:
			// x.f read: a method value when f is a method of x's type, a
			// method expression when x is a type ((*T).M, pkg.T.M, T[X].M),
			// a field read otherwise — no symbol. Recorded Speculatively, and
			// only with evidence of what x is: the resolver keeps it only if
			// it resolves to a declaration.
			recv := operand.Text(c.src)
			recvType := ""
			if operand.Type(c.lang) == "identifier" {
				recvType = c.receiverType(recv, n.StartByte(), env)
				if c.ownFields[strings.TrimPrefix(recvType, "*")][field.Text(c.src)] {
					// A field the file's own struct declares: the selection
					// (depth 0) is that field, no method.
					break
				}
			}
			chain := ""
			if recvType == "" {
				chain = c.receiverChain(n)
			}
			if recvType != "" || chain != "" {
				capped := ""
				if chain != "" {
					capped = "strong"
				}
				c.refs = append(c.refs, language.ReferenceDraft{
					Name:          field.Text(c.src),
					Kind:          string(reference.KindValueReference),
					Container:     container,
					Location:      nodeLocation(n, c.file),
					ReceiverExpr:  recv,
					ReceiverType:  recvType,
					ReceiverChain: chain,
					ConfidenceCap: capped,
					Speculative:   true,
				})
			}
		case write:
			recv := operand.Text(c.src)
			recvType := c.receiverType(recv, n.StartByte(), env)
			c.refs = append(c.refs, language.ReferenceDraft{
				Name:          field.Text(c.src),
				Kind:          string(reference.KindWrite),
				Container:     container,
				Location:      nodeLocation(n, c.file),
				ReceiverExpr:  recv,
				ReceiverType:  recvType,
				TargetKinds:   string(symbol.KindField),
				ConfidenceCap: c.localCap(field.Text(c.src), recv, recvType, n.StartByte()),
			})
		}
	}
	c.walk(operand, container, env)
}

// writeTarget records the left side of an assignment, an increment or a
// range assignment: a name or pkg.X written, a field written; anything else
// (an index, a dereference) is walked for its uses.
func (c *goRefCollector) writeTarget(n *ts.Node, container string, env goTypeEnv) {
	switch n.Type(c.lang) {
	case "identifier":
		c.valueUse(n, container, reference.KindWrite)
	case "selector_expression":
		c.selectorUse(n, container, env, true)
	case "parenthesized_expression":
		for _, ch := range goNamed(n) {
			c.writeTarget(ch, container, env)
		}
	default:
		c.walk(n, container, env)
	}
}

// literalValue walks a composite literal's elements; keysAreValues says
// whether an element key is an expression (a map, slice or array literal)
// or may be a field name (any other, or an elided, type).
func (c *goRefCollector) literalValue(n *ts.Node, container string, env goTypeEnv, keysAreValues bool) {
	for _, el := range goNamed(n) {
		switch el.Type(c.lang) {
		case "keyed_element":
			parts := goNamed(el)
			for i, part := range parts {
				inner := part
				if inner.Type(c.lang) == "literal_element" && len(goNamed(inner)) == 1 {
					inner = goNamed(inner)[0]
				}
				switch {
				case inner.Type(c.lang) == "literal_value":
					c.literalValue(inner, container, env, false) // an elided type
				case i == 0 && !keysAreValues && inner.Type(c.lang) == "identifier":
					// a field name
				default:
					c.walk(inner, container, env)
				}
			}
		case "literal_element":
			for _, inner := range goNamed(el) {
				if inner.Type(c.lang) == "literal_value" {
					c.literalValue(inner, container, env, false)
				} else {
					c.walk(inner, container, env)
				}
			}
		default:
			c.walk(el, container, env)
		}
	}
}

// keysAreValues reports whether a composite literal of type node t has
// expression keys: a map, slice or array type written in the literal.
func keysAreValues(t *ts.Node, lang *ts.Language) bool {
	switch t.Type(lang) {
	case "map_type", "slice_type", "array_type", "implicit_length_array_type":
		return true
	}
	return false
}
