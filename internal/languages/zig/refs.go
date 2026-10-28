package zig

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
)

// ctx is what a reference is written in.
type ctx struct {
	s         *scope
	container string // the Qualified of the innermost declaration that is a symbol
	prefix    string // the identity prefix of declarations made here
	ret       *ts.Node
	retScope  *scope // the enclosing function's result type, and its scope
}

// walkContainer walks the members of container sc, whose declaration is n.
func (x *extractor) walkContainer(sc *scope, n *ts.Node, c ctx, depth int) {
	if x.tooDeep(n, depth) || x.typ(n) == "error_set_declaration" {
		return // an error set's names are its declarations, no expressions
	}
	c.s, c.prefix, c.ret, c.retScope = sc, sc.prefix, nil, nil
	if sc.symbol {
		c.container = sc.prefix
	}
	for _, ch := range x.named(n) {
		d := x.declOf[keyOf(ch)]
		switch x.typ(ch) {
		case "variable_declaration":
			if d == nil {
				x.walkNode(ch, c, reference.KindValueReference, depth+1)
				continue
			}
			dc := c
			dc.container, dc.prefix = d.ident, d.ident
			x.walkNode(d.typ, dc, reference.KindTypeUse, depth+1)
			if d.body != nil {
				x.walkContainer(d.body, d.init, dc, depth+1)
			} else {
				x.walkResult(d.init, d.typ, sc, dc, depth+1)
			}
		case "function_declaration":
			if d != nil {
				x.walkFunction(d, c, depth+1)
			}
		case "container_field":
			x.walkNode(x.field(ch, "type"), c, reference.KindTypeUse, depth+1)
			x.walkResult(x.initializer(ch), x.field(ch, "type"), sc, c, depth+1)
		case "test_declaration":
			if d == nil {
				continue
			}
			tc := c
			tc.container, tc.prefix = d.ident, d.ident
			for _, part := range x.named(ch) {
				switch x.typ(part) {
				case "identifier": // a decltest names the declaration it tests
					x.emitPath(part, reference.KindValueReference, tc, depth+1)
				case "block":
					x.walkBlock(part, tc, depth+1)
				}
			}
		case "comptime_declaration":
			cc := c
			cc.prefix = sc.prefix + "::comptime"
			x.walkNode(x.firstNamed(ch), cc, reference.KindValueReference, depth+1)
		default:
			// A backing or tag type (enum(u8), union(enum)), usingnamespace.
			x.walkNode(ch, c, reference.KindTypeUse, depth+1)
		}
	}
}

// walkFunction walks function d: its parameter types, result type and body.
func (x *extractor) walkFunction(d *decl, c ctx, depth int) {
	fs := d.fnScope
	fc := ctx{s: fs, container: d.ident, prefix: d.ident, ret: x.field(d.fn, "type"), retScope: fs}
	name, body := x.field(d.fn, "name"), x.field(d.fn, "body")
	for _, ch := range x.named(d.fn) {
		switch {
		case name != nil && keyOf(ch) == keyOf(name):
		case body != nil && keyOf(ch) == keyOf(body):
			x.walkBlock(ch, fc, depth+1)
		case x.typ(ch) == "parameters":
			for _, p := range x.named(ch) {
				x.walkNode(x.field(p, "type"), fc, reference.KindTypeUse, depth+1)
			}
		default:
			x.walkNode(ch, fc, reference.KindTypeUse, depth+1)
		}
	}
}

// walkBlock walks a block: its locals are visible after their declaration.
func (x *extractor) walkBlock(n *ts.Node, c ctx, depth int) {
	if x.tooDeep(n, depth) {
		return
	}
	c.s = newScope(c.s, c.prefix, false)
	for _, st := range x.named(n) {
		switch x.typ(st) {
		case "variable_declaration":
			x.walkLocal(st, c, depth+1)
		case "comptime_statement":
			if inner := x.firstNamed(st); inner != nil && x.typ(inner) == "variable_declaration" {
				x.walkLocal(inner, c, depth+1)
				continue
			}
			x.walkNode(st, c, reference.KindValueReference, depth+1)
		default:
			x.walkNode(st, c, reference.KindValueReference, depth+1)
		}
	}
}

