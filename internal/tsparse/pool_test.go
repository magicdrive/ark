package tsparse

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// treeSnapshot records all nodes, including anonymous tokens and their spans.
func treeSnapshot(lang *ts.Language, tree *ts.Tree, src []byte) string {
	var out strings.Builder
	var walk func(*ts.Node)
	walk = func(n *ts.Node) {
		fmt.Fprintf(&out, "%s %d:%d %v:%v %q\n", n.Type(lang), n.StartByte(), n.EndByte(), n.StartPoint(), n.EndPoint(), src[n.StartByte():n.EndByte()])
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return out.String()
}

func TestProductionPool_ConcurrentTreeOwnership(t *testing.T) {
	for _, fixture := range []struct {
		lang       *ts.Language
		src, other string
	}{
		{grammars.GoLanguage(), "package p\nfunc F%d() {}\n", "package q\nvar X = 42\n"},
		{grammars.PhpLanguage(), "<?php function F%d() {}\n", "<?php class Other { public function run() { return 42; } }"},
		{grammars.TypescriptLanguage(), "export function F%d() {}\n", "export const other = { value: 42 };\n"},
	} {
		for worker := 0; worker < 8; worker++ {
			t.Run(fmt.Sprintf("%s/%d", fixture.lang.Name, worker), func(t *testing.T) {
				t.Parallel()
				lang := fixture.lang
				src := []byte(fmt.Sprintf(fixture.src, worker))
				want := treeSnapshot(lang, production(t, lang, string(src)), src)
				first, err := parseProduction(lang, src)
				if err != nil || first == nil {
					t.Fatalf("initial parse: %v", err)
				}
				defer first.Release()
				for i := 0; i < 64; i++ {
					other := []byte(fixture.other + strings.Repeat(" ", i*16))
					tree, err := parseProduction(lang, other)
					if err != nil || tree == nil {
						t.Fatalf("subsequent parse: %v", err)
					}
					bad := tree.RootNode().HasError()
					tree.Release()
					if bad {
						t.Fatal("unexpected parse error")
					}
					if got := treeSnapshot(lang, first, src); got != want {
						t.Fatalf("retained tree changed:\n%s", got)
					}
				}
			})
		}
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

// TestProductionPool_HeapPlateau is an opt-in diagnostic, not a timing gate.
// Compare live heap after full GC at equal lease-cycle boundaries.
func TestProductionPool_HeapPlateau(t *testing.T) {
	if os.Getenv("ARK_POOL_HEAP_PROBE") != "1" {
		t.Skip("set ARK_POOL_HEAP_PROBE=1")
	}
	lang := grammars.GoLanguage()
	src := []byte("package p\n" + strings.Repeat("var X = 42\n", 256))
	for batch := 0; batch < 6; batch++ {
		for i := 0; i < 1024; i++ {
			tree, err := parseProduction(lang, src)
			if err != nil || tree == nil {
				t.Fatalf("parse: %v", err)
			}
			bad := tree.RootNode().HasError()
			tree.Release()
			if bad {
				t.Fatal("unexpected parse error")
			}
		}
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		t.Logf("batch=%d parses=%d heap_alloc=%d heap_objects=%d heap_inuse=%d", batch, (batch+1)*1024, m.HeapAlloc, m.HeapObjects, m.HeapInuse)
	}
}

// TestProductionPool_IdleHeap measures a live lease near its retirement age.
func TestProductionPool_IdleHeap(t *testing.T) {
	if os.Getenv("ARK_POOL_HEAP_PROBE") != "1" {
		t.Skip("set ARK_POOL_HEAP_PROBE=1")
	}
	lang := grammars.GoLanguage()
	clear := func() {
		productionParserPool.Lock()
		delete(productionParserPool.idle, lang.Name)
		productionParserPool.Unlock()
	}
	clear()
	t.Cleanup(clear)
	measure := func(label string) {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		t.Logf("%s heap_alloc=%d heap_objects=%d heap_inuse=%d", label, m.HeapAlloc, m.HeapObjects, m.HeapInuse)
	}
	for batch := 0; batch < 6; batch++ {
		for i := 0; i < maxParserUses-1; i++ {
			src := []byte(fmt.Sprintf("package p\nvar X = %d\n", i) + strings.Repeat("// padding\n", 2048))
			tree, err := parseProduction(lang, src)
			if err != nil || tree == nil {
				t.Fatalf("parse: %v", err)
			}
			bad := tree.RootNode().HasError()
			tree.Release()
			if bad {
				t.Fatal("unexpected parse error")
			}
		}
		productionParserPool.Lock()
		age := productionParserPool.idle[lang.Name].uses
		productionParserPool.Unlock()
		if age != maxParserUses-1 {
			t.Fatalf("idle lease age = %d", age)
		}
		measure(fmt.Sprintf("batch=%d idle_age=%d", batch, age))
		clear()
		measure(fmt.Sprintf("batch=%d cleared", batch))
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

func BenchmarkMigrationWorkload(b *testing.B) {
	lang := grammars.GoLanguage()
	for _, fixture := range []struct {
		name string
		src  []byte
	}{
		{"Small", []byte("package p\nfunc F(x int) int { return x + 1 }\n")},
		{"Medium", []byte("package p\n" + strings.Repeat("var X = 42\n", 1024))},
		{"Large", []byte("package p\n" + strings.Repeat("var X = 42\n", 8192))},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(fixture.src)))
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					tree, err := parseProduction(lang, fixture.src)
					if err != nil || tree == nil {
						b.Errorf("parse: %v", err)
						return
					}
					bad := tree.RootNode().HasError()
					tree.Release()
					if bad {
						b.Error("unexpected parse error")
						return
					}
				}
			})
		})
	}
}
