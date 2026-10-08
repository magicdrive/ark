// Package languages is the composition root for Ark's language support. It is
// the single, explicit place that wires concrete providers and their
// Tree-sitter grammars into a canonical language.Registry.
//
// Design rules:
//   - internal/language owns the mechanism (Descriptor, Registry, SupportLevel,
//     Provider) and must stay free of any parser dependency.
//   - This package owns the concrete set: which languages exist, each one's
//     provider, grammar, and tested support level.
//   - The registry is built once, explicitly and deterministically, from the
//     ordered spec list below — no init-time self-registration and no global
//     mutable registry. Adding a language = append one spec here.
package languages

import (
	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/php"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/terraform"
	"github.com/magicdrive/ark/internal/languages/typescript"
)

// spec couples a provider with its Tree-sitter grammar and tested support
// level. Language identity and file extensions are taken from the provider
// itself so they have exactly one source of truth.
type spec struct {
	provider     language.Provider
	grammar      func() *ts.Language
	supportLevel language.SupportLevel
}

// specList is the canonical, ordered set of supported languages. This is the
// ONE place to edit when adding a language.
func specList() []spec {
	return []spec{
		{golang.NewProvider(), grammars.GoLanguage, language.SupportLevelContextQualityCertified},
		{typescript.NewProvider(), grammars.TypescriptLanguage, language.SupportLevelContextQualityCertified},
		{typescript.NewTSXProvider(), grammars.TsxLanguage, language.SupportLevelContextQualityCertified},
		{javascript.NewProvider(), grammars.JavascriptLanguage, language.SupportLevelReferences},
		{python.NewProvider(), grammars.PythonLanguage, language.SupportLevelReferences},
		// PHP (PHP-1..8): symbols + members + imports + references + typed
		// relations + resolution + typed graph, certified for agent-oriented
		// context quality. Held at Graph (not ContextQualityCertified) because
		// import/namespace/inherited-member resolution precision is honestly
		// limited vs a compiler; see README.md, "Language Support".
		{php.NewProvider(), grammars.PhpLanguage, language.SupportLevelGraph},
		// Terraform (.tf / .tfvars): declarations, address references,
		// module-scoped resolution, local module outputs and the dependency
		// graph (references / depends_on edges). Graph, not
		// ContextQualityCertified: no context-quality benchmark covers it.
		{terraform.NewProvider(), grammars.HclLanguage, language.SupportLevelGraph},
	}
}

// The registry and grammar map are built once, at package initialization, by
// evaluating build() — no init() func, no self-registration, no post-build
// mutation. Both are immutable for the lifetime of the process.
var registry, grammarByID = build()

func build() (*language.Registry, map[language.Language]func() *ts.Language) {
	specs := specList()
	descriptors := make([]language.Descriptor, 0, len(specs))
	grammarMap := make(map[language.Language]func() *ts.Language, len(specs))
	for _, s := range specs {
		id := s.provider.Language()
		descriptors = append(descriptors, language.Descriptor{
			Language:     id,
			Extensions:   s.provider.Extensions(),
			SupportLevel: s.supportLevel,
			Provider:     s.provider,
		})
		grammarMap[id] = s.grammar
	}
	return language.MustNewRegistry(descriptors...), grammarMap
}

// Registry returns the canonical, immutable language registry.
func Registry() *language.Registry { return registry }

// Grammar returns the Tree-sitter grammar getter for a language identity.
func Grammar(id language.Language) (func() *ts.Language, bool) {
	g, ok := grammarByID[id]
	return g, ok
}
