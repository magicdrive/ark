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
// The forest route is not part of gotreesitter's stable API; the version is
// pinned and TestParse_* pin the behavior relied on here.
package tsparse

import (
	ts "github.com/odvcencio/gotreesitter"
)

// Parse parses src with lang. The caller releases the returned tree.
func Parse(lang *ts.Language, src []byte) (*ts.Tree, error) {
	p := ts.NewParser(lang)
	p.SetAdmissionCandidateRoute(false)
	tree, err := p.Parse(src)
	if err != nil || tree == nil || !tree.RootNode().HasError() {
		return tree, err
	}
	alt := ts.NewParser(lang)
	alt.SetAdmissionCandidateRoute(true)
	if recovered, altErr := alt.Parse(src); altErr == nil && recovered != nil {
		if !recovered.RootNode().HasError() {
			tree.Release()
			return recovered, nil
		}
		recovered.Release()
	}
	forest := ts.NewParser(lang)
	if recovered, ok := forest.ParseForestExperimental(src); ok && recovered != nil {
		if !recovered.RootNode().HasError() {
			tree.Release()
			return recovered, nil
		}
		recovered.Release()
	}
	return tree, nil
}
