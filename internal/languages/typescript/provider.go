// Package typescript implements the TypeScript and TSX language providers.
//
// One implementation serves both grammars: TSX only adds JSX nodes. The
// provider records syntax and structural evidence only (symbols, containment,
// references, module bindings, exports, declared receiver types). It never
// resolves anything and never executes repository code, tsc or tsserver.
package typescript

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Provider extracts TypeScript (.ts) or TSX (.tsx) files.
type Provider struct {
	lang   language.Language
	tsLang func() *ts.Language
}

// NewProvider returns the TypeScript (.ts) provider.
func NewProvider() *Provider {
	return &Provider{lang: "typescript", tsLang: grammars.TypescriptLanguage}
}

// NewTSXProvider returns the TSX (.tsx) provider.
func NewTSXProvider() *Provider {
	return &Provider{lang: "tsx", tsLang: grammars.TsxLanguage}
}

func (p *Provider) Language() language.Language { return p.lang }

// DialectOf: TSX is TypeScript with JSX; .ts and .tsx files share one name
// space (language.Dialect).
func (p *Provider) DialectOf() language.Language { return "typescript" }

func (p *Provider) Extensions() []string {
	if p.lang == "tsx" {
		return []string{".tsx"}
	}
	return []string{".ts"}
}

// CacheVersion must change whenever extraction semantics change.
//
//	"1":   top-level symbols, bare references, path-only imports.
//	"ts-2": members + containment, qualified containers, module bindings /
//	        exports / ModuleSpec candidates, ModuleScoped, ReceiverType,
//	        heritage and JSX references.
//	"ts-3": syntax-error diagnostics (treediag).
//	"ts-4": trees the production parser route misparsed (tsparse).
//	"ts-5": generic calls the parser read as comparisons; type parameters
//	        of nested signatures, mapped-type keys and `infer` names are not
//	        type uses; forest-route trees (tsparse).
//	"ts-6": the recovered type arguments follow TypeScript's lexical,
//	        reserved-word and line-break rules.
//	"ts-7": local scopes (ecmascript) cap names a local shadows; CommonJS
//	        require bindings and module.exports exports; utility types and
//	        locally shadowed names are no receiver type.
//	"ts-8": members of const objects and constructor functions' prototypes,
//	        super references, static and instance members of one name
//	        apart, Extraction.MembersShareNames.
//	"ts-9": the classes of CSS Modules files used through an import
//	        (cssmodules.go), by path evidence.
//	"ts-10": the placement of class members (static or instance,
//	        SymbolDraft.Placement).
//	"ts-11": value reads of declared and bound names (reads.go).
//	"ts-12": CSS Modules classes of `export default s.x` and of a
//	        destructuring of the class map (cssmodules.go).
//	"ts-13": value reads denote values only (no interface or type alias),
//	        none in an enum's members or a with body; calls in a with body
//	        are candidates.
//	"ts-14": no CommonJS export of a name the module may write another value
//	        to: a later spread, computed or same key in the module.exports
//	        object, any other write of exports.a or the exports object
//	        (commonjs.go).
//	"ts-16": CommonJS exports are Mutable (language.ExportDraft): a
//	        resolution through one is a Candidate at most, the exports
//	        object being changeable by routes no static rule closes
//	        (docs/development/web-semantic-fidelity/commonjs-boundary).
//	        ts-15 (an unreleased audit of those routes) is skipped so that
//	        no cache of it is read.
func (p *Provider) CacheVersion() string { return "ts-16" }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	tsLang := p.tsLang()
	tree, err := tsparse.Parse(tsLang, src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{treediag.ParseFailed(file, err)},
		}, nil
	}
	defer tree.Release()

	e := newExtractor(tsLang, src, file, dialectTypeScript)
	e.program(tree.RootNode())
	return language.Extraction{
		Symbols:           e.symbols,
		References:        e.refs,
		Imports:           e.imports,
		Bindings:          e.bindings,
		Exports:           e.exports,
		ModuleScoped:      e.isModule,
		MembersShareNames: true,
		Diagnostics:       treediag.ParseErrors(tree.RootNode(), tsLang, file),
	}, nil
}

func nodeLocation(node *ts.Node, file source.FileID) source.Location {
	return source.Location{
		File: file,
		Range: source.Range{
			Start: source.Position{
				Line:   node.StartPoint().Row + 1,
				Column: node.StartPoint().Column + 1,
			},
			End: source.Position{
				Line:   node.EndPoint().Row + 1,
				Column: node.EndPoint().Column + 1,
			},
		},
	}
}
