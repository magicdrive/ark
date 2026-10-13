package index_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/reference"
)

// Reference identity (reference.NewReferenceID): a reference's ID is derived
// from its language, file, kind, name and span — nothing else — so that
//
//   - every reference of an index has its own ID, in every language, even
//     where references of one name and kind start at one position (a chain of
//     same-named calls: f(x).V.f(), $(a).data().data());
//   - an index built again from the same sources, with or without the
//     extraction cache, gives every reference the same ID;
//   - the file is part of it: identical code in two files never shares one;
//   - editing one file changes no other file's IDs.
//
// IDs are not cached (the cache stores extraction drafts) and are no
// persistence contract across Ark versions: they change with what an
// extraction states (its spans, names, kinds).

// referenceIdentityRepo has, in every language, a chain of calls of one name
// (references sharing a start where the provider spans a member reference
// from its receiver), and two files of identical content.
var referenceIdentityRepo = map[string]string{
	"go.mod": "module example.com/rid\n\ngo 1.22\n",
	"g/g.go": `package g

type V struct{ V *V }

func (v *V) F() *V { return v }

func F(x int) *V { return &V{} }

func use() {
	F(1).V.F()
	F(1).F().F()
}
`,
	"g/copy1.go":   "package g\n\nfunc c1() { F(2).F() }\n",
	"g/copy2.go":   "package g\n\nfunc c1() { F(2).F() }\n",
	"j/a.js":       "function run(a) {\n  $(a).data('x').data('y');\n  a.f().f().f();\n}\n",
	"t/a.ts":       "export function run(a: any) {\n  a.f().f().f();\n}\n",
	"p/a.php":      "<?php\nfunction run($a) {\n  $a->f()->f()->f();\n}\n",
	"y/a.py":       "def run(a):\n    a.f().f().f()\n",
	"r/src/lib.rs": "pub fn run(a: A) {\n    a.f().f().f();\n}\npub struct A;\nimpl A { pub fn f(&self) -> A { A } }\n",
	"tf/main.tf":   "resource \"a\" \"b\" {\n  x = module.m.out\n}\nmodule \"m\" {\n  source = \"./m\"\n}\n",
}

// sharedStartDirs are the fixture's directories whose provider states
// references of one name and kind starting at one position (Go, JavaScript,
// Python: a member reference spans its whole selector). TypeScript, PHP and
// Rust span a member reference from its name, so theirs never share a start;
// their IDs must be unique all the same.
var sharedStartDirs = []string{"g", "j", "y"}

// refRow is a reference as identity sees it.
type refRow struct {
	ID               reference.ReferenceID
	File, Kind, Name string
	SL, SC, EL, EC   uint32
}

