package tsparse

import (
	"strings"
	"testing"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// nestedRange is valid Go (go/parser accepts it; the reference Tree-sitter
// runtime with the same tree-sitter-go commit parses it without error) that
// gotreesitter's production route reads with `l {` as a composite literal,
// wrapping the rest of the function in an ERROR node. Minimized from
// internal/index/completeness_test.go, where it hid four test functions.
const nestedRange = `package q
func h() {
	for range l {
		z.e(func() {
			for range a {
				k.k(y.f(s.x), s.x != "")
			}
			for range s {
				if m != a.c[n] {
				}
			}
		})
	}
}
`

// nestedRangeLiteral is valid Go that both the production and the
// candidate route reject (the reference runtime parses it cleanly; its forest
// route does too, field names included). Its structure is minimized from a
// real test file whose remaining 29 functions it hid.
const nestedRangeLiteral = `package v1
func v2() {
	for v3, v4 := range v5 {
		v6.v7( func() {
			for v3, v8 := range v4.v9 {
				v9[v10.v11{}] = v12{
				}
			}
			if v13() > v13() {
				v6.v14("")
			}
			for v15, v16 := range v17 {
				if v16.v18().v19 != v4.v20[v15] {
					v6.v14( v4.v20[v15], )
				}
			}
		})
	}
}
`

// candidateOnly is valid Go the production route parses exactly as the
// reference runtime does, and the candidate route as a different, error-free
// tree (a type instantiation where the reference has a generic call).
const candidateOnly = `package p

func f() {
	x := &T{
		F: pkg.New[pkg.K, *other.V](),
	}
	_ = x
}
`

// routesDiffer is invalid Go both routes reject, with different trees.
const routesDiffer = "package app\n\nfunc Broken( {"

func candidate(t *testing.T, lang *ts.Language, src string) *ts.Tree {
	t.Helper()
	p := ts.NewParser(lang)
	p.SetAdmissionCandidateRoute(true)
	tree, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)
	return tree
}

func production(t *testing.T, lang *ts.Language, src string) *ts.Tree {
	t.Helper()
	p := ts.NewParser(lang)
	p.SetAdmissionCandidateRoute(false)
	tree, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)
	return tree
}

func parse(t *testing.T, lang *ts.Language, src string) *ts.Tree {
	t.Helper()
	tree, err := Parse(lang, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)
	return tree
}

// --- The contract Ark's analysis relies on (any gotreesitter version) ---

// referenceShape is a fragment of the reference runtime's tree for each
// valid repro (field names aside).
var referenceShape = map[string][]string{
	// `for range l` ranges over the identifier l; the braces are the loop's
	// block — no composite literal anywhere.
	nestedRange: {"(for_statement (range_clause (identifier)) (block"},
	// `range v4.v9` ranges over a selector; the composite literals are the
	// index `v10.v11{}` and its value only.
	nestedRangeLiteral: {"(range_clause (expression_list (identifier) (identifier)) (selector_expression (identifier) (field_identifier))) (block"},
	// A generic call, not a type instantiation.
	candidateOnly: {"(call_expression (selector_expression (identifier) (field_identifier)) (type_arguments"},
}

func TestParse_ValidSourceHasTheReferenceTree(t *testing.T) {
	lang := grammars.GoLanguage()
	for src, frags := range referenceShape {
		got := parse(t, lang, src).RootNode()
		if got.HasError() {
			t.Errorf("valid source has an error:\n%s", src)
			continue
		}
		sx := got.SExpr(lang)
		for _, f := range frags {
			if !strings.Contains(sx, f) {
				t.Errorf("tree lacks %s:\n%s", f, sx)
			}
		}
		if src == nestedRange && strings.Contains(sx, "composite_literal") {
			t.Errorf("composite literal in:\n%s", sx)
		}
		if src == nestedRangeLiteral && strings.Count(sx, "composite_literal") != 2 {
			t.Errorf("composite literals in:\n%s", sx)
		}
	}
}

func TestParse_InvalidSourceKeepsTheProductionTree(t *testing.T) {
	for name, c := range map[string]struct {
		lang *ts.Language
		src  string
	}{
		"go":               {grammars.GoLanguage(), "package a\nfunc A( {\nfunc B() {}\n"},
		"go-routes-differ": {grammars.GoLanguage(), routesDiffer},
		"php":              {grammars.PhpLanguage(), "<?php\nclass A { function m( {\n"},
		"ts":               {grammars.TypescriptLanguage(), "export class B {\n  m( {\n}\n"},
		"js":               {grammars.JavascriptLanguage(), "function f( {\n"},
		"python":           {grammars.PythonLanguage(), "def f(:\n    pass\n"},
		"hcl":              {grammars.HclLanguage(), "resource \"a\" \"b\" {\n  x = [\n"},
	} {
		prod := production(t, c.lang, c.src).RootNode()
		got := parse(t, c.lang, c.src).RootNode()
		if !got.HasError() {
			t.Errorf("%s: an invalid source parsed without error", name)
		}
		if prod.SExpr(c.lang) != got.SExpr(c.lang) {
			t.Errorf("%s: the production tree (and its diagnostics) was not kept", name)
		}
	}
}

