package html

import (
	"bytes"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/magicdrive/ark/internal/languages/css"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/source"
)

// Embedded regions (decision D31 of docs/development/web-semantic-fidelity):
// the text of a <style> element is read as CSS, that of a <script> element
// without src as JavaScript, each by its language's own extractor, and what
// they state is located in the document's own source.
//
// A region is read only when the tree the standard builds holds exactly the
// text the tokenizer reported (embeddedText): its element is one the
// document's tree verified, in the HTML or the SVG namespace, outside any
// <template> (inert content), and its text — line breaks normalized as the
// input stream normalizes them — is the element's only content. An SVG
// <style> or <script> is no raw text (character references and CDATA
// sections are read there): its text matches only when it holds neither, and
// a classic SVG script without href is read. Raw text has no character references, so
// every byte of it is a byte of the source: locations map one to one.
//
// Identities. A style element states the identities a stylesheet of the
// document's path would ("<FileID> @keyframes <name>", "<FileID> --x"):
// the document's style elements share one cascade. A classic script's
// top-level declarations are the document's globals, "<FileID> <name>"
// (classic scripts share one global scope); a module script's are its own,
// "<FileID> module#<k> <name>" (k counts the document's module scripts from
// 1). None can equal a document's ("<FileID>") or an element's
// ("<FileID>#…") identity, nor one another's: a JavaScript name holds no
// space, '@' or leading "--".
//
// References. A script's references reach declarations only by these
// identities (IdentityInRepository), never by a name: the document's
// globals are not another document's, nor — by name — an external
// script's. A module script's name is its own declaration's (a symbol, or a
// binding no symbol stands for: unresolved), or an import (unresolved: its
// module is not modelled), or a global. A script that does not run (one the
// end of the input closes, a classic nomodule script, ...: runs) is not
// read. A reference with
// a receiver (a.m(), this.m()) is not stated: what a document's global
// holds is not known from its declarations. Each is Strong at most: an
// external script may declare the same global, and scripts run in order.

// embeddedText returns the span of t's raw text when the tree holds exactly
// that text as the content of the element t produced.
func (e *extractor) embeddedText(t *tag) (span, bool) {
	if t.text.start < 0 || !t.inTree || t.namespace != "" && t.namespace != "svg" || len(t.scope) != 0 || t.el == nil {
		return span{}, false
	}
	raw := e.src[t.text.start:t.text.end]
	if bytes.IndexByte(raw, 0) >= 0 || bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		// The tree replaces NUL; a byte order mark would be read as one.
		return span{}, false
	}
	var got strings.Builder
	for c := t.el.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.TextNode {
			return span{}, false
		}
		got.WriteString(c.Data)
	}
	if got.String() != normalizeNewlines(raw) {
		return span{}, false
	}
	return t.text, true
}

// normalizeNewlines replaces CR LF and CR by LF, as the input stream does.
func normalizeNewlines(b []byte) string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// embedded reads the document's style and script elements.
func (e *extractor) embedded() {
	modules := 0
	for i := range e.doc.tags {
		t := &e.doc.tags[i]
		switch t.name {
		case "style":
			if s, ok := e.embeddedText(t); ok && e.styleIsCSS(t) {
				e.embedStyle(s)
			}
		case "script":
			module, ok := e.scriptType(t)
			if !ok || !t.closed || !e.runs(t, module) {
				continue
			}
			if t.namespace == "svg" {
				// An SVG script runs as a classic script; one with an href
				// fetches its source instead of running its text.
				if has, known := e.present(t, "href"); module || has || !known {
					continue
				}
				if has, known := e.present(t, "xlink:href"); has || !known {
					continue
				}
			}
			s, ok := e.embeddedText(t)
			if !ok {
				continue
			}
			prefix := string(e.file) + " "
			if module {
				modules++
				prefix += "module#" + strconv.Itoa(modules) + " "
			}
			e.embedScript(s, prefix, module)
		}
	}
}

