package index

import (
	"testing"

	"github.com/magicdrive/ark/internal/reference"
)

// Merged edges of one (from, to, kind) report the reference kind that says
// the most — a call before a type use — whatever order the references came
// in (the reverse edge's display kind depends on it: called_by, not
// type_used_by, for a symbol that both calls and names another).
func TestEdgeDedupe_RefKindIsOrderIndependent(t *testing.T) {
	call := GraphEdge{From: "a", To: "b", Kind: EdgeCalledBy, RefKind: reference.KindCall}
	use := GraphEdge{From: "a", To: "b", Kind: EdgeCalledBy, RefKind: reference.KindTypeUse}
	imp := GraphEdge{From: "a", To: "b", Kind: EdgeCalledBy, RefKind: reference.KindImport}
	for _, in := range [][]GraphEdge{{call, use, imp}, {use, imp, call}, {imp, call, use}} {
		out := dedupeEdges(in)
		if len(out) != 1 || out[0].RefKind != reference.KindCall {
			t.Errorf("%v merged into %v, want one edge of kind call", in, out)
		}
	}
	if out := dedupeEdges([]GraphEdge{imp, use}); len(out) != 1 || out[0].RefKind != reference.KindTypeUse {
		t.Errorf("type use and import merged into %v, want type_use", out)
	}
}
