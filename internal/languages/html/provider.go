// Package html is Ark's HTML provider (.html, .htm).
//
// It states what an HTML document's own text fixes: the document, the
// elements that declare an id, the references to an id within the document
// (IDREF attributes, fragments) and links to other HTML documents by a
// relative path. The parser is golang.org/x/net/html: positions come from
// its tokenizer, facts from its tree builder (the HTML standard's), which
// decides by insertion mode what the tokenizer decides by tag name alone —
// so an attribute is used only when the tree has it on an element its own
// start tag produced (document.go, verify). Reasons and measurements:
// docs/development/web-semantic-fidelity/decisions.md (D19, D21).
//
// Identity. Every file is IdentityOnly: a reference reaches a declaration
// only by its qualified identity, never by a name match (an id is unique,
// at most, within its document). A document's Qualified is its FileID, an
// element's "<FileID>#<id>" — or, inside the content of <template>s (each a
// tree scope of its own), "<FileID>#<k1> <k2> <id>" with k the ordinals of
// the enclosing templates, outermost first. An id holding ASCII whitespace
// is no symbol (the standard forbids it; IDREFS cannot name it), so the
// spaces of a scope never meet an id's.
//
// Facts about the whole document come from the tree, not the tokenizer
// alone: an element hidden from the tokenizer (inside what it reads as raw
// text) may run a script, set the base or carry an id. An id the tree gives
// more elements than the provider states is no symbol at all.
//
// Confidence. A reference's identity is fixed by the document (Exact when
// one element declares it), lowered where the text does not fix the target:
//   - a document with a script, an on* attribute, a javascript: URL, an
//     iframe srcdoc, template syntax or a framework's bindings may change
//     its DOM at run time: Strong at most;
//   - a reference inside a <template>'s content resolves within it, but the
//     tree its content is cloned into is not known: Candidate at most —
//     except a declarative shadow root's (shadowrootmode), whose content is
//     that tree;
//   - a link to another document: Strong at most (which file a URL serves
//     depends on the server).
//
// A page's stylesheets and scripts — fetched as the standard's rules say
// (refs.go: stylesheetLink, loadsJavaScript, modulePreload) — are linked by
// path to the stylesheet's or script's own declaration in its language
// (language.ReferenceDraft.FileIdentity): the one relation between two
// languages. Strong at most.
//
// The text of a <style> and of a <script> without src is read by the CSS
// and JavaScript extractors and located in the document (embed.go): its
// declarations are the document's, prefixed by its path; a script's
// references reach them by that identity only.
//
// Images, fonts and media are linked to the index's asset nodes
// (assets.go, internal/asset) the same way, by path.
//
// Not stated: <a name> and <map name> targets, root-relative and absolute
// URLs, directory URLs, links of a document with <base href>, files that are
// neither a source nor an asset, event handler attributes (on*), and the
// references with a receiver of an inline script.
package html

import (
	"context"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/css"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"

	xhtml "golang.org/x/net/html"
)

// cacheVersion is the provider's own version: "html-4" read the inline
// styles and scripts (embed.go), "html-5" links to asset files (assets.go),
// "html-6" the style and classic script elements of SVG. What it states of them is the CSS and
// JavaScript extractors', so CacheVersion joins their versions to it: a
// change of either invalidates the cached documents too.
const cacheVersion = "html-6"

// Diagnostic codes.
const (
	// DiagEOFInTag: a tag cut off by the end of the input; the standard drops
	// it, losing what it states.
	DiagEOFInTag = "html_eof_in_tag"
	// DiagTreeUnverified: the file's attributes could not be verified
	// against the tree (document.go, verify); no id or reference is stated.
	DiagTreeUnverified = "html_tree_unverified"
	// DiagAttributeMismatch: a tag's attribute spans did not match the
	// tokenizer's attributes; its attributes are not used.
	DiagAttributeMismatch = "html_attribute_mismatch"
)

