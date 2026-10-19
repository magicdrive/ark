// Package tsparse is the one way Ark parses source with gotreesitter.
//
// gotreesitter's production GLR route rejects some valid source that the
// reference Tree-sitter runtime, with the same grammar, parses cleanly (e.g. a
// Go `for range x {` whose `x {` it keeps reading as a composite literal
// inside nested function literals): the region becomes an ERROR node and the
// declarations in it are lost. Its other routes — the admission candidate
// route and the GSS-forest route — parse some of those inputs, but replacing
// the production route with either everywhere changes the trees of files the
// production route already parses correctly (differential testing against the
// reference runtime: candidate route, Go 75 regressions; forest route, Go 19,
// TypeScript 4).
//
// So Parse keeps the production tree unless it contains an error, and only
// then tries the candidate route and, if that also errs, the forest route,
// keeping a tree only if it contains no error. A tree without an error is a
// complete derivation of the input by the grammar, so the fallback never
// invents structure the grammar does not derive; it only recovers a
// derivation the production route missed. When every route reports errors
// (or the forest route declines), the production tree — and its
// diagnostics — are kept exactly as before. The process-wide default is never
// changed.
//
// The forest route's cost is not predictable: comparing the alternatives it
// keeps can walk every comment before them again, so a run of comments after
// an ambiguous prefix (Rust: an attribute followed by doc comments) costs
// time cubic in its length — tens of seconds on a 44 KB file — and it is
// spent in full even when the route then declines. So, for a language whose
// tokens do not depend on the parse (probeComments), Parse first probes the
// forest route on a copy of the source in which the production tree's
// comments are blanked — each byte replaced by a space, line breaks inside a
// block comment included, so every offset is kept — and runs the forest
// route on the source only when the probe accepts. The probe's tree is never
// returned.
//
// Why a declining probe means the forest route would decline (the claim the
// skip rests on): a comment is an extra, which the grammar admits between
// any two tokens, exactly like whitespace; replacing it by whitespace that
// adds no line-break token (tree-sitter-go's `\n` is one: the statement
// terminator) changes which token sequences derive only if some derivation
// reads its bytes as something other than a comment. In the Go and Rust
// grammars a comment is valid in every lex state and is read as a comment
// wherever it starts — their lexical grammars, unlike JavaScript's or PHP's,
// give a comment no other reading — so the comment the production route
// read outside an ERROR is a comment in every derivation. This is an
// argument about the grammars, checked by differential tests, not a proof
// about the runtime. Only comments outside an ERROR
// are blanked: inside one, the production route lexed in error recovery. For
// any other language — where the parse decides the tokens (JavaScript: a
// regular expression or a division; TSX: JSX text; PHP: `?>` ends a comment;
// Python: indentation) — no probe runs and the forest route runs as before.
// A probe that declined wrongly could only keep the production tree and its
// diagnostics: it never makes Parse return a tree the grammar does not
// derive.
//
// An error-free tree is not proof of the intended derivation where the
// grammar is ambiguous (Go `f[T](x)`, TypeScript `f<T>(x)`): the extractors
// treat those shapes by the language's own rules, not by the tree alone.
//
// Ownership: Parse returns either a tree the caller owns and releases, or an
// error and no tree. Every tree it does not return — the production tree when
// a fallback is kept, a rejected fallback tree, any tree on an error or a
// panic — it releases itself, once.
//
// Dependency gate: the routes are gotreesitter v0.56.0 behavior, and the
// forest route is not part of its stable API. When upgrading gotreesitter,
// TestRoute_* must still pass (they pin which route each known input takes;
// a failure means the route behavior changed and the fallback must be
// re-measured against the reference runtime), as must TestParse_* (the
// contract Ark's analysis relies on, independent of routes).
package tsparse

import (
	"sync"

	ts "github.com/odvcencio/gotreesitter"
)

// route names the parser route whose tree Parse returned.
type route string

const (
	routeProduction route = "production"
	routeCandidate  route = "candidate"
	routeForest     route = "forest"
)

// Parse parses src with lang. The caller releases the returned tree.
func Parse(lang *ts.Language, src []byte) (*ts.Tree, error) {
	tree, _, err := parseRoute(lang, src)
	return tree, err
}

// parseRoute is Parse, also naming the route of the returned tree.
func parseRoute(lang *ts.Language, src []byte) (*ts.Tree, route, error) {
	return parseTraced(lang, src, nil)
}

