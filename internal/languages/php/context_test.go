package php_test

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	arkctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/contextquality"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/php"
)

func ctxDir(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "context", name)
}

func phpProviders() []language.Provider { return []language.Provider{php.NewProvider()} }

// phpScenarios returns the agent-oriented context scenarios. Budgets default to
// a generous size unless a scenario specifically probes truncation.
func phpScenarios() []contextquality.Scenario {
	return []contextquality.Scenario{
		{
			Name: "inheritance", Task: "Understand UserService's inherited structure",
			TargetQualified: "UserService", Depth: 2, MaxTokens: 8000,
			Required: []string{"UserService", "BaseService"},
		},
		{
			Name: "interface", Task: "Understand the contract implemented",
			TargetQualified: "DbUserRepository", Depth: 2, MaxTokens: 8000,
			Required: []string{"DbUserRepository", "UserRepository"},
		},
		{
			Name: "trait", Task: "Understand composed behavior",
			TargetQualified: "UserService", Depth: 2, MaxTokens: 8000,
			Required: []string{"UserService", "LogsActivity"},
		},
		{
			Name: "constructor", Task: "Understand explicit dependencies",
			TargetQualified: "UserService.__construct", Depth: 2, MaxTokens: 8000,
			Required: []string{"UserService.__construct", "UserRepository", "Logger"},
		},
		{
			Name: "static_call", Task: "Understand the called factory",
			TargetQualified: "Service.run", Depth: 2, MaxTokens: 8000,
			Required: []string{"Service.run", "UserFactory.create"},
		},
		{
			Name: "same_class", Task: "Understand same-class method call",
			TargetQualified: "Service.a", Depth: 2, MaxTokens: 8000,
			Required: []string{"Service.a", "Service.b"},
		},
		{
			Name: "ambiguous", Task: "Do not fabricate an ambiguous static target",
			TargetQualified: "App\\Service.run", Depth: 2, MaxTokens: 8000,
			Required:   []string{"App\\Service.run"},
			Irrelevant: []string{"A\\User.create", "B\\User.create"},
		},
		{
			Name: "namespace_collision", Task: "Do not pull a colliding type",
			TargetQualified: "D\\consume", Depth: 2, MaxTokens: 8000,
			Required:   []string{"D\\consume"},
			Irrelevant: []string{"A\\User", "B\\User", "C\\User"},
		},
		{
			Name: "multihop", Task: "Reach the next semantic hop within budget",
			TargetQualified: "Controller.handle", Depth: 2, MaxTokens: 8000,
			Required: []string{"Controller.handle", "Service.run", "Repo.find"},
		},
		{
			Name: "noise", Task: "Select related code, not the whole neighborhood",
			TargetQualified: "Service.run", Depth: 2, MaxTokens: 8000,
			Required: []string{"Service.run", "Helper.go"},
			Irrelevant: []string{
				"Unrelated1", "Unrelated2", "Unrelated3",
				"unrelatedUtilA", "unrelatedUtilB", "unrelatedUtilC",
			},
		},
	}
}

// TestContext_Scenarios asserts the quality contract for every scenario:
// target first, full recall of resolvable required items, zero irrelevant
// inclusion, and token budget respected.
func TestContext_Scenarios(t *testing.T) {
	for _, s := range phpScenarios() {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			res, err := contextquality.Evaluate(ctxDir(s.Name), phpProviders(), s)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if !res.TargetFound || !res.TargetIncluded {
				t.Fatalf("target %q not found/included", s.TargetQualified)
			}
			if len(res.Selected) == 0 || res.Selected[0].Reason != "target" {
				t.Fatalf("first item must be the target, got %+v", res.Selected)
			}
			if res.RequiredRecall != 1.0 {
				t.Errorf("recall = %.2f, want 1.00; missing %v", res.RequiredRecall, res.MissingRequired)
			}
			if len(res.IrrelevantIncluded) != 0 {
				t.Errorf("irrelevant symbols pulled into context: %v", res.IrrelevantIncluded)
			}
			if !res.TargetTruncated && res.EstimatedTokens > res.BudgetTokens {
				t.Errorf("tokens %d exceed budget %d without TargetTruncated", res.EstimatedTokens, res.BudgetTokens)
			}
		})
	}
}

// TestContext_RelationReasons verifies typed relations surface with semantic
// reason labels derived from the graph EdgeKind (language-neutral).
func TestContext_RelationReasons(t *testing.T) {
	cases := []struct {
		fixture, target, related, reason string
	}{
		{"inheritance", "UserService", "BaseService", "extends"},
		{"interface", "DbUserRepository", "UserRepository", "implements"},
		{"trait", "UserService", "LogsActivity", "uses_trait"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.fixture, func(t *testing.T) {
			s := contextquality.Scenario{Name: c.fixture, TargetQualified: c.target, Depth: 2, MaxTokens: 8000}
			res, err := contextquality.Evaluate(ctxDir(c.fixture), phpProviders(), s)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, sel := range res.Selected {
				if sel.Qualified == c.related {
					found = true
					if sel.Reason != c.reason {
						t.Errorf("%s reason = %q, want %q", c.related, sel.Reason, c.reason)
					}
				}
			}
			if !found {
				t.Errorf("%s not in context", c.related)
			}
		})
	}
}

// TestContext_AmbiguousNoFabrication: ambiguous/colliding targets must never be
// fabricated into context.
func TestContext_AmbiguousNoFabrication(t *testing.T) {
	for _, s := range phpScenarios() {
		if s.Name != "ambiguous" && s.Name != "namespace_collision" {
			continue
		}
		res, err := contextquality.Evaluate(ctxDir(s.Name), phpProviders(), s)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.IrrelevantIncluded) != 0 {
			t.Errorf("[%s] ambiguous candidates fabricated into context: %v", s.Name, res.IrrelevantIncluded)
		}
	}
}

