package context_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	ctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/symbol"
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

			for _, must := range sc.MustInclude {
				if !present[must] {
					t.Errorf("must_include %q not found in result (budget %d, got %d items)",
						must, sc.Budget, len(result.Items))
				}
			}

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

			if result.Stats.EstimatedTokens > sc.Budget {
				t.Errorf("tokens %d exceeded budget %d", result.Stats.EstimatedTokens, sc.Budget)
			}
		})
	}
}
