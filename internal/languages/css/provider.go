// Package css is Ark's CSS provider (.css).
//
// It states what a stylesheet's own text fixes: the stylesheet, its
// @keyframes, @font-face families, @property registrations and custom
// property declarations, the local classes of a CSS Modules file
// (.module.css), and the references to them — animation names, font
// families, var(), composes — @import links to other stylesheets, and links
// to asset files (images, fonts: url(), src(), image-set(); internal/asset).
// The stylesheet is read as CSS Syntax Level 3 has it (reader.go,
// tokenizer.go; decisions D24, D25 of docs/development/web-semantic-fidelity).
//
// Identity. Every file is IdentityOnly: a reference reaches a declaration
// only by its qualified identity, "<FileID> @keyframes <name>",
// "<FileID> @font-face <family>", "<FileID> @property --x",
// "<FileID> --x", "<FileID> .<class>" — never by a name match in another
// file: which stylesheets one document loads together is not CSS's to say.
//
// Confidence. Names resolve within their own stylesheet: an animation name
// or a font family Strong at most (another stylesheet of the page may
// define the name later and win), a custom property Candidate at most (its
// value comes from the element tree's inheritance), a local class of the
// same CSS Modules file Exact, one composed from another file Strong (the
// bundler resolves the path), an @import Strong.
package css

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/asset"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/weburl"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

const cacheVersion = "css-4"

// Diagnostic codes, "css_" and the reader's problem: see severity.
const diagPrefix = "css_"

// severity is the severity of each problem the reader reports: an error
// where written content is lost or cannot be read, a warning where the
// standard's recovery loses nothing or the construct is a common hack.
var severity = map[string]language.DiagnosticSeverity{
	"bad_string":            language.SeverityError,
	"bad_url":               language.SeverityError,
	"rule_from_declaration": language.SeverityError,
	"stray_in_prelude":      language.SeverityError,
	"encoding":              language.SeverityError,
	"invalid":               language.SeverityWarning,
	"eof_in_block":          language.SeverityWarning,
	"eof_in_comment":        language.SeverityWarning,
	"eof_in_string":         language.SeverityWarning,
	"eof_in_url":            language.SeverityWarning,
	"replacement_character": language.SeverityWarning,
	"charset":               language.SeverityWarning,
	"too_deep":              language.SeverityWarning,
}

var messages = map[string]string{
	"bad_string":            "a string broken by a newline: the declaration is dropped",
	"bad_url":               "an invalid url(): the declaration is dropped",
	"rule_from_declaration": "a declaration followed by a rule without \";\" is read as one rule: the declaration is lost",
	"stray_in_prelude":      "a \";\" or \"}\" in a selector: the rule is dropped",
	"encoding":              "a UTF-16 byte order mark: the stylesheet is not read as UTF-8",
	"invalid":               "a rule without a block, dropped",
	"eof_in_block":          "a block is left open at the end of the file",
	"eof_in_comment":        "a comment is left open at the end of the file",
	"eof_in_string":         "a string is left open at the end of the file",
	"eof_in_url":            "a url() is left open at the end of the file",
	"replacement_character": "a NUL or an invalid UTF-8 byte, read as U+FFFD",
	"charset":               "an @charset other than UTF-8: the stylesheet is read as UTF-8",
	"too_deep":              "blocks nested deeper than the parser reads",
}

// Provider implements language.Provider for CSS.
type Provider struct{}

// NewProvider returns the CSS provider.
func NewProvider() *Provider { return &Provider{} }

func (*Provider) Language() language.Language { return "css" }
func (*Provider) Extensions() []string        { return []string{".css"} }
func (*Provider) CacheVersion() string        { return cacheVersion }

// Extract states the stylesheet's declarations and references.
func (*Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	if err := ctx.Err(); err != nil {
		return language.Extraction{}, err
	}
	e := &extractor{file: file, src: src, lines: newLines(src), r: read(src),
		module: strings.HasSuffix(strings.ToLower(string(file)), ".module.css")}
	e.run()
	return language.Extraction{
		Symbols:      e.symbols,
		References:   e.refs,
		Diagnostics:  e.diags,
		IdentityOnly: true,
	}, nil
}

