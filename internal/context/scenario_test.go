package context_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/symbol"

	ctx "github.com/magicdrive/ark/internal/context"
)

type scenario struct {
	Name              string   `json:"name"`
	TargetName        string   `json:"target_name"`
	TargetFile        string   `json:"target_file"`
	Budget            int      `json:"budget"`
	MustInclude       []string `json:"must_include"`
	ShouldInclude     []string `json:"should_include"`
	MustNotPrioritize []string `json:"must_not_prioritize"`
}

type scenarioFile struct {
	Scenarios []scenario `json:"scenarios"`
}

func TestScenarios(t *testing.T) {
	data, err := os.ReadFile("testdata/scenarios.json")
	if err != nil {
		t.Fatalf("read scenarios.json: %v", err)
	}
	var sf scenarioFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("parse scenarios.json: %v", err)
	}

	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), fixtureDir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	eng := ctx.New(idx, fixtureDir)

	for _, sc := range sf.Scenarios {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			var targetID symbol.SymbolID
			for _, sym := range idx.FindSymbols(sc.TargetName) {
				if sc.TargetFile == "" || strings.Contains(string(sym.Location.File), sc.TargetFile) {
					targetID = sym.ID
					break
				}
			}
			if targetID == "" {
				t.Skipf("target %q not found in fixture", sc.TargetName)
			}

			result, err := eng.Build(context.Background(), ctx.Request{
				Target:    targetID,
				MaxTokens: sc.Budget,
				MaxDepth:  2,
			})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			present := make(map[string]bool)
			for _, item := range result.Items {
				present[item.Symbol.Name] = true
				present[item.Symbol.Qualified] = true
			}

			// must_include assertions.
			criticalMissed := 0
			for _, must := range sc.MustInclude {
				if !present[must] {
					t.Errorf("must_include %q not found in result (budget %d, got %d items)",
						must, sc.Budget, len(result.Items))
					criticalMissed++
				}
			}

			// should_include: advisory only — log but don't fail.
			for _, should := range sc.ShouldInclude {
				if !present[should] {
					t.Logf("ADVISORY: should_include %q not found (budget %d)", should, sc.Budget)
				}
			}

			// must_not_prioritize: must not appear in top-3.
			if len(result.Items) >= 3 {
				top := make(map[string]bool)
				for _, item := range result.Items[:3] {
					top[item.Symbol.Name] = true
					top[item.Symbol.Qualified] = true
				}
				for _, bad := range sc.MustNotPrioritize {
					if top[bad] {
						t.Errorf("must_not_prioritize %q appears in top-3 items", bad)
					}
				}
			}

			// Token budget: target-presence invariant allows small overrun.
			if result.Stats.EstimatedTokens > sc.Budget+200 {
				t.Errorf("tokens %d far exceeded budget %d (tolerance +200)",
					result.Stats.EstimatedTokens, sc.Budget)
			}

			// Quality metrics (logged, not fatal).
			recall := qualityRecall(sc.MustInclude, present)
			t.Logf("quality: recall=%.0f%% items=%d/%d tokens=%d/%d target_truncated=%v",
				recall*100,
				result.Stats.SelectedItems, result.Stats.TotalCandidates,
				result.Stats.EstimatedTokens, sc.Budget,
				result.Stats.TargetTruncated)
			_ = criticalMissed
		})
	}
}

// qualityRecall computes the fraction of must_include symbols that were found.
func qualityRecall(mustInclude []string, present map[string]bool) float64 {
	if len(mustInclude) == 0 {
		return 1.0
	}
	found := 0
	for _, s := range mustInclude {
		if present[s] {
			found++
		}
	}
	return float64(found) / float64(len(mustInclude))
}

// TestRanker_ScoreBreakdownDeterministic verifies that ranking scores are
// deterministic and that the breakdown explains the total.
func TestRanker_ScoreBreakdownDeterministic(t *testing.T) {
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), fixtureDir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found in fixture")
	}

	eng := ctx.New(idx, fixtureDir)
	req := ctx.Request{Target: syms[0].ID, MaxTokens: 8000, MaxDepth: 2}

	r1, err := eng.Build(context.Background(), req)
	if err != nil {
		t.Fatalf("Build 1: %v", err)
	}
	r2, err := eng.Build(context.Background(), req)
	if err != nil {
		t.Fatalf("Build 2: %v", err)
	}

	if len(r1.Items) != len(r2.Items) {
		t.Fatalf("non-deterministic item count: %d vs %d", len(r1.Items), len(r2.Items))
	}

	for i := range r1.Items {
		a, b := r1.Items[i], r2.Items[i]
		if a.Score != b.Score {
			t.Errorf("item[%d] %q: score not deterministic: %v vs %v",
				i, a.Symbol.Qualified, a.Score, b.Score)
		}
		if len(a.ScoreBreakdown) != len(b.ScoreBreakdown) {
			t.Errorf("item[%d] %q: breakdown length differs", i, a.Symbol.Qualified)
		}

		// Verify breakdown sums to total (within float tolerance).
		total := 0.0
		for _, v := range a.ScoreBreakdown {
			total += v
		}
		if fmt.Sprintf("%.4f", total) != fmt.Sprintf("%.4f", a.Score) {
			t.Errorf("item[%d] %q: breakdown sum %v != score %v",
				i, a.Symbol.Qualified, total, a.Score)
		}
	}
}

// TestRanker_TargetAlwaysHighestScore verifies that the target symbol scores
// higher than any other candidate.
func TestRanker_TargetAlwaysHighestScore(t *testing.T) {
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), fixtureDir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found in fixture")
	}

	eng := ctx.New(idx, fixtureDir)
	result, err := eng.Build(context.Background(), ctx.Request{
		Target:    syms[0].ID,
		MaxTokens: 8000,
		MaxDepth:  2,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(result.Items) == 0 {
		t.Fatal("expected at least one item")
	}

	// Target must be the first item (highest-scored).
	if result.Items[0].Reason != "target" {
		t.Errorf("first item reason = %q, want %q", result.Items[0].Reason, "target")
	}

	// Target score must be >= all others.
	targetScore := result.Items[0].Score
	for i, item := range result.Items[1:] {
		if item.Score > targetScore {
			t.Errorf("item[%d] %q (score %.2f) outscores target (%.2f)",
				i+1, item.Symbol.Qualified, item.Score, targetScore)
		}
	}
}