// Provider implements language.Provider for HTML.
type Provider struct{}

// NewProvider returns the HTML provider.
func NewProvider() *Provider { return &Provider{} }

func (*Provider) Language() language.Language { return "html" }
func (*Provider) Extensions() []string        { return []string{".html", ".htm"} }
func (*Provider) CacheVersion() string {
	return cacheVersion + "+" + css.NewProvider().CacheVersion() + "+" + javascript.NewProvider().CacheVersion()
}

// Extract states the file's document, its id elements and its references.
func (*Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	if err := ctx.Err(); err != nil {
		return language.Extraction{}, err
	}
	if strings.Contains(string(file), "#") {
		// A '#' in the path would let a document's identity equal an
		// element's ("a.html#x.html" of file a.html and id x.html); such a
		// file cannot be linked by a relative URL either.
		return language.Extraction{IdentityOnly: true}, nil
	}
	e := &extractor{file: file, src: src, lines: newLines(src)}
	e.run()
	return language.Extraction{
		Symbols:      e.symbols,
		References:   e.refs,
		Diagnostics:  e.diags,
		IdentityOnly: true,
	}, nil
}

type extractor struct {
	file  source.FileID
	src   []byte
	lines lines
	doc   *document

	dynamic  bool // the DOM may change at run time
	base     bool // the document has a <base href>
	symbols  []language.SymbolDraft
	refs     []language.ReferenceDraft
	refSeen  map[string]bool
	diags    []language.Diagnostic
	ordinals map[int]int  // template tag index → its ordinal (1-based)
	shadows  map[int]bool // template tag index → shadowRoot's answer
}

func (e *extractor) run() {
	e.symbols = append(e.symbols, language.SymbolDraft{
		Name:      path.Base(string(e.file)),
		Qualified: string(e.file),
		Kind:      symbol.KindDocument,
		Location:  e.loc(0, len(e.src)),
		StartByte: 0,
		EndByte:   uint32(len(e.src)),
		Exported:  true,
	})
	d := tokenize(e.src)
	e.doc = d
	if d.eofInTag.start >= 0 {
		// A start tag's attributes are lost; an end tag's only its closing.
		sev := language.SeverityError
		if d.eofEndTag {
			sev = language.SeverityWarning
		}
		e.diag(sev, DiagEOFInTag, "a tag is cut off by the end of the file and dropped", d.eofInTag)
	}
	if d.failed {
		e.diag(language.SeverityError, DiagEOFInTag, "the tokenizer stopped before the end of the file", span{0, len(e.src)})
	}
	for _, i := range d.mismatch {
		t := d.tags[i]
		e.diag(language.SeverityWarning, DiagAttributeMismatch, "the attributes of <"+t.name+"> could not be located; they are not used", span{t.start, t.end})
	}
	d.verify(e.src)
	if !d.verified {
		e.diag(language.SeverityWarning, DiagTreeUnverified, "the document tree could not be built to the end or the attributes could not be verified against it; no id or reference is stated", span{0, len(e.src)})
		return
	}
	e.dynamic = d.hasScript || d.dynAttr || d.tree.dynamic || hasTemplateSyntax(string(e.src)) || controlFlowBlock.Match(e.src)
	e.base = d.hasBase || d.tree.base
	e.ordinals = map[int]int{}
	for i, t := range d.tags {
		if t.name == "template" {
			e.ordinals[i] = len(e.ordinals) + 1
		}
	}
	var elements []language.SymbolDraft
	stated := map[string]int{}
	for i := range d.tags {
		t := &d.tags[i]
		scope, ok := e.scopeKey(t)
		if !t.inTree || !ok {
			continue
		}
		for j, a := range t.attrs {
			if !t.verified[j] || !t.matched {
				continue
			}
			if a.key == "id" && validID(a.val) {
				stated[a.val]++
				elements = append(elements, language.SymbolDraft{
					Name:      a.val,
					Qualified: string(e.file) + "#" + scope + a.val,
					Kind:      symbol.KindElement,
					Location:  e.loc(t.start, t.end),
					StartByte: uint32(t.start),
					EndByte:   uint32(t.end),
					Parent:    string(e.file),
					Signature: "<" + t.name + ` id="` + a.val + `">`,
					Exported:  true,
				})
				continue
			}
			e.attrRefs(t, a, scope)
		}
	}
	// An id the tree gives more elements than the tokenizer showed (an
	// element inside what the tokenizer reads as raw text, a clone) is
	// declared by an element no symbol stands for: the stated ones are not
	// the only ones, so none is.
	for _, el := range elements {
		if d.tree.ids[el.Name] <= stated[el.Name] {
			e.symbols = append(e.symbols, el)
		}
	}
	e.embedded()
}

