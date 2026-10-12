package rust

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// items emits the declarations of a module or block body and walks
// everything else in it. The body's names were collected (collect).
func (x *extractor) items(s *scope, body *ts.Node, c ctx, depth int) {
	if body == nil || x.tooDeep(body, depth) {
		return
	}
	var attrs []string
	for i := 0; i < body.ChildCount(); i++ {
		n := body.Child(i)
		if !n.IsNamed() {
			continue
		}
		t := x.typ(n)
		if t == "attribute_item" {
			attrs = append(attrs, collapse(x.text(n)))
			continue
		}
		if t == "inner_attribute_item" || t == "line_comment" || t == "block_comment" {
			continue
		}
		x.item(s, n, c, attrs, depth)
		attrs = nil
	}
}

// item emits one declaration (or walks one statement).
func (x *extractor) item(s *scope, n *ts.Node, c ctx, attrs []string, depth int) {
	name := x.itemName(n)
	switch x.typ(n) {
	case "mod_item":
		if name == "" {
			return
		}
		ident := qualify(s, name)
		x.declareSymbol(n, name, ident, symbol.KindModule, attrs, "", "")
		if b := x.field(n, "body"); b != nil {
			child := x.bodies[b.StartByte()]
			if child == nil || !s.isModule() {
				// A module declared in a block: its items are named by no
				// path of the crate.
				child = newScope(ident, s.module.modPath, nil, nil, false)
				x.collect(child, b, depth+1)
			}
			x.items(child, b, ctx{container: ident}, depth+1)
		}
	case "function_item", "function_signature_item":
		if name == "" {
			return
		}
		x.function(s, n, c, attrs, name, qualify(s, name), symbol.KindFunction, "", "", depth)
	case "struct_item", "union_item":
		if name == "" {
			return
		}
		ident := qualify(s, name)
		x.declareSymbol(n, name, ident, symbol.KindStruct, attrs, "", "")
		g := c.generics.with(x.genericNames(x.field(n, "type_parameters"))...)
		ic := ctx{container: ident, generics: g}
		x.fields(s, x.field(n, "body"), name, ident, ic, depth)
		x.walkChildrenExcept(s, n, ic, depth, "name", "body")
	case "enum_item":
		if name == "" {
			return
		}
		ident := qualify(s, name)
		x.declareSymbol(n, name, ident, symbol.KindEnum, attrs, "", "")
		g := c.generics.with(x.genericNames(x.field(n, "type_parameters"))...)
		ic := ctx{container: ident, generics: g}
		if list := x.field(n, "body"); list != nil {
			var vattrs []string
			for _, v := range x.named(list) {
				switch x.typ(v) {
				case "attribute_item":
					vattrs = append(vattrs, collapse(x.text(v)))
					continue
				case "line_comment", "block_comment":
					continue
				case "enum_variant":
					if vn := x.itemName(v); vn != "" {
						x.declareSymbol(v, vn, ident+"::"+vn, symbol.KindConstant, vattrs, name, ident)
						x.walkChildrenExcept(s, v, ic, depth+1, "name")
					}
				}
				vattrs = nil
			}
		}
		x.walkChildrenExcept(s, n, ic, depth, "name", "body")
	case "trait_item":
		if name == "" {
			return
		}
		x.trait(s, n, c, attrs, name, depth)
	case "impl_item":
		x.impl(s, n, c, attrs, depth)
	case "type_item":
		if name == "" {
			return
		}
		ident := qualify(s, name)
		x.declareSymbol(n, name, ident, symbol.KindTypeAlias, attrs, "", "")
		g := c.generics.with(x.genericNames(x.field(n, "type_parameters"))...)
		x.walkChildrenExcept(s, n, ctx{container: ident, generics: g}, depth, "name")
	case "const_item", "static_item":
		if name == "" {
			return
		}
		kind := symbol.KindConstant
		if x.typ(n) == "static_item" {
			kind = symbol.KindVariable
		}
		ident := qualify(s, name)
		x.declareSymbol(n, name, ident, kind, attrs, "", "")
		x.walkChildrenExcept(s, n, ctx{container: ident, generics: c.generics, impl: c.impl, locals: c.locals.with(x.bindings(n)...)}, depth, "name")
	case "macro_definition":
		if name == "" {
			return
		}
		ident := s.ident + "::" + name + "!"
		if hasAttr(attrs, "macro_export") && x.lay.ok {
			ident = x.lay.crate + "::" + name + "!" // exported at the crate root
		}
		x.declareSymbol(n, name, ident, symbol.KindMacro, attrs, "", "")
		x.macros = append(x.macros, macroDef{name: name, qualified: ident, end: n.EndByte(), module: s, itemFree: x.itemFreeRules(n)})
	case "foreign_mod_item":
		x.items(s, x.field(n, "body"), c, depth+1)
	case "use_declaration":
		x.useRefs(s, n, c)
	case "extern_crate_declaration":
		// Another package's crate: nothing of the repository is named.
	default:
		x.walk(s, n, c, depth+1)
	}
}

