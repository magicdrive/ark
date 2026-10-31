// Package toml is Ark's TOML provider (.toml).
//
// Two layers, kept apart, as for YAML. The document layer (tomldoc) reads
// the document with github.com/pelletier/go-toml/v2's parser into the tree
// of tables and keys it defines, with their bytes, and its problems. The
// meaning layer (manifest) reads a Cargo manifest (Cargo.toml) or a Python
// pyproject.toml from that tree. This package states, for every TOML file:
//
//   - the file itself (a document, Qualified: its FileID);
//   - its tables (kind table: a [header] table, a table a header implies or
//     dotted keys define, an inline table, an element of an array of
//     tables) and its other keys (kind key: a value, an array of tables),
//     by their JSON Pointer: Name "/a/b", Qualified "<FileID>#/a/b" ("~"
//     and "/" in a key are "~0" and "~1"), at most maxDepth parts deep, the
//     shallower first when the file has more than maxSymbols (toml_truncated
//     past them). An element of an array of tables is named by its `name`
//     key when it has one ("/bin/cli"; the n-th element of one name
//     "/bin/cli#<n>"; a name with "#" is none), else by its place among the
//     unnamed ones ("/bin/#1"):
//     inserting a named element renames no other. Array elements and
//     values are no declarations; a key that is no one-line name
//     (declname.IsName) declares nothing, nor does what it holds;
//   - contains: the file each declaration at its top, a table each
//     declaration in it, an array of tables its elements (Exact: the
//     file's own structure);
//   - the problems tomldoc and manifest report, as diagnostics;
//
// and, for a manifest, what manifest states: its package, dependencies and
// their relations, declared at the pointers of the tables and keys they
// are written as (so a dependency is no key besides).
//
// Identity. Every file is IdentityOnly: a reference reaches a declaration
// only by its qualified identity, never by a name match — a key "name" of
// one file is no key of another, a Cargo dependency "regex" no Python
// requirement "regex".
//
// Bounds. tomldoc reads at most tomldoc.MaxBytes; a file states at most
// maxSymbols declarations and maxRefs references.
package toml

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/languages/toml/manifest"
	"github.com/magicdrive/ark/internal/languages/toml/tomldoc"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

const cacheVersion = "toml-1"

const (
	maxDepth   = 6
	maxSymbols = 5000
	maxRefs    = 20000
)

// CodeTruncated reports a file whose declarations or references go past
// the provider's bounds.
const CodeTruncated = "toml_truncated"

// Provider implements language.Provider for TOML.
type Provider struct{}

// NewProvider returns the TOML provider.
func NewProvider() *Provider { return &Provider{} }

func (*Provider) Language() language.Language { return "toml" }
func (*Provider) Extensions() []string        { return []string{".toml"} }
func (*Provider) CacheVersion() string        { return cacheVersion }

// Extract states the file's declarations, references and problems.
func (*Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	if err := ctx.Err(); err != nil {
		return language.Extraction{}, err
	}
	e := &extractor{file: string(file), d: tomldoc.Parse(src), lines: newLines(src)}
	e.run()
	return language.Extraction{
		Symbols:      e.symbols,
		References:   e.refs,
		Diagnostics:  e.diags,
		Aliases:      e.aliases,
		IdentityOnly: true,
	}, nil
}

// decl is a declaration the file may state.
type decl struct {
	node            *tomldoc.Node // nil for a manifest's own declaration
	parent          int           // index of the parent declaration, -1 for the document
	depth           int
	name, qualified string
	kind            symbol.SymbolKind
	span, at        tomldoc.Span // its bytes; where its contains is located
	signature       string
}

type extractor struct {
	file     string
	d        *tomldoc.Doc
	lines    lines
	symbols  []language.SymbolDraft
	refs     []language.ReferenceDraft
	aliases  []language.AliasDraft
	diags    []language.Diagnostic
	full     bool
	diagsCut bool
}

