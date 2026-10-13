package index_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/source"
)

// A Speculative reference (language.ReferenceDraft.Speculative) resolved to
// no candidate — even proven outside the repository — is no reference: a
// field read t.field, or r.URL of an external type, leaves no trace in the
// per-file and per-container lists, the statistics, unresolved samples or
// non-edge relations. One resolved to a declaration — the method value
// t.Method — is an ordinary reference, with its edge.
func TestSpeculativeReferences(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"p/p.go": `package p

type T struct{ field int }

func (T) Method() {}

func Use(t T) {
	_ = t.field
	f := t.Method
	f()
}
`,
		"p/ext.go": `package p

import "net/http"

// A member of a type outside the repository may as well be a field: no
// reference, not even one proven outside.
func UseExt(r *http.Request) {
	_ = r.URL
}
`,
	})
	idx, err := index.New(context.Background(), root, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	file := source.FileID("p/p.go")
	use := symbolIn(t, idx, file, "Use")
	method := symbolIn(t, idx, file, "T.Method")

	total := 0
	for _, f := range idx.Files() {
		for _, r := range idx.ReferencesByFile(f) {
			total++
			if r.Name == "field" {
				t.Errorf("the field read is a reference: %+v", r)
			}
		}
	}
	if got := idx.Stats().References; got != total {
		t.Errorf("stats count %d references, the files list %d", got, total)
	}
	for _, r := range idx.ReferencesByContainer(use.ID) {
		if r.Name == "field" {
			t.Errorf("the field read is a reference of Use: %+v", r)
		}
	}
	// Use's one unresolved reference is the call of the local function
	// value f; the field read is none.
	if s := idx.UnresolvedOutgoing(use.ID); s.Total != 1 || len(s.References) != 1 || s.References[0].Name != "f" {
		t.Errorf("unresolved outgoing of Use: %+v", s)
	}
	for _, rel := range idx.NonEdgeRelationsFrom(use.ID) {
		t.Errorf("a non-edge relation from Use: %+v", rel)
	}
	ext := symbolIn(t, idx, "p/ext.go", "UseExt")
	// UseExt's one outgoing reference outside the repository is its
	// parameter's type http.Request; the field read r.URL is none.
	for _, r := range idx.UnresolvedOutgoing(ext.ID).References {
		if r.Name == "URL" {
			t.Errorf("the external field read is counted: %+v", r)
		}
	}
	for _, r := range idx.ReferencesByFile("p/ext.go") {
		if r.Name == "URL" {
			t.Errorf("the external field read is a reference: %+v", r)
		}
	}
	found := false
	for _, e := range idx.GetCallees(use.ID) {
		if e.To == method.ID && e.RefKind == "value_reference" {
			found = true
		}
	}
	if !found {
		t.Errorf("the method value t.Method is no edge to T.Method: %+v", idx.GetCallees(use.ID))
	}
}
