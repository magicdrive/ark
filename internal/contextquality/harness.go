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
	"fmt"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"

	ctxengine "github.com/magicdrive/ark/internal/context"
)

// Scenario is an agent-oriented context expectation over a fixture repository.
type Scenario struct {
	Name            string
	Task            string
	TargetQualified string
	// TargetFile pins the target to one file when its qualified name is
	// declared in several files. The harness never picks among several
	// same-named targets: an unpinned ambiguous target is an error.
	TargetFile string
	// TargetLine pins the target to the declaration starting on that line,
	// for overloads, which share their qualified name and their file.
	TargetLine   uint32
	Depth        int
	MaxTokens    int
	IncludeTests bool

	// Required / Irrelevant entries are qualified names, optionally pinned to a
	// file as "path/to/file.ext#Qualified" when the qualified name alone is not
	// unique in the fixture repository, and to the declaration's first line as
	// "Qualified@line" or "path/to/file.ext#Qualified@line" for overloads.
	Required   []string // qualified names that SHOULD appear in context
	Optional   []string // useful-but-not-required (not scored for/against)
	Irrelevant []string // qualified names that should NOT crowd out required
}

// Selected is one item in the produced context, in selection order.
type Selected struct {
	Qualified  string
	Reason     string
	Confidence string
	Tokens     int
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
	// UnknownEntries are the scenario's Required / Optional / Irrelevant
	// entries that denote no symbol of the repository: a check against them
	// can never fail, so a scenario should have none.
	UnknownEntries []string

	EstimatedTokens int
	BudgetTokens    int
	SelectedCount   int
	TotalCandidates int
	Truncated       int

	// UnattributedCallers / UnattributedCallees are the context's completeness
	// signal (context.Stats): target references that are not graph edges.
	UnattributedCallers int
	UnattributedCallees int
}

// Evaluate builds an index over root with the given providers, runs the Context
// Engine for the scenario's target, and measures quality.
func Evaluate(root string, providers []language.Provider, s Scenario) (Result, error) {
	idx, err := index.New(context.Background(), root, providers)
	if err != nil {
		return Result{}, err
	}
	var targets []symbol.Symbol
	for _, t := range idx.FindSymbolsByQualified(s.TargetQualified) {
		if (s.TargetFile == "" || string(t.Location.File) == s.TargetFile) &&
			(s.TargetLine == 0 || t.Location.Range.Start.Line == s.TargetLine) {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		return Result{}, nil // TargetFound stays false
	}
	if len(targets) > 1 {
		files := make([]string, len(targets))
		for i, t := range targets {
			files[i] = fmt.Sprintf("%s:%d", t.Location.File, t.Location.Range.Start.Line)
		}
		sort.Strings(files)
		return Result{}, fmt.Errorf("contextquality: target %q is ambiguous (%s); set Scenario.TargetFile or TargetLine",
			s.TargetQualified, strings.Join(files, ", "))
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

		UnattributedCallers: res.Stats.UnattributedCallers,
		UnattributedCallees: res.Stats.UnattributedCallees,
	}

	selectedSet := make(map[string]bool, len(res.Items))
	for _, it := range res.Items {
		out.Selected = append(out.Selected, Selected{
			Qualified:  it.Symbol.Qualified,
			Reason:     it.Reason,
			Confidence: it.Confidence.String(),
			Tokens:     it.Tokens,
		})
		pinned := string(it.Symbol.Location.File) + "#" + it.Symbol.Qualified
		line := fmt.Sprintf("@%d", it.Symbol.Location.Range.Start.Line)
		selectedSet[it.Symbol.Qualified] = true
		selectedSet[pinned] = true
		selectedSet[it.Symbol.Qualified+line] = true
		selectedSet[pinned+line] = true
		if it.Symbol.ID == targets[0].ID {
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

	for _, lists := range [][]string{s.Required, s.Optional, s.Irrelevant} {
		for _, e := range lists {
			if !denotesSymbol(idx, e) {
				out.UnknownEntries = append(out.UnknownEntries, e)
			}
		}
	}

	sort.Strings(out.MissingRequired)
	sort.Strings(out.IrrelevantIncluded)
	sort.Strings(out.UnknownEntries)
	return out, nil
}

// denotesSymbol reports whether a scenario entry — "Qualified", optionally
// pinned as "file#Qualified" and suffixed "@line" — names an indexed symbol.
func denotesSymbol(idx *index.RepositoryIndex, entry string) bool {
	file, q := "", entry
	if i := strings.Index(q, "#"); i >= 0 {
		file, q = q[:i], q[i+1:]
	}
	line := ""
	if i := strings.LastIndex(q, "@"); i >= 0 {
		q, line = q[:i], q[i+1:]
	}
	for _, sym := range idx.FindSymbolsByQualified(q) {
		if (file == "" || string(sym.Location.File) == file) &&
			(line == "" || fmt.Sprint(sym.Location.Range.Start.Line) == line) {
			return true
		}
	}
	return false
}
