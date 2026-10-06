package php_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/resolver"
)

// PHP-Q0 oracle for qualified identity evidence.
//
// The scenarios below pin down, over complete repositories (fixtures under
// testdata/qualid), which graph edges PHP may and may not produce when the
// lexical name of a type is fixed by the language's own name-resolution rules
// (namespace / use / use-as / fully-qualified syntax):
//
//   - an external FQN (`use Illuminate\Http\Request`) must never fall back to
//     a same-named repository class: no Strong/Exact edge, no edge at all;
//   - an alias of an external FQN must never fall back to a suffix match;
//   - a repository-declared FQN resolves Exact with `qualified_identity`
//     evidence; a duplicated FQN is Candidate (no edge).
//
// Edge sets are compared exactly, so any additional (fabricated) edge from the
// probed symbol fails the test as well.

type edgeView struct {
	To   string
	Kind string
	Conf string
	Ev   string // comma-joined evidence kinds
}

func (e edgeView) String() string {
	return fmt.Sprintf("-%s/%s-> %s [%s]", e.Kind, e.Conf, e.To, e.Ev)
}

// edgesFrom returns every outgoing graph edge of the unique symbol `from`.
func edgesFrom(t *testing.T, idx *index.RepositoryIndex, from string) []edgeView {
	t.Helper()
	syms := idx.FindSymbolsByQualified(from)
	if len(syms) != 1 {
		t.Fatalf("symbol %q: want exactly one declaration, got %d", from, len(syms))
	}
	var out []edgeView
	for _, e := range idx.GetCallees(syms[0].ID) {
		to, _ := idx.GetSymbol(e.To)
		var evs []string
		for _, ev := range e.Evidence {
			evs = append(evs, string(ev.Kind))
		}
		sort.Strings(evs)
		out = append(out, edgeView{To: to.Qualified, Kind: string(e.Kind), Conf: e.Confidence.String(), Ev: strings.Join(evs, ",")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// evQualifiedIdentity is the evidence kind of an authoritative qualified
// identity match (resolver.EvidenceQualifiedIdentity). It is spelled out here
// so the oracle states the contract independently of the implementation.
const evQualifiedIdentity = "qualified_identity"

// exactQI is the expected edge shape for authoritative qualified identity.
func exactQI(kind, to string) edgeView {
	return edgeView{To: to, Kind: kind, Conf: resolver.ConfidenceExact.String(), Ev: evQualifiedIdentity}
}

func wantEdges(t *testing.T, idx *index.RepositoryIndex, from string, want ...edgeView) {
	t.Helper()
	got := edgesFrom(t, idx, from)
	sort.Slice(want, func(i, j int) bool { return want[i].String() < want[j].String() })
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("edges from %s:\n got  %v\n want %v", from, got, want)
	}
}

// --- negative oracle -------------------------------------------------------

// Vendor shadowing: `use Illuminate\Http\Request` names an external class even
// though the repository declares App\Models\Request. Nothing may bind to it.
func TestQualifiedIdentity_VendorShadowing(t *testing.T) {
	idx := phpIndex(t, "qualid/vendor_shadow")
	for _, m := range []string{"a", "b", "c", "d"} {
		from := `App\Http\C.` + m
		if got := edgesFrom(t, idx, from); len(got) != 0 {
			t.Errorf("%s must have no edges (external FQN), got %v", from, got)
		}
	}
}

// Alias suffix: `use Vendor\Package\Factory as Factory; Factory::make()` must
// not bind to App\Pdf\PdfFactory.make by suffix similarity.
func TestQualifiedIdentity_AliasSuffix(t *testing.T) {
	idx := phpIndex(t, "qualid/alias_suffix")
	if got := edgesFrom(t, idx, `App\Http\C.a`); len(got) != 0 {
		t.Errorf("external alias must not resolve to PdfFactory.make, got %v", got)
	}
}

// Duplicate FQN: two declarations of the same qualified identity are
// ambiguity, i.e. Candidate — never a fabricated unique edge.
func TestQualifiedIdentity_DuplicateFQN(t *testing.T) {
	idx := phpIndex(t, "qualid/duplicate_fqn")
	for _, m := range []string{"a", "b", "c"} {
		from := `App\Http\C.` + m
		if got := edgesFrom(t, idx, from); len(got) != 0 {
			t.Errorf("%s: duplicate FQN must yield no edge, got %v", from, got)
		}
	}
}

// --- positive oracle -------------------------------------------------------

func TestQualifiedIdentity_RepositoryLocalUse(t *testing.T) {
	idx := phpIndex(t, "qualid/local_use")
	svc := `App\Services\`
	wantEdges(t, idx, `App\Http\C.a`, exactQI("calls", svc+"Foo.make"))
	wantEdges(t, idx, `App\Http\C.b`, exactQI("uses_type", svc+"Foo"), exactQI("calls", svc+"Foo.run"))
	wantEdges(t, idx, `App\Http\C.c`, exactQI("calls", svc+"Foo"))
	wantEdges(t, idx, `App\Http\D`, exactQI("extends", svc+"Foo"))
	wantEdges(t, idx, `App\Http\E`, exactQI("implements", svc+"Contract"))
	wantEdges(t, idx, `App\Http\F`, exactQI("uses_trait", svc+"LogsActivity"))
}

func TestQualifiedIdentity_AliasedUse(t *testing.T) {
	idx := phpIndex(t, "qualid/alias_use")
	svc := `App\Services\`
	wantEdges(t, idx, `App\Http\C.a`, exactQI("calls", svc+"Foo.make"))
	wantEdges(t, idx, `App\Http\C.b`, exactQI("uses_type", svc+"Foo"), exactQI("calls", svc+"Foo.run"))
	wantEdges(t, idx, `App\Http\C.c`, exactQI("calls", svc+"Foo"))
	wantEdges(t, idx, `App\Http\D`, exactQI("extends", svc+"Foo"))
	wantEdges(t, idx, `App\Http\E`, exactQI("implements", svc+"Contract"))
	wantEdges(t, idx, `App\Http\F`, exactQI("uses_trait", svc+"LogsActivity"))
}

func TestQualifiedIdentity_NamespaceLocal(t *testing.T) {
	idx := phpIndex(t, "qualid/namespace_local")
	svc := `App\Services\`
	wantEdges(t, idx, svc+`Caller.a`, exactQI("calls", svc+"Foo.make"))
	wantEdges(t, idx, svc+`Caller.b`, exactQI("uses_type", svc+"Foo"), exactQI("calls", svc+"Foo.run"))
	wantEdges(t, idx, svc+`Caller.c`, exactQI("calls", svc+"Foo"))
	// namespace\Foo is relative to the current namespace.
	wantEdges(t, idx, svc+`Caller.d`, exactQI("calls", svc+"Foo.make"))
}

func TestQualifiedIdentity_FullyQualified(t *testing.T) {
	idx := phpIndex(t, "qualid/fully_qualified")
	svc := `App\Services\`
	wantEdges(t, idx, `App\Http\C.a`, exactQI("calls", svc+"Foo.make"))
	wantEdges(t, idx, `App\Http\C.b`, exactQI("uses_type", svc+"Foo"), exactQI("calls", svc+"Foo.run"))
	wantEdges(t, idx, `App\Http\C.c`, exactQI("calls", svc+"Foo"))
}

// Two classes share a short name; only the explicitly named FQN may resolve.
func TestQualifiedIdentity_SameShortNameDifferentNamespaces(t *testing.T) {
	idx := phpIndex(t, "qualid/shortname_two_ns")
	wantEdges(t, idx, `App\Http\C.a`, exactQI("calls", `App\B\Item.make`))
	wantEdges(t, idx, `App\Http\C.c`, exactQI("calls", `App\B\Item`))
	// No `use`: the same-namespace Item is the only identity.
	wantEdges(t, idx, `App\A\UsesLocal.a`, exactQI("calls", `App\A\Item.make`))
}

// `use` tables are scoped to their namespace block: the same alias denotes a
// different class in each block.
func TestQualifiedIdentity_UseTableIsPerNamespaceBlock(t *testing.T) {
	idx := phpIndex(t, "qualid/namespace_scope")
	wantEdges(t, idx, `A\C1.f`, exactQI("calls", `X\Foo.m`))
	wantEdges(t, idx, `B\C2.f`, exactQI("calls", `Y\Foo.m`))
	wantEdges(t, idx, `A2\M1.f`, exactQI("calls", `X\Foo.m`))
	wantEdges(t, idx, `B2\M2.f`, exactQI("calls", `Y\Foo.m`))
}
