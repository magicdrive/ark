package php_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/php"
)

func phpIndex(t *testing.T, fixture string) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), testdataDir(t, fixture), []language.Provider{php.NewProvider()})
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

// hasEdgeTo reports whether `from` has any outgoing edge to a symbol whose
// qualified name equals `to`.
func hasEdgeTo(idx *index.RepositoryIndex, from, to string) bool {
	syms := idx.FindSymbolsByQualified(from)
	if len(syms) == 0 {
		return false
	}
	for _, e := range idx.GetCallees(syms[0].ID) {
		if s, ok := idx.GetSymbol(e.To); ok && s.Qualified == to {
			return true
		}
	}
	return false
}

// outgoingTargets lists the qualified targets of edges from `from`.
func outgoingTargets(idx *index.RepositoryIndex, from string) []string {
	syms := idx.FindSymbolsByQualified(from)
	if len(syms) == 0 {
		return nil
	}
	var out []string
	for _, e := range idx.GetCallees(syms[0].ID) {
		if s, ok := idx.GetSymbol(e.To); ok {
			out = append(out, s.Qualified)
		}
	}
	return out
}

// TestResolution_StaticUniqueReceiver: `User::create()` where User.create is the
// only create in the file resolves to User.create (honest, correct).
func TestResolution_StaticUniqueReceiver(t *testing.T) {
	idx := phpIndex(t, "res_static_unique")
	if !hasEdgeTo(idx, "f", "User.create") {
		t.Errorf("expected edge f -> User.create; got %v", outgoingTargets(idx, "f"))
	}
}

// TestResolution_VariableReceiverAmbiguous: `$x->save()` with two save methods
// and no type inference must NOT create a unique edge (honest Candidate).
func TestResolution_VariableReceiverAmbiguous(t *testing.T) {
	idx := phpIndex(t, "res_var_receiver")
	for _, to := range []string{"User.save", "Order.save"} {
		if hasEdgeTo(idx, "f", to) {
			t.Errorf("variable receiver $x->save() must not resolve uniquely; unexpected edge f -> %s", to)
		}
	}
}

// TestKnownIssue_StaticReceiverFalseExact (the name is historical) pins the fix
// of a former false Exact: a static call `User::create()` resolved via the
// resolver's same-file bare-name match, which ignored the explicit type
// receiver, to an unrelated same-file method `SuperUser.create` — a false Exact
// AND a false graph edge. The language-neutral fix constrains every name-based
// stage to members of an explicit type receiver (resolver.constrainReceiver).
func TestKnownIssue_StaticReceiverFalseExact(t *testing.T) {
	idx := phpIndex(t, "res_static_false")
	if hasEdgeTo(idx, "f", "SuperUser.create") {
		t.Errorf("User::create() must not resolve to the unrelated SuperUser.create")
	}
}