// TestContext_Determinism: repeated evaluation yields identical ordering,
// reasons, and token accounting.
func TestContext_Determinism(t *testing.T) {
	s := contextquality.Scenario{Name: "multihop", TargetQualified: "Controller.handle", Depth: 2, MaxTokens: 8000}
	first, err := contextquality.Evaluate(ctxDir("multihop"), phpProviders(), s)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 100 {
		got, err := contextquality.Evaluate(ctxDir("multihop"), phpProviders(), s)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Selected) != len(first.Selected) {
			t.Fatalf("run %d: selection count differs", i)
		}
		for j := range got.Selected {
			if got.Selected[j] != first.Selected[j] {
				t.Fatalf("run %d: item %d differs: %+v vs %+v", i, j, got.Selected[j], first.Selected[j])
			}
		}
	}
}

// TestContext_TokenBudgets: across budgets the target is always present, order
// is target-first, and tokens stay within budget (unless the target itself is
// truncated).
func TestContext_TokenBudgets(t *testing.T) {
	for _, budget := range []int{30, 100, 8000} {
		s := contextquality.Scenario{Name: "multihop", TargetQualified: "Controller.handle", Depth: 2, MaxTokens: budget}
		res, err := contextquality.Evaluate(ctxDir("multihop"), phpProviders(), s)
		if err != nil {
			t.Fatal(err)
		}
		if !res.TargetIncluded {
			t.Errorf("budget %d: target dropped", budget)
		}
		if res.Selected[0].Reason != "target" {
			t.Errorf("budget %d: first item not target", budget)
		}
		if !res.TargetTruncated && res.EstimatedTokens > budget {
			t.Errorf("budget %d: tokens %d exceed budget", budget, res.EstimatedTokens)
		}
	}
}

// TestContext_TargetOverBudget: a target larger than the budget is still
// represented and TargetTruncated is signalled (existing Hardening preserved).
func TestContext_TargetOverBudget(t *testing.T) {
	s := contextquality.Scenario{Name: "over_budget", TargetQualified: "Big.run", Depth: 1, MaxTokens: 5}
	res, err := contextquality.Evaluate(ctxDir("over_budget"), phpProviders(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TargetIncluded {
		t.Error("target must be represented even when it exceeds the budget")
	}
	if !res.TargetTruncated {
		t.Error("TargetTruncated must be signalled when the target exceeds the budget")
	}
}

// TestContext_IncludeTestsNoOpForPHP documents that PHP currently has NO entry
// in the generic test-file detector (isTestFile), so IncludeTests is a no-op
// for PHP. We intentionally do NOT add a PHP-specific test heuristic (that is a
// separate, generic concern). This test freezes the current honest behavior.
func TestContext_IncludeTestsNoOpForPHP(t *testing.T) {
	base := contextquality.Scenario{Name: "include_tests", TargetQualified: "ServiceTest.testRun", Depth: 2, MaxTokens: 8000}
	withTests := base
	withTests.IncludeTests = true
	a, err := contextquality.Evaluate(ctxDir("include_tests"), phpProviders(), base)
	if err != nil {
		t.Fatal(err)
	}
	b, err := contextquality.Evaluate(ctxDir("include_tests"), phpProviders(), withTests)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Selected) != len(b.Selected) {
		t.Errorf("IncludeTests changed PHP context (%d vs %d); PHP has no test-file detection, so it should be a no-op", len(a.Selected), len(b.Selected))
	}
}

// TestContext_MCPSerialization verifies the MCP-facing serialization carries
// the typed relation reason and budget/truncation stats through to agents.
func TestContext_MCPSerialization(t *testing.T) {
	idx, err := index.New(context.Background(), ctxDir("inheritance"), phpProviders())
	if err != nil {
		t.Fatal(err)
	}
	targets := idx.FindSymbolsByQualified("UserService")
	if len(targets) == 0 {
		t.Fatal("target missing")
	}
	eng := arkctx.New(idx, ctxDir("inheritance"))
	res, err := eng.Build(context.Background(), arkctx.Request{Target: targets[0].ID, MaxTokens: 8000, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := arkctx.FormatJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{`"reason": "extends"`, `"BudgetTokens"`, `"TargetTruncated"`} {
		if !strings.Contains(js, want) {
			t.Errorf("MCP JSON missing %q\n%s", want, js)
		}
	}
}

// TestContextBaseline measures (does not strictly assert) the current Context
// Engine quality for every PHP scenario and prints a matrix for review.
func TestContextBaseline(t *testing.T) {
	for _, s := range phpScenarios() {
		res, err := contextquality.Evaluate(ctxDir(s.Name), phpProviders(), s)
		if err != nil {
			t.Fatalf("[%s] evaluate: %v", s.Name, err)
		}
		fmt.Printf("== %s ==\n", s.Name)
		fmt.Printf("   recall=%.2f irrelevantRatio=%.2f tokens=%d selected=%d/%d truncated=%d targetTruncated=%v\n",
			res.RequiredRecall, res.IrrelevantRatio, res.EstimatedTokens, res.SelectedCount, res.TotalCandidates, res.Truncated, res.TargetTruncated)
		if len(res.MissingRequired) > 0 {
			fmt.Printf("   MISSING required: %v\n", res.MissingRequired)
		}
		if len(res.IrrelevantIncluded) > 0 {
			fmt.Printf("   IRRELEVANT included: %v\n", res.IrrelevantIncluded)
		}
		for i, sel := range res.Selected {
			fmt.Printf("   [%d] %-28s reason=%-16s tokens=%d\n", i, sel.Qualified, sel.Reason, sel.Tokens)
		}
	}
}
