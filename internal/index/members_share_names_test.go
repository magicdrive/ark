package index_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
)

// In ECMAScript a type's methods and properties share one name space
// (language.Extraction.MembersShareNames): a call of this.m() inherited
// through B, which declares a property m, denotes B's m, not A's method m.
// The flag survives the extraction cache: a warm build resolves alike.
func TestMembersShareNames_InheritedLookupSurvivesCache(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"m.js": "export class A {\n  m() {}\n}\n" +
			"export class B extends A {\n  m = make()\n}\n" +
			"export class C extends B {\n  run() { this.m() }\n}\n" +
			"export class D extends A {\n  run() { this.m() }\n}\n" +
			"function make() {}\n",
	})
	edges := func(idx *index.RepositoryIndex) []string {
		var out []string
		for _, s := range idx.SymbolsByFile("m.js") {
			for _, e := range idx.GetCallees(s.ID) {
				if to, ok := idx.GetSymbol(e.To); ok {
					out = append(out, s.Qualified+" -> "+to.Qualified+" "+e.Confidence.String())
				}
			}
		}
		sort.Strings(out)
		return out
	}
	providers := languages.Registry().Providers()
	plain, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	want := edges(plain)
	for _, e := range []string{"C.run -> B.m exact", "D.run -> A.m exact"} {
		found := false
		for _, g := range want {
			found = found || g == e
		}
		if !found {
			t.Errorf("no edge %q in %v", e, want)
		}
	}
	for _, g := range want {
		if g == "C.run -> A.m exact" {
			t.Errorf("C.run's this.m() skipped B's property m: %v", want)
		}
	}
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"cold", "warm"} {
		idx, err := index.NewWithCache(context.Background(), root, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := edges(idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s cache: edges %v, want %v", run, got, want)
		}
	}
}
