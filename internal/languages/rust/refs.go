package rust

import (
	"regexp"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Target kinds a name position can denote (ReferenceDraft.TargetKinds): a
// module and a function of one name share an identity, the namespace tells
// them apart.
const (
	valueKinds  = "function,method,constant,variable,struct"
	typeKinds   = "struct,enum,trait,type_alias"
	structKinds = "struct,type_alias,constant" // a braced struct or variant
)

// walk observes the references of an expression, a statement or a type.
func (x *extractor) walk(s *scope, n *ts.Node, c ctx, depth int) {
	if n == nil || x.tooDeep(n, depth) {
		return
	}
	switch x.typ(n) {
	case "token_tree", "attribute_item", "inner_attribute_item", "line_comment", "block_comment",
		"macro_definition", "lifetime", "field_identifier", "shorthand_field_identifier",
		"primitive_type", "self", "string_literal", "raw_string_literal", "char_literal",
		"integer_literal", "float_literal", "boolean_literal", "label", "visibility_modifier":
		return
	case "block":
		x.block(s, n, c.container, c, depth+1)
		return
	case "macro_invocation":
		x.macroRef(s, n, c)
		return
	case "identifier":
		x.valueRef(s, n, []string{x.text(n)}, c, reference.KindValueReference, false)
		return
	case "scoped_identifier":
		x.pathExpr(s, n, c, reference.KindValueReference, false, depth)
		return
	case "call_expression":
		x.call(s, n, c, depth)
		return
	case "struct_expression":
		x.typeRefOf(s, x.field(n, "name"), c, reference.KindConstruction, structKinds, depth+1)
		x.walk(s, x.field(n, "body"), c, depth+1)
		return
	case "field_expression":
		x.walk(s, x.field(n, "value"), c, depth+1)
		return
	case "type_identifier", "scoped_type_identifier", "generic_type":
		x.typeRef(s, n, c, reference.KindTypeUse, depth+1)
		return
	case "type_binding":
		// `Item = T`: Item is the trait's associated type, a member name.
		x.walk(s, x.field(n, "type"), c, depth+1)
		return
	case "type_parameter", "constrained_type_parameter", "optional_type_parameter", "const_parameter":
		x.walkChildrenExcept(s, n, c, depth, "name", "left")
		return
	case "closure_parameters":
		for _, p := range x.named(n) {
			if x.typ(p) == "parameter" {
				x.walk(s, p, c, depth+1)
			} else {
				x.pattern(s, p, c, depth+1)
			}
		}
		return
	case "function_item", "function_signature_item", "struct_item", "union_item", "enum_item",
		"trait_item", "impl_item", "type_item", "const_item", "static_item", "mod_item",
		"use_declaration", "foreign_mod_item", "extern_crate_declaration":
		// An item outside a block body (cannot happen in valid Rust).
		return
	}
	x.walkChildrenExcept(s, n, c, depth)
}

// walkChildrenExcept walks n's named children but those of the given
// fields; a child in a pattern position is walked as a pattern.
func (x *extractor) walkChildrenExcept(s *scope, n *ts.Node, c ctx, depth int, skip ...string) {
	if n == nil || x.tooDeep(n, depth) {
		return
	}
	for i := 0; i < n.ChildCount(); i++ {
		ch := n.Child(i)
		if !ch.IsNamed() {
			continue
		}
		f := n.FieldNameForChild(i, x.lang)
		if f != "" && contains(skip, f) {
			continue
		}
		if f == "pattern" {
			x.pattern(s, ch, c, depth+1)
			continue
		}
		x.walk(s, ch, c, depth+1)
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// pattern observes the paths of a pattern; its plain identifiers bind
// names (a constant or a unit struct written as a bare identifier is not
// told apart from a binding: it is not observed).
func (x *extractor) pattern(s *scope, n *ts.Node, c ctx, depth int) {
	if n == nil || x.tooDeep(n, depth) {
		return
	}
	switch x.typ(n) {
	case "identifier", "shorthand_field_identifier", "field_identifier", "self", "mutable_specifier":
		return
	case "scoped_identifier":
		x.pathExpr(s, n, c, reference.KindValueReference, false, depth)
		return
	case "tuple_struct_pattern":
		t := x.field(n, "type")
		if x.typ(t) == "identifier" {
			x.valueRef(s, t, []string{x.text(t)}, c, reference.KindValueReference, false)
		} else {
			x.pathExpr(s, t, c, reference.KindValueReference, false, depth)
		}
		for i := 0; i < n.ChildCount(); i++ {
			if ch := n.Child(i); ch.IsNamed() && n.FieldNameForChild(i, x.lang) != "type" {
				x.pattern(s, ch, c, depth+1)
			}
		}
		return
	case "struct_pattern":
		x.typeRefOf(s, x.field(n, "type"), c, reference.KindTypeUse, structKinds, depth+1)
		for _, fp := range x.named(n) {
			if x.typ(fp) == "field_pattern" {
				x.pattern(s, x.field(fp, "pattern"), c, depth+1)
			}
		}
		return
	case "block", "closure_expression", "macro_invocation", "token_tree":
		// Const blocks and macros in patterns are not observed.
		return
	case "match_pattern":
		// `pat if guard`: the guard is an expression.
		for i := 0; i < n.ChildCount(); i++ {
			ch := n.Child(i)
			if !ch.IsNamed() {
				continue
			}
			if n.FieldNameForChild(i, x.lang) == "condition" {
				x.walk(s, ch, c, depth+1)
			} else {
				x.pattern(s, ch, c, depth+1)
			}
		}
		return
	}
	for _, ch := range x.named(n) {
		switch x.typ(ch) {
		case "type_identifier", "scoped_type_identifier", "generic_type":
			x.typeRef(s, ch, c, reference.KindTypeUse, depth+1)
		default:
			x.pattern(s, ch, c, depth+1)
		}
	}
}

// bindings returns the names the patterns below n bind (parameters, let,
// match arms, if/while let, for, closures), nested items excluded. It
// over-approximates a scope's locals: a name it returns is not observed in
// a value position of the function, which loses references, never binds
// one wrongly.
func (x *extractor) bindings(n *ts.Node) []string {
	var out []string
	var visit func(n *ts.Node, inPattern bool, depth int)
	visit = func(n *ts.Node, inPattern bool, depth int) {
		if n == nil || depth > maxDepth {
			return
		}
		t := x.typ(n)
		switch t {
		case "function_item", "function_signature_item", "struct_item", "union_item", "enum_item",
			"trait_item", "impl_item", "mod_item", "macro_definition", "token_tree":
			return
		}
		if inPattern {
			switch t {
			case "identifier", "shorthand_field_identifier":
				out = append(out, strings.TrimPrefix(x.text(n), "r#"))
				return
			case "scoped_identifier", "type_identifier", "scoped_type_identifier", "generic_type":
				return
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			ch := n.Child(i)
			if !ch.IsNamed() {
				continue
			}
			f := n.FieldNameForChild(i, x.lang)
			switch {
			case inPattern && f == "type":
				// The path of a tuple struct or struct pattern.
			case f == "condition":
				visit(ch, false, depth+1) // a match guard is an expression
			case f == "pattern" || t == "closure_parameters":
				visit(ch, true, depth+1)
			case inPattern && t == "field_pattern" && f == "name":
				if x.field(n, "pattern") == nil {
					visit(ch, true, depth+1) // `Foo { a }` binds a
				}
			default:
				visit(ch, inPattern, depth+1)
			}
		}
	}
	visit(n, false, 0)
	return out
}

// call observes a call: a path's call, or a method call (whose target is
// not lexically known).
func (x *extractor) call(s *scope, n *ts.Node, c ctx, depth int) {
	fn := x.field(n, "function")
	if fn != nil && x.typ(fn) == "generic_function" {
		x.walk(s, x.field(fn, "type_arguments"), c, depth+1)
		fn = x.field(fn, "function")
	}
	switch x.typ(fn) {
	case "identifier":
		x.valueRef(s, fn, []string{x.text(fn)}, c, reference.KindCall, true)
	case "scoped_identifier":
		x.pathExpr(s, fn, c, reference.KindCall, true, depth)
	case "field_expression":
		// value.method(): which method depends on the value's type and the
		// traits in scope — observed, never given an identity.
		recv := x.field(fn, "value")
		if name := x.field(fn, "field"); name != nil {
			x.emit(language.ReferenceDraft{
				Name:         x.text(name),
				Kind:         string(reference.KindCall),
				Container:    c.container,
				Location:     x.loc(name),
				ReceiverExpr: collapse(x.text(recv)),
				IsCall:       true,
			})
		}
		x.walk(s, recv, c, depth+1)
	case "index_expression":
		// m[k](): the callee is chosen at run time.
		x.emit(language.ReferenceDraft{
			Name:      collapse(x.text(fn)),
			Kind:      string(reference.KindCall),
			Container: c.container,
			Location:  x.loc(fn),
			IsCall:    true,
			Dynamic:   true,
		})
		x.walk(s, fn, c, depth+1)
	default:
		x.walk(s, fn, c, depth+1)
	}
	x.walk(s, x.field(n, "arguments"), c, depth+1)
}

// pathExpr observes a path in an expression or pattern (a::b::c).
func (x *extractor) pathExpr(s *scope, n *ts.Node, c ctx, kind reference.ReferenceKind, isCall bool, depth int) {
	if n == nil {
		return
	}
	segs := x.pathSegments(n)
	if segs == nil {
		// A path through a generic or qualified type (<T as Tr>::f,
		// Vec::<u8>::new): observed, its types walked, no identity.
		if name := x.field(n, "name"); name != nil {
			x.emit(language.ReferenceDraft{
				Name:         x.text(name),
				Kind:         string(kind),
				Container:    c.container,
				Location:     x.loc(name),
				ReceiverExpr: collapse(x.text(x.field(n, "path"))),
				IsCall:       isCall,
			})
		}
		x.walk(s, x.field(n, "path"), c, depth+1)
		return
	}
	x.valueRef(s, n, segs, c, kind, isCall)
}

// valueRef emits the reference of a path in a value position. A plain
// name a local binding or a generic parameter shadows is not observed; an
// unresolved plain name is not observed either, unless called (it is a
// prelude or local name far more often than an item).
func (x *extractor) valueRef(s *scope, n *ts.Node, segs []string, c ctx, kind reference.ReferenceKind, isCall bool) {
	if len(segs) == 0 {
		return
	}
	if len(segs) == 1 && (c.locals[segs[0]] || c.generics[segs[0]]) {
		return
	}
	var r resolved
	if !c.generics[segs[0]] || segs[0] == "Self" {
		// T::f() of a generic parameter T: its target is the type argument's.
		r = x.resolveIn(s, segs, nsValue, c)
	}
	if !r.ok && len(segs) == 1 && !isCall {
		return
	}
	if r.ok && kind == reference.KindCall && (r.kind == symbol.KindStruct || segs[len(segs)-1] == "Self") {
		kind = reference.KindConstruction
	}
	x.emitPath(n, segs, r, c, kind, isCall, valueKinds)
}

// typeRef emits the reference of a type position: a path, its generic
// arguments walked.
func (x *extractor) typeRef(s *scope, n *ts.Node, c ctx, kind reference.ReferenceKind, depth int) {
	x.typeRefOf(s, n, c, kind, typeKinds, depth)
}

// typeRefOf is typeRef of a position that can denote kinds: a braced struct
// expression or pattern names a struct, a union or an enum variant.
func (x *extractor) typeRefOf(s *scope, n *ts.Node, c ctx, kind reference.ReferenceKind, kinds string, depth int) {
	if n == nil || x.tooDeep(n, depth) {
		return
	}
	switch x.typ(n) {
	case "generic_type", "generic_type_with_turbofish":
		x.typeRefOf(s, x.field(n, "type"), c, kind, kinds, depth+1)
		x.walk(s, x.field(n, "type_arguments"), c, depth+1)
		return
	case "type_identifier", "scoped_type_identifier", "identifier", "scoped_identifier":
	default:
		x.walk(s, n, c, depth+1)
		return
	}
	segs := x.pathSegments(n)
	if segs == nil {
		x.walk(s, x.field(n, "path"), c, depth+1)
		return
	}
	if len(segs) == 1 && c.generics[segs[0]] && segs[0] != "Self" {
		return
	}
	r := x.typePath(s, n, c)
	x.emitPath(n, segs, r, c, kind, false, kinds)
}

// typePath resolves a type path written in s, in a type position.
func (x *extractor) typePath(s *scope, n *ts.Node, c ctx) resolved {
	segs := x.pathSegments(n)
	if len(segs) == 0 || (c.generics[segs[0]] && segs[0] != "Self") {
		return resolved{}
	}
	return x.resolveIn(s, segs, nsType, c)
}

// resolveIn resolves a path written in s, Self included: in an impl, Self
// is the implemented type when a path fixes it; in a trait, Self is the
// implementing type, unknown — but Self::name of the trait's own item is
// that item.
func (x *extractor) resolveIn(s *scope, segs []string, ns namespace, c ctx) resolved {
	if segs[0] != "Self" {
		return x.resolvePath(s, segs, ns, false, 0)
	}
	ic := c.impl
	if ic == nil || !ic.self.ok {
		return resolved{}
	}
	if ic.isTrait {
		if len(segs) == 2 && ic.own[segs[1]] {
			return resolved{ident: ic.self.ident + "::" + segs[1], ok: true}
		}
		return resolved{}
	}
	if len(segs) == 1 {
		return ic.self
	}
	return resolved{ident: ic.self.ident + "::" + joinPath(segs[1:]), ok: true}
}

// emitPath appends a path's reference: with its identity when resolved.
func (x *extractor) emitPath(n *ts.Node, segs []string, r resolved, c ctx, kind reference.ReferenceKind, isCall bool, kinds string) {
	d := language.ReferenceDraft{
		Name:      segs[len(segs)-1],
		Kind:      string(kind),
		Container: c.container,
		Location:  x.loc(n),
		IsCall:    isCall,
	}
	if r.ok {
		d.NameQualified = r.ident
		d.IdentityInRepository = true
		d.TargetKinds = kinds
		if r.capped {
			d.ConfidenceCap = "candidate"
		}
	} else if len(segs) > 1 {
		d.ReceiverExpr = joinPath(segs[:len(segs)-1])
	}
	x.emit(d)
}

// emit appends a reference; one of no name (a node error recovery
// inserted) is not a reference.
func (x *extractor) emit(d language.ReferenceDraft) {
	if d.Name == "" || d.Name == "!" {
		return
	}
	x.refs = append(x.refs, d)
}

// useRefs emits an import reference per path a use declaration names.
func (x *extractor) useRefs(s *scope, n *ts.Node, c ctx) {
	var visit func(t *ts.Node, prefix []string, depth int)
	visit = func(t *ts.Node, prefix []string, depth int) {
		if t == nil || depth > 16 {
			return
		}
		switch x.typ(t) {
		case "identifier", "self", "super", "crate", "scoped_identifier", "use_as_clause", "use_wildcard":
			node := t
			var segs []string
			switch x.typ(t) {
			case "use_as_clause":
				node = x.field(t, "path")
				segs = x.pathSegments(node)
			case "use_wildcard":
				if inner := x.firstNamed(t); inner != nil {
					segs = x.pathSegments(inner)
				}
			default:
				segs = x.pathSegments(t)
			}
			path := append(append([]string(nil), prefix...), segs...)
			if len(path) > 1 && path[len(path)-1] == "self" {
				path = path[:len(path)-1]
			}
			if len(path) == 0 || (len(path) == 1 && (path[0] == "self" || path[0] == "super" || path[0] == "crate")) && x.typ(t) != "use_wildcard" {
				return
			}
			r := x.resolvePath(s, path, nsAny, true, 0)
			d := language.ReferenceDraft{
				Name:      path[len(path)-1],
				Kind:      string(reference.KindImport),
				Container: c.container,
				Location:  x.loc(node),
			}
			if r.ok {
				d.NameQualified = r.ident
				d.IdentityInRepository = true
				if r.capped {
					d.ConfidenceCap = "candidate"
				}
			} else if len(path) > 1 {
				d.ReceiverExpr = joinPath(path[:len(path)-1])
			}
			x.emit(d)
		case "scoped_use_list":
			p := append(append([]string(nil), prefix...), x.pathSegments(x.field(t, "path"))...)
			visit(x.field(t, "list"), p, depth+1)
		case "use_list":
			for _, ch := range x.named(t) {
				visit(ch, prefix, depth+1)
			}
		}
	}
	visit(x.field(n, "argument"), nil, 0)
}

// macroRef emits a macro invocation's reference. A plain name is the
// latest macro_rules! definition of this file in textual scope — unless
// something between that definition and the invocation may bring another
// (hazards), or none is in scope: then, a `use` of the scope may name it by
// path. A path names the macro by its module (#[macro_export]: the crate
// root). The invocation's token tree is not parsed.
func (x *extractor) macroRef(s *scope, n *ts.Node, c ctx) {
	m := x.field(n, "macro")
	if m == nil {
		return
	}
	segs := x.pathSegments(m)
	if len(segs) == 0 {
		return
	}
	name := segs[len(segs)-1]
	d := language.ReferenceDraft{
		Name:      name + "!",
		Kind:      string(reference.KindCall),
		Container: c.container,
		Location:  x.loc(m),
		IsCall:    true,
	}
	var r resolved
	if len(segs) == 1 {
		if def, ok := x.textualMacro(s, name, n.StartByte()); ok {
			r = resolved{ident: def.qualified, ok: true}
		} else if b := x.useOf(s, name); b != nil && len(b.path) > 1 {
			r = x.macroPath(b.scope, b.path, true)
		}
	} else if segs[0] != "Self" {
		r = x.macroPath(s, segs, false)
	}
	if r.ok {
		d.NameQualified = r.ident
		d.IdentityInRepository = true
		d.TargetKinds = string(symbol.KindMacro)
		if r.capped {
			d.ConfidenceCap = "candidate"
		}
	} else if len(segs) > 1 {
		d.ReceiverExpr = joinPath(segs[:len(segs)-1])
	}
	x.emit(d)
}

// macroPath resolves a macro's path: its module, then the macro's name.
func (x *extractor) macroPath(s *scope, segs []string, inUse bool) resolved {
	mod := x.resolvePath(s, segs[:len(segs)-1], nsType, inUse, 0)
	if !mod.ok || (mod.kind != "" && mod.kind != symbol.KindModule) {
		return resolved{}
	}
	return resolved{ident: mod.ident + "::" + segs[len(segs)-1] + "!", capped: mod.capped, ok: true}
}

// useOf is the use binding a plain name has in s or its enclosing scopes
// of the module, the first scope declaring or importing it deciding.
func (x *extractor) useOf(s *scope, name string) *useBinding {
	for sc := s; sc != nil; sc = sc.parent {
		if b, ok := sc.uses[name]; ok {
			return b
		}
		if len(sc.globs) > 0 || sc.opaque {
			return nil
		}
	}
	return nil
}

// textualMacro is the macro_rules! definition a plain invocation at offset
// at names: the latest one of this file ending before it, defined in the
// invocation's scope or an enclosing one — when nothing between them may
// bring another macro of that name into scope.
func (x *extractor) textualMacro(s *scope, name string, at uint32) (macroDef, bool) {
	var best macroDef
	found := false
	for _, d := range x.macros {
		if d.name == name && d.end <= at && encloses(d.module, s) && (!found || d.end > best.end) {
			best, found = d, true
		}
	}
	if !found {
		return macroDef{}, false
	}
	for _, d := range x.macros {
		if d.name == name && d.end > best.end && d.end <= at && d.qualified != best.qualified {
			return macroDef{}, false // a later definition outside the scope chain
		}
	}
	for _, h := range x.hazards {
		if h.at > best.end && h.at < at && (h.scope == nil || encloses(h.scope, s)) {
			return macroDef{}, false
		}
	}
	return best, true
}

// encloses reports whether scope outer is s or encloses it in this file.
func encloses(outer, s *scope) bool {
	for sc := s; sc != nil; {
		if sc == outer {
			return true
		}
		if sc.parent != nil {
			sc = sc.parent
		} else {
			sc = sc.lexical
		}
	}
	return false
}

// findHazards records the #[macro_use] attributes — a module or crate they
// mark may bring macros into textual scope after it, anywhere below — and
// the names of the file's macro_rules! definitions.
func (x *extractor) findHazards(n *ts.Node, depth int) {
	if n == nil || depth > maxDepth {
		return
	}
	switch x.typ(n) {
	case "token_tree":
		return
	case "attribute_item":
		if strings.Contains(x.text(n), "macro_use") {
			x.hazards = append(x.hazards, hazard{at: n.EndByte()})
		}
		return
	}
	if x.typ(n) == "macro_definition" {
		x.macroNames = append(x.macroNames, x.itemName(n))
		return
	}
	for _, ch := range x.named(n) {
		x.findHazards(ch, depth+1)
	}
}

// itemFreeInvocation reports an invocation of a macro_rules! macro of this
// file whose expansions are never items: a plain name in textual scope, or a
// path naming exactly one definition seen so far.
func (x *extractor) itemFreeInvocation(s *scope, m *ts.Node) bool {
	segs := x.pathSegments(x.field(m, "macro"))
	switch {
	case len(segs) == 1:
		def, ok := x.textualMacro(s, segs[0], m.StartByte())
		return ok && def.itemFree
	case len(segs) > 1:
		r := x.macroPath(s, segs, false)
		if !r.ok {
			return false
		}
		var found []macroDef
		for _, d := range x.macros {
			if d.qualified == r.ident {
				found = append(found, d)
			}
		}
		return len(found) == 1 && found[0].itemFree
	}
	return false
}

// fragmentColon is the colon of a macro fragment specifier with the space
// Rust allows around it.
var fragmentColon = regexp.MustCompile(`\s*:\s*`)

// definesMacro reports a macro_rules! definition of name anywhere in this
// file.
func (x *extractor) definesMacro(name string) bool {
	for _, n := range x.macroNames {
		if n == name {
			return true
		}
	}
	return false
}

// itemFreeRules reports a macro_rules! definition none of whose expansions
// can declare a name in the invoking scope, by its tokens alone: no item
// keyword, no fragment of kind item, stmt or tt, no `let` binding a name
// the invoker supplies, and a nested invocation only of a standard
// expression macro.
func (x *extractor) itemFreeRules(n *ts.Node) bool {
	text := x.text(n)
	if name := x.field(n, "name"); name != nil {
		text = string(x.src[name.EndByte():n.EndByte()])
	}
	// `$n: ident` is `$n:ident`: one word per fragment.
	text = fragmentColon.ReplaceAllString(text, ":")
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '_' || r == '!' || r == '$' || r == ':' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
	// macro_rules! is hygienic: a binding the rules introduce themselves is
	// not visible to the invoker; one whose name the invoker supplies
	// (`let $n = …` with $n:ident or a pattern) is.
	binds, names := false, false
	for _, w := range words {
		if strings.HasSuffix(w, ":item") || strings.HasSuffix(w, ":stmt") || strings.HasSuffix(w, ":tt") {
			return false
		}
		if strings.HasSuffix(w, ":ident") || strings.HasSuffix(w, ":pat") || strings.HasSuffix(w, ":pat_param") {
			names = true
		}
		if w == "let" {
			binds = true
			continue
		}
		if i := strings.Index(w, "!"); i >= 0 {
			// A nested invocation: only a standard expression macro, which
			// this file does not redefine.
			name := strings.TrimPrefix(w[:i], "$")
			if i != len(w)-1 || !expressionMacro(name) || x.definesMacro(name) {
				return false
			}
			continue
		}
		for _, part := range strings.Split(w, ":") {
			switch strings.TrimPrefix(part, "$") {
			case "fn", "struct", "enum", "union", "trait", "impl", "mod", "use", "static", "const", "type", "macro_rules", "extern":
				return false
			}
		}
	}
	return !(binds && names)
}
