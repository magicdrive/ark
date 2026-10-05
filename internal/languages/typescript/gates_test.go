package typescript_test

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// TestCertificationGates is the certification summary over the realistic
// fixture repositories:
//
//	False Exact resolution = 0   (negative oracle: edges that must never exist)
//	Fabricated graph edges = 0   (every edge: real endpoints, Exact/Strong, structural evidence)
//	Determinism failures   = 0   (repeated builds are byte-identical)
//
// Required-context recall = 1.00 is asserted per scenario in context_test.go.
func TestCertificationGates(t *testing.T) {
	// Evidence kinds that justify an Exact/Strong edge. same_package and
	// unique_repo_match are proximity/uniqueness heuristics: they must never be
	// the justification of a TypeScript edge (files are module-scoped).
	structural := []string{"same_lexical_scope", "same_file", "module_binding", "receiver_type"}

	forbidden := map[string][]string{
		"cq_ts": {
			// unknown receiver: no edge to ANY save, even though one is typed elsewhere
			"src/api/handler.ts:untypedHandler",
			// unique method name + untyped receiver: uniqueness is not evidence
			"src/api/handler.ts:untypedFind",
			// external `User` must not bind to the repository's own User
			"src/api/external.ts:external",
			// the same-name OrderRepository.save is never a callee of the user side
			"-> src/repo/order-repository.ts:OrderRepository.save",
			"-> src/service/order-service.ts",
			"-> src/service/unrelated.ts",
		},
		"cq_tsx": {
			"src/pages/Editor.tsx:saveUnknown",
			"-> src/legacy/UserCard.tsx",
			"-> src/data/order-store.tsx",
			"-> span", "-> div", "-> button",
		},
	}

	for _, fixture := range []string{"cq_ts", "cq_tsx"} {
		t.Run(fixture, func(t *testing.T) {
			root := cqRoot(fixture)
			idx, err := index.New(context.Background(), root, tsProviders())
			if err != nil {
				t.Fatal(err)
			}
			edges := edgeSet(t, idx) // fails on fabricated targets
			checkGraphInvariants(t, idx)
			if len(edges) == 0 {
				t.Fatal("no edges: fixture is not exercising the graph")
			}

			// Evidence: every edge carries structural evidence.
			for _, f := range idx.Files() {
				for _, s := range idx.SymbolsByFile(f) {
					for _, e := range idx.GetCallees(s.ID) {
						ok := false
						for _, ev := range e.Evidence {
							for _, k := range structural {
								if string(ev.Kind) == k {
									ok = true
								}
							}
						}
						if !ok {
							t.Errorf("%s:%s edge lacks structural evidence: %+v", f, s.Qualified, e.Evidence)
						}
					}
				}
			}

			// Negative oracle: false Exact = 0.
			for _, e := range edges {
				for _, bad := range forbidden[fixture] {
					if strings.Contains(e, bad) && (strings.HasPrefix(bad, "->") || strings.HasPrefix(e, bad+" ")) {
						t.Errorf("false edge %q matches forbidden %q", e, bad)
					}
				}
			}

			// Determinism: identical edge set across 25 rebuilds.
			want := strings.Join(edges, "\n")
			for i := range 25 {
				again, err := index.New(context.Background(), root, tsProviders())
				if err != nil {
					t.Fatal(err)
				}
				if got := strings.Join(edgeSet(t, again), "\n"); got != want {
					t.Fatalf("run %d non-deterministic", i)
				}
			}
			t.Logf("%s: %d edges, 0 false-Exact, 0 fabricated, 0 determinism failures", fixture, len(edges))
		})
	}
}
