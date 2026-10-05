package typescript_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/contextquality"
)

// Context-quality certification scenarios. They drive the REAL pipeline
// (filesystem → provider → index → resolver → graph → context engine) through
// the language-neutral harness; nothing semantic is mocked.
//
// Gates for every scenario: required-context recall = 1.00 and no irrelevant
// symbol included. The measured recall / irrelevant ratio / token usage are
// logged per scenario (not averaged).

type cqCase struct {
	name string
	root string // testdata fixture repository
	contextquality.Scenario
}

func cqRoot(name string) string { return filepath.Join("testdata", name) }

func (c cqCase) run(t *testing.T) contextquality.Result {
	t.Helper()
	res, err := contextquality.Evaluate(cqRoot(c.root), tsProviders(), c.Scenario)
	if err != nil {
		t.Fatalf("[%s] %v", c.name, err)
	}
	if !res.TargetFound || !res.TargetIncluded {
		t.Fatalf("[%s] target %q found=%v included=%v", c.name, c.TargetQualified, res.TargetFound, res.TargetIncluded)
	}
	order := make([]string, len(res.Selected))
	for i, s := range res.Selected {
		order[i] = fmt.Sprintf("%s(%s)", s.Qualified, s.Reason)
	}
	t.Logf("[%s] recall=%.2f irrelevant=%.2f (%d/%d selected) tokens=%d/%d order: %s",
		c.name, res.RequiredRecall, res.IrrelevantRatio, res.SelectedCount, res.TotalCandidates,
		res.EstimatedTokens, res.BudgetTokens, strings.Join(order, ", "))
	if res.RequiredRecall != 1 {
		t.Errorf("[%s] required recall = %.2f, missing %v", c.name, res.RequiredRecall, res.MissingRequired)
	}
	if len(res.IrrelevantIncluded) != 0 {
		t.Errorf("[%s] irrelevant context included: %v", c.name, res.IrrelevantIncluded)
	}
	return res
}

const (
	userSvc   = "src/service/user-service.ts"
	userRepo  = "src/repo/user-repository.ts"
	orderRepo = "src/repo/order-repository.ts"
	userDom   = "src/domain/user.ts"
)

func tsCases() []cqCase {
	return []cqCase{
		{name: "A service method", root: "cq_ts", Scenario: contextquality.Scenario{
			Name: "A", TargetQualified: "UserService.create", Depth: 2, MaxTokens: 4000,
			Required: []string{"UserService.create", userRepo + "#UserRepository.save", userDom + "#User", "UserService.audit"},
			Irrelevant: []string{
				"UnrelatedService", "UnrelatedService.process", orderRepo + "#OrderRepository.save", "Order",
				"UserServiceMetrics", "UserServiceMetrics.record", // same file
				"OrderService", "OrderService.create", "OrderService.audit", // same member names
			},
		}},
		{name: "B imported alias", root: "cq_ts", Scenario: contextquality.Scenario{
			// DomainUser is an alias of User re-exported through a barrel.
			Name: "B", TargetQualified: "makeUser", Depth: 2, MaxTokens: 4000,
			Required:   []string{"makeUser", userDom + "#User"},
			Irrelevant: []string{"UnrelatedService", "Order", "UserRepository"},
		}},
		{name: "C ambiguity: typed receiver", root: "cq_ts", Scenario: contextquality.Scenario{
			Name: "C-typed", TargetQualified: "typedHandler", Depth: 2, MaxTokens: 4000,
			Required:   []string{"typedHandler", userRepo + "#UserRepository.save"},
			Irrelevant: []string{orderRepo + "#OrderRepository.save", "OrderRepository"},
		}},
		{name: "C ambiguity: unknown receiver", root: "cq_ts", Scenario: contextquality.Scenario{
			// No type evidence: BOTH `save` methods stay out (nothing fabricated).
			Name: "C-untyped", TargetQualified: "untypedHandler", Depth: 2, MaxTokens: 4000,
			Required: []string{"untypedHandler"},
			Irrelevant: []string{
				userRepo + "#UserRepository.save", orderRepo + "#OrderRepository.save",
				"UserRepository", "OrderRepository",
			},
		}},
		{name: "C ambiguity: unique name, untyped receiver", root: "cq_ts", Scenario: contextquality.Scenario{
			Name: "C-unique", TargetQualified: "untypedFind", Depth: 2, MaxTokens: 4000,
			Required:   []string{"untypedFind"},
			Irrelevant: []string{userRepo + "#UserRepository.find", "UserRepository"},
		}},
		{name: "D inheritance", root: "cq_ts", Scenario: contextquality.Scenario{
			Name: "D", TargetQualified: "UserService", Depth: 1, MaxTokens: 4000,
			Required:   []string{"UserService", "BaseService", "Service"},
			Irrelevant: []string{"UnrelatedService", "OrderRepository"},
		}},
		{name: "E barrel re-export", root: "cq_ts", Scenario: contextquality.Scenario{
			Name: "E", TargetQualified: "build", Depth: 2, MaxTokens: 4000,
			Required:   []string{"build", userDom + "#User"},
			Irrelevant: []string{"Order", "UserRepository"},
		}},
		{name: "external import is not a repository dependency", root: "cq_ts", Scenario: contextquality.Scenario{
			// `User` comes from "some-package"; the repository's own `User`
			// class merely shares the name and must not be pulled in.
			Name: "X", TargetQualified: "external", Depth: 2, MaxTokens: 4000,
			Required:   []string{"external"},
			Irrelevant: []string{userDom + "#User", "UserRepository"},
		}},
	}
}

