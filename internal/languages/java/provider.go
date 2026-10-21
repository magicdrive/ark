// Package java provides a Tree-sitter-based extraction Provider for Java
// (`.java`).
//
// # Identity
//
// A Java type is named by its canonical name: its package and the types it
// is nested in (`com.example.Outer.Inner`; a type of the unnamed package is
// its simple name alone). A member is its type's name and its own
// (`com.example.Outer.run`, a field `com.example.Outer.count`), a
// constructor `TYPE.<init>`. Overloads share an identity — they are told
// apart by the index's declaration ordinals and by their Parameters, never
// by a name. A type declared in a body has no canonical name: a local class
// is `ENCLOSING.method.Local` (with "#N" from its second namesake in the
// file), an anonymous class `ENCLOSING.<anonymous>#N` — identities no other
// file can compute, which only this file's lexical scopes reach. Every file
// is IdentityOnly: no name-based stage reaches a Java declaration, and a
// Java reference without the evidence below is Unresolved.
//
// # Lookups
//
// Which declaration a Java name denotes depends on other files — the
// members a supertype declares, the types of the file's package, those an
// on-demand import brings — so the provider states the language's own
// search (JLS 6.4–6.5, 15.12.1) as a Lookup (language.ReferenceDraft.
// Lookup) and the resolver answers it against the repository. The provider
// decides alone only what the file fixes: a local variable, parameter or
// pattern binding (its uses are no references), a type parameter, a local
// class, an enclosing type's own members, a single-type import, a type of
// the same file. Outward from the reference, scope by scope:
//
//   - a type name: block scopes (local classes), then each enclosing type's
//     member types — its own, then (a step) those it inherits —, then a
//     single-type import or a type of the file, the package's types (a
//     step: the package's other files), and the on-demand imports with
//     java.lang (one step);
//   - a method name: each enclosing type that has a member method of the
//     name (its own, those it inherits, those of java.lang.Object), the
//     innermost deciding whatever the arguments; then the static imports
//     (one step). The call is resolved among that type's methods of the
//     name — inherited ones included — that accept its argument count;
//   - a variable name: the block scopes, then each enclosing type's fields,
//     then the static imports;
//   - a qualified name: its head as a variable, then as a type (its
//     members then followed), then as a package (JLS 6.5.2).
//
// A receiver's type is known when the file declares it: this, super, a
// type name, a local variable, parameter or own field with a declared type
// (`var` initialized by a class instance creation), a cast, a class
// instance creation, a type variable's one bound (read where the type
// parameter is declared). Overloads are told apart by argument count and by
// the argument types the file proves (ArgumentTypes); only those the call
// may access are candidates. Two left are Candidates.
//
// What a scope outside the file may hold is not guessed: an identity the
// repository does not declare or a supertype outside it leaves what follows
// at Strong, and nothing found outside the repository; a type whose
// declarations cannot be listed — every type of a file the parser rejects
// outside a body (an import, an extends clause may be lost), a type whose
// body it rejects — caps what is found in it, and past it, at Candidate. java.lang.Object,
// java.lang.Enum and java.lang.Record are implicit supertypes outside the
// repository whose members the language fixes (ImplicitMembers), as are an
// enum's values() and valueOf(String), a record's accessors and a class's
// default constructor.
//
// Not resolved: a method call on a method's result or on a field of
// another type (no return or field types are followed), a lambda
// parameter of inferred type, a method reference's overload (its target
// type decides), a switch label's enum constant (the selector's type
// decides), an annotation element name, and anything in a region the
// parser rejected. A call's target is its static declaration; dynamic
// dispatch is not modelled.
//
// Invariants: Extract never panics, never returns a Tree-sitter node, keeps
// every byte range inside the source, and bounds its recursion (maxDepth).
package java

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Provider extracts code intelligence from Java files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "java" }

func (p *Provider) Extensions() []string { return []string{".java"} }

// CacheVersion must change whenever extraction semantics change.
func (p *Provider) CacheVersion() string { return "java-1" }

// Diagnostic codes this provider states, beside treediag's.
const diagNestingTooDeep = "nesting_too_deep" // syntax below maxDepth is not observed

func (p *Provider) Extract(_ context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.JavaLanguage()
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
		Diagnostics:  x.diags,
		IdentityOnly: true,
		Package:      x.pkg,
		SourceRoot:   sourceRoot(file, x.pkg),
	}, nil
}

// sourceRoot is the directory where the path of package pkg starts in file's
// path ("" when the file's directory does not end with it).
func sourceRoot(file source.FileID, pkg string) string {
	dir := filepath.ToSlash(filepath.Dir(string(file)))
	if dir == "." {
		dir = ""
	}
	if pkg == "" {
		return dir + "/"
	}
	suffix := strings.ReplaceAll(pkg, ".", "/")
	switch {
	case dir == suffix:
		return "/"
	case strings.HasSuffix(dir, "/"+suffix):
		return strings.TrimSuffix(dir, suffix)
	}
	return ""
}
