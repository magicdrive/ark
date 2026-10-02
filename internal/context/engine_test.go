package context_test

import (
	"context"
	"testing"

	ctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

const fixtureDir = "testdata/fixtures/user_service"

func buildIndex(t *testing.T) *index.RepositoryIndex {
	t.Helper()
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), fixtureDir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

func TestEngine_TargetAlwaysFirst(t *testing.T) {
	idx := buildIndex(t)
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
	if result.Items[0].Reason != "target" {
		t.Errorf("first item reason = %q, want %q", result.Items[0].Reason, "target")
	}
}

func TestEngine_BudgetRespected(t *testing.T) {
	idx := buildIndex(t)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found")
	}

	eng := ctx.New(idx, fixtureDir)
	budget := 50
	result, err := eng.Build(context.Background(), ctx.Request{
		Target:    syms[0].ID,
		MaxTokens: budget,
		MaxDepth:  2,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Stats.EstimatedTokens > budget {
		t.Errorf("tokens %d exceeded budget %d", result.Stats.EstimatedTokens, budget)
	}
}

func TestEngine_Deterministic(t *testing.T) {
	idx := buildIndex(t)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found")
	}

	eng := ctx.New(idx, fixtureDir)
	req := ctx.Request{Target: syms[0].ID, MaxTokens: 8000, MaxDepth: 2}

	r1, err1 := eng.Build(context.Background(), req)
	r2, err2 := eng.Build(context.Background(), req)
	if err1 != nil || err2 != nil {
		t.Fatalf("Build errors: %v / %v", err1, err2)
	}

	if len(r1.Items) != len(r2.Items) {
		t.Fatalf("item count differs: %d vs %d", len(r1.Items), len(r2.Items))
	}
	for i := range r1.Items {
		if r1.Items[i].Symbol.ID != r2.Items[i].Symbol.ID {
			t.Errorf("item[%d] ID differs: %v vs %v", i, r1.Items[i].Symbol.ID, r2.Items[i].Symbol.ID)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		text string
		min  int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world foo bar", 4},
	}
	for _, tc := range cases {
		got := ctx.EstimateTokens(tc.text)
		if got < tc.min {
			t.Errorf("EstimateTokens(%q) = %d, want >= %d", tc.text, got, tc.min)
		}
	}
}

func TestFormat_NonEmpty(t *testing.T) {
	idx := buildIndex(t)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found")
	}

	eng := ctx.New(idx, fixtureDir)
	result, err := eng.Build(context.Background(), ctx.Request{
		Target:    syms[0].ID,
		MaxTokens: 8000,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	text := ctx.Format(result)
	if text == "" {
		t.Error("Format returned empty string")
	}
}
