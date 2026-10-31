// Package markdown is Ark's Markdown provider (.md, .markdown).
//
// Two layers, kept apart. The document layer (parse.go) reads the file with
// github.com/yuin/goldmark — CommonMark 0.31.2 with the GitHub Flavored
// Markdown extensions and footnotes — and knows headings, links, fenced code
// blocks and raw HTML ids, located in the bytes as stored. This file states
// what they are to Ark, for every Markdown file:
//
//   - the file itself, a document (Qualified: its FileID — the target of
//     path evidence, symbol.DeclaresFile);
//   - a section for each heading (ATX or setext, H1 to H6), named by its
//     text content, identified by GitHub's anchor of it,
//     "<FileID>#<anchor>" (slug.go: lower case, punctuation and symbols
//     removed, spaces as "-", the n-th repeat "-n"). A section spans its
//     heading and what follows up to the next heading of its level or a
//     higher one; a heading inside a block quote or a list item spans
//     itself. A document contains its sections, a section the sections
//     under it (reference.KindContains);
//   - a code block for each fenced code block whose info string names a
//     language, named by it ("<container>::<language>", the n-th of a
//     language in one container "<container>::<language>[n]"); its content is
//     never read as code, and a block without a language, an indented code
//     block or a paragraph declares nothing;
//   - an anchor for each id (and <a name>) of raw HTML, "<FileID>#<id>":
//     a fragment names it as it names a heading's anchor;
//   - the links, images, resolved reference links and autolinks, contained
//     by the innermost section around them (dest.go reads a destination):
//     "#x" an id reference to this file's anchor x; a relative path path
//     evidence (FileIdentity) to that repository file's own declaration —
//     whatever its language: a Markdown, HTML or YAML document, a
//     stylesheet, a script, an asset —, "b.md#x" an id reference to that
//     Markdown file's anchor x; a path that leaves the repository a
//     resource link without identity (unresolved); a URL with an authority
//     a resource link outside the repository, named by its scheme and host
//     only (a webhook's path is its secret).
//
// Identity. Every file is IdentityOnly: a reference reaches a declaration
// only by its qualified identity, never by a name match — a section
// "Usage" of one file is no section of another, and no name of code.
// A missing file or anchor is Unresolved (IdentityInRepository), never
// outside the repository. A reference link reaches its definition through
// goldmark (CommonMark: a reference without a definition is no link, but
// text); a definition is no link itself.
//
// Names. Every name, qualified name and reference name is one line of at
// most 256 bytes without control or format characters (declname.IsName);
// a text that is none names nothing (a heading's anchor stands in for its
// text when the anchor is a name).
//
// Secrets. A heading or an HTML id whose text holds what Ark's recommended
// secret policy masks (internal/secrets: "password = …", a token format)
// declares nothing — no name or qualified name shows it; a fragment that
// names it is unresolved. A link from the repository's root ("/b.md",
// "/b.md#x") is capped at Strong.
//
// Bounds. A file over maxBytes declares itself only (markdown_too_large);
// a file states at most maxSymbols declarations and maxRefs references
// (markdown_truncated past them); the cost guard (guard.go) bounds the
// time goldmark takes (markdown_degenerate). Markdown has no syntax errors:
// every byte sequence is a document, read the same way every time; a file
// that is no UTF-8 text is reported as broken (markdown_invalid_utf8).
package markdown

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/secrets"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

const cacheVersion = "markdown-1"

const (
	maxBytes   = 4 << 20
	maxSymbols = 5000
	maxRefs    = 20000
)

// Diagnostic codes.
const (
	// CodeTooLarge reports a file over maxBytes: it declares itself only.
	CodeTooLarge = "markdown_too_large"
	// CodeTruncated reports a file whose declarations or references go
	// past the provider's bounds.
	CodeTruncated = "markdown_truncated"
	// CodeInvalidUTF8 reports a file that is no UTF-8 text (an error: the
	// one way a Markdown file is broken). It is read all the same — a
	// name never holds an invalid byte.
	CodeInvalidUTF8 = "markdown_invalid_utf8"
)

