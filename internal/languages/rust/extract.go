package rust

import (
	"regexp"
	"sort"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// maxDepth bounds the recursion over the syntax tree.
const maxDepth = 256

type extractor struct {
	lang *ts.Language
	src  []byte
	file source.FileID
	lay  layout

	modules map[string]*scope // the modules declared in this file, by identity
	// bodies are the scopes of the inline modules, by their body's offset:
	// two #[cfg] alternatives of one module share an identity, never a scope.
	bodies map[uint32]*scope
	macros []macroDef
	enums  map[string]*enumInfo // the enums declared in this file, by identity
	// hazards are where syntax may bring a macro_rules! macro into textual
	// scope unseen: a #[macro_use] module or crate (scope nil: anywhere
	// after it), an item or statement macro invocation of unknown expansion
	// (in its own scope and the scopes it encloses).
	hazards    []hazard
	macroNames []string // every macro_rules! name defined in the file

	symbols []language.SymbolDraft
	aliases []language.AliasDraft
	refs    []language.ReferenceDraft
	diags   []language.Diagnostic
	deep    bool // a nesting_too_deep diagnostic was reported
}

// enumInfo is what a glob of an enum of this file brings: its variants —
// no macro can add one — when it is declared once (#[cfg] alternatives may
// have different variants).
type enumInfo struct {
	declarations int
	variants     map[string]bool
}

// declareEnum records an enum declaration's variants.
func (x *extractor) declareEnum(ident string, n *ts.Node) {
	e := x.enums[ident]
	if e == nil {
		e = &enumInfo{variants: map[string]bool{}}
		x.enums[ident] = e
	}
	e.declarations++
	for _, v := range x.named(x.field(n, "body")) {
		if x.typ(v) == "enum_variant" {
			if vn := x.itemName(v); vn != "" {
				e.variants[vn] = true
			}
		}
	}
}

// macroDef is a macro_rules! definition: textually scoped from its end.
type hazard struct {
	at    uint32
	scope *scope
}

type macroDef struct {
	name      string
	qualified string
	end       uint32
	module    *scope
	// itemFree: no expansion of the macro is an item — its rules mention no
	// item keyword, no macro invocation and no fragment that may carry an
	// item (item, stmt, tt).
	itemFree bool
}

func newExtractor(lang *ts.Language, src []byte, file source.FileID) *extractor {
	return &extractor{lang: lang, src: src, file: file, lay: fileLayout(file), modules: map[string]*scope{}, bodies: map[uint32]*scope{}, enums: map[string]*enumInfo{}}
}

func (x *extractor) run(root *ts.Node) {
	ident := unscopedKey(x.file)
	var path []string
	if x.lay.ok {
		ident, path = modulePath(x.lay.crate, x.lay.modPath), x.lay.modPath
	}
	file := newScope(ident, path, nil, nil, x.lay.ok)
	x.modules[ident] = file
	x.collect(file, root, 0)
	x.findHazards(root, 0)
	x.items(file, root, ctx{}, 0)
	x.aliasesOf(file)
	for _, m := range x.bodies {
		x.aliasesOf(m)
	}
	sort.Slice(x.aliases, func(i, j int) bool {
		a, b := x.aliases[i], x.aliases[j]
		if a.Qualified != b.Qualified {
			return a.Qualified < b.Qualified
		}
		return a.Target < b.Target
	})
}

// aliasesOf records the `use` bindings of module m as aliases: m::name names
// what the binding's path does (a re-export, or an import a path from a
// descendant module reaches). Only a binding fixed without edition doubt is
// one; a glob brings names no alias states.
func (x *extractor) aliasesOf(m *scope) {
	if !m.known {
		return
	}
	for name, b := range m.uses {
		if b.glob {
			continue
		}
		r := x.resolveUse(b, 0)
		alias := m.ident + "::" + name
		if !r.ok || r.capped || r.ident == alias {
			continue
		}
		x.aliases = append(x.aliases, language.AliasDraft{Qualified: alias, Target: r.ident, Location: x.loc(b.node)})
	}
}

// ctx is what a declaration or reference is written in.
type ctx struct {
	container string   // the enclosing declaration's Qualified
	impl      *implCtx // the enclosing impl or trait
	locals    nameSet  // value names a local binding shadows
	generics  nameSet  // type names a generic parameter shadows
}

// implCtx is an impl or trait body: what Self is.
type implCtx struct {
	self    resolved        // Self's identity, when a path fixes it
	trait   string          // the trait implemented (or declared): its identity or text
	isTrait bool            // a trait's own body
	prefix  string          // the Qualified prefix of the body's items ("TYPE::", "<T as R>::")
	own     map[string]bool // a trait's own item names (Self::name in its body)
}

type nameSet map[string]bool

func (n nameSet) with(names ...string) nameSet {
	if len(names) == 0 {
		return n
	}
	out := make(nameSet, len(n)+len(names))
	for k := range n {
		out[k] = true
	}
	for _, k := range names {
		out[k] = true
	}
	return out
}

func (x *extractor) text(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(x.src)
}

func (x *extractor) field(n *ts.Node, name string) *ts.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name, x.lang)
}

