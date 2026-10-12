package rust

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/symbol"
)

// A scope is a module or a block: the names its items and `use`
// declarations bring, visible to the whole scope whatever their order
// (Rust items and imports are not sequential). A name is looked up in the
// innermost block, then the enclosing ones, then their module — never in an
// enclosing module: a Rust module does not see its parent's names.
type scope struct {
	ident   string   // a module's identity; a block's prefix for its items
	modPath []string // a module's path below the crate root
	module  *scope   // the module this scope belongs to (itself for a module)
	parent  *scope   // the enclosing scope within the module; nil for a module
	// lexical is a module's enclosing module in this file (inline modules);
	// nil for the file's own module, whose parent is not in this file.
	lexical *scope

	items map[string][]item
	uses  map[string]*useBinding
	globs []*useBinding
	cfg   map[string]bool // names a #[cfg]-conditional item or use binds

	// opaque: an item-level macro invocation may declare names, so the
	// scope's declarations are not all known (a glob of it is not followed,
	// and a name it does not declare may still exist).
	opaque bool
	// generated: an attribute of one of the scope's items may generate
	// names (a derive or an attribute macro other than the compiler's
	// built-in ones): a glob of the scope brings unknown names.
	generated bool
	// known: identities are computed for the scope (the file has a layout).
	known bool
}

// item is one declaration a scope's name denotes.
type item struct {
	qualified string
	kind      symbol.SymbolKind
	module    *scope // the module's scope, for a module declared in this file
	vis       visibility
}

// visibility is who may name an item or a `use` binding of a module from
// outside it — what a glob of the module brings to another module.
type visibility int

const (
	visPrivate    visibility = iota // the module and its descendants only
	visPublic                       // `pub`, `pub(crate)`: the whole crate
	visRestricted                   // `pub(super)`, `pub(in path)`, 2015 `crate`: some modules
)

// useBinding is one name a `use` declaration brings into a scope.
type useBinding struct {
	path      []string // the path as written, "self" of a use list resolved
	scope     *scope   // where it is declared
	node      *ts.Node
	glob      bool
	vis       visibility
	resolving bool
	done      bool
	target    resolved
}

// resolved is a path's identity, if the language's lexical rules fix one.
type resolved struct {
	ident  string
	module *scope // the module the identity names, when declared in this file
	kind   symbol.SymbolKind
	// capped: the identity rests on a reading that depends on the crate's
	// edition (Cargo.toml): the reference is at most a Candidate.
	capped bool
	ok     bool
}

func newScope(ident string, modPath []string, parent, lexical *scope, known bool) *scope {
	s := &scope{
		ident:   ident,
		modPath: modPath,
		parent:  parent,
		lexical: lexical,
		items:   map[string][]item{},
		uses:    map[string]*useBinding{},
		cfg:     map[string]bool{},
		known:   known,
	}
	if parent == nil {
		s.module = s
	} else {
		s.module = parent.module
	}
	return s
}

func (s *scope) isModule() bool { return s.module == s }

// declare records an item of the scope.
func (s *scope) declare(name, qualified string, kind symbol.SymbolKind, module *scope, vis visibility) {
	s.items[name] = append(s.items[name], item{qualified: qualified, kind: kind, module: module, vis: vis})
}

// visibilityOf is how visible what scope sc binds to name in namespace ns
// is from outside it: ok false when it binds nothing there; restricted when
// its declarations of the name differ (#[cfg] alternatives).
func (sc *scope) visibilityOf(name string, ns namespace) (v visibility, ok bool) {
	first := true
	for _, it := range sc.items[name] {
		if !inNamespace(it.kind, ns) {
			continue
		}
		if first {
			v, first = it.vis, false
		} else if it.vis != v {
			return visRestricted, true
		}
	}
	if !first {
		return v, true
	}
	if b, ok := sc.uses[name]; ok {
		return b.vis, true
	}
	return visPrivate, false
}

// within reports whether module m is ancestor or itself an enclosing module
// of module inner in this file.
func within(inner, m *scope) bool {
	for sc := inner; sc != nil; sc = sc.lexical {
		if sc == m {
			return true
		}
	}
	return false
}

// bind records a name a `use` (or `extern crate`) brings. A name bound
// twice — #[cfg] alternatives (`#[cfg(unix)] use imp::X; #[cfg(not(unix))]
// use other::X;`), or two imports of the same thing — is ambiguous: it is
// bound to nothing, never to either path.
func (s *scope) bind(name string, b *useBinding) {
	if prev, dup := s.uses[name]; dup {
		vis := b.vis
		if prev.vis != vis {
			vis = visRestricted
		}
		// It stays a binding — of nothing — with the visibility its
		// declarations give: a glob of the scope brings the name (to resolve
		// to nothing), it does not leave it to an outer scope.
		b = &useBinding{scope: s, node: b.node, done: true, vis: vis}
	}
	s.uses[name] = b
}

