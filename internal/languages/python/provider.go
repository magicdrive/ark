// Package python implements the Python language provider.
//
// The provider reads one file and states what Python's own rules prove about
// it; it never resolves anything, never consults another file and never
// executes code.
//
// Declarations: functions, classes and methods at any depth (a block —
// if, try, with, for, while — is no scope), nested functions and classes
// (Qualified "outer.inner", Parent "outer"), module variables and class
// attributes (the first plain or annotated assignment of a name), and the
// instance attributes a method assigns through its proven self
// (`self.x = …`, a field of the class). A @typing.overload signature and a
// property's @x.setter / .getter / .deleter are no declarations of their
// own. A function or class under a decorator the provider does not know
// states SymbolDraft.ConfidenceCap "candidate", and so do the members of such
// a class: its name is bound to the decorator's result, which may be another
// object (decorators.go).
//
// Scopes (scope.go): module, class, function, lambda, comprehension and the
// annotation scope of PEP 695 type parameters, with Python's LEGB rules — a
// name bound anywhere in a function is local to all of it, a class body is
// no scope of its methods, global / nonlocal, walrus targets bind in the
// enclosing function, a comprehension's first iterable belongs to the
// enclosing scope. A reference whose name (or receiver root) a local binding
// may denote, a module name bound in several ways (a declaration and an
// import, or a binding no declaration states: for, with, except, del, an
// augmented assignment, a match capture, a global write), a name a later
// star import may rebind and a name nothing binds (a builtin, a star import)
// is capped at Candidate (refs.go, capFor).
//
// Modules (modules.go): Extraction.ModuleScoped — a name resolves only
// through the file's own declarations and its import bindings, never by
// proximity or uniqueness. Module-level imports are BindingDrafts whose
// ModuleSpec candidates follow from the importing file's path alone: a
// relative import from its package directory; an absolute one from every
// ancestor directory that is no package (ModuleCandidate.Unless: its
// __init__.py) and src/, the roots sys.path holds as Ark models it. The
// module's attributes are its ExportDrafts: every module-level name bound by
// declarations only or by imports only.
//
// Receivers: self (cls) is the first parameter of a function defined in a
// class body that no unknown decorator wraps and nothing rebinds
// (ReceiverType: the class); super().m() in a class with one plain base B is
// B's m, capped at Strong (cooperative multiple inheritance may reach
// another class at run time). Any other receiver has no type evidence.
//
// Not modelled (unresolved or Candidate, never guessed): getattr / setattr
// and computed calls (Dynamic), monkey patching, importlib and __import__,
// metaclass-generated attributes, module __getattr__, the types of local
// variables, names a star import brings, string annotations, Python 2
// implicit relative imports.
package python

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Provider extracts Python source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "python" }
func (p *Provider) Extensions() []string        { return []string{".py", ".pyw"} }

// CacheVersion must change whenever extraction semantics change.
//
//	"4": top-level definitions, calls and import paths.
//	"5": scopes, members, nested declarations, variables, import bindings
//	     and exports (ModuleScoped), inheritance, type uses, decorators,
//	     self / cls / super receivers, Dynamic calls, declaration caps.
func (p *Provider) CacheVersion() string { return "5" }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.PythonLanguage()
	tree, err := tsparse.Parse(lang, src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{treediag.ParseFailed(file, err)},
		}, nil
	}
	defer tree.Release()

	root := tree.RootNode()
	e := newExtractor(lang, src, file)
	e.declare(root)
	e.reference(root)
	e.moduleBindings()
	return language.Extraction{
		Symbols:           e.symbols,
		References:        e.refs,
		Imports:           e.imports,
		Bindings:          e.bindings,
		Exports:           e.exports,
		ModuleScoped:      true,
		MembersShareNames: true,
		LexicalNesting:    true,
		Diagnostics:       treediag.ParseErrors(root, lang, file),
	}, nil
}
