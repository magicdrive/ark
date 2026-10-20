package python

import (
	"sort"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A decorator binds the decorated name to the value it returns. The
// declaration exists whatever the decorator does — it is a symbol, and the
// calls its body makes are its edges — but what the name denotes at run time
// is the decorator's result. Only the decorators below are known to leave it
// the declaration, by their documented semantics:
//
//   - the same object: abc.abstractmethod, typing.override / final /
//     runtime_checkable / no_type_check, functools.wraps(f) (applied to a
//     wrapper, it returns that wrapper), functools.total_ordering,
//     dataclasses.dataclass (slots=True rebuilds the class from the same
//     body and members), enum.unique;
//   - a descriptor or wrapper whose call or access runs the declaration's
//     body: staticmethod, classmethod, property, functools.cache /
//     lru_cache / cached_property;
//   - typing.overload: a signature the implementation after it replaces (no
//     declaration at all).
//
// Any other one may return anything — another function, another class, an
// object of its own — so the declaration states SymbolDraft.ConfidenceCap
// "candidate": a reference to the name is never resolved to it Strong or
// Exact, and forms no edge (ARCHITECTURE.md §2: Strong is one target by weaker
// evidence; here the target itself is unknown). Stacked decorators are known
// only when every one is. A decorator is recognised by what its name is bound
// to, never by its spelling alone: a builtin only where nothing in the file
// rebinds it, a library function only through an import of its module in
// the file.

// decoratorForm is how a known decorator may be written: applied itself
// (@d), and/or called with arguments that only configure it (@d(...): the
// call's result is the decorator — wraps(f), lru_cache(maxsize=1),
// dataclass(frozen=True)); classOnly where its result is only what it is
// documented to be in a class body (property, classmethod, staticmethod,
// cached_property: descriptors).
type decoratorForm struct {
	bare, called, classOnly bool
	// keywordsOnly: a call is the factory form only without positional
	// arguments (lru_cache(maxsize=1); lru_cache(f) decorates f itself, and
	// the result is applied to the def), save an integer or None
	// (lru_cache(128)).
	keywordsOnly bool
}

var (
	plain     = decoratorForm{bare: true}
	inClass   = decoratorForm{bare: true, classOnly: true}
	factory   = decoratorForm{called: true}
	bareOrKws = decoratorForm{bare: true, called: true, keywordsOnly: true}
)

// knownDecorators maps a module to the decorators of it that are known.
var knownDecorators = map[string]map[string]decoratorForm{
	"functools":         {"wraps": factory, "cache": plain, "lru_cache": bareOrKws, "cached_property": inClass, "total_ordering": plain},
	"abc":               {"abstractmethod": plain},
	"typing":            {"override": plain, "final": plain, "runtime_checkable": plain, "no_type_check": plain, "overload": plain},
	"typing_extensions": {"override": plain, "final": plain, "runtime_checkable": plain, "no_type_check": plain, "overload": plain},
	"dataclasses":       {"dataclass": bareOrKws},
	"enum":              {"unique": plain},
}

// builtinDecorators are the builtins that are known decorators.
var builtinDecorators = map[string]decoratorForm{"staticmethod": inClass, "classmethod": inClass, "property": inClass}

// definition is one def or class, for applyDecorators: its symbol (-1 when
// it states none), its body's scope, the scope it is defined in and its
// decorators.
type definition struct {
	sym        int
	name       string
	isClass    bool
	body       *scope
	sc         *scope
	decorators []*ts.Node
}

// decoratorCallee is the expression a decorator applies: d of @d and of
// @d(...). The second result is whether the decorator calls it first.
func (e *extractor) decoratorCallee(d *ts.Node) (*ts.Node, bool) {
	if d.NamedChildCount() == 0 {
		return nil, false
	}
	expr := d.NamedChild(0)
	if e.typ(expr) == "call" {
		return e.field(expr, "function"), true
	}
	return expr, false
}

// dotted is the text of an identifier or of a chain of attributes of
// identifiers (a.b.c), "" for any other expression.
func (e *extractor) dotted(n *ts.Node) string {
	if n == nil {
		return ""
	}
	switch e.typ(n) {
	case "identifier":
		return e.text(n)
	case "attribute":
		obj := e.dotted(e.field(n, "object"))
		attr := e.field(n, "attribute")
		if obj == "" || attr == nil {
			return ""
		}
		return obj + "." + e.text(attr)
	}
	return ""
}

// isAccessorOf reports a decorator written as a property accessor of the
// def's own name (@x.setter, .getter, .deleter on def x).
func (e *extractor) isAccessorOf(d *ts.Node, name string) bool {
	callee, called := e.decoratorCallee(d)
	dn := e.dotted(callee)
	return !called && name != "" && (dn == name+".setter" || dn == name+".getter" || dn == name+".deleter")
}

// decoratorKind classifies one decorator: known, and which one (a builtin's
// name or the library function's: "overload" for typing's). sc is the scope
// the decorated definition is in.
func (e *extractor) decoratorKind(d *ts.Node, sc *scope) (known bool, builtin string) {
	callee, called := e.decoratorCallee(d)
	dn := e.dotted(callee)
	if dn == "" {
		return false, ""
	}
	fits := func(f decoratorForm) bool {
		if f.classOnly && sc.kind != scopeClass {
			return false
		}
		if !called {
			return f.bare
		}
		return f.called && (!f.keywordsOnly || e.configuresOnly(d))
	}
	parts := strings.Split(dn, ".")
	if len(parts) == 1 {
		name := parts[0]
		// A builtin: nothing in the file binds the name, and no star import
		// may (star imports bind names no one lists).
		if f, ok := builtinDecorators[name]; ok && e.lookup(sc, name) == nil && !e.hasStar && !e.builtinAssigned(name) && fits(f) {
			return true, name
		}
		// from M import name
		if mod, imported, ok := e.importedName(sc, name); ok && imported != "" && !e.moduleAttributeAssigned(mod, imported) {
			if f, ok := knownDecorators[mod][imported]; ok && fits(f) {
				return true, imported
			}
		}
		return false, ""
	}
	if len(parts) == 2 && !e.assignedAttribute(sc, parts[0], parts[1]) {
		// M.name, M bound by import M (or import M as alias), and the file
		// assigns no M.name of its own (functools.cache = …)
		if mod, imported, ok := e.importedName(sc, parts[0]); ok && imported == "" {
			if f, ok := knownDecorators[mod][parts[1]]; ok && fits(f) {
				return true, parts[1]
			}
		}
	}
	return false, ""
}

// configuresOnly reports whether a called decorator @d(...) passes no
// positional argument but an integer or None: lru_cache(maxsize=1),
// lru_cache(128), dataclass(frozen=True) configure the decorator; d(f)
// would apply it to f and decorate the def with the result.
func (e *extractor) configuresOnly(d *ts.Node) bool {
	if d.NamedChildCount() == 0 {
		return false
	}
	args := e.field(d.NamedChild(0), "arguments")
	if args == nil {
		return false
	}
	literal := func(n *ts.Node) bool {
		switch e.typ(n) {
		case "integer", "none", "true", "false", "string":
			return true
		}
		return false
	}
	for i := range args.NamedChildCount() {
		a := args.NamedChild(i)
		switch {
		case e.typ(a) == "comment", e.typ(a) == "integer", e.typ(a) == "none":
		case e.typ(a) == "keyword_argument" && e.field(a, "value") != nil && literal(e.field(a, "value")):
			// lru_cache(maxsize=f) treats a callable maxsize as the function
			// to decorate: only a literal configures
		default:
			return false
		}
	}
	return true
}

// moduleAttributeAssigned reports whether the file assigns attr of the module
// mod through any import of it (functools.cache = …): a from-import of the
// name may then read the assigned value, wherever the assignment is.
func (e *extractor) moduleAttributeAssigned(mod, attr string) bool {
	for _, a := range e.attrAssigns {
		if m, i, ok := e.importedName(a.sc, a.obj); ok && i == "" && m == mod && a.attr == attr {
			return true
		}
	}
	return false
}

// builtinAssigned reports whether the file assigns the builtin name through
// an import of the builtins module (builtins.staticmethod = …).
func (e *extractor) builtinAssigned(name string) bool {
	for _, a := range e.attrAssigns {
		if m, i, ok := e.importedName(a.sc, a.obj); ok && i == "" && m == "builtins" && a.attr == name {
			return true
		}
	}
	return false
}

// assignedAttribute reports whether the file assigns obj.attr where obj
// names what a reference at sc reads as obj (functools.cache = replace) —
// through any import of the same module (import functools as ft).
func (e *extractor) assignedAttribute(sc *scope, obj, attr string) bool {
	b := e.lookup(sc, obj)
	mod, imported, isImport := e.importedName(sc, obj)
	for _, a := range e.attrAssigns {
		if a.attr != attr {
			continue
		}
		if a.obj == obj && e.lookup(a.sc, obj) == b {
			return true
		}
		if m, i, ok := e.importedName(a.sc, a.obj); isImport && ok && imported == "" && i == "" && m == mod {
			return true
		}
	}
	return false
}

// importedName reports the module-level import that binds name where sc
// reads it, when that is the name's only binding: the module as written and
// the imported name ("" for import M).
func (e *extractor) importedName(sc *scope, name string) (module, imported string, ok bool) {
	if e.lookup(sc, name) != e.mod {
		return "", "", false
	}
	b := e.mod.binds[name]
	if b == nil || b.imports != 1 || b.symbols()+b.other > 0 || (e.hasStar && b.last < e.lastStar) {
		return "", "", false // rebound, or a later star import may rebind it
	}
	for _, im := range e.moduleImports {
		if im.local == name && im.scope == "" {
			return im.module, im.imported, true
		}
	}
	return "", "", false
}

// applyDecorators decides, once every binding is known, what the decorators
// of each definition change: an unknown one caps the declaration, @property
// makes a method a property, and a function in a class body has a self (or
// cls) unless an unknown decorator or @staticmethod wraps it.
//
// A typing overload is a signature the implementation after it replaces at
// run time: it states no declaration and binds nothing. A function whose
// name the class body binds in another way too (f = staticmethod(f), an
// import, a loop) has no proven self: the name's value is not the function.
func (e *extractor) applyDecorators() {
	// properties of each class body so far, in source order: the names an
	// accessor (@x.setter def x) may extend
	properties := map[*scope]map[string]bool{}
	for _, d := range e.defs {
		unknown, static, property, overload, accessor := false, false, false, false, false
		for _, dec := range d.decorators {
			if e.isAccessorOf(dec, d.name) {
				// @x.setter: the property x of the same class body, declared
				// before (x.setter is then property.setter); anything else
				// is an unknown decorator.
				if b := d.sc.binds[d.name]; d.sc.kind == scopeClass && properties[d.sc][d.name] &&
					b != nil && b.vars+b.imports+b.other+b.params == 0 {
					accessor = true
					continue
				}
				unknown = true
				continue
			}
			known, which := e.decoratorKind(dec, d.sc)
			switch {
			case !known:
				unknown = true
			case which == "staticmethod":
				static = true
			case which == "property":
				property = true
			case which == "overload":
				overload = true
			}
		}
		if d.sym >= 0 {
			s := &e.symbols[d.sym]
			if unknown {
				s.ConfidenceCap = capCandidate
			}
			if property && s.Kind == symbol.KindMethod {
				s.Kind = symbol.KindProperty
			}
			if (overload || accessor) && !unknown {
				e.dropped[d.sym] = true
				e.unbind(d.sc, d.name, d.isClass)
			}
			if property && !unknown && d.sc.kind == scopeClass {
				if properties[d.sc] == nil {
					properties[d.sc] = map[string]bool{}
				}
				properties[d.sc][d.name] = true
			}
		}
		b := d.sc.binds[d.name]
		rebound := b == nil || b.vars+b.imports+b.other+b.params > 0 || d.sc.globals[d.name]
		d.body.static = static
		if fn := d.body; fn.inClass != nil && fn.firstParam != "" && !unknown && !static && !overload && !rebound {
			fn.selfName = fn.firstParam
			fn.selfClass = fn.inClass
		}
	}
}

// unbind takes back one declaration binding of name in sc (a typing
// overload's), leaving no binding where it was the only one.
func (e *extractor) unbind(sc *scope, name string, isClass bool) {
	b := sc.binds[name]
	if b == nil || b.decl == 0 {
		return
	}
	b.decl--
	if isClass {
		b.classes--
	}
	if b.decl+b.vars+b.imports+b.params+b.other == 0 {
		delete(sc.binds, name)
	}
}

// memberCaps caps the members a lookup through their class may reach
// although the class body does not leave them bound to the declaration: a
// name the class body also binds by an import, a loop, del or the like, or
// declares global (its def is the module's then); a member the file assigns
// through the class (C.x = …, monkey patching); and every member of a class
// under an unknown decorator (the class's name is bound to the decorator's
// result, whose members are not proven to be the class's) — the class's own
// cap, nested classes included.
func (e *extractor) memberCaps() {
	byQual := map[string][]int{}
	for i, s := range e.symbols {
		byQual[s.Qualified] = append(byQual[s.Qualified], i)
	}
	capMembers := func(classQual, name, limit string) {
		for _, i := range byQual[classQual+"."+name] {
			if s := &e.symbols[i]; s.Parent == classQual && s.Receiver != "" {
				s.ConfidenceCap = lowerCap(s.ConfidenceCap, limit)
			}
		}
	}
	for _, d := range e.defs {
		if !d.isClass || d.name == "" {
			continue
		}
		cs := d.body
		for name, b := range cs.binds {
			if b.imports+b.other > 0 {
				capMembers(cs.qual, name, capCandidate)
			}
		}
		for name := range cs.globals {
			capMembers(cs.qual, name, capCandidate)
		}
	}
	for _, a := range e.attrAssigns {
		b := e.lookup(a.sc, a.obj)
		if b == nil || b.binds[a.obj] == nil || b.binds[a.obj].classes == 0 {
			continue
		}
		cq := qualify(b, a.obj)
		capMembers(cq, a.attr, capCandidate)
		// A class that declares no member of the name gets one, so that a
		// lookup stops there instead of reaching a base's (D.helper = None
		// hides C.helper of D's base C).
		if len(byQual[cq+"."+a.attr]) == 0 && len(byQual[cq]) > 0 {
			e.symbols = append(e.symbols, language.SymbolDraft{
				Name: a.attr, Qualified: cq + "." + a.attr, Kind: symbol.KindField,
				Location: nodeLocation(a.at, e.file), StartByte: a.at.StartByte(), EndByte: a.at.EndByte(),
				Exported: !isPrivate(a.attr), Parent: cq, Receiver: a.obj, Visibility: memberVisibility(a.attr),
				ConfidenceCap: capCandidate,
			})
			byQual[cq+"."+a.attr] = append(byQual[cq+"."+a.attr], len(e.symbols)-1)
		}
	}
	// A class's cap reaches its members, outer classes first.
	idx := make([]int, len(e.symbols))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return strings.Count(e.symbols[idx[a]].Qualified, ".") < strings.Count(e.symbols[idx[b]].Qualified, ".")
	})
	for _, i := range idx {
		s := &e.symbols[i]
		if s.Receiver == "" || s.Parent == "" {
			continue
		}
		for _, p := range byQual[s.Parent] {
			if ps := e.symbols[p]; ps.Kind == symbol.KindClass && ps.ConfidenceCap != "" {
				s.ConfidenceCap = lowerCap(s.ConfidenceCap, ps.ConfidenceCap)
			}
		}
	}
}

// lowerCap is the lower of two caps ("" none, "strong", "candidate").
func lowerCap(a, b string) string {
	switch {
	case a == capCandidate || b == capCandidate:
		return capCandidate
	case a == "strong" || b == "strong":
		return "strong"
	}
	return ""
}