// walkLocal walks a declaration of a block and binds its name for what
// follows: a container it declares is a declaration of its own (a symbol),
// any other name a local.
func (x *extractor) walkLocal(st *ts.Node, c ctx, depth int) {
	name, typ, init := x.declName(st), x.field(st, "type"), x.initializer(st)
	x.walkNode(typ, c, reference.KindTypeUse, depth+1)
	if x.isContainer(init) && name != "" && name != "_" && x.hasToken(st, "var") {
		// A comptime var's container is declared, but the name may be
		// assigned another: it is a local whose value is unknown.
		word := strings.TrimSuffix(x.typ(init), "_declaration")
		sc := x.openContainer(init, c.prefix+"::{"+word+"}", word, c.s, false, depth)
		x.emitScope(sc)
		x.walkContainer(sc, init, c, depth+1)
		c.s.bind(&decl{name: name, scope: c.s, local: true, mutable: true, typ: typ, node: st, v: &val{}})
		return
	}
	if x.isContainer(init) && name != "" && name != "_" {
		ident := c.prefix + "::" + name
		sc := x.scopeOf[keyOf(init)]
		if sc == nil {
			sc = x.openContainer(init, ident, name, c.s, true, depth)
		} else {
			sc.parent = c.s // collected with its function: now its scope is known
		}
		d := &decl{name: name, ident: sc.prefix, scope: c.s, node: st, body: sc, init: init, typ: typ}
		x.emitDecl(d, c.container, c.s)
		c.s.bind(d)
		dc := c
		dc.container = d.ident
		x.walkContainer(sc, init, dc, depth+1)
		return
	}
	x.walkResult(init, typ, c.s, c, depth+1)
	d := &decl{name: name, scope: c.s, local: true, typ: typ, init: init, mutable: x.hasToken(st, "var"), node: st}
	if d.mutable && x.typ(st.Parent()) == "comptime_statement" {
		d.v = &val{} // a comptime var may be assigned a value of another type
	}
	x.valOf(d, 0)
	c.s.bind(d)
}

// walkResult walks an expression whose result type is the declared type
// rt, written in scope rs: a decl or enum literal there (`.init()`,
// `.red`) is a member of that type.
func (x *extractor) walkResult(v, rt *ts.Node, rs *scope, c ctx, depth int) {
	if v == nil {
		return
	}
	lit := v
	if x.typ(lit) == "try_expression" {
		lit = x.firstNamed(lit)
	}
	var member *ts.Node
	isCall := false
	switch {
	case lit != nil && x.typ(lit) == "field_expression" && x.field(lit, "object") == nil:
		member = x.field(lit, "member")
	case lit != nil && x.typ(lit) == "call_expression":
		if f := x.field(lit, "function"); f != nil && x.typ(f) == "field_expression" && x.field(f, "object") == nil {
			member, isCall = x.field(f, "member"), true
		}
	}
	if member == nil || rt == nil {
		x.walkNode(v, c, reference.KindValueReference, depth+1)
		return
	}
	if t := x.denote(rt, rs, 0).nsOne(); t != "" {
		kind := reference.KindValueReference
		if isCall {
			kind = reference.KindCall
		}
		x.emitMembers(val{ns: []string{t}}, []*ts.Node{member}, kind, c)
	}
	if isCall {
		x.walkNode(x.field(lit, "arguments"), c, reference.KindValueReference, depth+1)
	}
}