// runs reports whether the text of a JavaScript <script> is run (HTML
// "prepare the script element"): it ends at its end tag (the "text"
// insertion mode sets "already started" on a script the end of the input
// closes, and an already started script returns at once), has no src ("If el
// has a src content attribute", the source is fetched instead), and, classic,
// has no nomodule ("If el has a nomodule content attribute and its type is
// classic, then return") and no for and event other than window and onload.
// An attribute the tree did not verify decides nothing: the text is not read.
func (e *extractor) runs(t *tag, module bool) bool {
	if has, known := e.present(t, "src"); has || !known {
		return false
	}
	if module {
		return true
	}
	if has, known := e.present(t, "nomodule"); has || !known {
		return false
	}
	hasFor, knownFor := e.present(t, "for")
	hasEvent, knownEvent := e.present(t, "event")
	if !knownFor || !knownEvent {
		return false
	}
	if hasFor && hasEvent {
		f, _ := e.attrState(t, "for")
		ev, _ := e.attrState(t, "event")
		f, ev = asciiLower(strings.Trim(f, asciiWhitespace)), asciiLower(strings.Trim(ev, asciiWhitespace))
		if f != "window" || ev != "onload" && ev != "onload()" {
			return false
		}
	}
	return true
}

// styleIsCSS reports whether a <style> is applied as CSS: a type, when
// present, empty or "text/css" (ASCII case insensitive, nothing stripped;
// HTML "update a style block").
func (e *extractor) styleIsCSS(t *tag) bool {
	typ, known := e.attrState(t, "type")
	if !known {
		return false
	}
	return typ == "" || asciiLower(typ) == "text/css"
}

func (e *extractor) embedStyle(s span) {
	x := css.ExtractEmbedded(e.file, e.src[s.start:s.end], !e.base)
	for _, d := range x.Symbols {
		d.Location = e.shift(d.Location, s.start)
		d.StartByte += uint32(s.start)
		d.EndByte += uint32(s.start)
		e.symbols = append(e.symbols, d)
	}
	for _, r := range x.References {
		r.Location = e.shift(r.Location, s.start)
		e.refs = append(e.refs, r)
	}
	for _, d := range x.Diagnostics {
		d.Location = e.shift(d.Location, s.start)
		e.diags = append(e.diags, d)
	}
}

func (e *extractor) embedScript(s span, prefix string, module bool) {
	x := typescript.ExtractEmbeddedJavaScript(e.file, e.src[s.start:s.end])
	global := string(e.file) + " "
	own := map[string]bool{}
	for _, d := range x.Symbols {
		if d.Parent == "" {
			own[d.Name] = true
		}
		d.Qualified = prefix + d.Qualified
		if d.Parent != "" {
			d.Parent = prefix + d.Parent
		} else {
			d.Parent = string(e.file)
		}
		d.Location = e.shift(d.Location, s.start)
		d.StartByte += uint32(s.start)
		d.EndByte += uint32(s.start)
		d.Receiver, d.MemberScope, d.ParameterScope = "", "", ""
		d.Exported = false
		e.symbols = append(e.symbols, d)
	}
	for _, r := range x.References {
		if r.ReceiverExpr != "" || r.FileIdentity {
			continue
		}
		if !r.Dynamic {
			p := global
			if module && (own[r.Name] || x.Imported[r.Name] || x.Locals[r.Name]) {
				p = prefix
			}
			r.NameQualified = p + r.Name
			r.IdentityInRepository = true
			if r.ConfidenceCap == "" || r.ConfidenceCap == "strong" {
				r.ConfidenceCap = "strong"
			} else {
				r.ConfidenceCap = "candidate"
			}
		}
		if r.Container != "" {
			r.Container = prefix + r.Container
		} else {
			r.Container = string(e.file)
		}
		r.Location = e.shift(r.Location, s.start)
		e.refs = append(e.refs, r)
	}
	for _, d := range x.Diagnostics {
		d.Location = e.shift(d.Location, s.start)
		e.diags = append(e.diags, d)
	}
}

// shift maps a location in an embedded text that starts at byte off of the
// source (line 1, column 1 at off) to the source's.
func (e *extractor) shift(l source.Location, off int) source.Location {
	p0 := e.lines.pos(off)
	f := func(p source.Position) source.Position {
		switch {
		case p.Line == 0:
			return p0
		case p.Line == 1:
			return source.Position{Line: p0.Line, Column: p0.Column + p.Column - 1}
		}
		return source.Position{Line: p0.Line + p.Line - 1, Column: p.Column}
	}
	return source.Location{File: e.file, Range: source.Range{Start: f(l.Range.Start), End: f(l.Range.End)}}
}