// Provider implements language.Provider for Markdown.
type Provider struct{}

// NewProvider returns the Markdown provider.
func NewProvider() *Provider { return &Provider{} }

func (*Provider) Language() language.Language { return "markdown" }
func (*Provider) Extensions() []string        { return []string{".md", ".markdown"} }
func (*Provider) CacheVersion() string        { return cacheVersion }

// Extract states the file's declarations and references.
func (*Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	if err := ctx.Err(); err != nil {
		return language.Extraction{}, err
	}
	e := newExtractor(file, src)
	e.run()
	return language.Extraction{
		Symbols:      e.symbols,
		References:   e.refs,
		Diagnostics:  e.diags,
		IdentityOnly: true,
	}, nil
}

type span struct{ start, end int }

type extractor struct {
	file  source.FileID
	slash string // the file's path, slash-separated
	sep   string // the separator of the index's file IDs
	src   []byte
	lines lines

	symbols []language.SymbolDraft
	refs    []language.ReferenceDraft
	diags   []language.Diagnostic
	full    bool

	sections []section // declared, in document order
}

// section is a declared section: its symbol's index, its span and its
// parent among sections (-1: the document).
type section struct {
	sym    int
	sp     span
	parent int
}

func (s section) holds(sp span) bool { return s.sp.start <= sp.start && sp.end <= s.sp.end }

func newExtractor(file source.FileID, src []byte) *extractor {
	sep := "/"
	f := string(file)
	if strings.Contains(f, "\\") && !strings.Contains(f, "/") {
		sep = "\\" // the index's file IDs separate with the OS's separator (Windows)
	}
	return &extractor{file: file, slash: strings.ReplaceAll(f, "\\", "/"), sep: sep, src: src, lines: newLines(src)}
}

func (e *extractor) run() {
	doc := string(e.file)
	e.symbol(path.Base(e.slash), doc, symbol.KindDocument, span{0, len(e.src)}, "", "")
	if len(e.src) > maxBytes {
		e.diags = append(e.diags, language.Diagnostic{
			Severity: language.SeverityWarning,
			Code:     CodeTooLarge,
			Message:  fmt.Sprintf("the file is larger than %d bytes: only the document itself is declared", maxBytes),
			Location: e.loc(0, 0),
		})
		return
	}
	if i := invalidUTF8(e.src); i >= 0 {
		e.diags = append(e.diags, language.Diagnostic{
			Severity: language.SeverityError,
			Code:     CodeInvalidUTF8,
			Message:  fmt.Sprintf("the file is not UTF-8 text: byte %d is no UTF-8 sequence", i),
			Location: e.loc(i, i+1),
		})
	}
	g := guard(e.src)
	d := parse(g.src)
	g.tables.merge(d.tables)
	for _, p := range g.problems() {
		e.diags = append(e.diags, language.Diagnostic{
			Severity: language.SeverityWarning, Code: CodeDegenerate, Message: p.message, Location: e.loc(p.start, p.end),
		})
	}
	e.headings(d.headings)
	count := map[string]int{} // code blocks per container and language
	taken := map[string]bool{}
	for _, c := range d.codes {
		if c.lang == "" || !declname.IsName(c.lang) {
			continue
		}
		parent := e.container(c.start)
		base := parent + "::" + c.lang
		// The n-th block of a language in a container is
		// "…::<language>[n]" (the first keeps its name; an ordinal a
		// language named "go[2]" took is skipped).
		n, q := count[base]+1, base
		for n > 1 && taken[base+"["+strconv.Itoa(n)+"]"] || n == 1 && taken[base] {
			n++
		}
		if n > 1 {
			q = base + "[" + strconv.Itoa(n) + "]"
		}
		count[base], taken[q] = n, true
		if declname.IsName(q) {
			e.symbol(c.lang, q, symbol.KindCodeBlock, span{c.start, c.end}, parent, "")
		}
	}
	for _, id := range d.ids {
		q := doc + "#" + id.id
		if !declname.IsName(id.id) || !declname.IsName(q) || secret(id.id) {
			continue
		}
		e.symbol(id.id, q, symbol.KindAnchor, span{id.start, id.end}, e.container(id.start), "")
	}
	for _, l := range d.links {
		e.link(l)
	}
}

