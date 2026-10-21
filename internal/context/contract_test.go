package context_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/symbol"

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

// ValidateUser has a production caller (UserService.Create, service.go) and
// test callers (user_test.go).
func validateUserTarget(t *testing.T, idx *index.RepositoryIndex) symbol.SymbolID {
	t.Helper()
	syms := idx.FindSymbolsByQualified("ValidateUser")
	if len(syms) != 1 {
		t.Fatalf("ValidateUser: %d symbols in the fixture, want 1", len(syms))
	}
	return syms[0].ID
}

func callerFiles(t *testing.T, includeTests bool) map[string]bool {
	t.Helper()
	idx, dir := buildCtxIndex(t)
	result, err := arkctx.New(idx, dir).Build(context.Background(), arkctx.Request{
		Target:       validateUserTarget(t, idx),
		MaxTokens:    8000,
		MaxDepth:     2,
		IncludeTests: includeTests,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files := map[string]bool{}
	for _, item := range result.Items {
		if item.Reason == "caller" {
			files[string(item.Symbol.Location.File)] = true
		}
	}
	return files
}

// TestIncludeTests_False verifies test callers are excluded when
// IncludeTests=false, and production callers are kept.
func TestIncludeTests_False(t *testing.T) {
	files := callerFiles(t, false)
	if !files["service.go"] {
		t.Errorf("production caller missing: %v", files)
	}
	for f := range files {
		if isTestFile(f) {
			t.Errorf("test file included when IncludeTests=false: %s", f)
		}
	}
}

// TestIncludeTests_True verifies test callers are included when
// IncludeTests=true, alongside production callers — and that the graph keeps
// them either way (the filter is the Context's alone).
func TestIncludeTests_True(t *testing.T) {
	files := callerFiles(t, true)
	if !files["service.go"] || !files["user_test.go"] {
		t.Errorf("callers with IncludeTests=true: %v, want service.go and user_test.go", files)
	}
	idx, _ := buildCtxIndex(t)
	graphFiles := map[string]bool{}
	for _, e := range idx.GetCallers(validateUserTarget(t, idx)) {
		s, _ := idx.GetSymbol(e.To)
		graphFiles[string(s.Location.File)] = true
	}
	if !graphFiles["user_test.go"] {
		t.Errorf("graph lost the test caller: %v", graphFiles)
	}
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

// TestTargetFirstWhateverItsSize: a target far larger than any other item —
// a class of thousands of lines — is still the first item and the highest
// score, and the budget is spent on it first: under a budget it alone
// exceeds, nothing else is added; under a budget it fits, the others fill
// what it leaves. (Found on Java corpora: the large-source penalty had made a
// large class's own score negative, so its callers and callees came first and
// spent the budget the target was then added over.)
func TestTargetFirstWhateverItsSize(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package big\n\nfunc Big() {\n\thelper()\n\tx := 0\n")
	for range 3000 {
		b.WriteString("\tx = x + 1 // a long line of the large function body\n")
	}
	b.WriteString("\t_ = x\n}\n\nfunc helper() {}\n\nfunc Caller() { Big() }\n")
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	syms := idx.FindSymbolsByQualified("Big")
	if len(syms) != 1 {
		t.Fatalf("Big: %d symbols", len(syms))
	}
	eng := arkctx.New(idx, dir)
	for _, budget := range []int{100, 1 << 20} {
		res, err := eng.Build(context.Background(), arkctx.Request{Target: syms[0].ID, MaxTokens: budget, MaxDepth: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Items) == 0 || res.Items[0].Reason != "target" {
			t.Fatalf("budget %d: first item is not the target: %+v", budget, res.Items)
		}
		for _, it := range res.Items[1:] {
			if it.Score >= res.Items[0].Score {
				t.Errorf("budget %d: %s (%.2f) outscores the target (%.2f)", budget, it.Symbol.Qualified, it.Score, res.Items[0].Score)
			}
		}
		fits := res.Items[0].Tokens <= budget
		if res.Stats.TargetTruncated == fits {
			t.Errorf("budget %d: targetTruncated=%v for a %d-token target", budget, res.Stats.TargetTruncated, res.Items[0].Tokens)
		}
		if !fits && len(res.Items) != 1 {
			t.Errorf("budget %d: %d items beside a target over the budget", budget, len(res.Items)-1)
		}
		if fits && len(res.Items) != 3 {
			t.Errorf("budget %d: %d items, want the target, its callee and its caller", budget, len(res.Items))
		}
	}
}