// Embedded is what a stylesheet embedded in another file (an HTML <style>
// element's text) states: ExtractEmbedded.
type Embedded struct {
	Symbols     []language.SymbolDraft
	References  []language.ReferenceDraft
	Diagnostics []language.Diagnostic
}

// ExtractEmbedded reads src, the text of an HTML <style> element of the file
// file, as a stylesheet of that file. Its declarations have the identities a
// stylesheet of the file's path would ("<FileID> @keyframes <name>", ...),
// so the style elements of one document share them; there is no stylesheet
// symbol (the file declares itself) and no CSS Modules class. Locations and
// byte offsets are in src (1:1 and 0 at its first byte): the host maps them.
// An @import is path evidence (FileIdentity), resolved against the file's
// path, and stated only with imports (a document with a <base> resolves it
// elsewhere).
func ExtractEmbedded(file source.FileID, src []byte, imports bool) Embedded {
	e := &extractor{file: file, src: src, lines: newLines(src), r: read(src), embedded: true, embeddedImports: imports}
	e.run()
	return Embedded{Symbols: e.symbols, References: e.refs, Diagnostics: e.diags}
}

type extractor struct {
	file       source.FileID
	src        []byte
	lines      lines
	r          *reader
	module     bool // a CSS Modules file
	symbols    []language.SymbolDraft
	refs       []language.ReferenceDraft
	diags      []language.Diagnostic
	classes    map[string]bool // CSS Modules classes stated
	imports    bool            // an @import here is still valid (before any other rule)
	importSeen bool            // an @import came before

	embedded        bool // src is a part of another file (ExtractEmbedded)
	embeddedImports bool // an embedded stylesheet states its @imports
}

func (e *extractor) qualified(rest string) string { return string(e.file) + " " + rest }

func (e *extractor) symbol(name, rest string, kind symbol.SymbolKind, start, end int) {
	e.symbols = append(e.symbols, language.SymbolDraft{
		Name:      name,
		Qualified: e.qualified(rest),
		Kind:      kind,
		Location:  e.loc(start, end),
		StartByte: uint32(start),
		EndByte:   uint32(end),
		Parent:    string(e.file),
		Exported:  true,
	})
}

func (e *extractor) ref(t token, name, rest, kind, cap string, target symbol.SymbolKind) {
	e.refs = append(e.refs, language.ReferenceDraft{
		Name:                 name,
		Kind:                 kind,
		Container:            string(e.file),
		Location:             e.loc(t.start, t.end),
		NameQualified:        rest,
		ConfidenceCap:        cap,
		TargetKinds:          string(target),
		IdentityInRepository: true,
	})
}

func (e *extractor) run() {
	if !e.embedded {
		e.symbols = append(e.symbols, language.SymbolDraft{
			Name:      path.Base(string(e.file)),
			Qualified: string(e.file),
			Kind:      symbol.KindStylesheet,
			Location:  e.loc(0, len(e.src)),
			EndByte:   uint32(len(e.src)),
			Exported:  true,
		})
	}
	e.classes = map[string]bool{}
	e.imports = true
	for k, it := range e.r.items {
		if it.parent == -1 && it.kind != itemProblem && it.kind != itemDecl {
			e.importOrder(it)
		}
		switch it.kind {
		case itemProblem:
			e.diags = append(e.diags, language.Diagnostic{
				Severity: severity[it.name],
				Code:     diagPrefix + it.name,
				Message:  messages[it.name],
				Location: e.loc(it.start, it.end),
			})
		case itemAtRule:
			e.atRule(k, it)
		case itemRule:
			if e.module {
				e.moduleClasses(it)
			}
		case itemDecl:
			e.declaration(it)
		}
	}
}

