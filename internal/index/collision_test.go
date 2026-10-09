package index_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/symbol"
)

// SymbolID collisions are forced by an injected IDFunc: production IDs
// (symbol.NewDeclarationID) except for the chosen declarations, which get one
// shared ID. Each IDFunc is a pure value — no global state.

const forcedID = symbol.SymbolID("c0111de0c0111de0")

// collide returns an IDFunc giving forcedID to the declarations whose
// "path|qualified" is listed (any ordinal), production IDs to the rest.
func collide(decls ...string) index.IDFunc {
	return func(lang, path string, kind symbol.SymbolKind, qualified string, ordinal int) symbol.SymbolID {
		if slices.Contains(decls, path+"|"+qualified) {
			return forcedID
		}
		return symbol.NewDeclarationID(lang, path, kind, qualified, ordinal)
	}
}

func buildWith(t *testing.T, root string, ids index.IDFunc) (*index.RepositoryIndex, error) {
	t.Helper()
	return index.NewWithIDs(context.Background(), root, languages.Registry().Providers(), cache.NopStore{}, ids)
}

func collisionOf(t *testing.T, err error) *index.IdentityCollisionError {
	t.Helper()
	var ce *index.IdentityCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("want an IdentityCollisionError, got %v", err)
	}
	return ce
}

func declLines(c index.IdentityCollision) []string {
	var out []string
	for _, d := range c.Declarations {
		out = append(out, string(d.Location.File)+":"+d.Qualified)
	}
	return out
}

const callsGo = `package x

func alpha() {
	beta()
}

func beta() {}

func gamma() {}
`

// A: two declarations of one file share an ID. No index is built: nothing
// can be overwritten, merged or resolved through the shared ID.
func TestIdentityCollision_SameFile(t *testing.T) {
	root := writeFiles(t, map[string]string{"go.mod": "module x\n\ngo 1.22\n", "a.go": callsGo})
	idx, err := buildWith(t, root, collide("a.go|alpha", "a.go|gamma"))
	if idx != nil {
		t.Fatal("an index was built over a SymbolID collision")
	}
	ce := collisionOf(t, err)
	if len(ce.Collisions) != 1 || ce.Collisions[0].ID != forcedID ||
		!reflect.DeepEqual(declLines(ce.Collisions[0]), []string{"a.go:alpha", "a.go:gamma"}) {
		t.Errorf("collisions: %+v", ce.Collisions)
	}
	diags := ce.Diagnostics()
	if len(diags) != 2 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	for _, d := range diags {
		if d.Code != language.DiagSymbolIDCollision || d.Severity != language.SeverityError || d.Location.File != "a.go" ||
			!strings.Contains(d.Message, string(forcedID)) {
			t.Errorf("diagnostic: %+v", d)
		}
	}
	if msg := err.Error(); !strings.Contains(msg, "1 SymbolID collision") || !strings.Contains(msg, "go function alpha at a.go:3:1") ||
		!strings.Contains(msg, "go function gamma at a.go:9:1") {
		t.Errorf("error message: %s", msg)
	}
}

// The old identity scheme (no ordinals) on two Go init functions is exactly
// a same-file collision: detected, never merged.
func TestIdentityCollision_DroppedOrdinalIsDetected(t *testing.T) {
	root := writeFiles(t, map[string]string{"go.mod": "module x\n\ngo 1.22\n", "boot.go": initsGo})
	noOrdinal := func(lang, path string, kind symbol.SymbolKind, qualified string, _ int) symbol.SymbolID {
		return symbol.NewSymbolID(lang, path, kind, qualified)
	}
	_, err := buildWith(t, root, noOrdinal)
	ce := collisionOf(t, err)
	if len(ce.Collisions) != 1 || !reflect.DeepEqual(declLines(ce.Collisions[0]), []string{"boot.go:init", "boot.go:init"}) {
		t.Errorf("collisions: %+v", ce.Collisions)
	}
}

// B: declarations of different files and languages. Detected over the whole
// repository, with the same report whichever file is read first.
func TestIdentityCollision_CrossFileAndDeterministic(t *testing.T) {
	for _, layout := range []map[string]string{
		{"a/one.go": callsGo, "z/two.py": "def delta():\n    pass\n"},
		{"z/one.go": callsGo, "a/two.py": "def delta():\n    pass\n"},
	} {
		layout["go.mod"] = "module x\n\ngo 1.22\n"
		root := writeFiles(t, layout)
		var goFile, pyFile string
		for p := range layout {
			switch {
			case strings.HasSuffix(p, ".go"):
				goFile = p
			case strings.HasSuffix(p, ".py"):
				pyFile = p
			}
		}
		ids := collide(goFile+"|beta", pyFile+"|delta")
		_, err1 := buildWith(t, root, ids)
		_, err2 := buildWith(t, root, ids)
		ce := collisionOf(t, err1)
		if err1.Error() != err2.Error() {
			t.Error("repeated builds report differently")
		}
		got := declLines(ce.Collisions[0])
		want := []string{goFile + ":beta", pyFile + ":delta"}
		slices.Sort(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("declarations %v, want %v (sorted by file)", got, want)
		}
	}
}

