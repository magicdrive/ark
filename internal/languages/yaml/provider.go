// Package yaml is Ark's YAML provider (.yaml, .yml).
//
// Two layers, kept apart. The document layer (yamldoc) reads the YAML
// stream with go.yaml.in/yaml/v3 and knows nodes, their bytes and the
// stream's problems. The meaning layer (actions) reads a GitHub Actions
// workflow or action from those nodes. This package states, for every YAML
// file:
//
//   - the anchors (&name) of its documents — "<FileID>#&name", the n-th
//     definition of a name in the file "<FileID>#&name#n" — and a
//     value_reference from each alias (*name) to the anchor it names
//     (Exact: the document fixes it), contained by the innermost
//     declaration around the alias;
//   - the problems yamldoc and actions report, as diagnostics;
//
// and, for a GitHub Actions file, what actions states; for any other file,
// the file itself (a document) and the keys of its mappings that a path of
// mappings reaches from a document's root, at most maxKeyDepth deep, by
// their JSON Pointer: "<FileID>#/a/b" in the first document,
// "<FileID>#<n>/a/b" in the n-th (from 0) after it. Scalar values, and keys
// under a sequence, are no declarations.
//
// Identity. Every file is IdentityOnly: a reference reaches a declaration
// only by its qualified identity, never by a name match — a key "name" of
// one file is no key of another.
//
// Bounds. yamldoc reads at most yamldoc.MaxBytes; a file states at most
// maxSymbols declarations and maxRefs references (yaml_truncated past
// them). Aliases are never expanded.
package yaml

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	goyaml "go.yaml.in/yaml/v3"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/yaml/actions"
	"github.com/magicdrive/ark/internal/languages/yaml/yamldoc"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

const cacheVersion = "yaml-1"

const (
	maxKeyDepth = 3
	maxSymbols  = 5000
	maxRefs     = 20000
)

// CodeTruncated reports a file whose declarations or references go past
// the provider's bounds.
const CodeTruncated = "yaml_truncated"

// Provider implements language.Provider for YAML.
type Provider struct{}

// NewProvider returns the YAML provider.
func NewProvider() *Provider { return &Provider{} }

func (*Provider) Language() language.Language { return "yaml" }
func (*Provider) Extensions() []string        { return []string{".yaml", ".yml"} }
func (*Provider) CacheVersion() string        { return cacheVersion }

// Extract states the file's declarations, references and problems.
func (*Provider) Extract(ctx context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	if err := ctx.Err(); err != nil {
		return language.Extraction{}, err
	}
	e := &extractor{file: file, s: yamldoc.Parse(src), lines: newLines(src)}
	e.run()
	return language.Extraction{
		Symbols:      e.symbols,
		References:   e.refs,
		Diagnostics:  e.diags,
		IdentityOnly: true,
	}, nil
}

type extractor struct {
	file     source.FileID
	s        *yamldoc.Stream
	lines    lines
	symbols  []language.SymbolDraft
	spans    []yamldoc.Span // of symbols, by index
	refs     []language.ReferenceDraft
	diags    []language.Diagnostic
	full     bool // a bound was reached and reported
	diagsCut bool // more diagnostics than maxDiagnostics, reported
}

func (e *extractor) run() {
	for _, p := range e.s.Problems {
		e.problem(p)
	}
	if e.s.ProblemsTruncated {
		e.diagsFull()
	}
	if r, ok := actions.Extract(string(e.file), e.s, maxSymbols, maxRefs); ok {
		for _, s := range r.Symbols {
			e.symbol(s.Name, s.Qualified, s.Kind, s.Span, s.Parent, s.Signature)
		}
		for _, r := range r.Refs {
			e.ref(language.ReferenceDraft{
				Name: r.Name, Kind: r.Kind, Container: r.Container, NameQualified: r.NameQualified,
				FileIdentity: r.FileIdentity, IdentityInRepository: r.InRepository, Dynamic: r.Dynamic,
				ConfidenceCap: r.Cap, TargetKinds: r.TargetKinds, IsCall: r.Kind == string(reference.KindCall),
			}, r.Span)
		}
		for _, p := range r.Problems {
			e.problem(p)
		}
		if r.ProblemsTruncated {
			e.diagsFull()
		}
	} else {
		e.symbol(path.Base(string(e.file)), string(e.file), symbol.KindDocument, yamldoc.Span{End: len(e.s.Src)}, "", "")
		for _, d := range e.s.Docs {
			prefix := string(e.file) + "#"
			if d.Index > 0 {
				prefix += strconv.Itoa(d.Index)
			}
			e.keys(d.Root, prefix, "", string(e.file), 1)
		}
	}
	e.anchors()
}

