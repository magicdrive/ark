// Package tsparse is the one way Ark parses source with gotreesitter.
//
// gotreesitter's production GLR route rejects some valid source that the
// reference Tree-sitter runtime, with the same grammar, parses cleanly (e.g. a
// Go `for range x {` whose `x {` it keeps reading as a composite literal
// inside nested function literals): the region becomes an ERROR node and the
// declarations in it are lost. Its admission candidate route parses those
// inputs — but replacing the production route everywhere changes the trees of
// files the production route already parses correctly (differential testing
// against the reference runtime: Go, 75 regressions).
//
// So Parse keeps the production tree unless it contains an error, and only
// then tries the candidate route, keeping that tree only if it contains no
// error. A tree without an error is a complete derivation of the input by the
// grammar, so the fallback never invents structure the grammar does not
// derive; it only recovers a derivation the production route missed. When
// both routes report errors, the production tree — and its diagnostics — are
// kept exactly as before. The process-wide default is never changed.
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
	recovered, altErr := alt.Parse(src)
	if altErr != nil || recovered == nil {
		return tree, nil
	}
	if recovered.RootNode().HasError() {
		recovered.Release()
		return tree, nil
	}
	tree.Release()
	return recovered, nil
}
