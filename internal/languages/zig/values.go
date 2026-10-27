package zig

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"
)

// maxValueDepth bounds the evaluation of what an expression denotes (a
// chain of constants naming constants): past it, it denotes nothing.
const maxValueDepth = 64

// denote is what expression n, written in scope s, denotes by the file's
// declarations (val). It states nothing it cannot prove.
func (x *extractor) denote(n *ts.Node, s *scope, depth int) val {
	if n == nil || s == nil || depth > maxValueDepth {
		return val{}
	}
	switch x.typ(n) {
	case "identifier":
		ds, unknown := lookup(s, identifierName(x.text(n)))
		switch {
		case unknown || len(ds) == 0:
			return val{}
		case len(ds) > 1:
			// Declared twice: the name is ambiguous; its identity is shared.
			if ds[0].ident != "" {
				return val{ns: []string{ds[0].ident}}
			}
			return val{}
		}
		return x.valOf(ds[0], depth+1)
	case "field_expression":
		obj, m := x.field(n, "object"), x.field(n, "member")
		if obj == nil || m == nil {
			return val{} // a decl or enum literal: its result type decides
		}
		return x.member(x.denote(obj, s, depth+1), identifierName(x.text(m)), depth+1)
	case "builtin_function":
		return x.builtinVal(n, s, depth)
	case "call_expression":
		return x.callVal(x.denote(x.field(n, "function"), s, depth+1), depth+1)
	case "struct_initializer":
		if t := x.denote(x.firstNamed(n), s, depth+1).nsOne(); t != "" {
			return val{typ: t}
		}
	case "try_expression", "catch_expression":
		v := x.denote(x.firstNamed(n), s, depth+1)
		if v.spec {
			return v // another file's result: its stated type is the payload's
		}
		if !v.errU {
			return val{} // not an error union of a proven type
		}
		v.errU = false
		return v
	case "parenthesized_expression", "comptime_expression", "nosuspend_expression":
		return x.denote(x.firstNamed(n), s, depth+1)
	case "dereference_expression":
		// p.*: the pointee, whose type is what a pointer's val states.
		return x.denote(x.firstNamed(n), s, depth+1)
	case "struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration", "error_set_declaration":
		if sc := x.scopeOf[keyOf(n)]; sc != nil {
			return val{ns: []string{sc.prefix}}
		}
	case "if_expression", "switch_expression":
		return x.alternatives(n, s, depth)
	}
	return val{}
}

