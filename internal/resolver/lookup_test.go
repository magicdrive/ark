package resolver_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// The lookup indexes must select exactly the symbols the full scans selected:
// they are a performance change and never a semantic one.

// extractTree builds FileIndexes for every supported file under root with the
// canonical providers and the repository-index conversion.
func extractTree(t *testing.T, root string) []resolver.FileIndex {
	t.Helper()
	providers := languages.Registry().Providers()
	byExt := map[string]int{}
	for i, p := range providers {
		for _, ext := range p.Extensions() {
			byExt[ext] = i
		}
	}
	var files []resolver.FileIndex
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && index.SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		i, ok := byExt[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		ex, err := providers[i].Extract(context.Background(), source.FileID(rel), src)
		if err != nil {
			return nil // partial failure is tolerated, as in indexing
		}
		files = append(files, index.NewFileIndex(string(providers[i].Language()), source.FileID(rel), ex))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// symKeys renders symbols for comparison, keeping duplicates.
func symKeys(syms []symbol.Symbol) []string {
	out := make([]string, len(syms))
	for i, s := range syms {
		out[i] = fmt.Sprintf("%s|%s|%s|%s|%s", s.ID, s.Location.File, s.Qualified, s.Name, s.Receiver)
	}
	return out
}

func sortedKeys(syms []symbol.Symbol) []string {
	k := symKeys(syms)
	sort.Strings(k)
	return k
}

// assertStagesMatchScans compares, for every reference (and a set of extra
// probe names), each indexed stage against its full-scan oracle. The scans of
// r.byQualified iterate a map, so those two stages are compared as multisets;
// the same-package scan walks files in order, so its order must match too.
func assertStagesMatchScans(t *testing.T, files []resolver.FileIndex, probes ...reference.Reference) int {
	t.Helper()
	r := resolver.New(files)
	checked := 0
	check := func(ref reference.Reference, fi resolver.FileIndex) {
		checked++
		recv, pkg, suf := r.StageLookups(ref, fi)
		wRecv, wPkg, wSuf := r.StageScans(ref, fi)
		if got, want := sortedKeys(recv), sortedKeys(wRecv); !reflect.DeepEqual(got, want) {
			t.Fatalf("receiverMatch(%q recv=%q) in %s:\n got  %v\n want %v", ref.Name, ref.ReceiverExpr, fi.FileID, got, want)
		}
		if got, want := symKeys(pkg), symKeys(wPkg); !reflect.DeepEqual(got, want) {
			t.Fatalf("samePackageMatch(%q) in %s:\n got  %v\n want %v", ref.Name, fi.FileID, got, want)
		}
		if got, want := sortedKeys(suf), sortedKeys(wSuf); !reflect.DeepEqual(got, want) {
			t.Fatalf("byNameSuffix(%q):\n got  %v\n want %v", ref.Name, got, want)
		}
	}
	for _, fi := range files {
		for _, ref := range fi.References {
			check(ref, fi)
		}
		for _, p := range probes {
			check(p, fi)
		}
	}
	return checked
}

func TestLookupIndexes_MatchScansOnFixtures(t *testing.T) {
	roots := []string{
		"../golden/testdata",
		"../languages/php/testdata",
		"../languages/typescript/testdata",
		"../contextquality/testdata",
		"../index/testdata",
	}
	if !testing.Short() {
		// The repository's own source: a large multi-package Go corpus with
		// many same-name methods across packages.
		roots = append(roots, "..")
	}
	for _, root := range roots {
		t.Run(root, func(t *testing.T) {
			files := extractTree(t, root)
			if len(files) == 0 {
				t.Fatalf("no files extracted under %s", root)
			}
			if n := assertStagesMatchScans(t, files); n == 0 {
				t.Fatalf("no references checked under %s", root)
			}
		})
	}
}

func mkSym(file, name, qualified, receiver string) symbol.Symbol {
	return symbol.Symbol{
		ID:        symbol.SymbolID(file + "#" + qualified + "#" + name),
		Name:      name,
		Qualified: qualified,
		Receiver:  receiver,
		Kind:      symbol.KindMethod,
		Language:  "go",
		Location:  source.Location{File: source.FileID(file)},
	}
}

// Edge cases the fixtures may not contain: duplicate FileIDs, a symbol whose
// Location.File differs from its FileIndex, dotted and dot-terminated
// qualified names, unqualified members, case-insensitive receiver suffixes,
// duplicate qualified names and same-named members of unrelated types.
func TestLookupIndexes_MatchScansOnEdgeCases(t *testing.T) {
	files := []resolver.FileIndex{
		{FileID: "p/a.go", Symbols: []symbol.Symbol{
			mkSym("p/a.go", "Save", "UserRepo.Save", "UserRepo"),
			mkSym("p/a.go", "Save", "OrderRepo.Save", "OrderRepo"),
			mkSym("p/a.go", "Save", "", "Loose"), // unqualified member
			mkSym("p/a.go", "c", "a.b.c", "b"),
			mkSym("p/a.go", "x", "ends.", ""),
		}},
		{FileID: "p/b.go", Symbols: []symbol.Symbol{
			mkSym("elsewhere/b.go", "Save", "UserRepo.Save", "userrepo"), // Location.File ≠ FileID
			mkSym("p/b.go", "Helper", "Helper", ""),
		}},
		{FileID: "p/b.go", Symbols: []symbol.Symbol{ // duplicate FileID
			mkSym("p/b.go", "Helper", "Helper", ""),
		}},
		{FileID: "q/c.go", Symbols: []symbol.Symbol{
			mkSym("q/c.go", "Save", "Repo.Save", "Repo"),
			mkSym("q/c.go", "Helper", "q.Helper", ""),
		}},
		{FileID: "root.go", Symbols: []symbol.Symbol{
			mkSym("root.go", "Helper", "Helper", ""),
		}},
	}
	probes := []reference.Reference{
		{Name: "Save", ReceiverExpr: "repo"},
		{Name: "Save", ReceiverExpr: "REPO"},
		{Name: "Save", ReceiverExpr: "UserRepo"},
		{Name: "Save", ReceiverExpr: "$this->repo"},
		{Name: "Save"},
		{Name: "c", ReceiverExpr: "b"},
		{Name: "b.c"},
		{Name: "a.b.c"},
		{Name: ""},
		{Name: "Helper"},
		{Name: "Missing", ReceiverExpr: "x"},
	}
	assertStagesMatchScans(t, files, probes...)
}

// Rebuilding the resolver from the same input yields identical resolutions,
// candidate order included.
func TestLookupIndexes_DeterministicRebuild(t *testing.T) {
	files := extractTree(t, "../languages/php/testdata")
	first := resolver.New(files).Resolve()
	for i := 0; i < 5; i++ {
		if again := resolver.New(files).Resolve(); !reflect.DeepEqual(first, again) {
			t.Fatalf("rebuild %d produced different resolutions", i)
		}
	}
}

// Characterization of the indexed stages through Resolve: confidence, evidence
// kind and candidate order are pinned to the pre-index behaviour.
func TestLookupIndexes_StageSemanticsPinned(t *testing.T) {
	typ := func(name, file string) symbol.Symbol {
		s := mkSym(file, name, name, "")
		s.Kind = symbol.KindClass
		return s
	}
	fn := func(name, file string) symbol.Symbol {
		s := mkSym(file, name, name, "")
		s.Kind = symbol.KindFunction
		return s
	}
	ref := func(name, recv, file string) reference.Reference {
		l := source.Location{File: source.FileID(file), Range: source.Range{Start: source.Position{Line: 9}}}
		return reference.Reference{
			ID: reference.NewReferenceID("go", l.File, reference.KindCall, name+"/"+recv, l), Name: name,
			Kind: reference.KindCall, Language: "go", Location: l, Container: "caller", ReceiverExpr: recv, IsCall: true,
		}
	}
	cases := []struct {
		name  string
		files []resolver.FileIndex
		ref   reference.Reference
		want  string // confidence | evidence kind | candidate qualified names in order
	}{
		{
			name: "type receiver selects its own member over a same-named member of an unrelated type",
			files: []resolver.FileIndex{
				{FileID: "a/x.go", Symbols: []symbol.Symbol{typ("UserRepo", "a/x.go"), mkSym("a/x.go", "Save", "UserRepo.Save", "UserRepo")}},
				{FileID: "b/y.go", Symbols: []symbol.Symbol{typ("OrderRepo", "b/y.go"), mkSym("b/y.go", "Save", "OrderRepo.Save", "OrderRepo")}},
			},
			ref:  ref("Save", "UserRepo", "c/z.go"),
			want: "strong | qualified_receiver | a/x.go:UserRepo.Save",
		},
		{
			name: "duplicate qualified member across files stays ambiguous, ordered by file",
			files: []resolver.FileIndex{
				{FileID: "b/y.go", Symbols: []symbol.Symbol{typ("Repo", "b/y.go"), mkSym("b/y.go", "Save", "Repo.Save", "Repo")}},
				{FileID: "a/x.go", Symbols: []symbol.Symbol{mkSym("a/x.go", "Save", "Repo.Save", "Repo")}},
			},
			ref:  ref("Save", "Repo", "c/z.go"),
			want: "candidate | qualified_receiver | a/x.go:Repo.Save | b/y.go:Repo.Save",
		},
		{
			name: "untyped receiver suffix match is capped",
			files: []resolver.FileIndex{
				{FileID: "a/x.go", Symbols: []symbol.Symbol{mkSym("a/x.go", "Save", "UserRepo.Save", "UserRepo")}},
			},
			ref:  ref("Save", "repo", "c/z.go"),
			want: "candidate | qualified_receiver | a/x.go:UserRepo.Save",
		},
		{
			name: "same package",
			files: []resolver.FileIndex{
				{FileID: "p/b.go", Symbols: []symbol.Symbol{fn("Helper", "p/b.go")}},
				{FileID: "q/c.go", Symbols: []symbol.Symbol{fn("Helper", "q/c.go")}},
			},
			ref:  ref("Helper", "", "p/a.go"),
			want: "strong | same_package | p/b.go:Helper",
		},
		{
			name: "same package ambiguity",
			files: []resolver.FileIndex{
				{FileID: "p/c.go", Symbols: []symbol.Symbol{fn("Helper", "p/c.go")}},
				{FileID: "p/b.go", Symbols: []symbol.Symbol{fn("Helper", "p/b.go")}},
			},
			ref:  ref("Helper", "", "p/a.go"),
			want: "candidate | same_package | p/b.go:Helper | p/c.go:Helper",
		},
		{
			name: "repository-wide ambiguity",
			files: []resolver.FileIndex{
				{FileID: "r/c.go", Symbols: []symbol.Symbol{fn("Helper", "r/c.go")}},
				{FileID: "q/c.go", Symbols: []symbol.Symbol{fn("Helper", "q/c.go")}},
			},
			ref:  ref("Helper", "", "p/a.go"),
			want: "candidate | candidate_set | q/c.go:Helper | r/c.go:Helper",
		},
		{
			name: "qualified-name suffix",
			files: []resolver.FileIndex{
				{FileID: "q/c.go", Symbols: []symbol.Symbol{func() symbol.Symbol {
					s := fn("Run", "q/c.go")
					s.Qualified = "pkg.Task.Run"
					return s
				}()}},
			},
			ref:  ref("Task.Run", "", "p/a.go"),
			want: "strong | unique_repo_match | q/c.go:pkg.Task.Run",
		},
	}
	for _, tc := range cases {
		files := append(tc.files, resolver.FileIndex{FileID: tc.ref.Location.File, References: []reference.Reference{tc.ref}})
		res := resolver.New(files).ResolveReference(tc.ref, files[len(files)-1])
		got := res.Confidence.String()
		if len(res.Evidence) > 0 {
			got += " | " + string(res.Evidence[0].Kind)
		}
		for _, c := range res.Candidates {
			got += " | " + string(c.File) + ":" + c.Qualified
		}
		t.Logf("%s => %s", tc.name, got)
		if tc.want != "" && got != tc.want {
			t.Errorf("%s:\n got  %s\n want %s", tc.name, got, tc.want)
		}
	}
}