// function emits a function or method and walks its signature and body.
func (x *extractor) function(s *scope, n *ts.Node, c ctx, attrs []string, name, ident string, kind symbol.SymbolKind, receiver, parent string, depth int) {
	x.declareSymbol(n, name, ident, kind, attrs, receiver, parent)
	g := c.generics.with(x.genericNames(x.field(n, "type_parameters"))...)
	body := x.field(n, "body")
	locals := c.locals.with(x.bindings(x.field(n, "parameters"))...)
	locals = locals.with(x.bindings(body)...)
	fc := ctx{container: ident, impl: c.impl, locals: locals, generics: g}
	x.walkChildrenExcept(s, n, fc, depth, "name", "body")
	if body != nil {
		x.block(s, body, ident, fc, depth+1)
	}
}

// block walks a block in a new block scope: its items, then the rest.
func (x *extractor) block(s *scope, b *ts.Node, owner string, c ctx, depth int) {
	bs := newScope(owner+"::{block}", s.module.modPath, s, nil, s.known)
	x.collect(bs, b, depth+1)
	x.items(bs, b, c, depth+1)
}

// trait emits a trait and its items: TRAIT::name.
func (x *extractor) trait(s *scope, n *ts.Node, c ctx, attrs []string, name string, depth int) {
	ident := qualify(s, name)
	x.declareSymbol(n, name, ident, symbol.KindTrait, attrs, "", "")
	g := c.generics.with(x.genericNames(x.field(n, "type_parameters"))...)
	body := x.field(n, "body")
	own := map[string]bool{}
	if body != nil {
		for _, ch := range x.named(body) {
			if in := x.itemName(ch); in != "" {
				own[in] = true
			}
		}
	}
	ic := &implCtx{trait: ident, isTrait: true, prefix: ident + "::", own: own}
	if s.known {
		ic.self = resolved{ident: ident, kind: symbol.KindTrait, ok: true}
	}
	tc := ctx{container: ident, impl: ic, generics: g.with("Self")}
	x.walkChildrenExcept(s, n, tc, depth, "name", "body")
	x.assocItems(s, body, name, ident, ic, tc, depth)
}

// impl emits an impl block and its items. An inherent impl's items are
// TYPE::name, a trait implementation's <TYPE as TRAIT>::name; a TYPE or
// TRAIT no path fixes is written as its text under the impl's scope, so
// no path reaches its items.
func (x *extractor) impl(s *scope, n *ts.Node, c ctx, attrs []string, depth int) {
	g := c.generics.with(x.genericNames(x.field(n, "type_parameters"))...)
	typeNode, traitNode := x.field(n, "type"), x.field(n, "trait")
	gc := ctx{container: c.container, generics: g, locals: c.locals}
	selfRes := x.typePath(s, typeNode, gc)
	selfIdent := "{" + s.ident + "}" + collapse(x.text(typeNode))
	if selfRes.ok && !selfRes.capped {
		selfIdent = selfRes.ident
	}
	ident := "impl " + selfIdent
	prefix := selfIdent + "::"
	name := "impl " + collapse(x.text(typeNode))
	var traitRes resolved
	traitIdent := ""
	if traitNode != nil {
		traitRes = x.typePath(s, traitNode, gc)
		traitIdent = "{" + s.ident + "}" + collapse(x.text(traitNode))
		if traitRes.ok && !traitRes.capped {
			traitIdent = traitRes.ident
		}
		ident = "<" + selfIdent + " as " + traitIdent + ">"
		prefix = ident + "::"
		name = "impl " + collapse(x.text(traitNode)) + " for " + collapse(x.text(typeNode))
	}
	x.declareSymbol(n, name, ident, symbol.KindImpl, attrs, "", "")
	hc := ctx{container: ident, generics: g, locals: c.locals}
	if traitNode != nil {
		x.typeRef(s, traitNode, hc, reference.KindImplements, depth+1)
	}
	if typeNode != nil {
		x.walk(s, typeNode, hc, depth+1)
	}
	x.walk(s, x.field(n, "type_parameters"), hc, depth+1)
	for _, w := range x.named(n) {
		if x.typ(w) == "where_clause" {
			x.walk(s, w, hc, depth+1)
		}
	}
	ic := &implCtx{trait: traitIdent, prefix: prefix}
	if selfRes.ok && !selfRes.capped {
		ic.self = selfRes
	}
	x.assocItems(s, x.field(n, "body"), baseTypeName(x, typeNode), ident, ic, ctx{container: ident, impl: ic, generics: g}, depth)
}