// headings declares the sections and their containment.
func (e *extractor) headings(hs []heading) {
	doc := string(e.file)
	slugs := newSlugger()
	// The section of a heading outside any container block ends at the
	// next such heading of its level or a higher one.
	ends := make([]int, len(hs))
	var open []int // headings outside containers whose section is open
	for i, h := range hs {
		ends[i] = h.end
		if h.nested {
			continue
		}
		for len(open) > 0 && hs[open[len(open)-1]].level >= h.level {
			ends[open[len(open)-1]] = h.start
			open = open[:len(open)-1]
		}
		open = append(open, i)
	}
	for _, i := range open {
		ends[i] = len(e.src)
	}
	var stack []int // declared sections around the current heading
	for i, h := range hs {
		anchor := slugs.next(h.text)
		end := e.trimEnd(h.end, ends[i])
		// The sections around this one are those whose span holds its own.
		for len(stack) > 0 && !e.sections[stack[len(stack)-1]].holds(span{h.start, end}) {
			stack = stack[:len(stack)-1]
		}
		q := doc + "#" + anchor
		name := strings.Join(strings.FieldsFunc(h.text, unicode.IsSpace), " ")
		if !declname.IsName(name) {
			name = anchor
		}
		if !declname.IsName(name) || !declname.IsName(q) || secret(h.text) || secret(name) {
			continue
		}
		parent, pq := -1, doc
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
			pq = e.symbols[e.sections[parent].sym].Qualified
		}
		if !e.symbol(name, q, symbol.KindSection, span{h.start, end}, pq, "h"+strconv.Itoa(h.level)) {
			return
		}
		e.sections = append(e.sections, section{sym: len(e.symbols) - 1, sp: span{h.start, end}, parent: parent})
		stack = append(stack, len(e.sections)-1)
		e.ref(language.ReferenceDraft{
			Name: q, Kind: string(reference.KindContains), Container: pq,
			NameQualified: q, IdentityInRepository: true, TargetKinds: string(symbol.KindSection),
		}, span{h.start, h.end})
	}
}

// trimEnd ends a section at its last byte that is no white space (but not
// before its heading's end).
func (e *extractor) trimEnd(min, end int) int {
	for end > min {
		switch e.src[end-1] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			end--
			continue
		}
		break
	}
	return end
}

// container names the innermost section whose span holds off, or the
// document.
func (e *extractor) container(off int) string {
	i := sort.Search(len(e.sections), func(i int) bool { return e.sections[i].sp.start > off }) - 1
	for i >= 0 {
		s := e.sections[i]
		if s.sp.start <= off && off < s.sp.end || s.sp.start == off {
			return e.symbols[s.sym].Qualified
		}
		i = s.parent
	}
	return string(e.file)
}

// TargetKinds of an id reference: a heading's anchor or an HTML id.
var anchorKinds = string(symbol.KindSection) + "," + string(symbol.KindAnchor)