// scopeKey is the tree-scope part of the identity of an id declared or
// referenced by t ("" for the document; "k1 k2 " for template content), and
// false when it is not known.
func (e *extractor) scopeKey(t *tag) (string, bool) {
	var b strings.Builder
	for _, k := range t.scope {
		o, ok := e.ordinals[k]
		if k < 0 || !ok {
			return "", false
		}
		b.WriteString(strconv.Itoa(o))
		b.WriteByte(' ')
	}
	return b.String(), true
}

// cap is the confidence cap of a same-document reference of t: Candidate
// inside the content of a template that is no declarative shadow root (its
// content is cloned into a tree not known; a shadow root's content is that
// root), Strong in a dynamic document.
func (e *extractor) cap(t *tag) string {
	for _, k := range t.scope {
		if !e.shadowRoot(k) {
			return "candidate"
		}
	}
	if e.dynamic {
		return "strong"
	}
	return ""
}

// shadowRoot reports whether the template tag k declares a shadow root:
// shadowrootmode open or closed, on a template whose parent can host a
// shadow root and hosts none yet (otherwise the standard inserts an
// ordinary template, HTML §13.2.6.4.4).
func (e *extractor) shadowRoot(k int) bool {
	// Each reference in a template's content asks: the answer, which walks
	// the host's subtree, is the template's (once per template, not per
	// reference — a quadratic time otherwise).
	if v, ok := e.shadows[k]; ok {
		return v
	}
	v := e.declaresShadowRootOf(k)
	if e.shadows == nil {
		e.shadows = map[int]bool{}
	}
	e.shadows[k] = v
	return v
}