// walkNode walks the expressions of n; kind is the reference kind of a
// name written in its position (a type use in a type, a value reference
// elsewhere).
func (x *extractor) walkNode(n *ts.Node, c ctx, kind reference.ReferenceKind, depth int) {
	if n == nil || x.tooDeep(n, depth) {
		return
	}
	switch x.typ(n) {
	case "identifier", "field_expression":
		x.emitPath(n, kind, c, depth+1)
	case "call_expression":
		x.walkCall(n, c, depth+1)
	case "builtin_function":
		x.walkBuiltin(n, c, depth+1)
	case "struct_initializer":
		for i, ch := range x.named(n) {
			if i == 0 && (x.typ(ch) == "identifier" || x.typ(ch) == "field_expression") {
				x.emitPath(ch, reference.KindConstruction, c, depth+1)
				continue
			}
			x.walkNode(ch, c, reference.KindValueReference, depth+1)
		}
	case "assignment_expression":
		if l := x.field(n, "left"); l != nil && x.typ(l) == "identifier" {
			x.emitPath(l, reference.KindWrite, c, depth+1)
		} else {
			x.walkNode(l, c, reference.KindValueReference, depth+1)
		}
		x.walkNode(x.field(n, "right"), c, reference.KindValueReference, depth+1)
	case "block":
		x.walkBlock(n, c, depth+1)
	case "variable_declaration":
		if !c.s.container {
			x.walkLocal(n, c, depth+1)
		}
	case "return_expression":
		if c.ret != nil {
			x.walkResult(x.firstNamed(n), c.ret, c.retScope, c, depth+1)
			return
		}
		x.walkChildren(n, c, kind, depth)
	case "pointer_type", "nullable_type", "slice_type", "array_type", "error_union_type", "function_signature", "parameters":
		x.walkChildren(n, c, reference.KindTypeUse, depth)
	case "parameter":
		x.walkNode(x.field(n, "type"), c, reference.KindTypeUse, depth+1)
	case "struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration", "error_set_declaration":
		sc := x.scopeOf[keyOf(n)]
		if sc == nil {
			word := strings.TrimSuffix(x.typ(n), "_declaration")
			sc = x.openContainer(n, c.prefix+"::{"+word+"}", word, c.s, false, depth)
			x.emitScope(sc)
		} else {
			sc.parent = c.s
		}
		x.walkContainer(sc, n, c, depth+1)
	case "block_label", "break_label", "payload", "string", "multiline_string", "integer", "float", "character", "comment", "builtin_type":
	default:
		x.walkChildren(n, c, kind, depth)
	}
}

// walkChildren walks n's children; a payload (`|x|`) binds its captures
// for the children after it.
func (x *extractor) walkChildren(n *ts.Node, c ctx, kind reference.ReferenceKind, depth int) {
	for _, ch := range x.named(n) {
		if x.typ(ch) == "payload" {
			c.s = newScope(c.s, c.prefix, false)
			for _, id := range x.named(ch) {
				if x.typ(id) == "identifier" {
					c.s.bind(&decl{name: identifierName(x.text(id)), scope: c.s, local: true, v: &val{}})
				}
			}
			continue
		}
		x.walkNode(ch, c, kind, depth+1)
	}
}

// walkCall walks a call: its callee (a call reference) and its arguments.
func (x *extractor) walkCall(n *ts.Node, c ctx, depth int) {
	f := x.field(n, "function")
	switch {
	case f == nil:
	case x.typ(f) == "identifier" || (x.typ(f) == "field_expression" && x.field(f, "object") != nil):
		x.emitPath(f, reference.KindCall, c, depth+1)
	case x.typ(f) == "field_expression":
		// A decl literal no declared type fixes: its target is the result
		// type's member, which needs inference.
	case x.typ(f) == "index_expression" || x.typ(f) == "array_access":
		x.refs = append(x.refs, language.ReferenceDraft{
			Name: collapse(x.text(f)), Kind: string(reference.KindCall), Container: c.container,
			Location: x.loc(f), IsCall: true, Dynamic: true,
		})
		x.walkNode(f, c, reference.KindValueReference, depth+1)
	default:
		x.walkNode(f, c, reference.KindValueReference, depth+1)
	}
	x.walkNode(x.field(n, "arguments"), c, reference.KindValueReference, depth+1)
}

// typeArgumentBuiltins are the builtins whose first argument is a type.
var typeArgumentBuiltins = map[string]bool{
	"@as": true, "@sizeOf": true, "@alignOf": true, "@bitSizeOf": true, "@typeInfo": true,
	"@typeName": true, "@hasDecl": true, "@hasField": true, "@FieldType": true, "@offsetOf": true,
	"@bitOffsetOf": true,
}

