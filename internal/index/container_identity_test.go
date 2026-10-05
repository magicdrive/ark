package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// symbolIn returns the single symbol with the given qualified name declared in file.
func symbolIn(t *testing.T, idx *index.RepositoryIndex, file source.FileID, qualified string) symbol.Symbol {
	t.Helper()
	var out []symbol.Symbol
	for _, s := range idx.SymbolsByFile(file) {
		if s.Qualified == qualified {
			out = append(out, s)
		}
	}
	if len(out) != 1 {
		t.Fatalf("%s: want exactly one %q, got %d", file, qualified, len(out))
	}
	return out[0]
}

// TestEdgeContainerIdentityIsFileScoped is the STOP-2 regression: a reference's
// container must be identified by (file, qualified). Two files that both declare
// `main` must not have their call edges attributed to whichever `main` happened
// to be indexed first (repository-global candidate zero).
func TestEdgeContainerIdentityIsFileScoped(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"a/main.go": "package main\nfunc helperA() {}\nfunc main() { helperA() }\n",
		"b/main.go": "package main\nfunc helperB() {}\nfunc main() { helperB() }\n",
	})
	idx, err := index.New(context.Background(), root, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file   source.FileID
		callee string
		other  string
	}{
		{"a/main.go", "helperA", "helperB"},
		{"b/main.go", "helperB", "helperA"},
	} {
		mainSym := symbolIn(t, idx, tc.file, "main")
		var got []string
		for _, e := range idx.GetCallees(mainSym.ID) {
			to, _ := idx.GetSymbol(e.To)
			got = append(got, to.Qualified)
			if to.Qualified == tc.other {
				t.Errorf("%s: fabricated edge main -> %s (container attributed across files)", tc.file, tc.other)
			}
		}
		if len(got) != 1 || got[0] != tc.callee {
			t.Errorf("%s: callees of main = %v, want [%s]", tc.file, got, tc.callee)
		}
		// referencesByContainer must follow the same (file, qualified) contract.
		for _, r := range idx.ReferencesByContainer(mainSym.ID) {
			if r.Location.File != tc.file {
				t.Errorf("%s: ReferencesByContainer(main) leaked reference from %s", tc.file, r.Location.File)
			}
		}
	}
}

// TestEdgeContainerAmbiguousInFileCreatesNoEdge: when one file declares the same
// qualified container twice, the container cannot be identified uniquely and the
// builder must not choose one of them.
func TestEdgeContainerAmbiguousInFileCreatesNoEdge(t *testing.T) {
	dup := sym("Caller")
	dup2 := sym("Caller")
	dup2.Kind = symbol.KindFunction
	p := fakeProvider{
		symbols: []language.SymbolDraft{dup, dup2, sym("Target")},
		refs:    []language.ReferenceDraft{ref("Target", "call")},
	}
	idx := buildFakeIndex(t, p)
	for _, s := range idx.SymbolsByFile("x.fake") {
		if s.Qualified != "Caller" {
			continue
		}
		if edges := idx.GetCallees(s.ID); len(edges) != 0 {
			t.Errorf("ambiguous container %s (%s) got edges %v; want none", s.Qualified, s.Kind, edges)
		}
		if refs := idx.ReferencesByContainer(s.ID); len(refs) != 0 {
			t.Errorf("ambiguous container %s got %d container refs; want none", s.Qualified, len(refs))
		}
	}
}
