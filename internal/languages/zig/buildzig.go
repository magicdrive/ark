package zig

import (
	"path"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
)

// Named modules.
//
// `@import("name")` names a module the build gives the importing module
// under that name; build.zig decides which, by code Ark never runs. What a
// build.zig states literally is read from its syntax alone:
//
//	b.addModule("name", .{ .root_source_file = b.path("src/x.zig"), ... })
//	m.addImport("name", mod)          mod a constant of the scope (lexically)
//	                                  or a createModule / addModule call
//	b.createModule(.{ ..., .imports = &.{ .{ .name = "name", .module = mod } } })
//
// where a module's root file is a `b.path("literal")` relative to
// build.zig's directory. Each states an alias {module}name → that file
// (language.AliasDraft), capped at Strong: a module of the name a
// dependency's build provides, or one the build computes, may be the one a
// file imports instead. Anything else — a name or path computed at build
// time, a module of a dependency (`b.dependency(...).module(...)`), a
// module passed through a struct field — states nothing, and an import of
// its name stays unresolved. std, builtin and root are the compiler's:
// never mapped.

// moduleIdent is the identity a named module import has.
func moduleIdent(name string) string { return "{module}" + name }

// reservedModules are the modules the compiler provides under their names.
var reservedModules = map[string]bool{"std": true, "builtin": true, "root": true}

// reservedPath reports an identity through a module the compiler provides.
func reservedPath(ident string) bool {
	name, ok := strings.CutPrefix(ident, "{module}")
	if !ok {
		return false
	}
	name, _, _ = strings.Cut(name, "::")
	return reservedModules[name]
}

// buildModules reads the module declarations of build.zig's call n,
// written in ctx c.
func (x *extractor) buildModules(n *ts.Node, c ctx) {
	f := x.field(n, "function")
	if f == nil || x.typ(f) != "field_expression" || x.field(f, "object") == nil {
		return
	}
	args := x.named(x.field(n, "arguments"))
	switch identifierName(x.text(x.field(f, "member"))) {
	case "addModule":
		if len(args) == 2 {
			if name, ok := x.stringLiteral(args[0]); ok {
				x.moduleAlias(name, x.rootSource(args[1]), args[0])
			}
			x.optionImports(args[1], c, 0)
		}
	case "createModule":
		if len(args) == 1 {
			x.optionImports(args[0], c, 0)
		}
	case "addImport":
		if len(args) == 2 {
			if name, ok := x.stringLiteral(args[0]); ok {
				x.moduleAlias(name, x.moduleRoot(args[1], c.s, 0), args[0])
			}
		}
	}
}

// optionImports reads the `.imports` of module options: each named import
// and its module.
func (x *extractor) optionImports(opts *ts.Node, c ctx, depth int) {
	list := x.option(opts, "imports")
	if list == nil || depth > 4 {
		return
	}
	if x.typ(list) == "unary_expression" { // &.{ ... }
		list = x.firstNamed(list)
	}
	if x.typ(list) != "anonymous_struct_initializer" {
		return
	}
	for _, entry := range x.named(x.firstNamed(list)) {
		if x.typ(entry) != "anonymous_struct_initializer" {
			continue
		}
		if name, ok := x.stringLiteral(x.option(entry, "name")); ok {
			x.moduleAlias(name, x.moduleRoot(x.option(entry, "module"), c.s, depth+1), entry)
		}
	}
}

// moduleRoot is the identity of the root file of the module expression e
// denotes: a createModule / addModule call, or a constant of the scope
// whose value is one. "" when the syntax does not state it.
func (x *extractor) moduleRoot(e *ts.Node, s *scope, depth int) string {
	if e == nil || depth > 8 {
		return ""
	}
	switch x.typ(e) {
	case "call_expression":
		f := x.field(e, "function")
		if f == nil || x.typ(f) != "field_expression" {
			return ""
		}
		args := x.named(x.field(e, "arguments"))
		switch identifierName(x.text(x.field(f, "member"))) {
		case "createModule":
			if len(args) == 1 {
				return x.rootSource(args[0])
			}
		case "addModule":
			if len(args) == 2 {
				return x.rootSource(args[1])
			}
		}
	case "identifier":
		ds, unknown := lookup(s, identifierName(x.text(e)))
		if unknown || len(ds) != 1 || ds[0].init == nil || ds[0].mutable {
			return ""
		}
		return x.moduleRoot(ds[0].init, ds[0].scope, depth+1)
	}
	return ""
}

// rootSource is the identity of the file a module's options name as its
// root source file: `.root_source_file = b.path("literal")`, relative to
// build.zig's directory. "" for any other value.
func (x *extractor) rootSource(opts *ts.Node) string {
	v := x.option(opts, "root_source_file")
	if v == nil || x.typ(v) != "call_expression" {
		return ""
	}
	f := x.field(v, "function")
	if f == nil || x.typ(f) != "field_expression" || identifierName(x.text(x.field(f, "member"))) != "path" {
		return ""
	}
	args := x.named(x.field(v, "arguments"))
	if len(args) != 1 {
		return ""
	}
	p, ok := x.stringLiteral(args[0])
	if !ok || !strings.HasSuffix(p, ".zig") || path.IsAbs(p) || strings.ContainsAny(p, "\\\x00") {
		return ""
	}
	key := path.Join(path.Dir(x.key), p)
	if key == ".." || strings.HasPrefix(key, "../") {
		return ""
	}
	return key
}

// option is the value of field name in an anonymous struct literal
// (`.{ .name = value }`), or nil.
func (x *extractor) option(lit *ts.Node, name string) *ts.Node {
	if lit == nil || x.typ(lit) != "anonymous_struct_initializer" {
		return nil
	}
	for _, a := range x.named(x.firstNamed(lit)) {
		if x.typ(a) != "assignment_expression" {
			continue
		}
		l := x.field(a, "left")
		if l != nil && x.typ(l) == "field_expression" && x.field(l, "object") == nil && identifierName(x.text(x.field(l, "member"))) == name {
			return x.field(a, "right")
		}
	}
	return nil
}

// moduleAlias states that module name is the file root, as build.zig maps
// it (capped at Strong).
func (x *extractor) moduleAlias(name, root string, at *ts.Node) {
	if name == "" || root == "" || reservedModules[name] || strings.Contains(name, "::") {
		return
	}
	a := language.AliasDraft{Qualified: moduleIdent(name), Target: root, Location: x.loc(at), ConfidenceCap: "strong"}
	for _, prev := range x.aliases {
		if prev.Qualified == a.Qualified && prev.Target == a.Target {
			return
		}
	}
	x.aliases = append(x.aliases, a)
}