func referenceRows(idx *index.RepositoryIndex) []refRow {
	var out []refRow
	for _, f := range idx.Files() {
		for _, r := range idx.ReferencesByFile(f) {
			rg := r.Location.Range
			out = append(out, refRow{r.ID, string(f), string(r.Kind), r.Name, rg.Start.Line, rg.Start.Column, rg.End.Line, rg.End.Column})
		}
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
	return out
}

func TestReferenceIdentity_UniqueInEveryLanguage(t *testing.T) {
	root := writeFiles(t, referenceIdentityRepo)
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	rows := referenceRows(idx)
	byID := map[reference.ReferenceID]refRow{}
	languagesSeen := map[string]bool{}
	for _, r := range rows {
		if prev, dup := byID[r.ID]; dup {
			t.Errorf("two references share ID %s:\n  %+v\n  %+v", r.ID, prev, r)
		}
		byID[r.ID] = r
		languagesSeen[filepath.Dir(r.File)] = true
	}
	// The chains: references of one name and kind starting at one position.
	starts := map[string]int{}
	for _, r := range rows {
		starts[fmt.Sprintf("%s|%s|%s|%d:%d", r.File, r.Kind, r.Name, r.SL, r.SC)]++
	}
	shared := map[string]bool{}
	for k, n := range starts {
		if n > 1 {
			shared[filepath.Dir(strings.SplitN(k, "|", 2)[0])] = true
		}
	}
	for _, dir := range sharedStartDirs {
		if !shared[dir] {
			t.Errorf("%s: no references sharing a start — the fixture tests nothing there", dir)
		}
	}
	if len(rows) == 0 || len(languagesSeen) < 6 {
		t.Errorf("references from %d directories: %v", len(languagesSeen), languagesSeen)
	}
}

func TestReferenceIdentity_SameSourcesSameIDs(t *testing.T) {
	root := writeFiles(t, referenceIdentityRepo)
	providers := languages.Registry().Providers()
	plain, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	want := referenceRows(plain)
	again, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	if got := referenceRows(again); !reflect.DeepEqual(got, want) {
		t.Error("a second build gives references other IDs")
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
		if got := referenceRows(idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s cache: references differ from an uncached build", run)
		}
	}
}

func TestReferenceIdentity_FileIsPartOfTheID(t *testing.T) {
	root := writeFiles(t, referenceIdentityRepo)
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	one, two := idx.ReferencesByFile("g/copy1.go"), idx.ReferencesByFile("g/copy2.go")
	if len(one) == 0 || len(one) != len(two) {
		t.Fatalf("copies: %d and %d references", len(one), len(two))
	}
	for i := range one {
		if one[i].ID == two[i].ID {
			t.Errorf("identical code in two files shares ID %s (%s)", one[i].ID, one[i].Name)
		}
	}
}

func TestReferenceIdentity_EditChangesOnlyItsFile(t *testing.T) {
	root := writeFiles(t, referenceIdentityRepo)
	providers := languages.Registry().Providers()
	before, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "g", "copy1.go"), []byte("package g\n\n// moved\n\nfunc c1() { F(2).F() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range before.Files() {
		b, a := before.ReferencesByFile(f), after.ReferencesByFile(f)
		same := reflect.DeepEqual(b, a)
		if f == "g/copy1.go" && same {
			t.Error("the edited file kept its references' positions and IDs")
		}
		if f != "g/copy1.go" && !same {
			t.Errorf("%s: references changed by an edit of another file", f)
		}
	}
}

// A reference whose ID another reference of the index already has — here a
// provider stating one reference twice, with another container — is left out
// with a reference_id_collision warning: the index keeps the first, and no
// resolution or edge is joined to the second.
func TestReferenceIdentity_CollisionIsLeftOut(t *testing.T) {
	first := ref("TCall", "call")
	second := ref("TCall", "call")
	second.Container = "Other"
	idx := buildFakeIndex(t, fakeProvider{
		symbols: []language.SymbolDraft{sym("Caller"), sym("Other"), sym("TCall")},
		refs:    []language.ReferenceDraft{first, second},
	})
	var calls []reference.Reference
	for _, r := range idx.ReferencesByFile("x.fake") {
		if r.Name == "TCall" {
			calls = append(calls, r)
		}
	}
	if len(calls) != 1 || calls[0].Container != "Caller" {
		t.Fatalf("want the first reference only, got %+v", calls)
	}
	collisions := 0
	for _, d := range idx.Diagnostics() {
		if d.Code == language.DiagReferenceIDCollision {
			collisions++
			if d.Severity != language.SeverityWarning {
				t.Errorf("collision diagnostic severity %q", d.Severity)
			}
		}
	}
	if collisions != 1 {
		t.Errorf("want one %s diagnostic, got %d: %+v", language.DiagReferenceIDCollision, collisions, idx.Diagnostics())
	}
	if got := edgeKindTo(idx, "TCall"); got != index.EdgeCalls {
		t.Errorf("edge Caller->TCall = %q, want %q", got, index.EdgeCalls)
	}
	others := idx.FindSymbolsByQualified("Other")
	if len(others) != 1 {
		t.Fatalf("Other: %d symbols", len(others))
	}
	if es := idx.GetCallees(others[0].ID); len(es) != 0 {
		t.Errorf("the left-out reference made edges from Other: %+v", es)
	}
}