// C/D: a parent, a caller or a callee sharing its ID with another
// declaration: no index, so no parent, edge or context can name the wrong
// declaration.
func TestIdentityCollision_ParentAndGraph(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"go.mod": "module x\n\ngo 1.22\n",
		"a.go":   callsGo,
		"svc.ts": "class UserService {\n  getUser() { return this.loadUser() }\n  loadUser() { return null }\n}\nfunction other() {}\n",
	})
	for name, ids := range map[string]index.IDFunc{
		"parent":   collide("svc.ts|UserService", "svc.ts|other"),
		"caller":   collide("a.go|alpha", "svc.ts|other"),
		"callee":   collide("a.go|beta", "a.go|gamma"),
		"member":   collide("svc.ts|UserService.loadUser", "a.go|gamma"),
		"3-way":    collide("a.go|alpha", "a.go|beta", "svc.ts|other"),
		"no-clash": collide("a.go|alpha"),
	} {
		idx, err := buildWith(t, root, ids)
		if name == "no-clash" {
			if err != nil || idx == nil {
				t.Errorf("%s: one declaration with a chosen ID is no collision: %v", name, err)
			}
			continue
		}
		if idx != nil {
			t.Errorf("%s: index built", name)
		}
		collisionOf(t, err)
	}
}

// G: cold and warm cache builds detect the same collision, and the cache
// holds only drafts: a production build over the same store afterwards is
// exactly an uncached build.
func TestIdentityCollision_CacheColdWarmAndClean(t *testing.T) {
	root := writeFiles(t, map[string]string{"go.mod": "module x\n\ngo 1.22\n", "a.go": callsGo})
	providers := []language.Provider{golang.NewProvider()}
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ids := collide("a.go|alpha", "a.go|beta")
	var reports []string
	for range 2 { // cold, warm
		_, err := index.NewWithIDs(context.Background(), root, providers, store, ids)
		reports = append(reports, collisionOf(t, err).Error())
	}
	if reports[0] != reports[1] {
		t.Errorf("cold and warm differ:\n%s\n%s", reports[0], reports[1])
	}
	warm, err := index.NewWithCache(context.Background(), root, providers, store)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plain.SymbolsByFile("a.go") {
		w, ok := warm.GetSymbol(s.ID)
		if !ok || w != s || !reflect.DeepEqual(names(warm, warm.GetCallees(s.ID)), names(plain, plain.GetCallees(s.ID))) {
			t.Errorf("%s differs after a collision build used the cache", s.Qualified)
		}
	}
}

// H: production IDs through NewWithIDs give the index NewWithCache gives:
// no collision, same symbols and edges.
func TestIdentityCollision_ProductionIDsUnchanged(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"go.mod": "module x\n\ngo 1.22\n", "a.go": callsGo, "boot.go": initsGo,
		"svc.ts": "class UserService {\n  getUser() { return this.loadUser() }\n  loadUser() { return null }\n}\n",
	})
	providers := languages.Registry().Providers()
	a, err := index.NewWithIDs(context.Background(), root, providers, cache.NopStore{}, symbol.NewDeclarationID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := index.NewWithCache(context.Background(), root, providers, cache.NopStore{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Stats().Symbols != b.Stats().Symbols || a.Stats().Relations != b.Stats().Relations {
		t.Errorf("stats %+v vs %+v", a.Stats(), b.Stats())
	}
	for _, f := range b.Files() {
		if !reflect.DeepEqual(a.SymbolsByFile(f), b.SymbolsByFile(f)) {
			t.Errorf("%s: symbols differ", f)
		}
	}
}

// Identical drafts stated twice are one declaration, not a collision.
func TestIdentityCollision_IdenticalDraftIsNoCollision(t *testing.T) {
	d := draftAt("Run", symbol.KindFunction, 1, 3)
	idx := buildFakeIndex(t, fakeProvider{symbols: []language.SymbolDraft{d, d, d}})
	if n := len(idx.SymbolsByFile("x.fake")); n != 1 {
		t.Errorf("%d symbols for one declaration", n)
	}
}