func tsxCases() []cqCase {
	return []cqCase{
		{name: "F TSX component", root: "cq_tsx", Scenario: contextquality.Scenario{
			Name: "F", TargetQualified: "UserPage", Depth: 1, MaxTokens: 4000,
			Required: []string{
				"UserPage",
				"src/components/UserCard.tsx#UserCard", // through the barrel to the definition
				"src/hooks/useUser.ts#useUser",
				"src/ui/Button.tsx#Button", // namespace JSX component <UI.Button />
			},
			Irrelevant: []string{"src/legacy/UserCard.tsx#UserCard", "Unrelated"},
		}},
		{name: "F2 const component", root: "cq_tsx", Scenario: contextquality.Scenario{
			Name: "F2", TargetQualified: "Home", Depth: 2, MaxTokens: 4000,
			Required:   []string{"Home", "UserPage", "src/components/UserCard.tsx#UserCard"},
			Irrelevant: []string{"src/legacy/UserCard.tsx#UserCard", "Unrelated"},
		}},
		{name: "H TSX alias + typed receiver", root: "cq_tsx", Scenario: contextquality.Scenario{
			// Store is an alias of UserStore; the typed receiver picks UserStore.save,
			// never the same-name OrderStore.save.
			Name: "H", TargetQualified: "saveUser", Depth: 2, MaxTokens: 4000,
			Required:   []string{"saveUser", "src/data/user-store.tsx#UserStore.save", "src/data/user-store.tsx#UserStore"},
			Irrelevant: []string{"src/data/order-store.tsx#OrderStore.save", "OrderStore", "Unrelated"},
		}},
		{name: "H2 TSX unknown receiver", root: "cq_tsx", Scenario: contextquality.Scenario{
			Name: "H2", TargetQualified: "saveUnknown", Depth: 2, MaxTokens: 4000,
			Required: []string{"saveUnknown"},
			Irrelevant: []string{
				"src/data/user-store.tsx#UserStore.save", "src/data/order-store.tsx#OrderStore.save",
				"UserStore", "OrderStore",
			},
		}},
		{name: "F3 component props type", root: "cq_tsx", Scenario: contextquality.Scenario{
			Name: "F3", TargetQualified: "UserCard", TargetFile: "src/components/UserCard.tsx", Depth: 1, MaxTokens: 4000,
			Required:   []string{"src/components/UserCard.tsx#UserCard", "UserCardProps"},
			Irrelevant: []string{"src/legacy/UserCard.tsx#UserCard", "UserPage"},
		}},
	}
}

func TestContextQuality_TypeScript(t *testing.T) {
	for _, c := range tsCases() {
		t.Run(c.name, func(t *testing.T) { c.run(t) })
	}
}

func TestContextQuality_TSX(t *testing.T) {
	for _, c := range tsxCases() {
		t.Run(c.name, func(t *testing.T) { c.run(t) })
	}
}

