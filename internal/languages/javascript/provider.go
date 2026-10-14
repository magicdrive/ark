// Package javascript implements the JavaScript language provider.
//
// JavaScript is extracted by the extractor the TypeScript provider uses
// (typescript.ExtractJavaScript): the two languages share the ECMAScript
// declarations, module bindings and exports, class members, receiver
// evidence and scopes. JavaScript keeps its own name space: a JavaScript
// file and a TypeScript file never resolve each other's names.
package javascript

import (
	"context"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/source"
)

// Provider extracts JavaScript source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "javascript" }
func (p *Provider) Extensions() []string {
	return []string{".js", ".mjs", ".cjs", ".jsx"}
}

// CacheVersion must change whenever extraction semantics change.
//
//	"4":    top-level declarations, calls and constructions, import paths.
//	"js-5": the shared ECMAScript extractor (typescript.ExtractJavaScript):
//	        class members, module bindings / exports / candidates,
//	        module scoping (ES modules and CommonJS), local scopes.
//	"js-6": members of const objects and constructor functions' prototypes,
//	        super references, Extraction.MembersShareNames.
//	"js-7": the script symbol of each file (symbol.KindScript), the target
//	        of an HTML <script src>.
//	"js-8": the classes of CSS Modules files used through an import
//	        (typescript/cssmodules.go), by path evidence.
//	"js-9": the placement of class and prototype members (static or
//	        instance, SymbolDraft.Placement); CommonJS exports a later
//	        assignment overwrites are not stated.
//	"js-10": value reads of declared and bound names (typescript/reads.go).
//	"js-11": CSS Modules classes of `export default s.x` and of a
//	        destructuring of the class map.
//	"js-12": value reads denote values only, none in a with body; calls in
//	        a with body are candidates.
//
// JavaScript is extracted by the TypeScript provider's extractor
// (typescript.ExtractJavaScript): the version joins the TypeScript
// provider's, so a change of the shared extractor invalidates JavaScript
// (and the HTML documents whose scripts it reads) too.
func (p *Provider) CacheVersion() string {
	return "js-12+" + typescript.NewProvider().CacheVersion()
}

func (p *Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	return typescript.ExtractJavaScript(ctx, file, src)
}
