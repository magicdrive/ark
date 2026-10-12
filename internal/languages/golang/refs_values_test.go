package golang

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// Inside an ERROR node the parser's recovery decides what a name looks like
// (a declaration's name may sit where a use would): no type use, value
// reference or write is recorded there.
func TestRefsValues_NothingInsideParseErrors(t *testing.T) {
	// Each case's name sits inside an ERROR node: a declaration's name
	// (Good) and a type name (Kind) the recovery placed there.
	for _, src := range []string{
		"package p\n\nfunc Broken( {\n\nfunc Good() {}\n",
		"package p\n\nfunc f() { _ = x.(Kind]) }\n",
	} {
		ex, err := NewProvider().Extract(context.Background(), source.FileID("x.go"), []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if len(ex.Diagnostics) == 0 {
			t.Fatalf("%q: expected a parse error", src)
		}
		for _, r := range ex.References {
			switch r.Kind {
			case "type_use", "value_reference", "write":
				for _, name := range []string{"Good", "Kind"} {
					if r.Name == name {
						t.Errorf("%q: %s %s recorded from inside an ERROR", src, r.Kind, r.Name)
					}
				}
			}
		}
	}
}
