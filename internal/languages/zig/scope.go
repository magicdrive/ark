package zig

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/symbol"
)

// scope is a lexical scope: a container (whose declarations are visible in
// the whole container, in any order), or a function's parameters, or a
// block (whose locals are visible after their declaration). Zig looks a
// name up from the innermost scope outwards, through the function bodies a
// container is declared in, up to the file's container.
type scope struct {
	parent    *scope
	container bool
	// prefix is the identity prefix of the declarations made in the scope:
	// a container's own identity, the enclosing declaration's in a block.
	prefix string
	// node is a container scope's declaration (the file's root for the
	// file), the identity @This() denotes in it.
	node *ts.Node
	// decls are the names the scope binds: a container's declarations, a
	// block's locals, a function's parameters. Several declarations of one
	// name (an error the compiler reports) are kept: the name is ambiguous.
	decls map[string][]*decl
	// fields are a container's fields: no name in scope, reached only
	// through a value of the container (or, for an enum, the type itself).
	fields map[string]*decl
	// opaque: a `usingnamespace` of the container may bring any name into it.
	opaque bool
	// enum: the container is an enum or an error set (its fields are values).
	enum bool
	// name is a container's name (a method's Receiver); symbol is whether
	// the container is a declaration of its own (references in it are its).
	name   string
	symbol bool
	// members are the container's declarations, fields and tests in source
	// order.
	members []*decl
}

func newScope(parent *scope, prefix string, container bool) *scope {
	s := &scope{parent: parent, prefix: prefix, container: container, decls: map[string][]*decl{}}
	if container {
		s.fields = map[string]*decl{}
	}
	return s
}

func (s *scope) bind(d *decl) {
	if d.name == "" || d.name == "_" {
		return
	}
	s.decls[d.name] = append(s.decls[d.name], d)
}

// enclosingContainer is the innermost container scope around s: the one
// @This() denotes.
func (s *scope) enclosingContainer() *scope {
	for c := s; c != nil; c = c.parent {
		if c.container {
			return c
		}
	}
	return nil
}

// decl is a name a scope binds.
type decl struct {
	name  string
	ident string // the declaration's identity; "" for a local
	kind  symbol.SymbolKind
	scope *scope // the scope it is declared in, whose names its text sees

	local     bool // a local, parameter or capture: no symbol, no reference
	mutable   bool // a var: its value is no namespace
	test      bool // a test declaration: no name in scope
	field     bool // a container field
	enumField bool // a field of an enum (or an error set's name): a value of the type

	init    *ts.Node // a variable's initializer
	typ     *ts.Node // a variable's, parameter's or field's declared type
	fn      *ts.Node // a function declaration
	body    *scope   // the scope of the container this declaration declares
	fnScope *scope   // a function's parameter scope (its result type is written there)
	node    *ts.Node // the declaration
	ret     *val     // a function's result, once computed
	// returns is the scope of the container a function's one return is (its
	// identity F::()), nil if none.
	returns *scope
	// anonymous are the containers a variable's value declares unnamed.
	anonymous []*scope

	v    *val // what the name denotes, once computed
	busy bool // its value is being computed (a cycle denotes nothing)
}

// val is what an expression denotes, as far as the file's declarations
// prove it — nothing is inferred:
//
//	ns    the identities whose declarations are the expression's members:
//	      a container, a file, a constant naming one (several: the
//	      alternatives a comptime condition chooses between)
//	typ   the identity of the type of the value the expression is (a
//	      pointer's: its pointee), when one type is proven
//	errU  the value is an error union of typ: `try` / `catch` yield typ
//	fn    the one function declaration of this file the expression names
//	spec  ns is a value of another file whose type that file states (a call
//	      result F::(), a member of it): a member is looked up there, and
//	      may be a field, which no declaration is (Speculative)
type val struct {
	ns   []string
	typ  string
	errU bool
	fn   *decl
	spec bool
}

// nsOne is the one namespace the value is, or "".
func (v val) nsOne() string {
	if len(v.ns) == 1 {
		return v.ns[0]
	}
	return ""
}

// lookup finds what name denotes in s: the declarations of the innermost
// scope binding it. unknown is true when Zig's lookup may reach something
// Ark cannot see first (a container with `usingnamespace`).
func lookup(s *scope, name string) (ds []*decl, unknown bool) {
	for c := s; c != nil; c = c.parent {
		if ds := c.decls[name]; len(ds) > 0 {
			return ds, false
		}
		if c.opaque {
			return nil, true
		}
	}
	return nil, false
}

// identifierName is the name an identifier node spells: `@"name"` is the
// name itself.
func identifierName(text string) string {
	if strings.HasPrefix(text, `@"`) && strings.HasSuffix(text, `"`) && len(text) >= 3 {
		inner := text[2 : len(text)-1]
		if strings.ContainsAny(inner, `\:.(){}`) {
			// An escape (not decoded), or a separator of identities: no
			// name is claimed.
			return ""
		}
		return inner
	}
	return text
}

// primitives are the names Zig predeclares: no declaration of the
// repository can be what they denote (a declaration may not shadow them).
var primitives = map[string]bool{
	"bool": true, "void": true, "noreturn": true, "type": true, "anyerror": true,
	"anyopaque": true, "anyframe": true, "anytype": true, "comptime_int": true,
	"comptime_float": true, "isize": true, "usize": true, "c_char": true,
	"c_short": true, "c_ushort": true, "c_int": true, "c_uint": true, "c_long": true,
	"c_ulong": true, "c_longlong": true, "c_ulonglong": true, "c_longdouble": true,
	"f16": true, "f32": true, "f64": true, "f80": true, "f128": true,
	"true": true, "false": true, "null": true, "undefined": true, "unreachable": true,
}

// isPrimitive reports a predeclared name: those above and the arbitrary
// bit-width integers (u0, i32, u1024, ...).
func isPrimitive(name string) bool {
	if primitives[name] {
		return true
	}
	if len(name) < 2 || (name[0] != 'u' && name[0] != 'i') {
		return false
	}
	for _, r := range name[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(name) == 2 || name[1] != '0'
}
