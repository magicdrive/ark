package php_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/resolver"
)

// TestQualifiedIdentityGates is the PHP counterpart of the TypeScript
// certification summary, scoped to qualified identity:
//
//	False Exact/Strong resolution = 0  (negative oracle: edges that must never exist)
//	Fabricated graph edges        = 0  (real endpoints; Exact/Strong; structural evidence)
//	Determinism failures          = 0  (repeated builds are byte-identical)
//
// Over the qualid fixtures every edge must rest on qualified_identity evidence
// and the number of edges is pinned, so an edge from any other (heuristic) path
// cannot appear unnoticed. Required-context recall is asserted by the
// positive oracle in qualified_identity_test.go.
func TestQualifiedIdentityGates(t *testing.T) {
	// Evidence that justifies an Exact/Strong edge. same_package and
	// unique_repo_match are proximity/uniqueness heuristics: they must not be
	// the justification of an edge that carries a lexical identity.
	structural := map[resolver.EvidenceKind]bool{
		resolver.EvidenceQualifiedIdentity: true,
		resolver.EvidenceSameLexicalScope:  true,
		resolver.EvidenceSameFile:          true,
		resolver.EvidenceReceiverType:      true,
	}

	fixtures := []struct {
		name  string
		edges int      // exact number of graph edges in the fixture
		never []string // "<from> -> <to>" pairs (to is a prefix) that must not exist
	}{
		{"vendor_shadow", 0, []string{`App\Http\C. -> App\Models\Request`}},
		{"alias_suffix", 0, []string{`App\Http\C. -> App\Pdf\PdfFactory`}},
		{"duplicate_fqn", 0, []string{`App\Http\C. -> App\Services\Foo`}},
		{"local_use", 7, nil},
		{"alias_use", 7, nil},
		{"namespace_local", 5, []string{`App\Services\Caller. -> App\Other\Foo`}},
		{"fully_qualified", 4, nil},
		{"shortname_two_ns", 3, []string{
			`App\Http\C. -> App\A\Item`,
			`App\A\UsesLocal. -> App\B\Item`,
		}},
		{"namespace_scope", 4, []string{
			`A\C1. -> Y\Foo`, `A2\M1. -> Y\Foo`,
			`B\C2. -> X\Foo`, `B2\M2. -> X\Foo`,
		}},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			idx := phpIndex(t, "qualid/"+fx.name)
			edges := gateEdges(t, idx, structural, true)
			if len(edges) != fx.edges {
				t.Errorf("fabricated or missing edges: got %d, want %d:\n%s", len(edges), fx.edges, strings.Join(edges, "\n"))
			}
			// Negative oracle: false Exact/Strong = 0.
			for _, e := range edges {
				for _, bad := range fx.never {
					from, toPrefix, _ := strings.Cut(bad, " -> ")
					if strings.HasPrefix(e, from) && strings.Contains(e, "-> "+toPrefix) {
						t.Errorf("false edge %q matches forbidden %q", e, bad)
					}
				}
			}
			// Determinism: identical graph and byte-identical serialized index.
			want := golden.IndexSnapshot(idx)
			for i := range 25 {
				again := phpIndex(t, "qualid/"+fx.name)
				if got := golden.IndexSnapshot(again); got != want {
					t.Fatalf("run %d: serialized index differs", i)
				}
				if got, w := strings.Join(gateEdges(t, again, structural, true), "\n"), strings.Join(edges, "\n"); got != w {
					t.Fatalf("run %d: edges differ", i)
				}
			}
		})
	}
}

// TestPHPFixtureEdgesHaveStructuralEvidence extends the structural-evidence
// gate to every other PHP fixture repository. After qualified identity, no edge
// of these fixtures rests on a proximity/uniqueness heuristic. (PHP functions and
// constants are out of scope and keep their existing rules; no fixture here
// produces such an edge.)
func TestPHPFixtureEdgesHaveStructuralEvidence(t *testing.T) {
	structural := map[resolver.EvidenceKind]bool{
		resolver.EvidenceQualifiedIdentity: true,
		resolver.EvidenceSameLexicalScope:  true,
		resolver.EvidenceSameFile:          true,
		resolver.EvidenceReceiverType:      true,
	}
	roots := []string{"corpus", "intsvc", "relations", "ambig", "res_static_unique", "res_static_false", "res_var_receiver"}
	for _, c := range []string{"ambiguous", "constructor", "include_tests", "inheritance", "interface", "multihop",
		"namespace_collision", "noise", "over_budget", "same_class", "static_call", "trait"} {
		roots = append(roots, "context/"+c)
	}
	for _, root := range roots {
		t.Run(root, func(t *testing.T) {
			gateEdges(t, phpIndex(t, root), structural, false)
		})
	}
}

// gateEdges lists every graph edge as "from -kind/conf-> to [evidence]" and
// fails the test for any edge that is not a well-formed, structurally justified
// Exact/Strong edge. When requireIdentity is set every edge must additionally
// rest on qualified_identity.
func gateEdges(t *testing.T, idx *index.RepositoryIndex, structural map[resolver.EvidenceKind]bool, requireIdentity bool) []string {
	t.Helper()
	var out []string
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				to, ok := idx.GetSymbol(e.To)
				if !ok {
					t.Errorf("fabricated edge: %s -> unknown symbol %s", s.Qualified, e.To)
					continue
				}
				var evs []string
				ok = false
				hasIdentity := false
				for _, ev := range e.Evidence {
					evs = append(evs, string(ev.Kind))
					if structural[ev.Kind] {
						ok = true
					}
					if ev.Kind == resolver.EvidenceQualifiedIdentity {
						hasIdentity = true
					}
				}
				line := fmt.Sprintf("%s -%s/%s-> %s [%s]", s.Qualified, e.Kind, e.Confidence, to.Qualified, strings.Join(evs, ","))
				if e.Confidence < resolver.ConfidenceStrong {
					t.Errorf("edge below Strong: %s", line)
				}
				if !ok {
					t.Errorf("edge lacks structural evidence: %s", line)
				}
				if requireIdentity && !hasIdentity {
					t.Errorf("edge does not rest on qualified_identity: %s", line)
				}
				out = append(out, line)
			}
		}
	}
	return out
}
