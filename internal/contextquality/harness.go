// Package contextquality is a small, language-neutral harness for measuring the
// quality of Context Engine output against agent-oriented scenarios. It drives
// the real Repository Index + Resolver + Context Engine (no mocking) and
// computes required-context recall, irrelevant-context ratio, token usage, and
// selection ordering.
//
// It is deliberately minimal: a Scenario describes a target and the expected
// required / optional / irrelevant symbols (by qualified name); Evaluate runs
// the pipeline and reports what was selected. The same harness is reusable for
// Java/C#/etc. — only the fixtures and provider list are language-specific.
package contextquality

import (
	"context"
	"sort"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"

	ctxengine "github.com/magicdrive/ark/internal/context"
)

// Scenario is an agent-oriented context expectation over a fixture repository.
type Scenario struct {
	Name            string
	Task            string
	TargetQualified string
	Depth           int
	MaxTokens       int
	IncludeTests    bool

	Required   []string // qualified names that SHOULD appear in context
	Optional   []string // useful-but-not-required (not scored for/against)
	Irrelevant []string // qualified names that should NOT crowd out required
}

// Selected is one item in the produced context, in selection order.
type Selected struct {
	Qualified string
	Reason    string
	Tokens    int
}

// Result is the measured quality of a scenario.
type Result struct {
	TargetFound     bool
	TargetIncluded  bool
	TargetTruncated bool

	Selected []Selected

	RequiredRecall     float64
	MissingRequired    []string
	IrrelevantIncluded []string
	IrrelevantRatio    float64

	EstimatedTokens int
	BudgetTokens    int
	SelectedCount   int
	TotalCandidates int
	Truncated       int
}

// Evaluate builds an index over root with the given providers, runs the Context
// Engine for the scenario's target, and measures quality.
func Evaluate(root string, providers []language.Provider, s Scenario) (Result, error) {
	idx, err := index.New(context.Background(), root, providers)
	if err != nil {
		return Result{}, err
	}
	targets := idx.FindSymbolsByQualified(s.TargetQualified)
	if len(targets) == 0 {
		return Result{}, nil // TargetFound stays false
	}

	eng := ctxengine.New(idx, root)
	res, err := eng.Build(context.Background(), ctxengine.Request{
		Target:       targets[0].ID,
		MaxTokens:    s.MaxTokens,
		MaxDepth:     s.Depth,
		IncludeTests: s.IncludeTests,
	})
	if err != nil {
		return Result{}, err
	}

	out := Result{
		TargetFound:     true,
		EstimatedTokens: res.Stats.EstimatedTokens,
		BudgetTokens:    res.Stats.BudgetTokens,
		SelectedCount:   res.Stats.SelectedItems,
		TotalCandidates: res.Stats.TotalCandidates,
		Truncated:       res.Stats.TruncatedItems,
		TargetTruncated: res.Stats.TargetTruncated,
	}

	selectedSet := make(map[string]bool, len(res.Items))
	for _, it := range res.Items {
		out.Selected = append(out.Selected, Selected{
			Qualified: it.Symbol.Qualified,
			Reason:    it.Reason,
			Tokens:    it.Tokens,
		})
		selectedSet[it.Symbol.Qualified] = true
		if it.Symbol.Qualified == s.TargetQualified {
			out.TargetIncluded = true
		}
	}

	// Required recall.
	hit := 0
	for _, req := range s.Required {
		if selectedSet[req] {
			hit++
		} else {
			out.MissingRequired = append(out.MissingRequired, req)
		}
	}
	if len(s.Required) > 0 {
		out.RequiredRecall = float64(hit) / float64(len(s.Required))
	} else {
		out.RequiredRecall = 1
	}

	// Irrelevant ratio.
	for _, irr := range s.Irrelevant {
		if selectedSet[irr] {
			out.IrrelevantIncluded = append(out.IrrelevantIncluded, irr)
		}
	}
	if out.SelectedCount > 0 {
		out.IrrelevantRatio = float64(len(out.IrrelevantIncluded)) / float64(out.SelectedCount)
	}

	sort.Strings(out.MissingRequired)
	sort.Strings(out.IrrelevantIncluded)
	return out, nil
}