func (x *extractor) typ(n *ts.Node) string { return n.Type(x.lang) }

// named returns n's named children in order. The runtime's NamedChild(i)
// scans the children on every call, so a loop over it is quadratic in a
// long list (an array of a hundred thousand literals); Child(i) is not.
func (x *extractor) named(n *ts.Node) []*ts.Node {
	if n == nil {
		return nil
	}
	out := make([]*ts.Node, 0, n.ChildCount())
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsNamed() {
			out = append(out, ch)
		}
	}
	return out
}

// firstNamed is n's first named child, or nil.
func (x *extractor) firstNamed(n *ts.Node) *ts.Node {
	for i := 0; n != nil && i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsNamed() {
			return ch
		}
	}
	return nil
}

func (x *extractor) loc(n *ts.Node) source.Location {
	return source.Location{
		File: x.file,
		Range: source.Range{
			Start: source.Position{Line: n.StartPoint().Row + 1, Column: n.StartPoint().Column + 1},
			End:   source.Position{Line: n.EndPoint().Row + 1, Column: n.EndPoint().Column + 1},
		},
	}
}

func (x *extractor) tooDeep(n *ts.Node, depth int) bool {
	if depth < maxDepth {
		return false
	}
	if !x.deep {
		x.deep = true
		x.diags = append(x.diags, language.Diagnostic{
			Severity: language.SeverityWarning,
			Code:     diagNestingTooDeep,
			Message:  "rust: syntax nested too deep; the rest of it is not analyzed",
			Location: x.loc(n),
		})
	}
	return true
}

// qualify is the identity of an item named name declared in scope s.
func qualify(s *scope, name string) string { return s.ident + "::" + name }

// itemName is an item's declared name (an identifier or type identifier).
func (x *extractor) itemName(n *ts.Node) string {
	name := x.field(n, "name")
	if name == nil || name.IsMissing() {
		return ""
	}
	return strings.TrimPrefix(x.text(name), "r#")
}