// keys states the keys of the mapping m (and of the mappings their values
// are, depth by depth) at the JSON Pointer path of a document whose
// declarations' identities start with prefix ("<FileID>#", "<FileID>#<n>").
// A key's name is its pointer ("/server/tls"), so a bare name of another
// language's declaration never meets a data key.
func (e *extractor) keys(m *goyaml.Node, prefix, at, parent string, depth int) {
	if m == nil || m.Kind != goyaml.MappingNode || depth > maxKeyDepth {
		return
	}
	seen := map[string]bool{}
	yamldoc.Pairs(m, func(k, v *goyaml.Node) {
		// A key that is no one-line name declares nothing (it is data
		// that would reach the output as an identifier); the second of
		// a duplicate key is a diagnostic only.
		if k.Kind != goyaml.ScalarNode || k.Tag == "!!merge" || !yamldoc.IsName(k.Value) || seen[k.Tag+"\x00"+k.Value] {
			return
		}
		seen[k.Tag+"\x00"+k.Value] = true
		ptr := at + "/" + pointer(k.Value)
		if e.symbol(ptr, prefix+ptr, symbol.KindKey, e.s.EntrySpan(k, v), parent, "") {
			e.keys(v, prefix, ptr, prefix+ptr, depth+1)
		}
	})
}

func pointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// anchors states every anchor of the file and the references of its
// aliases.
func (e *extractor) anchors() {
	type alias struct {
		n   *goyaml.Node
		pos int
	}
	identity := map[*goyaml.Node]string{}
	count := map[string]int{}
	var aliases []alias
	seen := map[*goyaml.Node]bool{}
	var walk func(n *goyaml.Node)
	walk = func(n *goyaml.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		if n.Kind == goyaml.AliasNode {
			aliases = append(aliases, alias{n, e.s.Start(n)})
			return
		}
		if n.Anchor != "" && yamldoc.IsName(n.Anchor) {
			count[n.Anchor]++
			q := string(e.file) + "#&" + n.Anchor
			if c := count[n.Anchor]; c > 1 {
				q += "#" + strconv.Itoa(c)
			}
			identity[n] = q
			e.symbol("&"+n.Anchor, q, symbol.KindAnchor, e.s.NodeSpan(n), "", "")
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, d := range e.s.Docs {
		walk(d.Root)
	}
	if len(aliases) == 0 {
		return
	}
	for _, a := range aliases {
		q, ok := identity[a.n.Alias]
		if !ok {
			continue
		}
		if len(e.refs) >= maxRefs {
			e.truncated(e.s.NodeSpan(a.n))
			break
		}
		sp := e.s.NodeSpan(a.n)
		e.ref(language.ReferenceDraft{
			Name: a.n.Value, Kind: string(reference.KindValueReference), Container: e.container(sp),
			NameQualified: q, IdentityInRepository: true, TargetKinds: string(symbol.KindAnchor),
		}, sp)
	}
}

// container names the innermost declaration whose bytes hold an alias at sp
// ("" for none). A declaration located at the alias itself — content the
// alias repeats (`steps: *common`) — is the alias's content, not around it.
func (e *extractor) container(sp yamldoc.Span) string {
	best := -1
	for i, s := range e.spans {
		if s == sp || s.Start > sp.Start || sp.End > s.End {
			continue
		}
		if best < 0 || s.End-s.Start <= e.spans[best].End-e.spans[best].Start {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return e.symbols[best].Qualified
}

func (e *extractor) symbol(name, qualified string, kind symbol.SymbolKind, sp yamldoc.Span, parent, signature string) bool {
	if len(e.symbols) >= maxSymbols {
		e.truncated(sp)
		return false
	}
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
	e.spans = append(e.spans, sp)
	return true
}

func (e *extractor) ref(r language.ReferenceDraft, sp yamldoc.Span) {
	if len(e.refs) >= maxRefs {
		e.truncated(sp)
		return
	}
	r.Location = e.loc(sp.Start, sp.End)
	e.refs = append(e.refs, r)
}

func (e *extractor) truncated(sp yamldoc.Span) {
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
const maxDiagnostics = 1000

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

func (e *extractor) problem(p yamldoc.Problem) {
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
	return source.Location{File: e.file, Range: source.Range{Start: e.lines.pos(start), End: e.lines.pos(end)}}
}