// walkBuiltin walks a builtin call: @import is an import of the file it
// names; the other builtins' arguments are expressions.
func (x *extractor) walkBuiltin(n *ts.Node, c ctx, depth int) {
	name, args := x.builtinName(n), x.builtinArgs(n)
	switch name {
	case "@import":
		if len(args) != 1 {
			return
		}
		spec, ok := x.stringLiteral(args[0])
		if !ok {
			return
		}
		d := language.ReferenceDraft{Name: spec, Kind: string(reference.KindImport), Container: c.container, Location: x.loc(args[0])}
		if t, inRepo, ok := importTarget(x.key, spec); ok {
			if !strings.HasPrefix(t, "{") && inRepo {
				// A relative file's import is the importer's: its identity
				// stands for the file (or a module a table binds under the
				// spec), never the file's own declarations (buildzig.go).
				x.relativeImports = append(x.relativeImports, [2]string{t, spec})
				t = importIdent(t)
			}
			d.NameQualified, d.IdentityInRepository = t, inRepo
			switch {
			case unprovenModule(t):
				// A named module (root): whatever file the build gives
				// it, which may be any file of the repository — but a
				// ZON file's data, unless a table Ark reads binds the name.
				x.namedImports = x.namedImports || strings.HasPrefix(t, "{zigenv}")
				d.IdentityInRepository, d.TentativeIdentity = false, true
				d.MayBeAnyFile = !strings.HasSuffix(spec, ".zon")
			case strings.HasPrefix(t, importPrefix):
				// A relative file — unless the module's import table has
				// the spec as a name (buildzig.go); a case-insensitive
				// file system opens it in another case.
				// Where a table Ark cannot read may bind the spec (an open
				// resolution), any file may be the one imported.
				d.PathCaseFold, d.MayBeAnyFile = true, true
			}
		} else if !strings.HasSuffix(spec, ".zon") {
			// A module name no identity can carry: unresolved, it may be
			// any file.
			d.TentativeIdentity, d.MayBeAnyFile = true, true
		}
		x.refs = append(x.refs, d)
		return
	case "@This":
		// The container it is written in: a use of that type.
		if sc := c.s.enclosingContainer(); sc != nil && sc.symbol {
			x.emit("@This", reference.KindTypeUse, n, c, sc.prefix, nil)
		}
		return
	case "@field":
		// @field(x, "name"): x's member name, observed (unresolved, any
		// declaration of the name may be it); a computed name is Dynamic.
		if len(args) == 2 {
			x.walkNode(args[0], c, reference.KindValueReference, depth+1)
			d := language.ReferenceDraft{Kind: string(reference.KindValueReference), Container: c.container, Location: x.loc(args[1]),
				ReceiverExpr: collapse(x.text(args[0]))}
			if name, ok := x.stringLiteral(args[1]); ok && name != "" {
				d.Name, d.TentativeIdentity = name, true
			} else {
				d.Name, d.Dynamic = collapse(x.text(n)), true
				x.walkNode(args[1], c, reference.KindValueReference, depth+1)
			}
			x.refs = append(x.refs, d)
			return
		}
	}
	for i, a := range args {
		if i == 0 && typeArgumentBuiltins[name] {
			x.walkNode(a, c, reference.KindTypeUse, depth+1)
			continue
		}
		x.walkNode(a, c, reference.KindValueReference, depth+1)
	}
}