// assocItems emits the items of a trait or impl body.
func (x *extractor) assocItems(s *scope, body *ts.Node, receiver, parent string, ic *implCtx, c ctx, depth int) {
	if body == nil || x.tooDeep(body, depth) {
		return
	}
	var attrs []string
	for _, n := range x.named(body) {
		t := x.typ(n)
		if t == "attribute_item" {
			attrs = append(attrs, collapse(x.text(n)))
			continue
		}
		if t == "line_comment" || t == "block_comment" {
			continue
		}
		name := x.itemName(n)
		switch t {
		case "function_item", "function_signature_item":
			if name != "" {
				x.function(s, n, c, attrs, name, ic.prefix+name, symbol.KindMethod, receiver, parent, depth+1)
			}
		case "const_item":
			if name != "" {
				ident := ic.prefix + name
				x.declareSymbol(n, name, ident, symbol.KindConstant, attrs, receiver, parent)
				x.walkChildrenExcept(s, n, ctx{container: ident, impl: ic, generics: c.generics, locals: c.locals.with(x.bindings(n)...)}, depth+1, "name")
			}
		case "type_item", "associated_type":
			if name != "" {
				ident := ic.prefix + name
				x.declareSymbol(n, name, ident, symbol.KindTypeAlias, attrs, receiver, parent)
				x.walkChildrenExcept(s, n, ctx{container: ident, impl: ic, generics: c.generics}, depth+1, "name")
			}
		case "macro_invocation":
			x.macroRef(s, n, c)
		}
		attrs = nil
	}
}

// fields emits a struct's or union's named fields: TYPE.field.
func (x *extractor) fields(s *scope, body *ts.Node, owner, ident string, c ctx, depth int) {
	if body == nil || x.typ(body) != "field_declaration_list" {
		if body != nil {
			x.walk(s, body, c, depth+1)
		}
		return
	}
	for _, f := range x.named(body) {
		if x.typ(f) != "field_declaration" {
			continue
		}
		if fn := x.itemName(f); fn != "" {
			x.declareSymbol(f, fn, ident+"."+fn, symbol.KindField, nil, owner, ident)
		}
		x.walk(s, x.field(f, "type"), c, depth+1)
	}
}

// declareSymbol appends one declaration.
func (x *extractor) declareSymbol(n *ts.Node, name, ident string, kind symbol.SymbolKind, attrs []string, receiver, parent string) {
	exported := false
	for _, v := range x.named(n) {
		if x.typ(v) == "visibility_modifier" && x.text(v) == "pub" {
			exported = true
		}
	}
	x.symbols = append(x.symbols, language.SymbolDraft{
		Name:      name,
		Qualified: ident,
		Kind:      kind,
		Location:  x.loc(n),
		StartByte: n.StartByte(),
		EndByte:   n.EndByte(),
		Parent:    parent,
		Receiver:  receiver,
		Signature: x.signature(n, attrs),
		Exported:  exported,
	})
}

// signature is a declaration's header — its text before its body —
// preceded by its attributes (a #[cfg] makes it conditional).
func (x *extractor) signature(n *ts.Node, attrs []string) string {
	end := n.EndByte()
	for _, f := range []string{"body", "value"} {
		if b := x.field(n, f); b != nil && b.StartByte() > n.StartByte() {
			end = b.StartByte()
			break
		}
	}
	head := collapse(string(x.src[n.StartByte():end]))
	head = strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(head, "=")), "{")
	head = strings.TrimSpace(head)
	if len(attrs) > 0 {
		head = strings.Join(attrs, " ") + " " + head
	}
	const limit = 400
	if len(head) > limit {
		head = head[:limit] + "…"
	}
	return head
}

// collapse folds whitespace runs to one space.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// hasAttr reports an attribute named name among attrs (#[name], #[name(...)]).
func hasAttr(attrs []string, name string) bool {
	for _, a := range attrs {
		a = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(a, "#["), "]"))
		if a == name || strings.HasPrefix(a, name+"(") || strings.HasPrefix(a, name+" ") {
			return true
		}
	}
	return false
}

// genericNames returns the type and const parameter names of a
// type_parameters node (lifetimes are not type names).
func (x *extractor) genericNames(tp *ts.Node) []string {
	if tp == nil {
		return nil
	}
	var out []string
	for _, p := range x.named(tp) {
		switch x.typ(p) {
		case "type_parameter", "const_parameter", "constrained_type_parameter", "optional_type_parameter":
			if nm := x.field(p, "name"); nm != nil {
				out = append(out, x.text(nm))
			} else if left := x.field(p, "left"); left != nil {
				out = append(out, x.text(left))
			}
		case "type_identifier":
			out = append(out, x.text(p))
		}
	}
	return out
}

// baseTypeName is the name of an impl's type as written, without path or
// generic arguments ("Repository" of `storage::Repository<T>`).
func baseTypeName(x *extractor, t *ts.Node) string {
	segs := x.pathSegments(t)
	if len(segs) == 0 {
		return collapse(x.text(t))
	}
	return segs[len(segs)-1]
}
