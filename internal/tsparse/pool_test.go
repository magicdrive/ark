package tsparse

import (
	"fmt"
	"strings"
	"testing"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestProductionPool_ConcurrentTreeOwnership(t *testing.T) {
	lang := grammars.GoLanguage()
	for worker := 0; worker < 16; worker++ {
		t.Run(fmt.Sprint(worker), func(t *testing.T) {
			t.Parallel()
			src := []byte(fmt.Sprintf("package p\nfunc F%d() {}\n", worker))
			want := production(t, lang, string(src)).RootNode().SExpr(lang)
			first, err := parseProduction(lang, src)
			if err != nil || first == nil {
				t.Fatalf("initial parse: %v", err)
			}
			defer first.Release()
			for i := 0; i < 64; i++ {
				tree, err := parseProduction(lang, []byte("package q\nvar X = 42\n"))
				if err != nil || tree == nil {
					t.Fatalf("subsequent parse: %v", err)
				}
				bad := tree.RootNode().HasError()
				tree.Release()
				if bad {
					t.Fatal("unexpected parse error")
				}
				if got := first.RootNode().SExpr(lang); got != want {
					t.Fatalf("retained tree changed: %s", got)
				}
			}
		})
	}
}

func TestProductionPool_Retention(t *testing.T) {
	lang := grammars.GoLanguage()
	clear := func() {
		productionParserPool.Lock()
		delete(productionParserPool.idle, lang.Name)
		productionParserPool.Unlock()
	}
	clear()
	t.Cleanup(clear)
	src := []byte("package p\nvar X = 1\n")
	run := func(src []byte) {
		t.Helper()
		tree, err := parseProduction(lang, src)
		if err != nil || tree == nil {
			t.Fatalf("parse: %v", err)
		}
		tree.Release()
	}
	idle := func() productionParserLease {
		productionParserPool.Lock()
		defer productionParserPool.Unlock()
		return productionParserPool.idle[lang.Name]
	}
	run(src)
	first := idle()
	if first.parser == nil || first.uses != 1 {
		t.Fatal("first lease not retained")
	}
	run(src)
	if got := idle(); got.parser != first.parser || got.uses != 2 {
		t.Fatal("parser not reused")
	}
	for i := 2; i < maxParserUses; i++ {
		run(src)
	}
	if idle().parser != nil {
		t.Fatal("expired lease retained")
	}
	run([]byte("package p\n//" + strings.Repeat("x", maxPooledSourceBytes)))
	if idle().parser != nil {
		t.Fatal("oversized source retained")
	}
	run(src)
	run([]byte(routesDiffer))
	if idle().parser != nil {
		t.Fatal("erroneous parse retained")
	}
}

func BenchmarkProductionPool(b *testing.B) {
	for _, fixture := range []struct {
		name string
		lang *ts.Language
		src  string
	}{
		{"Go", grammars.GoLanguage(), "package p\nfunc F(x int) int { return x + 1 }\n"},
		{"PHP", grammars.PhpLanguage(), "<?php class A { public function f() { return 1; } }"},
		{"TypeScript", grammars.TypescriptLanguage(), "export function f(x: number): number { return x + 1; }"},
	} {
		for _, mode := range []string{"Pooled", "Fresh"} {
			b.Run(fixture.name+"/"+mode, func(b *testing.B) {
				src := []byte(fixture.src)
				b.ReportAllocs()
				b.SetBytes(int64(len(src)))
				for i := 0; i < b.N; i++ {
					var tree *ts.Tree
					var err error
					if mode == "Pooled" {
						tree, err = parseProduction(fixture.lang, src)
					} else {
						p := ts.NewParser(fixture.lang)
						p.SetAdmissionCandidateRoute(false)
						tree, err = p.Parse(src)
					}
					if err != nil || tree == nil {
						b.Fatalf("parse: %v", err)
					}
					bad := tree.RootNode().HasError()
					tree.Release()
					if bad {
						b.Fatal("unexpected parse error")
					}
				}
			})
		}
	}
}
