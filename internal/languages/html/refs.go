package html

import (
	"path"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/languages/internal/weburl"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// IDREF attributes: an id (single) or a whitespace-separated list of ids
// (list) of the same tree scope. elements nil means any element of the
// namespace.
type idref struct {
	list     bool
	elements map[string]bool
}

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// htmlIDRefs are the IDREF attributes of HTML elements.
var htmlIDRefs = map[string]idref{
	"for":  {elements: set("label")},
	"form": {elements: set("button", "fieldset", "input", "object", "output", "select", "textarea")},
	// list: input in a state that has a suggestions list (listInput below)
	// headers: td, th; popovertarget: button, input in a button state
	// (popoverTarget below)
	"headers":    {list: true, elements: set("td", "th")},
	"commandfor": {elements: set("button")},
	"itemref":    {list: true},
}

// ariaIDRefs are the ARIA relationship attributes, on any element of any
// namespace: an IDREFS (list true) or one ID reference, whose whole value
// is the id (ARIA 1.2; aria-details and aria-errormessage are lists only
// from ARIA 1.3, so read as one id — a value with whitespace names none).
var ariaIDRefs = map[string]bool{
	"aria-controls": true, "aria-describedby": true, "aria-flowto": true, "aria-labelledby": true, "aria-owns": true,
	"aria-activedescendant": false, "aria-details": false, "aria-errormessage": false,
}

// svgPaint are the SVG presentation attributes whose value may be a
// url(#id) reference to an element of the document.
var svgPaint = set("fill", "stroke", "clip-path", "mask", "filter", "marker-start", "marker-mid", "marker-end")

// attrRefs states the references attribute a of tag t makes.
func (e *extractor) attrRefs(t *tag, a attr, scope string) {
	if hasTemplateSyntax(a.val) {
		return
	}
	if ok, srcset := e.assetAttr(t, a); ok {
		e.assets(t, a, srcset)
		if t.name != "link" {
			return
		}
		// A <link> may also be a stylesheet (rel="icon stylesheet"): its
		// rel tokens are read below too.
	}
	switch t.namespace {
	case "":
		if r, ok := htmlIDRefs[a.key]; ok && (r.elements == nil || r.elements[t.name]) {
			e.idrefs(t, a, scope, r.list)
			return
		}
		if a.key == "for" && t.name == "output" { // an IDREFS, unlike label's
			e.idrefs(t, a, scope, true)
			return
		}
		if a.key == "list" && listInput(t) {
			e.idrefs(t, a, scope, false)
			return
		}
		if a.key == "popovertarget" && popoverTarget(t) {
			e.idrefs(t, a, scope, false)
			return
		}
		switch {
		case a.key == "href" && (t.name == "a" || t.name == "area"):
			e.link(t, a, true)
			return
		case a.key == "src" && (t.name == "iframe" || t.name == "frame"):
			e.link(t, a, false) // "#x" loads another instance of the document
			return
		case a.key == "href" && t.name == "link":
			rel, ok := e.attrState(t, "rel")
			if !ok {
				return
			}
			switch r := tokens(rel); {
			case r["stylesheet"] && e.stylesheetLink(t):
				e.resource(t, a, symbol.KindStylesheet, ".css")
			case r["modulepreload"] && e.modulePreload(t):
				e.resource(t, a, symbol.KindScript, ".js", ".mjs")
			}
			return
		case a.key == "src" && t.name == "script":
			if e.loadsJavaScript(t) {
				e.resource(t, a, symbol.KindScript, ".js", ".mjs")
			}
			return
		}
	case "svg":
		switch {
		case t.name == "a" && (a.key == "href" || a.key == "xlink:href"):
			// An SVG hyperlink navigates as an HTML one: its fragment names
			// the document's element.
			e.link(t, a, true)
			return
		case a.key == "href" || a.key == "xlink:href":
			e.fragmentOnly(t, a, scope)
			return
		case svgPaint[a.key]:
			e.paint(t, a, scope)
			return
		}
	}
	if list, ok := ariaIDRefs[a.key]; ok {
		e.idrefs(t, a, scope, list)
	}
}

// inputType is t's type attribute, ASCII lower-cased and trimmed ("" when
// absent: the Text state).
func inputType(t *tag) string {
	for _, a := range t.attrs {
		if a.key == "type" {
			return asciiLower(a.val) // an enumerated attribute: no whitespace trimmed
		}
	}
	return ""
}

// listInput reports whether t's list attribute applies: an input in a state
// with suggestions (text, search, url, tel, email, the date and time states,
// number, range, color; an unknown or absent type is Text).
func listInput(t *tag) bool {
	if t.name != "input" {
		return false
	}
	switch inputType(t) {
	case "hidden", "password", "checkbox", "radio", "file", "submit", "image", "reset", "button":
		return false
	}
	return true
}

// popoverTarget reports whether t's popovertarget is one: on a button, or
// an input in the Submit, Image, Reset or Button state.
func popoverTarget(t *tag) bool {
	if t.name == "button" {
		return true
	}
	if t.name != "input" {
		return false
	}
	switch inputType(t) {
	case "submit", "image", "reset", "button":
		return true
	}
	return false
}

// idrefs states the ids an IDREF (list false) or IDREFS (list true) value
// names, in t's tree scope.
func (e *extractor) idrefs(t *tag, a attr, scope string, list bool) {
	if !list {
		if validID(a.val) {
			e.idRef(t, a.val, scope, e.valueSpan(a, a.val))
		}
		return
	}
	for _, id := range strings.FieldsFunc(a.val, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) }) {
		if validID(id) {
			e.idRef(t, id, scope, e.valueSpan(a, id))
		}
	}
}

