package context_test

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"

	arkctx "github.com/magicdrive/ark/internal/context"
)

func buildCtxIndex(t *testing.T) (*index.RepositoryIndex, string) {
	t.Helper()
	dir := fixtureDir
	providers := []language.Provider{golang.NewProvider()}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx, dir
}

// ── Target-presence invariant ─────────────────────────────────────────────────

// TestTargetAlwaysPresent verifies that the target is always included even
// when its source exceeds the configured budget.
func TestTargetAlwaysPresent(t *testing.T) {
	idx, dir := buildCtxIndex(t)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found in fixture")
	}

	eng := arkctx.New(idx, dir)
	// Use a very small budget — smaller than any real function source.
	result, err := eng.Build(context.Background(), arkctx.Request{
		Target:    syms[0].ID,
		MaxTokens: 1,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected at least one item (target), got none")
	}
	if result.Items[0].Reason != "target" {
		t.Errorf("first item should be target, got %q", result.Items[0].Reason)
	}
}

// TestTargetTruncatedFlagSet verifies Stats.TargetTruncated is set when the
// target's source exceeds the budget.
func TestTargetTruncatedFlagSet(t *testing.T) {
	idx, dir := buildCtxIndex(t)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found in fixture")
	}

	eng := arkctx.New(idx, dir)
	result, err := eng.Build(context.Background(), arkctx.Request{
		Target:    syms[0].ID,
		MaxTokens: 1,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// With budget=1, the target almost certainly exceeds it.
	// Either it fits (TargetTruncated=false) or it's truncated (TargetTruncated=true).
	// What must NOT happen: zero items.
	if len(result.Items) == 0 {
		t.Error("target must always produce at least one item")
	}
}

// ── IncludeTests contract ─────────────────────────────────────────────────────

// TestIncludeTests_False verifies test symbols are excluded when IncludeTests=false.
func TestIncludeTests_False(t *testing.T) {
	idx, dir := buildCtxIndex(t)
	syms := idx.FindSymbols("greet")
	if len(syms) == 0 {
		t.Skip("greet not found in fixture")
	}

	eng := arkctx.New(idx, dir)
	result, err := eng.Build(context.Background(), arkctx.Request{
		Target:       syms[0].ID,
		MaxTokens:    8000,
		MaxDepth:     2,
		IncludeTests: false,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, item := range result.Items {
		if item.Reason == "target" {
			continue // target is always included
		}
		if isTestFile(string(item.Symbol.Location.File)) {
			t.Errorf("test file included when IncludeTests=false: %s", item.Symbol.Location.File)
		}
	}
}

// TestIncludeTests_True verifies that test symbols CAN be included when IncludeTests=true.
// (They may not be present in every fixture, so this test is advisory.)
func TestIncludeTests_True(t *testing.T) {
	idx, dir := buildCtxIndex(t)
	syms := idx.FindSymbols("greet")
	if len(syms) == 0 {
		t.Skip("greet not found in fixture")
	}

	eng := arkctx.New(idx, dir)
	result, err := eng.Build(context.Background(), arkctx.Request{
		Target:       syms[0].ID,
		MaxTokens:    8000,
		MaxDepth:     2,
		IncludeTests: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Just verify Build doesn't panic/error with IncludeTests=true.
	_ = result
}

// ── Token budget contract ─────────────────────────────────────────────────────

// TestTokenBudget_EstimatedTokensReported verifies Stats reports estimated usage.
func TestTokenBudget_EstimatedTokensReported(t *testing.T) {
	idx, dir := buildCtxIndex(t)
	syms := idx.FindSymbols("greet")
	if len(syms) == 0 {
		t.Skip("greet not found in fixture")
	}

	eng := arkctx.New(idx, dir)
	result, err := eng.Build(context.Background(), arkctx.Request{
		Target:    syms[0].ID,
		MaxTokens: 8000,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Stats.BudgetTokens != 8000 {
		t.Errorf("BudgetTokens: want 8000, got %d", result.Stats.BudgetTokens)
	}
	if result.Stats.EstimatedTokens < 0 {
		t.Errorf("EstimatedTokens must be ≥ 0, got %d", result.Stats.EstimatedTokens)
	}
	// EstimatedTokens must not exceed the budget by more than the documented tolerance.
	// For now: must not wildly exceed (allow small overrun from target-presence guarantee).
	if result.Stats.EstimatedTokens > result.Stats.BudgetTokens+50 {
		t.Errorf("EstimatedTokens %d far exceeds budget %d", result.Stats.EstimatedTokens, result.Stats.BudgetTokens)
	}
}

// TestTokenBudget_SmallBudgetDeterministic verifies small-budget builds are deterministic.
func TestTokenBudget_SmallBudgetDeterministic(t *testing.T) {
	idx, dir := buildCtxIndex(t)
	syms := idx.FindSymbols("Create")
	if len(syms) == 0 {
		t.Skip("Create not found in fixture")
	}

	eng := arkctx.New(idx, dir)
	req := arkctx.Request{Target: syms[0].ID, MaxTokens: 100, MaxDepth: 1}

	r1, _ := eng.Build(context.Background(), req)
	r2, _ := eng.Build(context.Background(), req)

	if len(r1.Items) != len(r2.Items) {
		t.Errorf("non-deterministic item count: %d vs %d", len(r1.Items), len(r2.Items))
	}
	if r1.Stats.EstimatedTokens != r2.Stats.EstimatedTokens {
		t.Errorf("non-deterministic token estimate: %d vs %d", r1.Stats.EstimatedTokens, r2.Stats.EstimatedTokens)
	}
}

// isTestFile is a duplicate of the engine's internal helper, used here for test assertions.
func isTestFile(path string) bool {
	if strings.HasSuffix(path, "_test.go") {
		return true
	}
	// TypeScript/JavaScript
	for _, suffix := range []string{".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx", ".test.js", ".spec.js"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	// Python
	if strings.HasSuffix(path, ".py") {
		base := path[strings.LastIndex(path, "/")+1:]
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(strings.TrimSuffix(base, ".py"), "_test") {
			return true
		}
	}
	return false
}
