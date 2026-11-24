package php

import (
	"context"
	"testing"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/tsparse"
)

// Keep the MCP regression fixture at the provider boundary, where a missing
// declaration can be distinguished from a graph resolution failure.
func TestPHPProvider_MixedClassParserCompatibility(t *testing.T) {
	src := []byte(`<?php
namespace App\Services;

use App\Models\A;

class Mixed
{
    public function none() { return 1; }
    public function onlyExternal($c) { return $c->makeWith('x'); }
    public function partial(A $a, $x, $m)
    {
        $a->save();
        $x->save();
        $this->$m();
        return new $m();
    }
    public function classString()
    {
        $cls = A::class;
        $obj = new $cls();
        return app(A::class);
    }
}
`)
	ex, err := NewProvider().Extract(context.Background(), source.FileID("Mixed.php"), src)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, sym := range ex.Symbols {
		found[sym.Qualified] = true
	}
	for _, name := range []string{"none", "onlyExternal", "partial", "classString"} {
		if !found[`App\Services\Mixed.`+name] {
			t.Errorf("missing method %s; extracted symbols: %v", name, found)
		}
	}
	if len(ex.Diagnostics) != 0 {
		t.Errorf("valid PHP produced diagnostics: %+v", ex.Diagnostics)
	}
	if !t.Failed() {
		return
	}

	lang := grammars.PhpLanguage()
	dump := func(label string, tree *ts.Tree, err error) {
		if tree == nil {
			t.Logf("%s: no tree, error=%v", label, err)
			return
		}
		defer tree.Release()
		root := tree.RootNode()
		t.Logf("%s: hasError=%v error=%v tree=%s", label, root.HasError(), err, root.SExpr(lang))
	}
	tree, err := tsparse.Parse(lang, src)
	dump("Ark", tree, err)
	for _, candidate := range []bool{false, true} {
		p := ts.NewParser(lang)
		p.SetAdmissionCandidateRoute(candidate)
		tree, err := p.Parse(src)
		label := "legacy"
		if candidate {
			label = "candidate"
		}
		dump(label, tree, err)
	}
	tree, accepted := ts.NewParser(lang).ParseForestExperimental(src)
	t.Logf("forest accepted=%v", accepted)
	dump("forest", tree, nil)
}
