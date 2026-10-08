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
// An error-free tree is not proof of the intended derivation where the
// grammar is ambiguous (Go `f[T](x)`, TypeScript `f<T>(x)`): the extractors
// treat those shapes by the language's own rules, not by the tree alone.
//
// Ownership: Parse returns either a tree the caller owns and releases, or an
// error and no tree. Every tree it does not return — the production tree when
// a fallback is kept, a rejected fallback tree, any tree on an error or a
// panic — it releases itself, once.
//
// Dependency gate: the routes are gotreesitter v0.55.1 behavior, and the
// forest route is not part of its stable API. When upgrading gotreesitter,
// TestRoute_* must still pass (they pin which route each known input takes;
// a failure means the route behavior changed and the fallback must be
// re-measured against the reference runtime), as must TestParse_* (the
// contract Ark's analysis relies on, independent of routes).
package tsparse

import (
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
func parseRoute(lang *ts.Language, src []byte) (_ *ts.Tree, _ route, err error) {
	var owned []*ts.Tree // trees to release unless returned
	defer func() {
		if r := recover(); r != nil {
			for _, t := range owned {
				t.Release()
			}
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

	p := ts.NewParser(lang)
	p.SetAdmissionCandidateRoute(false)
	tree, err := p.Parse(src)
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
	if recovered, ok := ts.NewParser(lang).ParseForestExperimental(src); keep(recovered) != nil && ok && !recovered.RootNode().HasError() {
		return handOver(recovered), routeForest, nil
	}
	return handOver(tree), routeProduction, nil
}
