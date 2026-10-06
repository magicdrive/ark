package golden_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/language"
)

// TestSnapshot_QualifiedIdentityRenderedOnlyWhenPresent: the snapshot prints
// qualified identity evidence when a provider sets it and nothing otherwise, so
// every committed snapshot of a provider that does not set it is unchanged.
func TestSnapshot_QualifiedIdentityRenderedOnlyWhenPresent(t *testing.T) {
	without := golden.ExtractionSnapshot("a.go", language.Extraction{
		References: []language.ReferenceDraft{{Name: "Println", Kind: "call", ReceiverExpr: "fmt", IsCall: true}},
	})
	if strings.Contains(without, "qualified") {
		t.Errorf("snapshot without evidence must not mention qualified identity:\n%s", without)
	}

	with := golden.ExtractionSnapshot("a.php", language.Extraction{
		References: []language.ReferenceDraft{
			{Name: "make", Kind: "call", ReceiverExpr: "Bar", ReceiverType: "Bar", ReceiverTypeQualified: `App\Services\Foo`, IsCall: true},
			{Name: "Foo", Kind: "construction", NameQualified: `App\Services\Foo`},
		},
	})
	for _, want := range []string{`receiver_type_qualified="App\\Services\\Foo"`, `name_qualified="App\\Services\\Foo"`} {
		if !strings.Contains(with, want) {
			t.Errorf("snapshot missing %s:\n%s", want, with)
		}
	}
}