func (e *extractor) run() {
	for _, p := range e.d.Problems {
		e.problem(p)
	}
	if e.d.ProblemsTruncated {
		e.diagsFull()
	}
	decls, ids := e.candidates()
	m, isManifest := manifest.Read(e.file, e.d, func(n *tomldoc.Node) string { return ids[n] })
	if isManifest {
		for i := range decls {
			if o, ok := m.Overrides[decls[i].node]; ok {
				decls[i].kind, decls[i].name, decls[i].signature = o.Kind, o.Name, o.Signature
			}
		}
		index := map[*tomldoc.Node]int{}
		for i, d := range decls {
			index[d.node] = i
		}
		for _, x := range m.Decls {
			p, ok := index[x.Parent]
			if !ok || decls[p].depth >= maxDepth {
				continue
			}
			decls = append(decls, decl{parent: p, depth: decls[p].depth + 1, name: x.Name, qualified: x.Qualified,
				kind: x.Kind, span: x.Span, at: x.Span, signature: x.Signature})
		}
		for _, p := range m.Problems {
			e.problem(p)
		}
	}

	// The shallower first, within the bound.
	order := make([]int, len(decls))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return decls[order[a]].depth < decls[order[b]].depth })
	kept := map[int]bool{}
	for _, i := range order {
		if len(kept) >= maxSymbols-1 {
			e.truncated(decls[i].at)
			break
		}
		if p := decls[i].parent; p >= 0 && !kept[p] {
			continue
		}
		kept[i] = true
	}
	sort.SliceStable(order, func(a, b int) bool { return decls[order[a]].span.Start < decls[order[b]].span.Start })

	declared := map[string]bool{e.file: true}
	e.symbol(documentName(e.file), e.file, symbol.KindDocument, tomldoc.Span{End: len(e.d.Src)}, "", "")
	for _, i := range order {
		if !kept[i] {
			continue
		}
		d := decls[i]
		parent := e.file
		if d.parent >= 0 {
			parent = decls[d.parent].qualified
		}
		e.symbol(d.name, d.qualified, d.kind, d.span, parent, d.signature)
		declared[d.qualified] = true
	}
	for _, i := range order {
		if !kept[i] {
			continue
		}
		d := decls[i]
		parent := e.file
		if d.parent >= 0 {
			parent = decls[d.parent].qualified
		}
		e.ref(language.ReferenceDraft{Name: d.qualified, Kind: string(reference.KindContains), Container: parent,
			NameQualified: d.qualified, IdentityInRepository: true, TargetKinds: string(d.kind)}, d.at)
	}
	aliased := map[string]bool{}
	for _, r := range m.Refs {
		if !declared[r.Container] || r.Local && r.NameQualified != "" && !declared[r.NameQualified] {
			continue
		}
		if e.ref(language.ReferenceDraft{Name: r.Name, Kind: r.Kind, Container: r.Container, NameQualified: r.NameQualified,
			IdentityInRepository: r.InRepository, ConfidenceCap: r.Cap, TargetKinds: r.TargetKinds}, r.Span) {
			aliased[r.NameQualified] = true
		}
	}
	// The aliases of the references stated (a reference left out leaves
	// its aliases unread).
	for _, a := range m.Aliases {
		if aliased[a.Qualified] && declname.IsName(a.Qualified) && declname.IsName(a.Target) {
			e.aliases = append(e.aliases, language.AliasDraft{Qualified: a.Qualified, Target: a.Target,
				Location: e.loc(a.Span.Start, a.Span.Start), ConfidenceCap: a.Cap})
		}
	}
}

