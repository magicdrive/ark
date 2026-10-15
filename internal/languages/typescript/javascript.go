package typescript

import (
	"bytes"
	"context"
	"path"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/tsparse"
)

// ExtractJavaScript extracts a JavaScript file (tree-sitter-javascript) with
// the extractor TypeScript uses: JavaScript and TypeScript share the
// ECMAScript declarations, module bindings and exports, class members,
// receiver evidence and scopes (internal/languages/internal/ecmascript).
// Only TypeScript has types, and only its grammar needs the generic-call
// correction. JavaScript keeps its own name space (language "javascript"):
// its modules name JavaScript files only (jsModuleSpec).
//
// A file is module-scoped when it imports or exports (ES module) or uses
// CommonJS (require("…"), module.exports, exports.x = …): Node.js runs a
// CommonJS file in a scope of its own, so a name it neither declares nor
// imports is not another file's. A file with neither is a classic script,
// whose top-level names share one global scope, and keeps the proximity rules.
func ExtractJavaScript(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.JavascriptLanguage()
	tree, err := tsparse.Parse(lang, src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{treediag.ParseFailed(file, err)},
		}, nil
	}
	defer tree.Release()

	root := tree.RootNode()
	e := newExtractor(lang, src, file, dialectJavaScript)
	e.program(root)
	// The script itself: the target of an HTML <script src> (path evidence
	// of another language, language.ReferenceDraft.FileIdentity).
	script := language.SymbolDraft{
		Name:      path.Base(string(file)),
		Qualified: string(file),
		Kind:      symbol.KindScript,
		Location:  wholeFile(file, src),
		StartByte: 0,
		EndByte:   uint32(len(src)),
		// No member of the program's API: an overview lists the file's
		// declarations, not the file (verbose maps list it).
		Exported: false,
	}
	return language.Extraction{
		Symbols:           append([]language.SymbolDraft{script}, e.symbols...),
		References:        e.refs,
		Imports:           e.imports,
		Bindings:          e.bindings,
		Exports:           e.exports,
		ModuleScoped:      e.isModule || e.commonJS(root),
		MembersShareNames: true,
		Diagnostics:       treediag.ParseErrors(root, lang, file),
	}, nil
}

// commonJS reports whether the program uses CommonJS: a require of a
// string, module.exports, or an assignment to exports.x.
func (e *extractor) commonJS(n *ts.Node) bool {
	switch e.typ(n) {
	case "call_expression":
		if fn := e.field(n, "function"); e.typ(fn) == "identifier" && e.text(fn) == "require" {
			if args := e.field(n, "arguments"); args != nil && args.NamedChildCount() > 0 && e.typ(args.NamedChild(0)) == "string" {
				return true
			}
		}
	case "member_expression":
		obj, prop := e.field(n, "object"), e.field(n, "property")
		if e.typ(obj) == "identifier" && e.text(obj) == "module" && e.text(prop) == "exports" {
			return true
		}
	case "assignment_expression":
		if left := e.field(n, "left"); e.typ(left) == "member_expression" {
			if obj := e.field(left, "object"); e.typ(obj) == "identifier" && e.text(obj) == "exports" {
				return true
			}
		}
	}
	for i := range n.NamedChildCount() {
		if e.commonJS(n.NamedChild(i)) {
			return true
		}
	}
	return false
}

// wholeFile is the location of all of src: from its first byte to its end
// (lines by '\n', columns in bytes, as tree-sitter's points).
func wholeFile(file source.FileID, src []byte) source.Location {
	line := uint32(bytes.Count(src, []byte("\n")) + 1)
	col := uint32(len(src) - (bytes.LastIndexByte(src, '\n') + 1) + 1)
	return source.Location{File: file, Range: source.Range{
		Start: source.Position{Line: 1, Column: 1},
		End:   source.Position{Line: line, Column: col},
	}}
}

// EmbeddedScript is what JavaScript embedded in another file (an HTML
// <script> element's text) states, located in that text (lines and
// columns from 1:1 at its first byte, byte offsets from 0): the host maps it.
type EmbeddedScript struct {
	Symbols    []language.SymbolDraft
	References []language.ReferenceDraft
	Imported   map[string]bool // the local names its imports bind
	// Locals are the names it declares at its top level other than as
	// symbols (a destructuring pattern; and, over-approximated, those of a
	// top-level block, loop or catch clause): ecmascript.ProgramLocals.
	Locals      map[string]bool
	Diagnostics []language.Diagnostic
}

// ExtractEmbeddedJavaScript extracts src, the text of a <script> element of
// the file file, as ExtractJavaScript does a file, without the script
// symbol (the text is no file of its own), module bindings or exports.
func ExtractEmbeddedJavaScript(file source.FileID, src []byte) EmbeddedScript {
	lang := grammars.JavascriptLanguage()
	tree, err := tsparse.Parse(lang, src)
	if err != nil {
		return EmbeddedScript{Diagnostics: []language.Diagnostic{treediag.ParseFailed(file, err)}}
	}
	defer tree.Release()
	root := tree.RootNode()
	e := newExtractor(lang, src, file, dialectJavaScript)
	e.program(root)
	out := EmbeddedScript{
		Symbols:     e.symbols,
		References:  e.refs,
		Imported:    map[string]bool{},
		Diagnostics: treediag.ParseErrors(root, lang, file),
	}
	for _, b := range e.bindings {
		out.Imported[b.Local] = true
	}
	out.Locals = e.moduleLocals.Values
	return out
}