// idRef states a reference to id in t's tree scope.
func (e *extractor) idRef(t *tag, id, scope string, s span) {
	e.addRef(language.ReferenceDraft{
		Name:          id,
		Kind:          string(reference.KindIDReference),
		Location:      e.loc(s.start, s.end),
		NameQualified: string(e.file) + "#" + scope + id,
		ConfidenceCap: e.cap(t),
		TargetKinds:   string(symbol.KindElement),
	})
}

// valueSpan locates part within a's value: exactly when the raw value is the
// value (no character reference, no newline normalization) and holds part
// once, else the whole value.
func (e *extractor) valueSpan(a attr, part string) span {
	s := a.valS
	if s.start < 0 {
		return a.nameS
	}
	raw := string(e.src[s.start:s.end])
	if raw == a.val && part != a.val {
		if i := strings.Index(raw, part); i >= 0 && strings.Count(raw, part) == 1 {
			return span{s.start + i, s.start + i + len(part)}
		}
	}
	return s
}

// fragmentOnly states an SVG reference (href, xlink:href) to an element of
// the document: "#id" only (another document's resource is not stated).
func (e *extractor) fragmentOnly(t *tag, a attr, scope string) {
	if e.base {
		return
	}
	v := weburl.Trim(a.val)
	if strings.HasPrefix(v, "#") {
		if id, ok := fragmentID(v[1:]); ok {
			e.idRef(t, id, scope, e.valueSpan(a, id))
		}
	}
}

// paint states a url(#id) reference of an SVG presentation attribute.
func (e *extractor) paint(t *tag, a attr, scope string) {
	v := strings.TrimSpace(a.val)
	if !strings.HasPrefix(v, "url(") || e.base {
		return
	}
	end := strings.IndexByte(v, ')')
	if end < 0 {
		return
	}
	inner := strings.Trim(strings.TrimSpace(v[len("url("):end]), `"'`)
	if strings.HasPrefix(inner, "#") {
		if id, ok := fragmentID(inner[1:]); ok {
			e.idRef(t, id, scope, e.valueSpan(a, id))
		}
	}
}