// emitPath states the references of a name or member path n of kind kind:
// the declaration its head names and each member a namespace or a proven
// receiver type fixes.
func (x *extractor) emitPath(n *ts.Node, kind reference.ReferenceKind, c ctx, depth int) {
	head, members, ok := x.splitPath(n)
	if !ok {
		return
	}
	if isPrefixType(x.typ(head)) {
		// `?*const a.B` derived as `(?*const a).B`: Zig's prefix operators
		// bind looser than member access, so the members continue the
		// innermost path (a.B), of which the prefixes make a type.
		inner := head
		for isPrefixType(x.typ(inner)) {
			for _, ch := range x.named(inner) {
				if keyOf(ch) != keyOf(x.lastNamed(inner)) {
					x.walkNode(ch, c, reference.KindValueReference, depth+1)
				}
			}
			inner = x.lastNamed(inner)
		}
		h, more, ok := x.splitPath(inner)
		if !ok || inner == nil {
			return
		}
		head, members = h, append(more, members...)
	}
	headKind := kind
	if len(members) > 0 {
		headKind = reference.KindValueReference
	}
	var v val
	switch x.typ(head) {
	case "identifier":
		name := identifierName(x.text(head))
		if name == "" || name == "_" || x.primitive(head) {
			return
		}
		ds, unknown := lookup(c.s, name)
		switch {
		case unknown || len(ds) == 0:
			if !unknown && isPrimitive(name) {
				return
			}
			if len(members) == 0 {
				x.emit(name, headKind, head, c, "", nil)
			}
		case len(ds) > 1:
			if ds[0].ident != "" {
				// Declared twice: each member is the namesake declarations'.
				x.emit(name, headKind, head, c, ds[0].ident, nil)
				v = val{ns: []string{ds[0].ident}}
			}
		case ds[0].local:
			v = x.valOf(ds[0], 0)
		default:
			x.emit(name, headKind, head, c, ds[0].ident, nil)
			v = x.valOf(ds[0], 0)
		}
	default:
		x.walkNode(head, c, reference.KindValueReference, depth+1)
		v = x.denote(head, c.s, 0)
	}
	x.emitMembers(v, members, kind, c)
}

// splitPath splits a member path into its head and its members, in order.
// ok is false for a decl or enum literal (`.a`, `.a.b`), whose head is the
// result type.
func (x *extractor) splitPath(n *ts.Node) (head *ts.Node, members []*ts.Node, ok bool) {
	head = n
	for head != nil && x.typ(head) == "field_expression" {
		obj := x.field(head, "object")
		if obj == nil {
			return nil, nil, false
		}
		members = append([]*ts.Node{x.field(head, "member")}, members...)
		head = obj
	}
	return head, members, head != nil
}

// isPrefixType reports a type built by a prefix operator.
func isPrefixType(t string) bool {
	switch t {
	case "pointer_type", "nullable_type", "slice_type", "array_type", "error_union_type":
		return true
	}
	return false
}

// emitMembers states the references of the members, in order, of a value
// v: the last of kind kind, the others value references. A member resolves
// by an identity only where v fixes where it is declared.
func (x *extractor) emitMembers(v val, members []*ts.Node, kind reference.ReferenceKind, c ctx) {
	for i, mn := range members {
		m := ""
		if mn != nil {
			m = identifierName(x.text(mn))
		}
		if m == "" {
			return
		}
		k := reference.KindValueReference
		if i == len(members)-1 {
			k = kind
		}
		call := k == reference.KindCall
		unresolvedCall := func() {
			if call {
				x.emit(m, k, mn, c, "", func(d *language.ReferenceDraft) { d.ReceiverExpr = x.receiverText(mn) })
			}
		}
		switch t := v.nsOne(); {
		case t != "":
			ident := t + "::" + m
			sc, known := x.containers[t]
			switch {
			case known && sc != nil:
				if ds := sc.decls[m]; len(ds) > 0 {
					x.emit(m, k, mn, c, ds[0].ident, nil)
					v = val{}
					if len(ds) == 1 {
						v = x.valOf(ds[0], 0)
					}
				} else if f := sc.fields[m]; f != nil {
					v = val{}
					if f.enumField {
						x.emit(m, k, mn, c, f.ident, nil)
						v = val{typ: t}
					}
				} else if sc.opaque {
					unresolvedCall()
					v = val{}
				} else {
					x.emit(m, k, mn, c, ident, nil) // undeclared: unresolved
					v = val{}
				}
			case known:
				// Two containers share t: what one declares may be the
				// other's field. Never more than a Candidate.
				x.emit(m, k, mn, c, ident, ambiguous)
				v = val{}
			case x.inFile(t) && !x.alternativeAlias[t]:
				unresolvedCall() // a member of a function or value of this file
				v = val{}
			default:
				var mod func(*language.ReferenceDraft)
				if v.spec {
					mod = x.unproven(call, mn)
				} else if v.tent || x.alternativeAlias[t] {
					mod = x.tentative(mn)
				}
				// Through std, builtin or root, which no repository file is,
				// only the last member says what the path is used for.
				if i == len(members)-1 || !reservedPath(ident) {
					x.emit(m, k, mn, c, ident, mod)
				}
				// The member is that file's: a container, or a value whose
				// members its type declares elsewhere.
				v = val{ns: []string{ident}, spec: v.spec, tent: !reservedPath(ident) || unprovenModule(ident)}
			}
		case v.typ != "" && !v.errU:
			t := v.typ
			sc, known := x.containers[t]
			switch {
			case known && sc != nil:
				ds, f := sc.decls[m], sc.fields[m]
				switch {
				case len(ds) == 1 && ds[0].fn != nil:
					if call {
						x.emit(m, k, mn, c, ds[0].ident, nil)
					}
					v = val{fn: ds[0]}
				case f != nil && !f.enumField:
					if call {
						// A field holding a function: what it calls is a value.
						x.emit(collapse(x.text(mn)), k, mn, c, "", func(d *language.ReferenceDraft) {
							d.Dynamic, d.ReceiverExpr = true, x.receiverText(mn)
						})
					}
					v = x.valOf(f, 0)
				default:
					unresolvedCall()
					v = val{}
				}
			case known:
				if call {
					x.emit(m, k, mn, c, t+"::"+m, ambiguous)
				}
				v = val{}
			case !x.inFile(t) || x.alternativeAlias[t]:
				if call {
					x.emit(m, k, mn, c, t+"::"+m, x.unproven(true, mn))
				}
				v = val{ns: []string{t + "::" + m}, spec: true}
			default:
				unresolvedCall()
				v = val{}
			}
		default:
			unresolvedCall()
			v = val{}
		}
	}
}