// alternatives is what a comptime choice between namespaces denotes: every
// branch's namespace, when every branch is one (none is chosen).
func (x *extractor) alternatives(n *ts.Node, s *scope, depth int) val {
	var branches []*ts.Node
	switch x.typ(n) {
	case "if_expression":
		for i, ch := range x.named(n) {
			if i == 0 || x.typ(ch) == "payload" {
				continue // the condition
			}
			if x.typ(ch) == "else_clause" {
				ch = x.lastNamed(ch)
			}
			branches = append(branches, ch)
		}
	case "switch_expression":
		for _, ch := range x.named(n) {
			if x.typ(ch) == "switch_case" {
				branches = append(branches, x.lastNamed(ch))
			}
		}
	}
	if len(branches) < 2 {
		return val{}
	}
	var out []string
	seen := map[string]bool{}
	for _, b := range branches {
		v := x.denote(b, s, depth+1)
		if len(v.ns) == 0 {
			return val{}
		}
		for _, t := range v.ns {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return val{ns: out}
}

// builtinVal is what a builtin call denotes: a file @import names, the
// container @This() is in, the type @as gives.
func (x *extractor) builtinVal(n *ts.Node, s *scope, depth int) val {
	args := x.builtinArgs(n)
	switch x.builtinName(n) {
	case "@import":
		if len(args) == 1 {
			if spec, ok := x.stringLiteral(args[0]); ok {
				if t, _, ok := importTarget(x.key, spec); ok {
					return val{ns: []string{t}}
				}
			}
		}
	case "@This":
		if c := s.enclosingContainer(); c != nil {
			return val{ns: []string{c.prefix}}
		}
	case "@as":
		if len(args) == 2 {
			if t := x.typeOf(args[0], s, depth+1); t != "" {
				return val{typ: t}
			}
		}
	}
	return val{}
}

// valOf is what a declaration's name denotes.
func (x *extractor) valOf(d *decl, depth int) val {
	if d.v != nil {
		return *d.v
	}
	if d.busy || depth > maxValueDepth {
		return val{}
	}
	d.busy = true
	defer func() { d.busy = false }()
	var v val
	switch {
	case d.body != nil:
		v = val{ns: []string{d.body.prefix}}
	case d.fn != nil:
		v = val{fn: d}
	case d.enumField:
		v = val{typ: d.scope.prefix}
	case d.field:
		v = val{typ: x.typeOf(d.typ, d.scope, depth+1)}
	default:
		typed := d.typ != nil && !x.isTypeKeyword(d.typ)
		if typed {
			v.typ = x.typeOf(d.typ, d.scope, depth+1)
		}
		if d.init != nil {
			iv := x.denote(d.init, d.scope, depth+1)
			if !typed {
				v.typ, v.errU = iv.typ, iv.errU
				v.fn = iv.fn
				if iv.spec && len(iv.ns) == 1 {
					// A value another file's declaration types: its members
					// are looked up there, a var's as a const's.
					v.ns, v.spec = iv.ns, true
				} else if !d.mutable {
					switch {
					case len(iv.ns) == 1:
						v.ns = iv.ns
					case len(iv.ns) > 1 && d.ident != "":
						// Alternatives: the constant's own identity, whose
						// aliases name each of them (a member is a
						// Candidate of every alternative).
						v.ns = []string{d.ident}
					}
				}
			}
		}
	}
	d.v = &v
	return v
}

// member is what member m of a value v denotes.
func (x *extractor) member(v val, m string, depth int) val {
	if t := v.nsOne(); t != "" {
		sc, known := x.containers[t]
		switch {
		case known && sc != nil:
			if ds := sc.decls[m]; len(ds) == 1 {
				return x.valOf(ds[0], depth+1)
			} else if len(ds) > 1 {
				return val{}
			}
			if f := sc.fields[m]; f != nil && f.enumField {
				return val{typ: t}
			}
			return val{}
		case known:
			return val{} // two containers of this file share the identity
		case x.inFile(t) && !x.alternativeAlias[t]:
			return val{} // a member of a function or a value of this file
		}
		// A container of another file (or a constant this file declares
		// to name several): its member's identity, whatever it is.
		return val{ns: []string{t + "::" + m}, spec: v.spec}
	}
	if t := v.typ; t != "" && !v.errU {
		sc, known := x.containers[t]
		switch {
		case known && sc != nil:
			if ds := sc.decls[m]; len(ds) == 1 && ds[0].fn != nil {
				return val{fn: ds[0]}
			}
			if f := sc.fields[m]; f != nil && !f.enumField {
				return x.valOf(f, depth+1)
			}
		case !known && (!x.inFile(t) || x.alternativeAlias[t]):
			// A value of another file's type: its member, a method (whose
			// result that file states) or a field.
			return val{ns: []string{t + "::" + m}, spec: true}
		}
	}
	return val{}
}

// callVal is what a call of v returns: by a function of this file, the
// container its one return is and the type its result type names; by a
// path into another file, the container that path's function may return
// (F::(), which only a function returning one container declares).
func (x *extractor) callVal(v val, depth int) val {
	if v.fn != nil {
		return x.result(v.fn, depth+1)
	}
	if t := v.nsOne(); t != "" && (!x.inFile(t) || x.alternativeAlias[t]) {
		return val{ns: []string{t + "::()"}, spec: true}
	}
	return val{}
}

// result is what a call of function d returns.
func (x *extractor) result(d *decl, depth int) val {
	if d.ret != nil {
		return *d.ret
	}
	if d.busy || depth > maxValueDepth {
		return val{}
	}
	d.busy = true
	defer func() { d.busy = false }()
	var v val
	if d.returns != nil {
		v.ns = []string{d.returns.prefix}
	}
	if rt := x.field(d.fn, "type"); rt != nil && !x.isTypeKeyword(rt) {
		if x.typ(rt) == "error_union_type" {
			v.typ, v.errU = x.typeOf(x.field(rt, "ok"), d.fnScope, depth+1), true
			if v.typ == "" {
				v.errU = false
			}
		} else {
			v.typ = x.typeOf(rt, d.fnScope, depth+1)
		}
	}
	d.ret = &v
	return v
}

// typeOf is the identity of the type whose members a value of declared type
// n has — a container (or a pointer to one: Zig dereferences one level for
// a member access) — or "". An optional, a slice, an array, an error union
// or a many-item pointer has no members of a container.
func (x *extractor) typeOf(n *ts.Node, s *scope, depth int) string {
	if n == nil || depth > maxValueDepth {
		return ""
	}
	switch x.typ(n) {
	case "pointer_type":
		if !x.singlePointer(n) {
			return ""
		}
		return x.typeOf(x.lastNamed(n), s, depth+1)
	case "parenthesized_expression":
		return x.typeOf(x.firstNamed(n), s, depth+1)
	case "field_expression":
		// The grammar derives `*const a.B` as `(*const a).B`; Zig's prefix
		// operators bind looser than member access: read it as `*const (a.B)`.
		var members []string
		cur := n
		for cur != nil && x.typ(cur) == "field_expression" && x.field(cur, "object") != nil {
			members = append([]string{identifierName(x.text(x.field(cur, "member")))}, members...)
			cur = x.field(cur, "object")
		}
		switch x.typ(cur) {
		case "pointer_type":
			if !x.singlePointer(cur) {
				return ""
			}
			v := x.denote(x.lastNamed(cur), s, depth+1)
			for _, m := range members {
				v = x.member(v, m, depth+1)
			}
			return v.nsOne()
		case "nullable_type", "slice_type", "array_type", "error_union_type":
			return ""
		}
		return x.denote(n, s, depth+1).nsOne()
	case "identifier", "call_expression", "builtin_function",
		"struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration":
		return x.denote(n, s, depth+1).nsOne()
	}
	return ""
}

// singlePointer reports a single-item pointer type (*T, *const T): Zig
// reaches the pointee's members through it.
func (x *extractor) singlePointer(n *ts.Node) bool {
	return n.ChildCount() > 0 && x.typ(n.Child(0)) == "*"
}

// isTypeKeyword reports the type `type`: a constant of that declared type
// is a type, its value its namespace.
func (x *extractor) isTypeKeyword(n *ts.Node) bool {
	return strings.TrimSpace(x.text(n)) == "type"
}

// inFile reports an identity of this file.
func (x *extractor) inFile(ident string) bool {
	return ident == x.key || strings.HasPrefix(ident, x.key+"::")
}
