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
	"github.com/magicdrive/ark/internal/source"
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
func (p *Provider) CacheVersion() string { return "ts-2" }

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	tsLang := p.tsLang()
	parser := ts.NewParser(tsLang)
	tree, err := parser.Parse(src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{{
				Severity: language.SeverityError,
				Message:  "parse failed: " + err.Error(),
				Location: source.Location{File: file},
			}},
		}, nil
	}
	defer tree.Release()

	e := newExtractor(tsLang, src, file)
	e.program(tree.RootNode())
	return language.Extraction{
		Symbols:      e.symbols,
		References:   e.refs,
		Imports:      e.imports,
		Bindings:     e.bindings,
		Exports:      e.exports,
		ModuleScoped: e.isModule,
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