// collect records the names a module or block body declares and imports,
// creating the scopes of the inline modules it declares (whose bodies are
// collected too: a path may name their items from anywhere in the file).
func (x *extractor) collect(s *scope, body *ts.Node, depth int) {
	if body == nil || x.tooDeep(body, depth) {
		return
	}
	for i := 0; i < body.ChildCount(); i++ {
		n := body.Child(i)
		if !n.IsNamed() {
			continue
		}
		name := x.itemName(n)
		if x.cfgAttribute(n) {
			x.markCfg(s, n, name)
		}
		vis := x.visibilityOf(n)
		if x.generativeAttribute(n) {
			// A derive or an attribute macro may add names to the scope.
			if s.isModule() {
				s.generated = true
			} else {
				s.opaque = true
			}
			x.hazards = append(x.hazards, hazard{at: n.StartByte(), scope: s})
		}
		switch x.typ(n) {
		case "mod_item":
			if name == "" {
				continue
			}
			ident := qualify(s, name)
			var child *scope
			if b := x.field(n, "body"); b != nil && s.isModule() {
				child = newScope(ident, append(append([]string(nil), s.modPath...), name), nil, s, s.known)
				x.bodies[b.StartByte()] = child
				if _, dup := x.modules[ident]; dup {
					// #[cfg] alternatives: which one a path reaches depends
					// on the configuration — none is followed.
					x.modules[ident] = ambiguousModule(ident)
				} else {
					x.modules[ident] = child
				}
				x.collect(child, b, depth+1)
			} else if x.pathAttribute(n) {
				// #[path = "..."]: the module's file is not where its path
				// says; no declaration of it is named through it.
				child = newScope(ident, nil, nil, s, false)
				child.opaque = true
			}
			s.declare(name, ident, symbol.KindModule, child, vis)
		case "function_item", "function_signature_item":
			if name != "" {
				s.declare(name, qualify(s, name), symbol.KindFunction, nil, vis)
			}
		case "struct_item", "union_item":
			if name != "" {
				s.declare(name, qualify(s, name), symbol.KindStruct, nil, vis)
			}
		case "enum_item":
			if name != "" {
				ident := qualify(s, name)
				s.declare(name, ident, symbol.KindEnum, nil, vis)
				x.declareEnum(ident, n)
			}
		case "trait_item":
			if name != "" {
				s.declare(name, qualify(s, name), symbol.KindTrait, nil, vis)
			}
		case "type_item":
			if name != "" {
				s.declare(name, qualify(s, name), symbol.KindTypeAlias, nil, vis)
			}
		case "const_item":
			if name != "" {
				s.declare(name, qualify(s, name), symbol.KindConstant, nil, vis)
			}
		case "static_item":
			if name != "" {
				s.declare(name, qualify(s, name), symbol.KindVariable, nil, vis)
			}
		case "foreign_mod_item":
			x.collect(s, x.field(n, "body"), depth+1)
		case "use_declaration":
			x.collectUse(s, n)
		case "macro_invocation":
			// An item-level macro may declare anything.
			s.opaque = true
			x.hazards = append(x.hazards, hazard{at: n.StartByte(), scope: s})
		case "expression_statement":
			// `foo!();` in a module is an item-level invocation too; in a
			// block, a standard expression macro or one of this file whose
			// expansions are never items declares nothing.
			if m := x.firstNamed(n); m != nil && x.typ(m) == "macro_invocation" &&
				(s.isModule() || !expressionMacro(x.macroName(m)) && !x.itemFreeInvocation(s, m)) {
				s.opaque = true
				x.hazards = append(x.hazards, hazard{at: m.StartByte(), scope: s})
			}
		case "extern_crate_declaration":
			// `extern crate foo as bar;` binds a crate of another package:
			// a name no identity of this repository fixes.
			if alias := x.field(n, "alias"); alias != nil {
				s.bind(x.text(alias), &useBinding{scope: s, node: n, done: true, vis: vis})
			} else if name != "" {
				s.bind(name, &useBinding{scope: s, node: n, done: true, vis: vis})
			}
		}
	}
}

// expressionMacro reports a standard library macro that expands to an
// expression or a statement, never to an item: its invocation leaves a
// block's declarations known.
func expressionMacro(name string) bool {
	switch name {
	case "println", "print", "eprintln", "eprint", "format", "format_args", "write", "writeln",
		"vec", "panic", "assert", "assert_eq", "assert_ne", "debug_assert", "debug_assert_eq",
		"debug_assert_ne", "todo", "unimplemented", "unreachable", "dbg", "matches", "concat",
		"stringify", "include_str", "include_bytes", "env", "option_env", "cfg", "line", "column",
		"file", "module_path":
		return true
	}
	return false
}

// macroName is the last segment of an invocation's macro path.
func (x *extractor) macroName(m *ts.Node) string {
	name := x.field(m, "macro")
	if name == nil {
		return ""
	}
	if x.typ(name) == "scoped_identifier" {
		name = x.field(name, "name")
	}
	return x.text(name)
}

// collectUse records the bindings of a use declaration.
func (x *extractor) collectUse(s *scope, n *ts.Node) {
	x.useTree(s, x.field(n, "argument"), nil, x.visibilityOf(n), n, 0)
}