// link states the reference of a hyperlink or a frame's source: an element
// of the same document ("#id", for a hyperlink: fragment navigation finds an
// element of the document tree, never of a template's content or a shadow
// root, so the identity is the document's), another HTML document of the
// repository by a relative path ("b.html"), or an element of one
// ("b.html#id").
func (e *extractor) link(t *tag, a attr, sameDocument bool) {
	if e.base {
		return // every relative URL, "#id" too, resolves against the base
	}
	v := weburl.Trim(a.val)
	if strings.HasPrefix(v, "#") {
		if !sameDocument {
			return
		}
		if id, ok := fragmentID(v[1:]); ok {
			e.idRef(t, id, "", e.valueSpan(a, id))
		}
		return
	}
	target, frag, ok := relativeDocument(string(e.file), v)
	if !ok {
		return
	}
	s := e.valueSpan(a, a.val)
	if frag == "" {
		e.addRef(language.ReferenceDraft{
			Name:          path.Base(target),
			Kind:          string(reference.KindResourceLink),
			Location:      e.loc(s.start, s.end),
			NameQualified: target,
			ConfidenceCap: "strong",
			TargetKinds:   string(symbol.KindDocument),
		})
		return
	}
	if strings.Contains(target, "#") {
		return
	}
	e.addRef(language.ReferenceDraft{
		Name:          frag,
		Kind:          string(reference.KindIDReference),
		Location:      e.loc(s.start, s.end),
		NameQualified: target + "#" + frag,
		ConfidenceCap: "strong",
		TargetKinds:   string(symbol.KindElement),
	})
}

// fragmentID returns the id a URL fragment names, and false when the
// fragment is not one id the provider can state: empty, "top" (the top of
// the document when no element has that id), percent-encoded or holding a
// character the URL parser percent-encodes (non-ASCII, C0 controls, space,
// '"', '<', '>', '`': the standard looks for the encoded id first), a
// backslash (a CSS escape in url(#…)), a text fragment, or not an id.
func fragmentID(f string) (string, bool) {
	if f == "" || asciiLower(f) == "top" || strings.Contains(f, ":~:") {
		return "", false
	}
	for i := 0; i < len(f); i++ {
		switch c := f[i]; {
		case c <= 0x20, c >= 0x7f, c == '%', c == '"', c == '<', c == '>', c == '`', c == '\\':
			return "", false
		}
	}
	return f, validID(f)
}