// link states the reference of a link.
func (e *extractor) link(l link) {
	sp := span{l.start, l.end}
	container := e.container(l.start)
	d := readDest(e.slash, l.dest)
	switch d.kind {
	case destAnchor:
		e.idRef(string(e.file), d.frag, container, sp, false)
	case destFile:
		ext := strings.ToLower(path.Ext(d.path))
		if d.frag != "" && (ext == ".md" || ext == ".markdown") {
			e.idRef(e.filePath(d.path), d.frag, container, sp, d.fromRoot)
			return
		}
		name := path.Base(d.path)
		if !declname.IsName(name) || !declname.IsName(d.path) {
			return
		}
		r := language.ReferenceDraft{
			Name: name, Kind: string(reference.KindResourceLink), Container: container,
			NameQualified: e.filePath(d.path), FileIdentity: true, IdentityInRepository: true,
			TargetKinds: targetKinds(d.path),
		}
		if d.fromRoot {
			// GitHub reads "/a.md" from the repository's root; the index's
			// root may be another directory.
			r.ConfidenceCap = "strong"
		}
		e.ref(r, sp)
	case destOutside:
		// No repository file: recorded, with no identity to resolve.
		if declname.IsName(d.path) {
			e.ref(language.ReferenceDraft{Name: d.path, Kind: string(reference.KindResourceLink), Container: container}, sp)
		}
	case destExternal:
		e.ref(language.ReferenceDraft{
			Name: d.url, Kind: string(reference.KindResourceLink), Container: container,
			NameQualified: "url:" + d.url,
		}, sp)
	}
}

// idRef states a reference to the anchor frag of the Markdown file whose
// FileID is file; fromRoot, a file named from the repository's root
// ("/b.md#x"), caps it at Strong as a path link from the root is.
func (e *extractor) idRef(file, frag, container string, sp span, fromRoot bool) {
	q := file + "#" + frag
	if !declname.IsName(frag) || !declname.IsName(q) {
		return
	}
	r := language.ReferenceDraft{
		Name: frag, Kind: string(reference.KindIDReference), Container: container,
		NameQualified: q, IdentityInRepository: true, TargetKinds: anchorKinds,
	}
	if fromRoot {
		r.ConfidenceCap = "strong"
	}
	e.ref(r, sp)
}

// secret reports whether text holds what Ark's recommended secret policy
// masks in a text ("password = …", a known token format as written,
// before an anchor lowers its case). A heading or an HTML id that does is
// declared as no section or anchor: the index shows a declaration's name
// and qualified name as they are (the MCP server masks names by a token's
// form only), and a fragment that names it is unresolved.
func secret(text string) bool {
	return len(secrets.Recommended().Find(text, secrets.Mode{})) > 0
}

// filePath spells a slash-separated repository path as the index's file
// IDs are spelled.
func (e *extractor) filePath(p string) string { return strings.ReplaceAll(p, "/", e.sep) }

func (e *extractor) symbol(name, qualified string, kind symbol.SymbolKind, sp span, parent, signature string) bool {
	if len(e.symbols) >= maxSymbols {
		e.truncated(sp)
		return false
	}
	e.symbols = append(e.symbols, language.SymbolDraft{
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Location:  e.loc(sp.start, sp.end),
		StartByte: uint32(sp.start),
		EndByte:   uint32(sp.end),
		Parent:    parent,
		Signature: signature,
		Exported:  true,
	})
	return true
}

func (e *extractor) ref(r language.ReferenceDraft, sp span) {
	if len(e.refs) >= maxRefs {
		e.truncated(sp)
		return
	}
	r.Location = e.loc(sp.start, sp.end)
	e.refs = append(e.refs, r)
}

func (e *extractor) truncated(sp span) {
	if e.full {
		return
	}
	e.full = true
	e.diags = append(e.diags, language.Diagnostic{
		Severity: language.SeverityWarning,
		Code:     CodeTruncated,
		Message:  fmt.Sprintf("the file states more than %d declarations or %d references: the rest are left out", maxSymbols, maxRefs),
		Location: e.loc(sp.start, sp.start),
	})
}

// invalidUTF8 returns the offset of src's first byte that starts no UTF-8
// sequence, or -1.
func invalidUTF8(src []byte) int {
	if utf8.Valid(src) {
		return -1
	}
	for i := 0; i < len(src); {
		r, n := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && n == 1 {
			return i
		}
		i += n
	}
	return -1
}

// lines maps byte offsets to 1-based line and byte column positions (lines
// end at '\n', as the other providers' positions do).
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