// visibilityOf is an item's declared visibility (its visibility_modifier).
func (x *extractor) visibilityOf(n *ts.Node) visibility {
	for _, ch := range x.named(n) {
		if x.typ(ch) != "visibility_modifier" {
			continue
		}
		switch strings.Join(strings.Fields(x.text(ch)), "") {
		case "pub", "pub(crate)":
			return visPublic
		case "pub(self)":
			return visPrivate
		}
		return visRestricted
	}
	return visPrivate
}

// useTree walks a use tree below prefix, recording each binding it makes.
func (x *extractor) useTree(s *scope, t *ts.Node, prefix []string, vis visibility, decl *ts.Node, depth int) {
	if t == nil || depth > 16 {
		return
	}
	switch x.typ(t) {
	case "identifier", "self", "super", "crate", "scoped_identifier":
		path := append(append([]string(nil), prefix...), x.pathSegments(t)...)
		if path == nil {
			return
		}
		local := path[len(path)-1]
		if local == "self" && len(path) > 1 {
			// `use a::b::{self}` binds b.
			path = path[:len(path)-1]
			local = path[len(path)-1]
		}
		if local == "self" || local == "super" || local == "crate" {
			return
		}
		s.bind(strings.TrimPrefix(local, "r#"), &useBinding{path: path, scope: s, node: t, vis: vis})
	case "use_as_clause":
		path := append(append([]string(nil), prefix...), x.pathSegments(x.field(t, "path"))...)
		alias := x.text(x.field(t, "alias"))
		if len(path) == 0 || alias == "" {
			return
		}
		if path[len(path)-1] == "self" && len(path) > 1 {
			path = path[:len(path)-1]
		}
		if alias == "_" {
			return // brings a trait's methods into scope, binding no name
		}
		s.bind(strings.TrimPrefix(alias, "r#"), &useBinding{path: path, scope: s, node: t, vis: vis})
	case "scoped_use_list":
		p := append(append([]string(nil), prefix...), x.pathSegments(x.field(t, "path"))...)
		x.useTree(s, x.field(t, "list"), p, vis, decl, depth+1)
	case "use_list":
		for _, ch := range x.named(t) {
			x.useTree(s, ch, prefix, vis, decl, depth+1)
		}
	case "use_wildcard":
		var p []string
		if inner := x.firstNamed(t); inner != nil {
			p = x.pathSegments(inner)
		}
		path := append(append([]string(nil), prefix...), p...)
		s.globs = append(s.globs, &useBinding{path: path, scope: s, node: t, glob: true, vis: vis})
	}
}

// pathSegments returns a simple path's segments as written ("a::b::c"),
// generic arguments dropped; nil for anything else.
func (x *extractor) pathSegments(n *ts.Node) []string {
	if n == nil || n.IsMissing() {
		return nil
	}
	switch x.typ(n) {
	case "identifier", "type_identifier", "self", "super", "crate":
		name := strings.TrimPrefix(x.text(n), "r#")
		if name == "" {
			return nil // a node error recovery inserted
		}
		return []string{name}
	case "scoped_identifier", "scoped_type_identifier":
		var head []string
		if p := x.field(n, "path"); p != nil {
			head = x.pathSegments(p)
			if head == nil {
				return nil // a path through a generic or qualified type
			}
		} else if strings.HasPrefix(x.text(n), "::") {
			return nil // `::name`: a crate of the extern prelude
		}
		name := x.field(n, "name")
		if name == nil || name.IsMissing() || strings.TrimPrefix(x.text(name), "r#") == "" {
			return nil
		}
		return append(head, strings.TrimPrefix(x.text(name), "r#"))
	case "generic_type":
		return x.pathSegments(x.field(n, "type"))
	}
	return nil
}

// pathAttribute reports a #[path] attribute (or a cfg_attr carrying one)
// among the attributes right before item n.
func (x *extractor) pathAttribute(n *ts.Node) bool {
	for p := n.PrevSibling(); p != nil; p = p.PrevSibling() {
		switch x.typ(p) {
		case "attribute_item":
			if pathAttr.MatchString(x.text(p)) {
				return true
			}
		case "line_comment", "block_comment":
		default:
			return false
		}
	}
	return false
}

var pathAttr = regexp.MustCompile(`(^|[^A-Za-z0-9_:])path\s*=`)

