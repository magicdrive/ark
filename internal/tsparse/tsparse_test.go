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

func TestParse_RecoversValidSourceTheProductionRouteRejects(t *testing.T) {
	lang := grammars.GoLanguage()
	if !production(t, lang, nestedRange).RootNode().HasError() {
		t.Log("the production route now parses the repro: the fallback is no longer exercised by it")
	}
	got := parse(t, lang, nestedRange).RootNode()
	if got.HasError() {
		t.Fatal("valid source still has an error")
	}
	sx := got.SExpr(lang)
	// The reference tree: `for range l` ranges over the identifier l and
	// the braces are the loop's block — no composite literal anywhere.
	if strings.Contains(sx, "composite_literal") || !strings.Contains(sx, "(for_statement (range_clause (identifier)) (block") {
		t.Errorf("tree differs from the reference shape:\n%s", sx)
	}
}

func TestParse_InvalidSourceKeepsTheProductionTree(t *testing.T) {
	for name, c := range map[string]struct {
		lang *ts.Language
		src  string
	}{
		"go": {grammars.GoLanguage(), "package a\nfunc A( {\nfunc B() {}\n"},
		// Both routes err here, with different trees: an erroring
		// alternative must never replace the production tree.
		"go-routes-differ": {grammars.GoLanguage(), "package app\n\nfunc Broken( {"},
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
			t.Errorf("%s: the production tree was not kept", name)
		}
	}
}

func TestParse_ValidSourceIsTheProductionTree(t *testing.T) {
	lang := grammars.GoLanguage()
	src := "package a\n\nfunc A() { B(x[i]) }\nfunc B[T any](t T) {}\n"
	if production(t, lang, src).RootNode().SExpr(lang) != parse(t, lang, src).RootNode().SExpr(lang) {
		t.Error("a valid source the production route parses must keep its tree")
	}
}

func TestParse_ForestRouteRecoversWhatBothOtherRoutesReject(t *testing.T) {
	lang := grammars.GoLanguage()
	if !production(t, lang, nestedRangeLiteral).RootNode().HasError() || !candidate(t, lang, nestedRangeLiteral).RootNode().HasError() {
		t.Log("a route now parses the repro: the forest fallback is no longer exercised by it")
	}
	got := parse(t, lang, nestedRangeLiteral).RootNode()
	if got.HasError() {
		t.Fatal("valid source still has an error")
	}
	sx := got.SExpr(lang)
	// The reference tree: `range v4.v9` ranges over a selector, and the
	// composite literal is the index `v10.v11{}` only.
	if !strings.Contains(sx, "(range_clause (expression_list (identifier) (identifier)) (selector_expression (identifier) (field_identifier))) (block") ||
		strings.Count(sx, "composite_literal") != 2 {
		t.Errorf("tree differs from the reference shape:\n%s", sx)
	}
}

// The candidate route is only a fallback: where the production tree has no
// error it is kept even though the candidate route also parses cleanly.
func TestParse_ProductionTreeWinsOverADifferentCleanCandidate(t *testing.T) {
	lang := grammars.GoLanguage()
	prod := production(t, lang, candidateOnly).RootNode()
	cand := candidate(t, lang, candidateOnly).RootNode()
	if prod.HasError() || cand.HasError() || prod.SExpr(lang) == cand.SExpr(lang) {
		t.Log("the routes no longer differ on this input: the assertion below is vacuous")
	}
	if got := parse(t, lang, candidateOnly).RootNode().SExpr(lang); got != prod.SExpr(lang) {
		t.Errorf("Parse returned the candidate tree:\n%s", got)
	}
}

func TestParse_Deterministic(t *testing.T) {
	lang := grammars.GoLanguage()
	for _, src := range []string{nestedRange, nestedRangeLiteral} {
		want := parse(t, lang, src).RootNode().SExpr(lang)
		for i := 0; i < 20; i++ {
			if got := parse(t, lang, src).RootNode().SExpr(lang); got != want {
				t.Fatalf("run %d differs", i)
			}
		}
	}
}
