package python

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/resolver"
)

const fixtureRepo = "testdata/repo"

func buildIndex(t testing.TB, dir string) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), dir, []language.Provider{NewProvider()})
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

// edgeLines renders every forward edge as "file:from -kind-> file:to confidence".
func edgeLines(idx *index.RepositoryIndex) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			out = append(out, fmt.Sprintf("%s:%s -%s-> %s:%s %s", s.Location.File, s.Qualified, e.Kind, to.Location.File, to.Qualified, e.Confidence))
		}
	}
	sort.Strings(out)
	return out
}

// TestGraph_Fixture compares the fixture's whole graph with the expected
// one. Absent on purpose:
//   - user.save(), self.store.put(…), service.create(…), user.validate():
//     receivers whose type no declaration proves (a local, an attribute,
//     a parameter, a staticmethod's first parameter);
//   - @dataclass, print(…), bool(…), str: names outside the repository;
//   - app.util's log through `import app.util` is an edge, `models.User`
//     through `from . import models` too (the submodule).
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, edgeLines(idx), []string{
		"app/models.py:Base.save -calls-> app/models.py:Base._write exact",
		"app/models.py:User -extends-> app/models.py:Base exact",
		"app/models.py:User.save -calls-> app/models.py:Base.save strong", // super(): run-time MRO
		"app/models.py:User.save -calls-> app/models.py:User.validate exact",
		"app/service.py:UserService.__init__ -uses_type-> app/storage.py:Store exact", // the alias Backend
		"app/service.py:UserService.create -calls-> app/models.py:User exact",
		"app/service.py:UserService.create -calls-> app/util.py:log exact",
		"app/service.py:UserService.create -uses_type-> app/models.py:User exact",
		"app/service.py:UserService.remove -calls-> app/service.py:audited exact", // the decorator
		"app/storage.py:Store.put -calls-> app/storage.py:Store.items exact",
		"app/util.py:_format -calls-> app/util.py:_format.wrap exact",
		"scripts/run.py:main -calls-> app/service.py:UserService exact", // through app/__init__.py
		"scripts/run.py:main -calls-> app/storage.py:Store exact",
		"scripts/run.py:main -calls-> scripts/run.py:open exact", // the module's open, not the builtin
	})
}

func equalLines(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("edges:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestGraph_Soundness: every edge joins two Python declarations, and is
// Exact unless its evidence is the one the provider caps at Strong (a
// super() call); no edge reaches a declaration under an unknown decorator.
func TestGraph_Soundness(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			if s.Language != "python" || to.Language != "python" {
				t.Errorf("%s -> %s joins %s and %s", s.Qualified, to.Qualified, s.Language, to.Language)
			}
			switch e.Confidence {
			case resolver.ConfidenceExact:
			case resolver.ConfidenceStrong:
				super := false
				for _, ev := range e.Evidence {
					super = super || strings.Contains(ev.Detail, `receiver "super()"`)
				}
				if !super {
					t.Errorf("%s -> %s: Strong without a capping evidence: %v", s.Qualified, to.Qualified, e.Evidence)
				}
			default:
				t.Errorf("%s -> %s: edge at %s", s.Qualified, to.Qualified, e.Confidence)
			}
			if to.ConfidenceCap == "candidate" {
				t.Errorf("%s -> %s: an edge to a declaration capped at Candidate", s.Qualified, to.Qualified)
			}
		}
	}
	// A declaration under an unknown decorator carries the cap.
	for _, s := range idx.FindSymbols("remove") {
		if s.Qualified == "UserService.remove" && s.ConfidenceCap != "candidate" {
			t.Errorf("UserService.remove: cap %q, want candidate", s.ConfidenceCap)
		}
	}
}