// A production tree without an error is never replaced.
func TestParse_CleanProductionTreeIsKept(t *testing.T) {
	lang := grammars.GoLanguage()
	for _, src := range []string{candidateOnly, "package a\n\nfunc A() { B(x[i]) }\nfunc B[T any](t T) {}\n"} {
		prod := production(t, lang, src).RootNode()
		if prod.HasError() {
			continue // not this contract's input
		}
		if got := parse(t, lang, src).RootNode().SExpr(lang); got != prod.SExpr(lang) {
			t.Errorf("a clean production tree was replaced:\n%s", got)
		}
	}
}

func TestParse_Deterministic(t *testing.T) {
	lang := grammars.GoLanguage()
	for _, src := range []string{nestedRange, nestedRangeLiteral, candidateOnly, routesDiffer} {
		tree, r0, err := parseRoute(lang, []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		want := tree.RootNode().SExpr(lang)
		tree.Release()
		for i := 0; i < 20; i++ {
			tree, r, err := parseRoute(lang, []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if got := tree.RootNode().SExpr(lang); got != want || r != r0 {
				t.Fatalf("run %d differs (route %s, first %s)", i, r, r0)
			}
			tree.Release()
		}
	}
}

// The returned tree is the caller's: parsing on — releasing rejected and
// replaced trees back to gotreesitter's pools — never touches it.
func TestParse_ReturnedTreeSurvivesLaterParses(t *testing.T) {
	lang := grammars.GoLanguage()
	kept := map[string]*ts.Tree{}
	want := map[string]string{}
	for _, src := range []string{nestedRange, nestedRangeLiteral, candidateOnly, routesDiffer} {
		kept[src] = parse(t, lang, src)
		want[src] = kept[src].RootNode().SExpr(lang)
	}
	for i := 0; i < 50; i++ {
		for _, src := range []string{nestedRange, nestedRangeLiteral, routesDiffer} {
			tree, err := Parse(lang, []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			tree.Release()
		}
	}
	for src, tree := range kept {
		if got := tree.RootNode().SExpr(lang); got != want[src] {
			t.Errorf("a returned tree changed after later parses:\n%s", src)
		}
	}
}

// --- gotreesitter v0.55.1 route behavior (the dependency gate) ---
//
// These pin which route each known input takes. A failure after a
// gotreesitter upgrade means route behavior changed: re-measure the fallback
// against the reference runtime (ARCHITECTURE.md §3) before updating them.

func routeOf(t *testing.T, lang *ts.Language, src string) route {
	t.Helper()
	tree, r, err := parseRoute(lang, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	tree.Release()
	return r
}

func forestClean(t *testing.T, lang *ts.Language, src string) bool {
	t.Helper()
	tree, ok := ts.NewParser(lang).ParseForestExperimental([]byte(src))
	if !ok || tree == nil {
		return false
	}
	defer tree.Release()
	return !tree.RootNode().HasError()
}

func TestRoute_NestedRangeTakesTheCandidateRoute(t *testing.T) {
	lang := grammars.GoLanguage()
	if !production(t, lang, nestedRange).RootNode().HasError() {
		t.Fatal("precondition: the production route must reject nestedRange")
	}
	if candidate(t, lang, nestedRange).RootNode().HasError() {
		t.Fatal("precondition: the candidate route must parse nestedRange")
	}
	if r := routeOf(t, lang, nestedRange); r != routeCandidate {
		t.Errorf("route %s, want %s", r, routeCandidate)
	}
}

func TestRoute_RangeAndLiteralIndexTakesTheForestRoute(t *testing.T) {
	lang := grammars.GoLanguage()
	if !production(t, lang, nestedRangeLiteral).RootNode().HasError() || !candidate(t, lang, nestedRangeLiteral).RootNode().HasError() {
		t.Fatal("precondition: the production and candidate routes must reject nestedRangeLiteral")
	}
	if !forestClean(t, lang, nestedRangeLiteral) {
		t.Fatal("precondition: the forest route must parse nestedRangeLiteral")
	}
	if r := routeOf(t, lang, nestedRangeLiteral); r != routeForest {
		t.Errorf("route %s, want %s", r, routeForest)
	}
}

func TestRoute_DifferentCleanCandidateIsNotTaken(t *testing.T) {
	lang := grammars.GoLanguage()
	prod := production(t, lang, candidateOnly).RootNode()
	cand := candidate(t, lang, candidateOnly).RootNode()
	if prod.HasError() || cand.HasError() || prod.SExpr(lang) == cand.SExpr(lang) {
		t.Fatal("precondition: both routes must parse candidateOnly cleanly, into different trees")
	}
	if r := routeOf(t, lang, candidateOnly); r != routeProduction {
		t.Errorf("route %s, want %s", r, routeProduction)
	}
}

func TestRoute_ErringAlternativesAreNotTaken(t *testing.T) {
	lang := grammars.GoLanguage()
	prod := production(t, lang, routesDiffer).RootNode()
	cand := candidate(t, lang, routesDiffer).RootNode()
	if !prod.HasError() || !cand.HasError() || prod.SExpr(lang) == cand.SExpr(lang) {
		t.Fatal("precondition: both routes must reject routesDiffer, into different trees")
	}
	if forestClean(t, lang, routesDiffer) {
		t.Fatal("precondition: the forest route must not parse routesDiffer")
	}
	if r := routeOf(t, lang, routesDiffer); r != routeProduction {
		t.Errorf("route %s, want %s", r, routeProduction)
	}
}