// cfgAttribute reports a #[cfg] or #[cfg_attr] attribute among the
// attributes right before item n.
func (x *extractor) cfgAttribute(n *ts.Node) bool {
	for p := n.PrevSibling(); p != nil; p = p.PrevSibling() {
		switch x.typ(p) {
		case "attribute_item":
			if t := strings.TrimSpace(strings.TrimPrefix(x.text(p), "#[")); strings.HasPrefix(t, "cfg") {
				return true
			}
		case "line_comment", "block_comment":
		default:
			return false
		}
	}
	return false
}

// markCfg records the names a #[cfg]-conditional item or use declaration
// binds in s.
func (x *extractor) markCfg(s *scope, n *ts.Node, name string) {
	if x.typ(n) != "use_declaration" {
		if name != "" {
			s.cfg[name] = true
		}
		return
	}
	var visit func(t *ts.Node, prefix []string, depth int)
	visit = func(t *ts.Node, prefix []string, depth int) {
		if t == nil || depth > 16 {
			return
		}
		switch x.typ(t) {
		case "use_as_clause":
			s.cfg[strings.TrimPrefix(x.text(x.field(t, "alias")), "r#")] = true
		case "scoped_use_list":
			visit(x.field(t, "list"), append(append([]string(nil), prefix...), x.pathSegments(x.field(t, "path"))...), depth+1)
		case "use_list":
			for _, ch := range x.named(t) {
				visit(ch, prefix, depth+1)
			}
		default:
			segs := append(append([]string(nil), prefix...), x.pathSegments(t)...)
			if len(segs) > 1 && segs[len(segs)-1] == "self" {
				segs = segs[:len(segs)-1] // `use a::b::{self}` binds b
			}
			if len(segs) > 0 {
				s.cfg[segs[len(segs)-1]] = true
			}
		}
	}
	visit(x.field(n, "argument"), nil, 0)
}

// inertAttributes are the attributes of the compiler and its tools that add
// no name to a scope: anything else — an attribute macro, `cfg_attr` (which
// may apply one) — may.
var inertAttributes = map[string]bool{
	"cfg": true, "allow": true, "warn": true, "deny": true, "forbid": true, "expect": true,
	"deprecated": true, "must_use": true, "inline": true, "cold": true, "doc": true, "repr": true,
	"non_exhaustive": true, "path": true, "macro_use": true, "macro_export": true, "test": true,
	"ignore": true, "should_panic": true, "bench": true, "no_mangle": true, "export_name": true,
	"link": true, "link_name": true, "link_section": true, "used": true, "automatically_derived": true,
	"track_caller": true, "target_feature": true, "global_allocator": true, "panic_handler": true,
	"rustfmt::skip": true, "derive": true,
}

// builtinDerives are the derives the compiler implements: each adds an impl,
// never a name.
var builtinDerives = map[string]bool{
	"Debug": true, "Clone": true, "Copy": true, "PartialEq": true, "Eq": true,
	"PartialOrd": true, "Ord": true, "Hash": true, "Default": true,
}

// generativeAttribute reports an attribute right before item n that may add
// names to its scope: one not inertAttributes, a tool attribute aside
// (clippy::…, rustfmt::…), or a derive of anything but builtinDerives.
func (x *extractor) generativeAttribute(n *ts.Node) bool {
	for p := n.PrevSibling(); p != nil; p = p.PrevSibling() {
		switch x.typ(p) {
		case "attribute_item":
			t := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(x.text(p)), "#["), "]"))
			name := t
			if i := strings.IndexAny(t, "( ="); i >= 0 {
				name = t[:i]
			}
			if strings.HasPrefix(name, "clippy::") || strings.HasPrefix(name, "rustfmt::") {
				continue
			}
			if !inertAttributes[name] {
				return true
			}
			if name == "derive" {
				args := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(t[len(name):]), "("), ")")
				for _, d := range strings.Split(args, ",") {
					d = strings.TrimSpace(d)
					d = strings.TrimPrefix(strings.TrimPrefix(d, "std::fmt::"), "core::fmt::")
					if d != "" && !builtinDerives[d] {
						return true
					}
				}
			}
		case "line_comment", "block_comment":
		default:
			return false
		}
	}
	return false
}