func documentName(file string) string {
	if i := strings.LastIndexAny(file, `/\`); i >= 0 {
		return file[i+1:]
	}
	return file
}

// candidates lists the tables and keys of the document that may be
// declarations, breadth first, and the identity of each.
func (e *extractor) candidates() ([]decl, map[*tomldoc.Node]string) {
	var decls []decl
	ids := map[*tomldoc.Node]string{}
	type item struct {
		n      *tomldoc.Node
		parent int
		ptr    string
		depth  int
	}
	queue := []item{{e.d.Root, -1, "", 0}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.depth >= maxDepth {
			continue
		}
		for _, c := range children(it.n) {
			ptr := it.ptr + "/" + c.token
			q := e.file + "#" + ptr
			if !declname.IsName(ptr) || !declname.IsName(q) {
				continue
			}
			ids[c.n] = q
			d := decl{node: c.n, parent: it.parent, depth: it.depth + 1, name: ptr, qualified: q, span: c.n.Span, at: c.n.KeySpan}
			d.kind, d.signature = kindOf(c.n, e.d.Src)
			decls = append(decls, d)
			if c.n.Kind != tomldoc.Value {
				queue = append(queue, item{c.n, len(decls) - 1, ptr, it.depth + 1})
			}
		}
	}
	return decls, ids
}

type child struct {
	n     *tomldoc.Node
	token string // its JSON Pointer token
}

// children are the entries of a table, the elements of an array of tables
// named as the package header states.
func children(n *tomldoc.Node) []child {
	var out []child
	if n.Kind == tomldoc.Table {
		for _, c := range n.Children {
			if declname.IsName(c.Key) || c.Key == "" {
				out = append(out, child{c, pointer(c.Key)})
			}
		}
		return out
	}
	named := map[string]int{}
	unnamed := 0
	for _, el := range n.Children {
		var tok string
		// A name with "#" could spell another element's token ("cli#2").
		if name, ok := el.Get("name").String(); ok && declname.IsName(name) && !strings.Contains(name, "#") {
			named[name]++
			tok = pointer(name)
			if k := named[name]; k > 1 {
				tok += "#" + strconv.Itoa(k)
			}
		} else {
			unnamed++
			tok = "#" + strconv.Itoa(unnamed)
		}
		out = append(out, child{el, tok})
	}
	return out
}

func kindOf(n *tomldoc.Node, src []byte) (symbol.SymbolKind, string) {
	switch n.Kind {
	case tomldoc.ArrayOfTables:
		return symbol.KindKey, "array of tables"
	case tomldoc.Value:
		return symbol.KindKey, string(n.VKind)
	}
	switch n.How {
	case tomldoc.Header, tomldoc.Element:
		if h := string(src[n.Header.Start:n.Header.End]); declname.IsName(h) {
			return symbol.KindTable, h
		}
		return symbol.KindTable, "table"
	case tomldoc.Inline:
		return symbol.KindTable, "inline table"
	case tomldoc.Dotted:
		return symbol.KindTable, "table (dotted keys)"
	}
	return symbol.KindTable, "table (implicit)"
}

func pointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func (e *extractor) symbol(name, qualified string, kind symbol.SymbolKind, sp tomldoc.Span, parent, signature string) {
	e.symbols = append(e.symbols, language.SymbolDraft{
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Location:  e.loc(sp.Start, sp.End),
		StartByte: uint32(sp.Start),
		EndByte:   uint32(sp.End),
		Parent:    parent,
		Signature: signature,
		Exported:  true,
	})
}

// ref states r; false when a bound leaves it out.
func (e *extractor) ref(r language.ReferenceDraft, sp tomldoc.Span) bool {
	if len(e.refs) >= maxRefs {
		e.truncated(sp)
		return false
	}
	r.Location = e.loc(sp.Start, sp.End)
	e.refs = append(e.refs, r)
	return true
}

func (e *extractor) truncated(sp tomldoc.Span) {
	if e.full {
		return
	}
	e.full = true
	e.diags = append(e.diags, language.Diagnostic{
		Severity: language.SeverityWarning,
		Code:     CodeTruncated,
		Message:  fmt.Sprintf("the file states more than %d declarations or %d references: the rest are left out", maxSymbols, maxRefs),
		Location: e.loc(sp.Start, sp.Start),
	})
}

// maxDiagnostics bounds the diagnostics a file reports.
const maxDiagnostics = tomldoc.MaxProblems

func (e *extractor) diagsFull() {
	if e.diagsCut {
		return
	}
	e.diagsCut = true
	e.diags = append(e.diags, language.Diagnostic{
		Severity: language.SeverityWarning,
		Code:     CodeTruncated,
		Message:  fmt.Sprintf("the file has more than %d problems: the rest are not reported", maxDiagnostics),
		Location: e.loc(0, 0),
	})
}

func (e *extractor) problem(p tomldoc.Problem) {
	if len(e.diags) >= maxDiagnostics {
		e.diagsFull()
		return
	}
	sev := language.SeverityWarning
	if p.Error {
		sev = language.SeverityError
	}
	e.diags = append(e.diags, language.Diagnostic{Severity: sev, Code: p.Code, Message: p.Message, Location: e.loc(p.Start, p.End)})
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
	return source.Location{File: source.FileID(e.file), Range: source.Range{Start: e.lines.pos(start), End: e.lines.pos(end)}}
}