// namespace selects the items a name position can denote.
type namespace int

const (
	nsType  namespace = iota // a type, trait, module (a path prefix too)
	nsValue                  // a function, constant, static, tuple struct
	nsAny                    // the last segment of an imported path
)

func inNamespace(k symbol.SymbolKind, ns namespace) bool {
	switch ns {
	case nsType:
		switch k {
		case symbol.KindModule, symbol.KindStruct, symbol.KindEnum, symbol.KindTrait, symbol.KindTypeAlias, symbol.KindType:
			return true
		}
		return false
	case nsValue:
		switch k {
		case symbol.KindFunction, symbol.KindConstant, symbol.KindVariable, symbol.KindStruct, symbol.KindMethod:
			return true
		}
		return false
	}
	return true
}

// maxResolveDepth bounds a resolution through `use` bindings and modules.
const maxResolveDepth = 32

// lookup resolves a single name in scope s and its enclosing scopes of the
// same module: a declared item, then a `use` binding, then the scope's glob
// when it is the only one and its names are all known (throughGlob). A name
// a known glob does not bring is looked up in the enclosing scope; a scope
// with another glob, or an opaque one, may bring the name: nothing is
// resolved.
func (x *extractor) lookup(s *scope, name string, ns namespace, depth int) resolved {
	for sc := s; sc != nil; sc = sc.parent {
		if r, ok := sc.local(x, name, ns, depth); ok {
			return r
		}
		if len(sc.globs) == 0 && !sc.opaque {
			continue
		}
		if len(sc.globs) == 1 && !sc.opaque {
			if sc.globs[0].resolving {
				// The glob's own path is being resolved: a glob brings no
				// name its path starts with (`use Kind::*;` finds Kind
				// outside it).
				continue
			}
			r, present, known := x.throughGlob(sc.globs[0], name, ns, depth)
			if present {
				return r
			}
			if known {
				continue
			}
		}
		return resolved{}
	}
	return resolved{}
}

// local resolves a name a scope itself declares or imports.
func (sc *scope) local(x *extractor, name string, ns namespace, depth int) (resolved, bool) {
	var found []item
	for _, it := range sc.items[name] {
		if inNamespace(it.kind, ns) {
			found = append(found, it)
		}
	}
	if len(found) > 0 {
		// Several items of one identity (cfg alternatives) are one identity;
		// items of several identities of one name in one namespace do not
		// compile, and are left unresolved.
		first := found[0]
		for _, it := range found[1:] {
			if it.qualified != first.qualified {
				return resolved{}, true
			}
		}
		if _, imported := sc.uses[name]; imported && sc.cfg[name] || sc.cfg[name] && len(sc.globs) > 0 {
			// `#[cfg(a)] mod imp; #[cfg(b)] use other as imp;` — or
			// `#[cfg(b)] use other::*;`: which one the name is depends on
			// the configuration.
			return resolved{}, true
		}
		module := first.module
		if len(found) > 1 && first.kind == symbol.KindModule {
			// #[cfg] alternatives of one module: its identity is fixed,
			// what a path through it reaches is not.
			module = ambiguousModule(first.qualified)
		}
		return resolved{ident: first.qualified, module: module, kind: first.kind, ok: sc.known}, true
	}
	if b, ok := sc.uses[name]; ok {
		return x.resolveUse(b, depth), true
	}
	return resolved{}, false
}

// throughGlob looks name up among what a glob import brings. known: the
// glob's names are all known — those of a module of this file with no
// item-level macro and no glob (its items and `use` bindings), or the
// variants of an enum declared once in this file — so present tells whether
// it brings name. A glob of anything else brings unknown names.
func (x *extractor) throughGlob(g *useBinding, name string, ns namespace, depth int) (r resolved, present, known bool) {
	target := x.resolveUse(g, depth)
	if !target.ok {
		return resolved{}, false, false
	}
	if m := target.module; m != nil {
		if m.opaque || len(m.globs) > 0 || m.generated {
			return resolved{}, false, false
		}
		if !within(g.scope.module, m) {
			// From outside m, a glob brings only what m makes visible there:
			// its public names; a private one is not brought (the name is
			// looked up further out — the prelude's, for one), a restricted
			// one may or may not be.
			switch v, ok := m.visibilityOf(name, ns); {
			case !ok || v == visPrivate:
				return resolved{}, false, true
			case v == visRestricted:
				return resolved{}, false, false
			}
		}
		r, ok := m.local(x, name, ns, depth+1)
		if !ok {
			return resolved{}, false, true
		}
		r.capped = r.capped || target.capped
		return r, true, true
	}
	if e := x.enums[target.ident]; e != nil && e.declarations == 1 && target.kind == symbol.KindEnum {
		if !e.variants[name] {
			return resolved{}, false, true
		}
		return resolved{ident: target.ident + "::" + name, kind: symbol.KindConstant, capped: target.capped, ok: true}, true, true
	}
	return resolved{}, false, false
}

