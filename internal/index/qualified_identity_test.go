package index_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/php"
	"github.com/magicdrive/ark/internal/source"
)

// TestNewFileIndex_PropagatesQualifiedIdentity: the provider's qualified
// identity evidence reaches the normalized reference unchanged, and drafts that
// carry none produce references that carry none.
func TestNewFileIndex_PropagatesQualifiedIdentity(t *testing.T) {
	fi := index.NewFileIndex(php.NewProvider(), source.FileID("app/C.php"), language.Extraction{
		References: []language.ReferenceDraft{
			{Name: "make", Kind: "call", ReceiverExpr: "Bar", ReceiverType: "Bar", ReceiverTypeQualified: `App\Services\Foo`, IsCall: true},
			{Name: "Foo", Kind: "construction", NameQualified: `App\Services\Foo`,
				Location: source.Location{Range: source.Range{Start: source.Position{Line: 1}}}},
			{Name: "save", Kind: "call", ReceiverExpr: "repo", IsCall: true,
				Location: source.Location{Range: source.Range{Start: source.Position{Line: 2}}}},
		},
	})
	if len(fi.References) != 3 {
		t.Fatalf("want 3 references, got %d", len(fi.References))
	}
	if r := fi.References[0]; r.ReceiverTypeQualified != `App\Services\Foo` || r.NameQualified != "" {
		t.Errorf("receiver-type identity not propagated: %+v", r)
	}
	if r := fi.References[1]; r.NameQualified != `App\Services\Foo` || r.ReceiverTypeQualified != "" {
		t.Errorf("name identity not propagated: %+v", r)
	}
	if r := fi.References[2]; r.NameQualified != "" || r.ReceiverTypeQualified != "" {
		t.Errorf("a draft without evidence must yield a reference without evidence: %+v", r)
	}
}