func (e *extractor) declaresShadowRootOf(k int) bool {
	if k < 0 || !declaresShadowRoot(e.doc.tags[k].el) {
		return false
	}
	el := e.doc.tags[k].el
	host := el.Parent
	if host == nil || host.Type != xhtml.ElementNode || host.Namespace != "" || !shadowHost(host.Data) {
		return false
	}
	// The host already has one when an earlier declaration is anywhere
	// below it: a browser attaches it at once and keeps it out of the tree,
	// while x/net keeps it, where the adoption agency may move it into a
	// formatting element's clone. (A declaration of a descendant host counts
	// too: a Candidate where Exact was due, never the reverse.)
	earlier := false
	var walk func(n *xhtml.Node) bool
	walk = func(n *xhtml.Node) bool {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c == el {
				return true
			}
			if declaresShadowRoot(c) {
				earlier = true
			}
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(host)
	return !earlier
}

// declaresShadowRoot reports whether n is an HTML template with
// shadowrootmode open or closed.
func declaresShadowRoot(n *xhtml.Node) bool {
	if n == nil || n.Type != xhtml.ElementNode || n.Namespace != "" || n.Data != "template" {
		return false
	}
	for _, a := range n.Attr {
		if a.Namespace == "" && asciiLower(a.Key) == "shadowrootmode" {
			switch asciiLower(a.Val) {
			case "open", "closed":
				return true
			}
			return false
		}
	}
	return false
}

// shadowHost reports whether an HTML element of the name can host a shadow
// root (DOM §attachShadow: a valid custom element name, or one of these).
func shadowHost(name string) bool {
	switch name {
	case "article", "aside", "blockquote", "body", "div", "footer", "h1", "h2", "h3", "h4", "h5", "h6",
		"header", "main", "nav", "p", "section", "span":
		return true
	}
	return customElementName(name)
}

// customElementName reports whether name is a valid custom element name
// (HTML §4.13.3): [a-z] (PCENChar)* '-' (PCENChar)*, not a reserved name.
func customElementName(name string) bool {
	switch name {
	case "annotation-xml", "color-profile", "font-face", "font-face-src", "font-face-uri",
		"font-face-format", "font-face-name", "missing-glyph":
		return false
	}
	if name == "" || name[0] < 'a' || name[0] > 'z' || !strings.Contains(name, "-") {
		return false
	}
	for _, r := range name {
		switch {
		case r == '-', r == '.', r == '_', r >= '0' && r <= '9', r >= 'a' && r <= 'z', r == 0xB7,
			r >= 0xC0 && r <= 0xD6, r >= 0xD8 && r <= 0xF6, r >= 0xF8 && r <= 0x37D, r >= 0x37F && r <= 0x1FFF,
			r >= 0x200C && r <= 0x200D, r >= 0x203F && r <= 0x2040, r >= 0x2070 && r <= 0x218F,
			r >= 0x2C00 && r <= 0x2FEF, r >= 0x3001 && r <= 0xD7FF, r >= 0xF900 && r <= 0xFDCF,
			r >= 0xFDF0 && r <= 0xFFFD, r >= 0x10000 && r <= 0xEFFFF:
		default:
			return false
		}
	}
	return true
}

// controlFlowBlock matches template control flow that renders elements
// conditionally or repeatedly — Angular's and Blade's blocks (@if (…),
// @for, @foreach, @switch, @defer, …), Svelte's ({#each …}, {#if …}) — and
// server-side includes (<!--#include …-->).
var controlFlowBlock = regexp.MustCompile(`@(if|for|foreach|forelse|switch|defer|while|each|unless|isset|empty)\s*[({]|\{[#:/](if|each|await|key|else|snippet)\b|<!--#`)

func (e *extractor) diag(sev language.DiagnosticSeverity, code, msg string, s span) {
	e.diags = append(e.diags, language.Diagnostic{Severity: sev, Code: code, Message: msg, Location: e.loc(s.start, s.end)})
}

// addRef records a reference once per name, kind and position (an IDREFS
// value naming an id twice, located by the whole value, is one reference).
func (e *extractor) addRef(r language.ReferenceDraft) {
	r.Container = string(e.file)
	r.IdentityInRepository = true
	key := string(r.Kind) + "\x00" + r.Name + "\x00" + r.NameQualified + "\x00" +
		strconv.Itoa(int(r.Location.Range.Start.Line)) + ":" + strconv.Itoa(int(r.Location.Range.Start.Column)) + ":" +
		strconv.Itoa(int(r.Location.Range.End.Line)) + ":" + strconv.Itoa(int(r.Location.Range.End.Column))
	if e.refSeen == nil {
		e.refSeen = map[string]bool{}
	}
	if e.refSeen[key] {
		return
	}
	e.refSeen[key] = true
	e.refs = append(e.refs, r)
}

// validID reports whether an id value can be a symbol: not empty, no ASCII
// whitespace (the standard forbids it), no template syntax.
func validID(v string) bool {
	return v != "" && !strings.ContainsAny(v, " \t\n\f\r") && !hasTemplateSyntax(v)
}

// hasTemplateSyntax reports whether s holds the syntax of a template
// language rendering it (Jinja/Liquid/Handlebars/Go templates, ERB/JSP/EJS,
// PHP, JavaScript template literals).
func hasTemplateSyntax(s string) bool {
	for _, m := range []string{"{{", "{%", "<%", "<?", "${"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
