// Package rust provides a Tree-sitter-based extraction Provider for Rust
// (`.rs`).
//
// # Identity
//
// A Rust item is named by its path: its crate, the modules down to it and
// its name (`crate::storage::Repository`). The provider derives the crate
// and the module path of a file lexically from the file's own
// repository-relative path, by Cargo's default target layout (layout.go) —
// no other file is read, Cargo.toml included, and nothing is executed. Every
// declaration's Qualified is that path, the crate written as its crate key
// ("crate" for the repository root package's library and main binary,
// "crates/app/crate" for another package's, "src/bin/x.rs/crate" for a
// binary target): two crates' items never share an identity. Within an item:
//
//	inherent method / associated item    TYPE::name          (TYPE the type's path)
//	trait method / associated item       TRAIT::name
//	trait implementation's item          <TYPE as TRAIT>::name
//	struct / union field                 TYPE.field          (no path names a field)
//	enum variant                         ENUM::Variant
//	item declared in a function body     FN::{block}::name
//	macro_rules! macro                   MODULE::name!       (#[macro_export]: the crate root)
//	impl block                           impl TYPE / <TYPE as TRAIT>
//
// So an inherent method and a trait implementation's method of one name are
// two declarations, as in Rust. A TYPE or TRAIT the provider cannot name by
// a path (a generic parameter, a type of another crate, a re-export) is
// written as its text under the impl's module ("{MODULE}Type"), which no
// path ever computes: its items are reached by no identity.
//
// A file whose layout is unknown (layout.go) names its declarations by its
// file path, which no path computes either.
//
// # References
//
// A reference resolves only by the identity the language's own lexical
// rules fix (ReferenceDraft.NameQualified; the file is IdentityOnly, so no
// name heuristic ever reaches a Rust declaration and no Rust reference is
// resolved by similarity):
//
//   - a path starting with crate, self, super or Self, or with a name an
//     item or a `use` of the enclosing block or module declares — a
//     module's own scope only: Rust names are not inherited from enclosing
//     modules;
//   - through `use` bindings (aliases included) and through modules
//     declared in this file, following their `use` bindings; a glob import
//     (`use m::*`) of a module of this file whose items are all known (no
//     item-level macro invocation, no derive or attribute macro but the
//     compiler's), or of an enum of this file, when it is the scope's only
//     glob — bringing, from outside the module, only its public names;
//   - a `use` whose first segment is a plain name, outside the crate root,
//     reads differently in Rust 2015 (crate-relative) and 2018 (scope-
//     relative), and the edition is in Cargo.toml: it is capped at
//     Candidate — the 2018 reading when the scope names its first segment,
//     the 2015 reading (from the crate root) when it does not;
//   - a macro by `macro_rules!` textual scope within this file.
//
// A module's `use` bindings are aliases (language.AliasDraft): `pub use
// storage::Repository;` in the crate root makes crate::Repository name
// crate::storage::Repository, so another file's path through the re-export
// resolves to the declaration. A glob, a binding of edition-dependent
// reading, and a block's bindings are none.
//
// Not resolved (no identity: Unresolved): a method call `value.method()`
// (the receiver's type needs inference and trait resolution), a name a
// glob may bring, a name of another crate (dependencies, `std`), a name a
// macro expansion may define, a path through a glob re-export of another
// file, and anything a local binding (`let`, a parameter, a closure or pattern
// binding, a macro statement binding a name its invocation supplies)
// shadows — such names are not observed at all. A name declared twice —
// `#[cfg]` alternatives, impls of one generic type, a module and a function
// — is ambiguous (Candidate), never one of them; a `#[cfg]` item in a scope
// with a glob, which may be the glob's name in another configuration, is
// unresolved.
//
// Token trees (macro invocation arguments, macro_rules bodies, attribute
// arguments) are not parsed: nothing a macro expands to is extracted.
//
// Invariants: Extract never panics, never returns a Tree-sitter node, keeps
// every byte range inside the source, and bounds its recursion (maxDepth).
package rust

import (
	"context"
	"path/filepath"

	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Provider extracts code intelligence from Rust files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "rust" }

func (p *Provider) Extensions() []string { return []string{".rs"} }

// CacheVersion must change whenever extraction semantics change.
func (p *Provider) CacheVersion() string { return "rust-4" }

// Diagnostic codes this provider states, beside treediag's.
const diagNestingTooDeep = "nesting_too_deep" // syntax below maxDepth is not observed

func (p *Provider) Extract(_ context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.RustLanguage()
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
	x.run(root)
	return language.Extraction{
		Symbols:      x.symbols,
		References:   x.refs,
		Aliases:      x.aliases,
		Diagnostics:  x.diags,
		IdentityOnly: true,
	}, nil
}

// unscopedKey names the declarations of a file without a layout: its path,
// which no Rust path computes.
func unscopedKey(file source.FileID) string {
	return "{" + filepath.ToSlash(string(file)) + "}"
}
