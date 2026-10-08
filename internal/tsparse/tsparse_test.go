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
		"go":     {grammars.GoLanguage(), "package a\nfunc A( {\nfunc B() {}\n"},
		"php":    {grammars.PhpLanguage(), "<?php\nclass A { function m( {\n"},
		"ts":     {grammars.TypescriptLanguage(), "export class B {\n  m( {\n}\n"},
		"js":     {grammars.JavascriptLanguage(), "function f( {\n"},
		"python": {grammars.PythonLanguage(), "def f(:\n    pass\n"},
		"hcl":    {grammars.HclLanguage(), "resource \"a\" \"b\" {\n  x = [\n"},
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

func TestParse_Deterministic(t *testing.T) {
	lang := grammars.GoLanguage()
	want := parse(t, lang, nestedRange).RootNode().SExpr(lang)
	for i := 0; i < 20; i++ {
		if got := parse(t, lang, nestedRange).RootNode().SExpr(lang); got != want {
			t.Fatalf("run %d differs", i)
		}
	}
}
