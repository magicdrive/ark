package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/symbol"
)

// Caller context: the Context Engine follows the graph's caller edges in the
// right direction, keeps callers to their own symbol's source, and spends no
// more than the budget on them.

func buildRepo(t *testing.T, files map[string]string) (*index.RepositoryIndex, string) {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	return idx, root
}

func symbolByQualified(t *testing.T, idx *index.RepositoryIndex, q string) symbol.Symbol {
	t.Helper()
	syms := idx.FindSymbolsByQualified(q)
	if len(syms) != 1 {
		t.Fatalf("want exactly one %q, got %d", q, len(syms))
	}
	return syms[0]
}

func build(t *testing.T, idx *index.RepositoryIndex, root string, target symbol.Symbol, maxTokens int) *Result {
	t.Helper()
	res, err := New(idx, root).Build(context.Background(), Request{Target: target.ID, MaxTokens: maxTokens, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func itemNames(res *Result) []string {
	out := make([]string, len(res.Items))
	for i, it := range res.Items {
		out[i] = it.Symbol.Qualified + "(" + it.Reason + ")"
	}
	return out
}

func hasItem(res *Result, qualified, reason string) bool {
	for _, it := range res.Items {
		if it.Symbol.Qualified == qualified && it.Reason == reason {
			return true
		}
	}
	return false
}

// The graph contract the engine relies on: GetCallers(target) edges run From
// the target To the caller; GetCallees(source) edges run From the source To the
// callee. The caller is therefore edge.To on the caller side.
func TestCallers_GraphDirectionContract(t *testing.T) {
	idx, root := buildRepo(t, map[string]string{
		"p/p.go": "package p\n\nfunc Target() {}\n\nfunc Caller() { Target() }\n",
	})
	target := symbolByQualified(t, idx, "Target")
	caller := symbolByQualified(t, idx, "Caller")

	callers := idx.GetCallers(target.ID)
	if len(callers) != 1 || callers[0].From != target.ID || callers[0].To != caller.ID || callers[0].Kind != index.EdgeCalledBy {
		t.Fatalf("GetCallers(Target) = %+v, want one called_by edge From=Target To=Caller", callers)
	}
	callees := idx.GetCallees(caller.ID)
	if len(callees) != 1 || callees[0].From != caller.ID || callees[0].To != target.ID {
		t.Fatalf("GetCallees(Caller) = %+v, want one edge From=Caller To=Target", callees)
	}

	res := build(t, idx, root, target, 4000)
	if !hasItem(res, "Caller", "caller") {
		t.Fatalf("context of Target lacks its caller: %v", itemNames(res))
	}
	res = build(t, idx, root, caller, 4000)
	if !hasItem(res, "Target", "direct callee") || hasItem(res, "Target", "caller") {
		t.Fatalf("context of Caller: want Target as direct callee only: %v", itemNames(res))
	}
}

// Direct callers across languages: a resolved caller is context.
func TestCallers_CrossLanguage(t *testing.T) {
	cases := []struct {
		name           string
		files          map[string]string
		target, caller string
	}{
		{"go", map[string]string{"p/p.go": "package p\n\ntype Repo struct{}\n\nfunc (Repo) Save() {}\n\nfunc Use() { var r Repo; r.Save() }\n"},
			"Repo.Save", "Use"},
		{"typescript", map[string]string{"src/a.ts": "export class Repo {\n  save() { return 1; }\n}\nexport function use(r: Repo) {\n  return r.save();\n}\n"},
			"Repo.save", "use"},
		{"php", map[string]string{"app/Repo.php": "<?php\nnamespace App;\n\nclass Repo\n{\n    public function save() { return 1; }\n}\n",
			"app/Use.php": "<?php\nnamespace App;\n\nclass UseIt\n{\n    public function run(Repo $r) { return $r->save(); }\n}\n"},
			`App\Repo.save`, `App\UseIt.run`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, root := buildRepo(t, tc.files)
			res := build(t, idx, root, symbolByQualified(t, idx, tc.target), 4000)
			if !hasItem(res, tc.caller, "caller") {
				t.Errorf("context of %s lacks caller %s: %v", tc.target, tc.caller, itemNames(res))
			}
			if res.Items[0].Reason != "target" {
				t.Errorf("target not first: %v", itemNames(res))
			}
		})
	}
}

// A caller that is a method of a large class contributes the method, never the
// class around it.
func TestCallers_SourceGranularity(t *testing.T) {
	var big strings.Builder
	big.WriteString("<?php\nnamespace App;\n\nclass Big\n{\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&big, "    public function filler%d() { return %d; }\n", i, i)
	}
	big.WriteString("    public function uses(Repo $r) { return $r->save(); }\n}\n")
	idx, root := buildRepo(t, map[string]string{
		"app/Repo.php": "<?php\nnamespace App;\n\nclass Repo\n{\n    public function save() { return 1; }\n}\n",
		"app/Big.php":  big.String(),
	})
	res := build(t, idx, root, symbolByQualified(t, idx, `App\Repo.save`), 8000)
	var caller *Item
	for i := range res.Items {
		if res.Items[i].Reason == "caller" {
			caller = &res.Items[i]
		}
		if res.Items[i].Symbol.Qualified == `App\Big` {
			t.Errorf("the caller's whole class entered the context (%d tokens)", res.Items[i].Tokens)
		}
	}
	if caller == nil || caller.Symbol.Qualified != `App\Big.uses` {
		t.Fatalf("want caller App\\Big.uses, got %v", itemNames(res))
	}
	if caller.Tokens > 40 || strings.Contains(caller.Source, "filler") {
		t.Errorf("caller source is not just the method (%d tokens):\n%s", caller.Tokens, caller.Source)
	}
}

func manyCallersRepo(t *testing.T, n int, hugeLines int) (*index.RepositoryIndex, string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("package p\n\nfunc Target() {}\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "\nfunc Caller%02d() { Target() }\n", i)
	}
	if hugeLines > 0 {
		b.WriteString("\nfunc Huge() {\n\tTarget()\n")
		for i := 0; i < hugeLines; i++ {
			fmt.Fprintf(&b, "\t_ = %d // padding padding padding padding\n", i)
		}
		b.WriteString("}\n")
	}
	return buildRepo(t, map[string]string{"p/p.go": b.String()})
}

// Callers obey the budget: a large budget takes all of them, a small one keeps
// a deterministic prefix of the ranking, a huge caller never displaces the
// target, and the target is always first.
func TestCallers_TokenBudget(t *testing.T) {
	idx, root := manyCallersRepo(t, 30, 600)
	target := symbolByQualified(t, idx, "Target")

	full := build(t, idx, root, target, 1_000_000)
	callers := 0
	for _, it := range full.Items {
		if it.Reason == "caller" {
			callers++
		}
	}
	if callers != 31 {
		t.Fatalf("large budget: want all 31 callers (Huge included), got %d: %v", callers, itemNames(full))
	}

	rank := map[string]int{}
	for i, it := range full.Items {
		rank[it.Symbol.Qualified] = i
	}
	for _, budget := range []int{200, 60, 5} {
		res := build(t, idx, root, target, budget)
		if res.Items[0].Symbol.Qualified != "Target" {
			t.Fatalf("budget %d: target not first: %v", budget, itemNames(res))
		}
		if !res.Stats.TargetTruncated && res.Stats.EstimatedTokens > budget {
			t.Errorf("budget %d exceeded: %d tokens", budget, res.Stats.EstimatedTokens)
		}
		if hasItem(res, "Huge", "caller") {
			t.Errorf("budget %d: the huge caller was included", budget)
		}
		if len(res.Items) >= len(full.Items) || res.Stats.TruncatedItems == 0 {
			t.Errorf("budget %d: every caller included unconditionally (%d items)", budget, len(res.Items))
		}
		last := -1
		for _, it := range res.Items {
			if r := rank[it.Symbol.Qualified]; r < last {
				t.Errorf("budget %d: %s out of ranking order", budget, it.Symbol.Qualified)
			} else {
				last = r
			}
		}
		again := build(t, idx, root, target, budget)
		if fmt.Sprint(itemNames(again)) != fmt.Sprint(itemNames(res)) || Format(again) != Format(res) {
			t.Errorf("budget %d: non-deterministic selection", budget)
		}
	}
}

// Cycles and recursion neither loop nor duplicate: every symbol is one
// candidate and one item.
func TestCallers_CycleAndDedup(t *testing.T) {
	idx, root := buildRepo(t, map[string]string{
		"p/p.go": "package p\n\nfunc A() { B(); A() }\n\nfunc B() { A() }\n",
	})
	a := symbolByQualified(t, idx, "A")
	first := build(t, idx, root, a, 4000)
	if first.Stats.TotalCandidates != 2 || len(first.Items) != 2 {
		t.Fatalf("want 2 candidates/items (A, B), got %d/%d: %v", first.Stats.TotalCandidates, len(first.Items), itemNames(first))
	}
	for i := 0; i < 3; i++ {
		if again := build(t, idx, root, a, 4000); Format(again) != Format(first) {
			t.Fatalf("run %d differs:\n%s\n---\n%s", i, Format(first), Format(again))
		}
	}
}

// flatRanker scores every non-target candidate equally, so selection order is
// decided by the tie-break alone.
type flatRanker struct{}

func (flatRanker) Rank(c candidate, _ int) Score {
	if c.reason == "target" {
		return Score{Total: 100}
	}
	return Score{Total: 1}
}

// Equal scores are ordered by SymbolID, not by the order candidates were
// collected in (callees before callers).
func TestCallers_TieBreakBySymbolID(t *testing.T) {
	idx, root := buildRepo(t, map[string]string{
		"p/p.go": "package p\n\nfunc Target() { Callee1(); Callee2(); Callee3() }\n\nfunc Callee1() {}\n\nfunc Callee2() {}\n\nfunc Callee3() {}\n\n" +
			"func CallerA() { Target() }\n\nfunc CallerB() { Target() }\n\nfunc CallerC() { Target() }\n",
	})
	target := symbolByQualified(t, idx, "Target")
	res, err := NewWithRanker(idx, root, flatRanker{}).Build(context.Background(), Request{Target: target.ID, MaxTokens: 4000, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	collected := []string{}
	for _, it := range res.Items[1:] {
		ids = append(ids, string(it.Symbol.ID))
		collected = append(collected, it.Reason)
	}
	if len(ids) != 6 {
		t.Fatalf("want 6 non-target items, got %v", itemNames(res))
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("equal-score items not in SymbolID order: %v", itemNames(res))
	}
	// Precondition for the test to mean something: collection order differs
	// from SymbolID order.
	if fmt.Sprint(collected) == "[direct callee direct callee direct callee caller caller caller]" {
		t.Skip("fixture IDs happen to sort callees first; tie-break not exercised")
	}
}