// relativeDocument resolves a relative URL written in document file to the
// repository HTML document it names, and its fragment's id ("" without
// one); a fragment that names no id the provider can state refuses it.
func relativeDocument(file, v string) (target, frag string, ok bool) {
	target, frag, ok = weburl.Relative(file, v, ".html", ".htm")
	if !ok || frag == "" {
		return target, "", ok
	}
	id, valid := fragmentID(frag)
	if !valid {
		return "", "", false
	}
	return target, id, true
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

// resource states a link to a repository file of another language by a
// relative path — a stylesheet, a script — as path evidence
// (FileIdentity): its target is the declaration of that file itself, in
// whatever language. Strong at most: which file a URL serves depends on
// the server.
func (e *extractor) resource(t *tag, a attr, kind symbol.SymbolKind, exts ...string) {
	if e.base {
		return
	}
	target, _, ok := weburl.Relative(string(e.file), weburl.Trim(a.val), exts...)
	if !ok {
		return
	}
	s := e.valueSpan(a, a.val)
	e.addRef(language.ReferenceDraft{
		Name:          path.Base(target),
		Kind:          string(reference.KindResourceLink),
		Location:      e.loc(s.start, s.end),
		NameQualified: target,
		FileIdentity:  true,
		ConfidenceCap: "strong",
		TargetKinds:   string(kind),
	})
}

// attrState returns the value of t's attribute key ("" when absent) and
// whether it is known: absent, or present and verified against the tree. A
// present attribute the tree did not verify (NUL in it, a later duplicate
// the tokenizer kept) is unknown: a decision resting on it is not made.
func (e *extractor) attrState(t *tag, key string) (string, bool) {
	for j, a := range t.attrs {
		if a.key == key {
			return a.val, t.verified[j]
		}
	}
	return "", true
}

// present reports whether t has the attribute key, and whether that is known.
func (e *extractor) present(t *tag, key string) (has, known bool) {
	for j, a := range t.attrs {
		if a.key == key {
			return true, t.verified[j]
		}
	}
	return false, true
}

// stylesheetLink reports whether a <link rel=stylesheet> is fetched: not
// disabled (HTML links.html: "If el's disabled attribute is set, then
// return false"), and a type, when present, of text/css (a type the user
// agent does not support is not fetched; an empty one neither, by the
// standard).
func (e *extractor) stylesheetLink(t *tag) bool {
	if has, known := e.present(t, "disabled"); has || !known {
		return false
	}
	typ, known := e.attrState(t, "type")
	if !known {
		return false
	}
	if has, _ := e.present(t, "type"); has {
		return asciiLower(strings.Trim(typ, asciiWhitespace)) == "text/css"
	}
	return true
}

// modulePreload reports whether a <link rel=modulepreload> fetches a
// script: its as, absent or empty, is "script", or a script-like
// destination.
func (e *extractor) modulePreload(t *tag) bool {
	as, known := e.attrState(t, "as")
	if !known {
		return false
	}
	switch asciiLower(strings.Trim(as, asciiWhitespace)) {
	case "", "script", "audioworklet", "paintworklet", "serviceworker", "sharedworker", "worker":
		return true
	}
	return false
}

// loadsJavaScript reports whether a <script> fetches its src as JavaScript
// (HTML "prepare the script element", steps 8–10): the type string is the
// type attribute — or, without one, "text/" plus a non-empty language
// attribute; an empty or absent one is JavaScript; one that is, with ASCII
// whitespace stripped, a JavaScript MIME type essence (ASCII case
// insensitive) is a classic script — stripped of ASCII whitespace only when
// it is the type attribute's —; one that is "module" (ASCII case
// insensitive, nothing stripped) a module script; any other is no script.
func (e *extractor) loadsJavaScript(t *tag) bool {
	_, ok := e.scriptType(t)
	return ok
}

// scriptType is loadsJavaScript's decision: ok when the <script> is
// JavaScript, module when it is a module script.
func (e *extractor) scriptType(t *tag) (module, ok bool) {
	typ, known := e.attrState(t, "type")
	lang, langKnown := e.attrState(t, "language")
	hasType, _ := e.present(t, "type")
	if !known || !hasType && !langKnown {
		return false, false
	}
	s := typ
	if !hasType {
		if lang == "" {
			return false, true
		}
		s = "text/" + lang
	}
	if s == "" {
		return false, true
	}
	// Only a type attribute's value is stripped of ASCII whitespace; one
	// made of the language attribute is compared as it is.
	if hasType && javaScriptMIME[asciiLower(strings.Trim(s, asciiWhitespace))] || javaScriptMIME[asciiLower(s)] {
		return false, true
	}
	if asciiLower(s) == "module" {
		return true, true
	}
	return false, false
}

// asciiWhitespace are the characters the standard strips (TAB, LF, FF, CR,
// SPACE): never other Unicode white space.
const asciiWhitespace = "\t\n\f\r "

// javaScriptMIME are the JavaScript MIME type essences (MIME Sniffing).
var javaScriptMIME = map[string]bool{
	"application/ecmascript": true, "application/javascript": true, "application/x-ecmascript": true,
	"application/x-javascript": true, "text/ecmascript": true, "text/javascript": true,
	"text/javascript1.0": true, "text/javascript1.1": true, "text/javascript1.2": true,
	"text/javascript1.3": true, "text/javascript1.4": true, "text/javascript1.5": true,
	"text/jscript": true, "text/livescript": true, "text/x-ecmascript": true, "text/x-javascript": true,
}

// tokens splits a space-separated token list (rel) into its ASCII
// lower-cased tokens.
func tokens(v string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) }) {
		m[asciiLower(f)] = true
	}
	return m
}
