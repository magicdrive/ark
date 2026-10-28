// Package zig provides a Tree-sitter-based extraction Provider for Zig
// (`.zig`), for the language as Zig 0.15 to 0.17 define it (the grammar is
// tree-sitter-grammars/tree-sitter-zig, whose corpus follows Zig 0.15).
//
// # Identity
//
// A Zig file is a struct: its top-level declarations are the members of the
// file's container, and `@import("other.zig")` is that other file's
// container, found by the path relative to the importing file. So a
// declaration is named by its file and the containers down to it, and the
// provider derives every identity from the file's own repository-relative
// path — no other file is read, build.zig included, and nothing is
// executed:
//
//	the file itself                      src/engine.zig                (kind module)
//	top-level declaration                src/engine.zig::Engine
//	declaration of a container           src/engine.zig::Engine::init  (struct, union, enum, opaque)
//	enum field, error set member         src/engine.zig::Color::red
//	struct / union field                 src/engine.zig::Engine.count  (no member access names a field)
//	declaration in a function body       src/engine.zig::main::Local
//	the container a function returns     src/list.zig::List::()        (its one return is a container)
//	other anonymous container            ...::{struct}                 (reached only through @This())
//	test                                 src/engine.zig::test "name"
//
// Every file is IdentityOnly: no name heuristic ever reaches a Zig
// declaration, and a reference without an identity is Unresolved.
//
// # References
//
// A reference resolves only by the identity Zig's own rules fix
// (ReferenceDraft.NameQualified, IdentityInRepository):
//
//   - a name by lexical scope — the block's locals declared before it, the
//     function's parameters and captures, then the declarations of the
//     enclosing containers, innermost first, through the function bodies a
//     container is declared in. A local, a parameter or a capture is not a
//     declaration Ark extracts: it is not observed. A primitive (u8, bool,
//     type, ...) is no reference;
//   - a member of a container this file declares (`Engine.init`,
//     `Self.init` with `const Self = @This();`), of a file a relative
//     `@import` names (`math.calculate` → src/math.zig::calculate), or of a
//     constant naming either (`const Vec = @import("vec.zig").Vec;`
//     makes `Vec.init` vec.zig's Vec::init);
//   - a method call `v.m()` whose receiver's type the file proves from
//     declarations alone: a declared type (`v: T`, `v: *T`, `v: *const T`,
//     a parameter `self: *Self`), a construction (`T{ ... }`), a call of a
//     function of this file whose declared result type is a type the file
//     names (`T.init()` returning `Self`, through `try` / `catch` for an
//     error union), a field of a struct of this file whose declared type the
//     file names. A receiver whose type is a container of another file is
//     looked up there (Speculative: the member may be a field holding a
//     function, which is no declaration);
//   - a decl or enum literal (`.init()`, `.red`) where a declared type
//     fixes its result type (`const e: Engine = .init();`, a return of a
//     function whose result type is written, a field's default value).
//
// A constant whose value is a namespace — an import, `@This()`, another
// container or a path to one, a call of a function whose one return is a
// container (`List(u32)`) — is a declaration of its own: a reference to it
// resolves to it, and a member path through it reaches the target's member
// (language.AliasDraft.Members). One a comptime condition chooses
// (`if (builtin.os.tag == .windows) A else B`) names each alternative: a
// member through it is a Candidate.
//
// A named module import (`@import("stdx")`) is the module the importing
// file's module's import table binds, as a build.zig states it, read by
// its syntax and capped at Strong (buildzig.go); `@import("x.zig")` too,
// before the file.
//
// Not resolved (Unresolved): `@import("std")`, `"builtin"`, `"root"`, a
// named module no build.zig Ark reads binds, and everything reached
// through them; a ZON import; a member of a
// value whose type needs inference (a generic parameter's, `anytype`'s, a
// capture's, a call result of another file's function); a name a
// `usingnamespace` of an enclosing container may bring; `@field`,
// `@call` targets and other computed names; what a comptime call returns
// other than a container it declares. Nothing is evaluated at comptime:
// every branch of a conditional is analyzed, none is chosen.
//
// Invariants: Extract never panics, never returns a Tree-sitter node, keeps
// every byte range inside the source, and bounds its recursion (maxDepth).
package zig

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Provider extracts code intelligence from Zig files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "zig" }

func (p *Provider) Extensions() []string { return []string{".zig"} }

// CacheVersion must change whenever extraction semantics change.
func (p *Provider) CacheVersion() string { return "zig-1" }

// Diagnostic codes this provider states, beside treediag's.
const diagNestingTooDeep = "nesting_too_deep" // syntax below maxDepth is not observed

func (p *Provider) Extract(_ context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.ZigLanguage()
	tree, err := tsparse.Parse(lang, src)
	if err != nil {
		return language.Extraction{
			Diagnostics:  []language.Diagnostic{treediag.ParseFailed(file, err)},
			IdentityOnly: true,
		}, nil
	}
	defer tree.Release()
	root := tree.RootNode()
	x := newExtractor(lang, src, file)
	x.diags = append(x.diags, treediag.ParseErrors(root, lang, file)...)
	if len(x.diags) == 0 {
		x.diags = append(x.diags, x.emptyNames(root)...)
	}
	x.run(root)
	return language.Extraction{
		Symbols:      x.symbols,
		References:   x.refs,
		Aliases:      x.aliases,
		Diagnostics:  x.diags,
		IdentityOnly: true,
	}, nil
}