// TestDeterminism: the fixture's index is the same built afresh, built
// again, restored from the extraction cache, and built from a copy whose
// files were written in reverse order; after an edit, a build that reuses
// the cache for the unchanged files equals one built afresh, and keeps no
// edge to a removed declaration.
func TestDeterminism(t *testing.T) {
	providers := []language.Provider{NewProvider()}
	snapshot := func(idx *index.RepositoryIndex, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return golden.IndexSnapshot(idx)
	}
	want := snapshot(index.New(context.Background(), fixtureRepo, providers))
	if got := snapshot(index.New(context.Background(), fixtureRepo, providers)); got != want {
		t.Error("a second build differs")
	}
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot(index.NewWithCache(context.Background(), fixtureRepo, providers, store)); got != want {
		t.Error("a build that fills the cache differs")
	}
	if got := snapshot(index.NewWithCache(context.Background(), fixtureRepo, providers, store)); got != want {
		t.Error("a build restored from the cache differs")
	}

	var files []string
	filepath.Walk(fixtureRepo, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	copyDir := t.TempDir()
	for i := len(files) - 1; i >= 0; i-- {
		rel, _ := filepath.Rel(fixtureRepo, files[i])
		data, err := os.ReadFile(files[i])
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(copyDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := snapshot(index.New(context.Background(), copyDir, providers)); got != want {
		t.Error("a copy written in another order differs")
	}

	copyStore, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapshot(index.NewWithCache(context.Background(), copyDir, providers, copyStore)) // fill before the edit
	edited := filepath.Join(copyDir, "app", "storage.py")
	if err := os.WriteFile(edited, []byte("class Store:\n    def put(self, item):\n        self.items().append(item)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warm, err := index.NewWithCache(context.Background(), copyDir, providers, copyStore)
	fresh := snapshot(index.New(context.Background(), copyDir, providers))
	if got := snapshot(warm, err); got != fresh {
		t.Error("after an edit, a build reusing the cache differs from a fresh one")
	}
	for _, l := range edgeLines(warm) {
		if strings.Contains(l, "Store.items") {
			t.Errorf("an edge to the removed declaration remains: %s", l)
		}
	}
}

// The extraction cache keeps every Extraction field the resolution reads:
// with nested namesakes (Extraction.LexicalNesting decides what the
// same-file stage sees), a build restored from the cache is the build
// afresh, and g's helper() is the module's function in both.
func TestDeterminism_NestedNamesakesSurviveCache(t *testing.T) {
	dir := t.TempDir()
	src := "def helper():\n    pass\n\n\ndef outer():\n    def helper():\n        pass\n    helper()\n\n\ndef g():\n    helper()\n"
	if err := os.WriteFile(filepath.Join(dir, "m.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	providers := []language.Provider{NewProvider()}
	fresh, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatal(err)
	}
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 { // fill, then restore
		warm, err := index.NewWithCache(context.Background(), dir, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if golden.IndexSnapshot(warm) != golden.IndexSnapshot(fresh) {
			t.Errorf("build %d with the cache differs from the build afresh", i)
		}
	}
	want := []string{"m.py:g -calls-> m.py:helper exact", "m.py:outer -calls-> m.py:outer.helper exact"}
	if got := edgeLines(fresh); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("edges %v, want %v", got, want)
	}
}

// TestGraph_CandidatesAreNoEdges: a call of a conditionally defined
// function is a candidate of each definition, and no edge.
func TestGraph_CandidatesAreNoEdges(t *testing.T) {
	dir := t.TempDir()
	src := "import sys\nif sys.version_info > (3,):\n    def compat():\n        pass\nelse:\n    def compat():\n        pass\n\n\ndef run():\n    compat()\n"
	if err := os.WriteFile(filepath.Join(dir, "m.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := buildIndex(t, dir)
	var run index.CandidateSample
	for _, s := range idx.FindSymbols("run") {
		if len(idx.GetCallees(s.ID)) != 0 {
			t.Errorf("run has edges %v", idx.GetCallees(s.ID))
		}
		run = idx.CandidateCalleeSample(s.ID)
	}
	if run.Total != 2 || len(run.Relations) != 2 {
		t.Errorf("run's candidate callees %+v, want both compat definitions", run)
	}
}

// TestExtractScalesLinearly: extraction time grows linearly with the source
// (scope analysis and capping are per name, never per pair of names).
func TestExtractScalesLinearly(t *testing.T) {
	gen := func(n int) []byte {
		var b strings.Builder
		b.WriteString("import os\nfrom pkg import helper\n")
		for i := range n {
			fmt.Fprintf(&b, "class C%d(Base):\n    def m(self, x):\n        self.n%d = helper(x)\n        return self.m(x) + f%d(x)\n\ndef f%d(y):\n    return [z for z in y if (w := z)]\n\n", i, i, i, i)
		}
		return []byte(b.String())
	}
	p := NewProvider()
	measure := func(src []byte) time.Duration {
		best := time.Duration(1 << 62)
		for range 3 {
			start := time.Now()
			if _, err := p.Extract(context.Background(), "m.py", src); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	// Ratios over several attempts, as the other scaling tests measure:
	// a load that rises between the two sizes raises one ratio alone.
	small, large := gen(500), gen(2000)
	for attempt := range 3 {
		ratio := float64(measure(large)) / float64(measure(small))
		if ratio <= 8 { // 4× the source; quadratic would be 16
			return
		}
		if attempt == 2 {
			t.Fatalf("2000 / 500 classes took %.1f× (more than linear in every attempt)", ratio)
		}
	}
}