// Scenario G: budget pressure. The target is always included, items are whole
// semantic units, the budget is honoured, required dependencies outrank
// unrelated symbols, and an oversized target keeps the TargetTruncated contract.
func TestContextQuality_BudgetPressure(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    cqCase
	}{
		{"typescript", cqCase{root: "cq_ts", Scenario: contextquality.Scenario{
			TargetQualified: "UserService.create", Depth: 2,
			Required: []string{"UserService.create", userRepo + "#UserRepository.save"},
		}}},
		{"tsx", cqCase{root: "cq_tsx", Scenario: contextquality.Scenario{
			TargetQualified: "UserPage", Depth: 1,
			Required: []string{"UserPage", "src/components/UserCard.tsx#UserCard"},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prevSel int
			for _, budget := range []int{4000, 400, 150, 80} {
				c := tc.c
				c.name = fmt.Sprintf("%s budget=%d", tc.name, budget)
				c.Scenario.MaxTokens = budget
				res, err := contextquality.Evaluate(cqRoot(c.root), tsProviders(), c.Scenario)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("[%s] selected=%d/%d tokens=%d/%d truncatedItems=%d targetTruncated=%v",
					c.name, res.SelectedCount, res.TotalCandidates, res.EstimatedTokens, budget, res.Truncated, res.TargetTruncated)
				if !res.TargetIncluded || len(res.Selected) == 0 || res.Selected[0].Qualified != c.TargetQualified {
					t.Errorf("[%s] target must always be included and first: %+v", c.name, res.Selected)
				}
				if !res.TargetTruncated && res.EstimatedTokens > budget {
					t.Errorf("[%s] budget exceeded without target truncation: %d > %d", c.name, res.EstimatedTokens, budget)
				}
				for _, s := range res.Selected {
					if s.Tokens <= 0 {
						t.Errorf("[%s] item %s has no tokens: not a complete semantic unit", c.name, s.Qualified)
					}
				}
				// Shrinking the budget never ADDS context.
				if prevSel != 0 && res.SelectedCount > prevSel {
					t.Errorf("[%s] selected %d > %d at a larger budget", c.name, res.SelectedCount, prevSel)
				}
				prevSel = res.SelectedCount
			}
			// Oversized target: stays included, TargetTruncated is set.
			c := tc.c
			c.Scenario.MaxTokens = 5
			res, err := contextquality.Evaluate(cqRoot(c.root), tsProviders(), c.Scenario)
			if err != nil {
				t.Fatal(err)
			}
			if !res.TargetIncluded || !res.TargetTruncated {
				t.Errorf("oversized target: included=%v targetTruncated=%v, want both true", res.TargetIncluded, res.TargetTruncated)
			}
		})
	}
}

// Required dependencies outrank everything else under pressure: with room for
// only one dependency, it is the exact `save` callee, not noise.
func TestContextQuality_RequiredOutranksNoise(t *testing.T) {
	c := cqCase{name: "pressure", root: "cq_ts", Scenario: contextquality.Scenario{
		TargetQualified: "UserService.create", Depth: 2, MaxTokens: 4000,
		Required: []string{"UserService.create"},
	}}
	full := c.run(t)
	if len(full.Selected) < 2 {
		t.Fatalf("expected the target plus at least one dependency, got %+v", full.Selected)
	}
	rank := map[string]int{}
	for i, s := range full.Selected {
		rank[s.Qualified] = i
		switch s.Qualified {
		case "UnrelatedService", "UnrelatedService.process", "OrderRepository.save", "Order",
			"OrderService.create", "OrderService.audit", "UserServiceMetrics.record":
			t.Errorf("noise %s selected", s.Qualified)
		}
	}
	// Under a smaller budget the selection is an order-preserving subset of the
	// full ranking: pressure drops context, it never reorders or invents it.
	c.Scenario.MaxTokens = 80
	res := c.run(t)
	last := -1
	for _, s := range res.Selected {
		r, ok := rank[s.Qualified]
		if !ok {
			t.Errorf("%s selected under pressure but not in the full context", s.Qualified)
			continue
		}
		if r < last {
			t.Errorf("%s out of order under pressure", s.Qualified)
		}
		last = r
	}
	if res.SelectedCount >= full.SelectedCount {
		t.Errorf("budget 80 should drop something: %d vs %d", res.SelectedCount, full.SelectedCount)
	}
}

// Determinism: identical output across repeated evaluations, ordering included.
func TestContextQuality_Deterministic(t *testing.T) {
	for _, c := range append(tsCases(), tsxCases()...) {
		render := func() string {
			res, err := contextquality.Evaluate(cqRoot(c.root), tsProviders(), c.Scenario)
			if err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%v|%v|%v|%d", res.Selected, res.MissingRequired, res.IrrelevantIncluded, res.EstimatedTokens)
		}
		want := render()
		for i := range 20 {
			if got := render(); got != want {
				t.Fatalf("[%s] run %d non-deterministic:\n%s\n%s", c.name, i, got, want)
			}
		}
	}
}