// ambiguous caps a member of an identity two declarations share.
func ambiguous(d *language.ReferenceDraft) { d.ConfidenceCap = "candidate" }

// unproven qualifies a member of a value another file types (a declared
// type of another file, a function's stated result type): the member may
// be a field, which is no declaration. A call is kept as observed — its
// identity names the target only if declared (TentativeIdentity) — any
// other member is kept only if a declaration has it (Speculative).
func (x *extractor) unproven(call bool, member *ts.Node) func(*language.ReferenceDraft) {
	return func(d *language.ReferenceDraft) {
		d.IdentityInRepository = false
		if call {
			// Unresolved, it may be any method of its name: its receiver
			// says it is a member (completeness attributes it to them).
			d.TentativeIdentity, d.ReceiverExpr = true, x.receiverText(member)
		} else {
			d.Speculative = true
		}
	}
}

// tentative qualifies a member of a namespace not proven one (val.tent):
// its identity names the target only if declared (TentativeIdentity).
// Undeclared, it may be a member of the type of a value another file
// declares, or of whatever file a build maps a module to: it is a member
// (its receiver), attributable to every declaration of its name.
func (x *extractor) tentative(member *ts.Node) func(*language.ReferenceDraft) {
	return func(d *language.ReferenceDraft) {
		d.IdentityInRepository = false
		d.TentativeIdentity, d.ReceiverExpr = true, x.receiverText(member)
	}
}

// emit states a reference; ident is its identity ("" for none).
func (x *extractor) emit(name string, k reference.ReferenceKind, at *ts.Node, c ctx, ident string, mod func(*language.ReferenceDraft)) {
	d := language.ReferenceDraft{
		Name: name, Kind: string(k), Container: c.container, Location: x.loc(at),
		IsCall: k == reference.KindCall,
	}
	if ident != "" {
		d.NameQualified = ident
		file := ident
		if i := strings.Index(file, "::"); i >= 0 {
			file = file[:i]
		}
		d.IdentityInRepository = file != ".." && !strings.HasPrefix(file, "../")
		// Another file's path: a case-insensitive file system may open it
		// under another case.
		d.PathCaseFold = file != x.key && (!strings.HasPrefix(file, "{") || strings.HasPrefix(file, importPrefix)) && d.IdentityInRepository
	}
	if mod != nil {
		mod(&d)
	}
	x.refs = append(x.refs, d)
}

// receiverText is the text of the expression a member is selected from.
func (x *extractor) receiverText(member *ts.Node) string {
	if p := member.Parent(); p != nil {
		if obj := x.field(p, "object"); obj != nil {
			return collapse(x.text(obj))
		}
	}
	return ""
}