// resolveUse resolves a `use` binding's path, once.
func (x *extractor) resolveUse(b *useBinding, depth int) resolved {
	if b.done {
		return b.target
	}
	if b.resolving || depth > maxResolveDepth {
		return resolved{} // a cycle of imports: nothing is fixed
	}
	b.resolving = true
	r := x.resolvePath(b.scope, b.path, nsAny, true, depth+1)
	b.resolving = false
	b.done, b.target = true, r
	return r
}

// resolvePath resolves a path written in scope s. inUse: a `use` path,
// whose first plain segment reads differently by edition outside the crate
// root.
func (x *extractor) resolvePath(s *scope, segs []string, ns namespace, inUse bool, depth int) resolved {
	if len(segs) == 0 || !s.known || depth > maxResolveDepth {
		return resolved{}
	}
	mod := s.module
	var cur resolved
	rest := segs
	switch first := segs[0]; first {
	case "crate":
		cur = x.moduleAt(nil)
		rest = segs[1:]
	case "self":
		cur = x.moduleOf(mod)
		rest = segs[1:]
	case "super":
		path := mod.modPath
		for len(rest) > 0 && rest[0] == "super" {
			if len(path) == 0 {
				return resolved{} // above the crate root
			}
			path = path[:len(path)-1]
			rest = rest[1:]
		}
		cur = x.moduleAt(path)
	case "Self":
		return resolved{} // resolved by the caller, which knows the impl
	default:
		pos := nsType
		if len(segs) == 1 {
			pos = ns
		}
		cur = x.lookup(s, first, pos, depth)
		if !cur.ok {
			if inUse && len(mod.modPath) > 0 && !externPrelude(first) {
				// Rust 2018 reads the path from this scope (where nothing
				// names it: another crate's), Rust 2015 from the crate root;
				// the edition is in Cargo.toml. The 2015 reading is a
				// Candidate.
				r := x.resolvePath(s, append([]string{"crate"}, segs...), ns, false, depth+1)
				r.capped = true
				return r
			}
			return resolved{}
		}
		if inUse && (len(mod.modPath) > 0 || !s.isModule()) {
			// Rust 2015 reads it from the crate root. In a block of the crate
			// root's module the two readings agree when the name is the
			// module's, not the block's.
			if alt := x.lookup(mod, first, pos, depth); len(mod.modPath) > 0 || !alt.ok || alt.ident != cur.ident {
				cur.capped = true
			}
		}
		rest = segs[1:]
	}
	if !cur.ok {
		return resolved{}
	}
	for i, seg := range rest {
		if seg == "self" || seg == "super" || seg == "crate" || seg == "Self" {
			return resolved{} // valid only first (or as a use list's self)
		}
		segNS := nsType
		if i == len(rest)-1 {
			segNS = ns
		}
		if cur.module != nil {
			// A module of this file: its own declarations and imports name
			// the segment (an import is followed). A name it neither
			// declares nor imports is no declaration of the repository,
			// unless a glob or a macro of the module may bring it.
			if r, ok := cur.module.local(x, seg, segNS, depth+1); ok {
				if !r.ok {
					return resolved{}
				}
				r.capped = r.capped || cur.capped
				cur = r
				continue
			}
			if len(cur.module.globs) > 0 || cur.module.opaque {
				return resolved{}
			}
		}
		cur = resolved{ident: cur.ident + "::" + seg, capped: cur.capped, ok: true}
	}
	return cur
}

// ambiguousModule stands for a module of several #[cfg] alternatives: a
// path through it resolves nothing (it declares nothing and is opaque).
func ambiguousModule(ident string) *scope {
	s := newScope(ident, nil, nil, nil, true)
	s.opaque = true
	return s
}

// externPrelude reports a crate both editions put in every crate's scope:
// a `use` path through it names no item of the crate root.
func externPrelude(name string) bool {
	switch name {
	case "std", "core", "alloc", "proc_macro", "test":
		return true
	}
	return false
}

// moduleAt is the module at path below the crate root.
func (x *extractor) moduleAt(path []string) resolved {
	ident := modulePath(x.lay.crate, path)
	return resolved{ident: ident, module: x.modules[ident], kind: symbol.KindModule, ok: x.lay.ok}
}

// moduleOf is module s itself.
func (x *extractor) moduleOf(s *scope) resolved {
	return resolved{ident: s.ident, module: s, kind: symbol.KindModule, ok: s.known}
}

// joinPath is a path's segments as written.
func joinPath(segs []string) string { return strings.Join(segs, "::") }