// parseTraced is parseRoute, counting forest runs into trace.
func parseTraced(lang *ts.Language, src []byte, trace *routeTrace) (_ *ts.Tree, _ route, err error) {
	var owned []*ts.Tree // trees to release unless returned
	defer func() {
		if r := recover(); r != nil {
			for _, t := range owned {
				t.Release()
			}
			// A parser a panic interrupted went back to the pool in whatever
			// state the panic left: drop the pool, later parses build anew.
			productionPools.Delete(lang.Name)
			panic(r)
		}
	}()
	keep := func(t *ts.Tree) *ts.Tree {
		if t != nil {
			owned = append(owned, t)
		}
		return t
	}
	// handOver releases every owned tree but t, which the caller now owns.
	handOver := func(t *ts.Tree) *ts.Tree {
		for _, o := range owned {
			if o != t {
				o.Release()
			}
		}
		owned = nil
		return t
	}

	tree, err := parseProduction(lang, src)
	keep(tree)
	if err != nil || tree == nil {
		handOver(nil)
		return nil, "", err
	}
	if !tree.RootNode().HasError() {
		return handOver(tree), routeProduction, nil
	}
	alt := ts.NewParser(lang)
	alt.SetAdmissionCandidateRoute(true)
	if recovered, altErr := alt.Parse(src); keep(recovered) != nil && altErr == nil && !recovered.RootNode().HasError() {
		return handOver(recovered), routeCandidate, nil
	}
	if !forestCanDerive(lang, tree, src, trace) {
		return handOver(tree), routeProduction, nil
	}
	trace.forest()
	if recovered, ok := ts.NewParser(lang).ParseForestExperimental(src); keep(recovered) != nil && ok && !recovered.RootNode().HasError() {
		return handOver(recovered), routeForest, nil
	}
	return handOver(tree), routeProduction, nil
}

// productionPools holds one gotreesitter ParserPool per language. Building
// a Parser rebuilds per-language tables (about 0.7 ms for Rust), which on a
// small file costs more than parsing it; every file takes the production
// route, so its parsers are reused. A pooled Parser follows the process-wide
// admission default, which Ark never changes and which is off: the
// production route, as an explicit SetAdmissionCandidateRoute(false) asks.
// Should the default ever be on, the pool is not used. ParserPool.Parse
// returns its parser to the pool whatever the parse does, and resets it
// before and after each parse; after a panic the language's pool is dropped
// (parseTraced) — a parse that loaded the pool before the drop may still
// take the parser the panic interrupted, reset by the pool.
var productionPools sync.Map // language name -> *ts.ParserPool

// parseProduction parses src on the production route.
func parseProduction(lang *ts.Language, src []byte) (*ts.Tree, error) {
	if ts.AdmissionCandidateRouteDefault() {
		p := ts.NewParser(lang)
		p.SetAdmissionCandidateRoute(false)
		return p.Parse(src)
	}
	pool, ok := productionPools.Load(lang.Name)
	if !ok {
		pool, _ = productionPools.LoadOrStore(lang.Name, ts.NewParserPool(lang))
	}
	if pp := pool.(*ts.ParserPool); pp.Language() == lang {
		return pp.Parse(src)
	}
	// A grammar loaded anew (gotreesitter's grammar cache evicts on
	// request): its own pool replaces the old one, so the pools stay one
	// per language.
	pp := ts.NewParserPool(lang)
	productionPools.Store(lang.Name, pp)
	return pp.Parse(src)
}

// probeComments are, per language whose tokens do not depend on the parse,
// the node types of its comments: the only nodes the probe blanks.
var probeComments = map[string]map[string]bool{
	"go":   {"comment": true},
	"rust": {"line_comment": true, "block_comment": true},
}

// forestCanDerive probes the forest route on src with the comments of the
// production tree blanked (see the package comment). It reports false only
// when the probe declines; a language without probeComments, and a source
// with no comment to blank, are not probed.
func forestCanDerive(lang *ts.Language, production *ts.Tree, src []byte, trace *routeTrace) bool {
	comments := probeComments[lang.Name]
	if comments == nil {
		return true
	}
	blanked, ok := blankComments(lang, comments, production.RootNode(), src)
	if !ok {
		return true
	}
	trace.probe()
	probe, accepted := ts.NewParser(lang).ParseForestExperimental(blanked)
	if probe != nil {
		defer probe.Release()
	}
	return accepted && probe != nil && !probe.RootNode().HasError()
}

// blankComments returns a copy of src in which every comment of root — an
// extra node of one of the comments types, holding no error — that is not
// inside an ERROR or MISSING node is blanked: each of its bytes becomes a
// space, line breaks inside a block comment included (the grammar reads the
// comment as one extra token; a kept line break would be tree-sitter-go's
// `\n` terminator token), a line break that ends the node (a Rust doc
// comment's) excepted. ok is false when there is none.
func blankComments(lang *ts.Language, comments map[string]bool, root *ts.Node, src []byte) (_ []byte, ok bool) {
	var out []byte
	stack := []*ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.IsError() || n.IsMissing() {
			continue
		}
		if n.IsExtra() {
			if n.HasError() || !comments[n.Type(lang)] {
				continue
			}
			if out == nil {
				out = append([]byte(nil), src...)
			}
			end := int(n.EndByte())
			for end > int(n.StartByte()) && end <= len(out) && (out[end-1] == '\n' || out[end-1] == '\r') {
				end-- // a line comment's own line break ends the line as before
			}
			for i := int(n.StartByte()); i < end && i < len(out); i++ {
				out[i] = ' ' // line breaks inside a block comment too: the comment is one extra
			}
			continue
		}
		for i := n.ChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	return out, out != nil
}

// routeTrace counts the forest runs of one parse (tests); nil counts
// nothing.
type routeTrace struct{ probes, forests int }

func (t *routeTrace) probe() {
	if t != nil {
		t.probes++
	}
}

func (t *routeTrace) forest() {
	if t != nil {
		t.forests++
	}
}
