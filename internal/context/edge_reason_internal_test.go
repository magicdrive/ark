package context

import (
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// Every edge kind has its reason, and only callees and dependencies are
// followed at depth 2 (a linked document's own links are not).
func TestReasonForEdge(t *testing.T) {
	for kind, want := range map[index.EdgeKind]struct {
		reason     string
		transitive bool
	}{
		index.EdgeCalls:      {"direct callee", true},
		index.EdgeUsesType:   {"direct callee", true},
		index.EdgeImports:    {"direct callee", true},
		index.EdgeExtends:    {"extends", false},
		index.EdgeImplements: {"implements", false},
		index.EdgeUsesTrait:  {"uses_trait", false},
		index.EdgeReferences: {"references", true},
		index.EdgeDependsOn:  {"depends_on", true},
		index.EdgeLinks:      {"links", false},
	} {
		r := reasonForEdge(kind)
		if r != want.reason || expandsTransitively(r) != want.transitive {
			t.Errorf("%s: reason %q transitive %v, want %q %v", kind, r, expandsTransitively(r), want.reason, want.transitive)
		}
	}
}