// templated reports whether the bytes of an item hold the syntax of a
// template language rendering the stylesheet: its names are not the text's.
func (e *extractor) templated(start, end int) bool {
	s := string(e.src[start:end])
	for _, m := range []string{"{{", "{%", "<%", "<?", "${"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// nonWS returns the tokens of [from, to) that are not whitespace.
func (e *extractor) nonWS(from, to int) []token {
	var out []token
	for _, t := range e.r.toks[from:to] {
		if t.typ != tWhitespace {
			out = append(out, t)
		}
	}
	return out
}

// cssWide are the keywords every property accepts; never a name.
var cssWide = map[string]bool{"initial": true, "inherit": true, "unset": true, "revert": true, "revert-layer": true, "default": true}

func lower(s string) string { return strings.ToLower(s) }

// importOrder keeps e.imports: an @import is valid only before every rule
// but @charset, @layer statements and other @imports (CSS Cascade 5 §2),
// and only at the top level.
func (e *extractor) importOrder(it item) {
	if it.kind == itemAtRule {
		switch lower(it.name) {
		case "import":
			if !it.block {
				e.importSeen = true
			}
			return
		case "charset":
			return
		case "layer":
			// A @layer statement before the first @import declares layers;
			// after one it is a rule between @imports, which makes the
			// later ones invalid (CSS Cascade 5 §2).
			if !it.block && !e.importSeen {
				return
			}
		}
	}
	e.imports = false
}

// inStyleRule reports whether an item is nested in a style rule: @keyframes,
// @font-face and @property are not rules there (CSS Nesting 1 allows
// conditional group rules, @layer and @scope).
func (e *extractor) inStyleRule(it item) bool {
	for p := it.parent; p >= 0; p = e.r.items[p].parent {
		if e.r.items[p].kind == itemRule {
			return true
		}
	}
	return false
}

func (e *extractor) atRule(k int, it item) {
	if e.templated(it.start, it.end) {
		return
	}
	switch lower(it.name) {
	case "keyframes", "-webkit-keyframes", "-moz-keyframes", "-o-keyframes", "font-face", "property":
		if e.inStyleRule(it) {
			return
		}
	}
	switch lower(it.name) {
	case "keyframes", "-webkit-keyframes", "-moz-keyframes", "-o-keyframes":
		p := e.nonWS(it.from, it.to)
		if len(p) != 1 || !it.block {
			return
		}
		switch t := p[0]; t.typ {
		case tIdent:
			if l := lower(t.value); l == "none" || cssWide[l] {
				return
			}
			e.symbol(t.value, "@keyframes "+t.value, symbol.KindKeyframes, it.start, it.end)
		case tString:
			if t.value != "" {
				e.symbol(t.value, "@keyframes "+t.value, symbol.KindKeyframes, it.start, it.end)
			}
		}
	case "font-face":
		// The last valid font-family descriptor names the face (CSS Fonts 4
		// §4: a repeated descriptor's last declaration is used, an invalid
		// one ignored); a face without src is no face.
		if !it.block {
			return
		}
		name, src := "", false
		for _, d := range e.r.items[k+1:] {
			if d.depth <= it.depth {
				break
			}
			if d.kind != itemDecl || d.parent != k {
				continue
			}
			switch lower(d.name) {
			case "font-family":
				if fams := e.families(d); len(fams) == 1 {
					name = fams[0].name
				}
			case "src":
				src = true
			}
		}
		if name != "" && src {
			e.symbol(name, "@font-face "+name, symbol.KindFontFace, it.start, it.end)
		}
	case "property":
		p := e.nonWS(it.from, it.to)
		if len(p) == 1 && p[0].typ == tIdent && strings.HasPrefix(p[0].value, "--") && it.block {
			e.symbol(p[0].value, "@property "+p[0].value, symbol.KindPropertyRegistration, it.start, it.end)
		}
	case "import":
		if it.parent == -1 && e.imports {
			e.importRule(it)
		}
	}
}

// importRule states the stylesheet an @import names by a relative URL.
func (e *extractor) importRule(it item) {
	if it.block {
		return // @import has no block: such a rule is invalid
	}
	p := e.nonWS(it.from, it.to)
	if len(p) == 0 {
		return
	}
	t, v := p[0], ""
	switch {
	case t.typ == tString || t.typ == tURL:
		v = t.value
	case t.typ == tFunction && lower(t.value) == "url" && len(p) > 1 && p[1].typ == tString:
		t, v = p[1], p[1].value
	default:
		return
	}
	if e.embedded && !e.embeddedImports {
		return
	}
	target, _, ok := weburl.Relative(string(e.file), weburl.Trim(v), ".css")
	if !ok {
		return
	}
	e.ref(t, path.Base(target), target, string(reference.KindResourceLink), "strong", symbol.KindStylesheet)
	if e.embedded {
		// Path evidence from another language's file.
		e.refs[len(e.refs)-1].FileIdentity = true
	}
}

type family struct {
	name    string
	at, end token // the family's first and last tokens
}

// families reads a font-family value: a comma-separated list of families,
// each a string or a sequence of idents (joined by single spaces), ASCII
// lower-cased; a generic family or a CSS-wide keyword names no face, and a
// value holding anything else (var(), numbers) none.
func (e *extractor) families(d item) []family {
	var out []family
	var seg []token
	flush := func() bool {
		switch {
		case len(seg) == 0:
			return false
		case len(seg) == 1 && seg[0].typ == tString:
			if seg[0].value == "" {
				return false
			}
			out = append(out, family{lower(seg[0].value), seg[0], seg[0]})
		default:
			words := make([]string, 0, len(seg))
			for _, t := range seg {
				if t.typ != tIdent {
					return false
				}
				words = append(words, t.value)
			}
			if name := lower(strings.Join(words, " ")); !generic[name] && !cssWide[name] {
				out = append(out, family{name, seg[0], seg[len(seg)-1]})
			}
		}
		seg = nil
		return true
	}
	for i := d.from; i < e.valueEnd(d); i++ {
		switch t := e.r.toks[i]; t.typ {
		case tWhitespace:
		case tComma:
			if !flush() {
				return nil
			}
		default:
			seg = append(seg, t)
		}
	}
	if !flush() {
		return nil
	}
	return out
}

var generic = map[string]bool{
	"serif": true, "sans-serif": true, "cursive": true, "fantasy": true, "monospace": true, "system-ui": true,
	"ui-serif": true, "ui-sans-serif": true, "ui-monospace": true, "ui-rounded": true, "math": true,
	"emoji": true, "fangsong": true,
}

// animationKeywords are the keywords of the animation shorthand: an ident
// of one is no animation name.
var animationKeywords = map[string]bool{
	"none": true, "ease": true, "ease-in": true, "ease-out": true, "ease-in-out": true, "linear": true,
	"step-start": true, "step-end": true, "infinite": true, "normal": true, "reverse": true,
	"alternate": true, "alternate-reverse": true, "forwards": true, "backwards": true, "both": true,
	"running": true, "paused": true, "auto": true, "replace": true, "add": true, "accumulate": true,
}

func (e *extractor) declaration(d item) {
	if strings.HasPrefix(d.name, "--") {
		e.symbol(d.name, d.name, symbol.KindCustomProperty, d.start, d.end)
	}
	if e.templated(d.start, d.end) {
		return
	}
	e.vars(d)
	if !strings.HasPrefix(d.name, "--") {
		// A custom property's url() is resolved where it is substituted.
		e.urls(d)
	}
	inFontFace := d.parent >= 0 && lower(e.r.items[d.parent].name) == "font-face" && e.r.items[d.parent].kind == itemAtRule
	switch lower(d.name) {
	case "animation-name":
		for _, seg := range e.segments(d) {
			if len(seg) == 1 {
				e.animationName(seg[0], false)
			}
		}
	case "animation":
		for _, seg := range e.segments(d) {
			var names []token
			for _, t := range seg {
				if t.typ == tString || t.typ == tIdent && !animationKeywords[lower(t.value)] && !cssWide[lower(t.value)] {
					names = append(names, t)
				}
			}
			if len(names) == 1 {
				e.animationName(names[0], true)
			}
		}
	case "font-family":
		if inFontFace {
			return // the face's own name (the at-rule's symbol)
		}
		for _, f := range e.families(d) {
			e.ref(token{start: f.at.start, end: f.end.end}, f.name, e.qualified("@font-face "+f.name), string(reference.KindValueReference), "strong", symbol.KindFontFace)
		}
	case "composes":
		if e.module {
			e.composes(d)
		}
	}
}

// urls states the asset files (internal/asset) a declaration's value names
// by a relative URL: url(x), url("x"), src("x") and the strings of
// image-set() (and -webkit-image-set()). Path evidence (FileIdentity), Strong
// at most, unresolved when the file is missing or no asset.
func (e *extractor) urls(d item) {
	if e.embedded && !e.embeddedImports {
		return // a document with a <base>: its URLs resolve elsewhere
	}
	link := func(t token, v string) {
		target, _, ok := weburl.Relative(string(e.file), weburl.Trim(v), asset.Extensions()...)
		if !ok {
			return
		}
		e.ref(t, path.Base(target), target, string(reference.KindResourceLink), "strong", symbol.KindAsset)
		e.refs[len(e.refs)-1].FileIdentity = true
	}
	next := func(i int) int {
		for i < d.to && e.r.toks[i].typ == tWhitespace {
			i++
		}
		return i
	}
	for i := d.from; i < d.to; i++ {
		t := e.r.toks[i]
		switch {
		case t.typ == tURL:
			link(t, t.value)
		case t.typ == tFunction && (lower(t.value) == "url" || lower(t.value) == "src"):
			if k := next(i + 1); k < d.to && e.r.toks[k].typ == tString {
				link(e.r.toks[k], e.r.toks[k].value)
			}
		case t.typ == tFunction && (lower(t.value) == "image-set" || lower(t.value) == "-webkit-image-set"):
			// A string at the start of an argument is an image's URL; a
			// nested function or block is skipped whole (its commas are
			// not the arguments').
			end := min(e.r.match[i], d.to)
			if end > i+1 && e.r.toks[end-1].typ == tRParen {
				end-- // the closing ) (none when the input ends first)
			}
			start := true
			for k := i + 1; k < end; k++ {
				switch tk := e.r.toks[k]; {
				case tk.typ == tWhitespace:
				case tk.typ == tString && start:
					link(tk, tk.value)
					start = false
				case tk.typ == tComma:
					start = true
				case tk.typ == tFunction || tk.typ == tLParen || tk.typ == tLBracket || tk.typ == tLBrace:
					k = e.r.match[k] - 1 // past its closing token
					start = false
				default:
					start = false
				}
			}
		}
	}
}

// segments splits a declaration value at its top-level commas into the
// non-whitespace tokens of each part; a part holding a block or function
// is nil (not a list of names).
func (e *extractor) segments(d item) [][]token {
	var out [][]token
	var cur []token
	ok := true
	for i := d.from; i < e.valueEnd(d); i++ {
		t := e.r.toks[i]
		switch t.typ {
		case tWhitespace:
		case tComma:
			if !ok {
				cur = nil
			}
			out = append(out, cur)
			cur, ok = nil, true
		case tFunction, tLParen, tLBracket, tLBrace:
			ok = false
			i = e.r.match[i] - 1
		default:
			cur = append(cur, t)
		}
	}
	if !ok {
		cur = nil
	}
	return append(out, cur)
}

// valueEnd is the end of a declaration's value without a trailing
// "!important" (its "!", whitespace and "important").
func (e *extractor) valueEnd(d item) int {
	k := d.to - 1
	for k >= d.from && e.r.toks[k].typ == tWhitespace {
		k--
	}
	if k < d.from || e.r.toks[k].typ != tIdent || lower(e.r.toks[k].value) != "important" {
		return d.to
	}
	k--
	for k >= d.from && e.r.toks[k].typ == tWhitespace {
		k--
	}
	if k < d.from || e.r.toks[k].typ != tDelim || e.r.toks[k].value != "!" {
		return d.to
	}
	return k
}

// animationName states a reference to the @keyframes a name names.
func (e *extractor) animationName(t token, shorthand bool) {
	switch t.typ {
	case tIdent:
		if l := lower(t.value); l == "none" || cssWide[l] || shorthand && animationKeywords[l] {
			return
		}
	case tString:
		if t.value == "" {
			return
		}
	default:
		return
	}
	e.ref(t, t.value, e.qualified("@keyframes "+t.value), string(reference.KindValueReference), "strong", symbol.KindKeyframes)
}

// vars states the custom properties var() names in a value (nested ones and
// fallbacks included).
func (e *extractor) vars(d item) {
	for i := d.from; i < d.to; i++ {
		t := e.r.toks[i]
		if t.typ != tFunction || lower(t.value) != "var" {
			continue
		}
		k := i + 1
		for k < d.to && e.r.toks[k].typ == tWhitespace {
			k++
		}
		if k < d.to {
			if a := e.r.toks[k]; a.typ == tIdent && strings.HasPrefix(a.value, "--") {
				e.ref(a, a.value, e.qualified(a.value), string(reference.KindValueReference), "candidate", symbol.KindCustomProperty)
			}
		}
	}
}

// composes states the classes a CSS Modules composition names: of this
// file (Exact), of another by a relative path (Strong), none "from global".
func (e *extractor) composes(d item) {
	ts := e.nonWS(d.from, d.to)
	var names []token
	k := 0
	for ; k < len(ts) && ts[k].typ == tIdent && lower(ts[k].value) != "from"; k++ {
		names = append(names, ts[k])
	}
	switch {
	case k == len(ts):
		for _, n := range names {
			e.ref(n, n.value, e.qualified("."+n.value), string(reference.KindValueReference), "", symbol.KindCSSClass)
		}
	case k+2 == len(ts) && ts[k+1].typ == tString:
		target, _, ok := weburl.Relative(string(e.file), weburl.Trim(ts[k+1].value), ".css")
		if !ok || !strings.HasSuffix(lower(target), ".module.css") {
			return
		}
		for _, n := range names {
			e.ref(n, n.value, target+" ."+n.value, string(reference.KindValueReference), "strong", symbol.KindCSSClass)
		}
	}
}

// moduleClasses states the local classes of a CSS Modules rule's selector,
// as postcss-modules-local-by-default (css-loader) reads it: a class
// selector is local unless the selector is in global mode — inside
// :global(…), or after :global (without parentheses) up to the end of that
// selector (a "," ends it), until :local. A pseudo-class's argument list
// (:is(…), :not(…), …) starts in the mode of where it stands, and changes
// inside it stay inside. A class is stated once, at its first selector.
func (e *extractor) moduleClasses(it item) {
	if e.templated(it.start, it.end) {
		return
	}
	var walk func(from, to int, entry bool)
	walk = func(from, to int, entry bool) {
		global := entry
		for i := from; i < to; i++ {
			t := e.r.toks[i]
			switch {
			case t.typ == tComma:
				global = entry
			case t.typ == tColon && i+1 < to && e.r.toks[i+1].typ == tIdent && lower(e.r.toks[i+1].value) == "global":
				global = true
				i++
			case t.typ == tColon && i+1 < to && e.r.toks[i+1].typ == tIdent && lower(e.r.toks[i+1].value) == "local":
				global = false
				i++
			case t.typ == tColon && i+1 < to && e.r.toks[i+1].typ == tFunction:
				f := e.r.toks[i+1]
				end := e.r.match[i+1] - 1 // the ")" (or past the end)
				switch lower(f.value) {
				case "global":
					walk(i+2, end, true)
				case "local":
					walk(i+2, end, false)
				default:
					walk(i+2, end, global)
				}
				i = end
			case t.typ == tDelim && t.value == "." && i+1 < to && e.r.toks[i+1].typ == tIdent && e.r.toks[i+1].start == t.end:
				n := e.r.toks[i+1]
				if !global && !e.classes[n.value] {
					e.classes[n.value] = true
					e.symbol(n.value, "."+n.value, symbol.KindCSSClass, t.start, n.end)
				}
				i++
			case t.typ == tFunction || t.typ == tLParen || t.typ == tLBracket:
				end := e.r.match[i] - 1
				if t.typ == tFunction || t.typ == tLParen {
					walk(i+1, end, global)
				}
				i = end
			}
		}
	}
	walk(it.from, it.to, false)
}

// lines maps byte offsets to 1-based line and byte column positions (lines
// end at '\n', as tree-sitter's points do for the other providers).
type lines []int

func newLines(src []byte) lines {
	l := lines{0}
	for i, c := range src {
		if c == '\n' {
			l = append(l, i+1)
		}
	}
	return l
}

func (l lines) pos(off int) source.Position {
	i := sort.Search(len(l), func(i int) bool { return l[i] > off }) - 1
	return source.Position{Line: uint32(i + 1), Column: uint32(off - l[i] + 1)}
}

func (e *extractor) loc(start, end int) source.Location {
	return source.Location{File: e.file, Range: source.Range{Start: e.lines.pos(start), End: e.lines.pos(end)}}
}
