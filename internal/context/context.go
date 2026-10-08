// Package context builds token-budgeted, relevance-ranked source context
// from a RepositoryIndex for use by LLMs and agents.
package context

import (
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Request specifies what context to build and how much to spend.
type Request struct {
	Target       symbol.SymbolID
	MaxTokens    int // estimated token budget (len(text)/4 approximation); target is always included
	MaxDepth     int // graph traversal depth (default 2); callers are collected one hop deep, callees at most two (collectCandidates)
	IncludeTests bool
}

// Item is one piece of context: a symbol and its source text.
type Item struct {
	Symbol         symbol.Symbol
	Source         string // source text for the symbol's range
	Reason         string // why this was included ("target", "direct callee", …)
	Score          float64
	ScoreBreakdown map[string]float64 // per-factor breakdown from the ranker
	Confidence     resolver.Confidence
	Tokens         int // estimated tokens for Source
}

// Stats describes budget usage for a context build.
type Stats struct {
	TotalCandidates int
	SelectedItems   int
	EstimatedTokens int
	BudgetTokens    int
	TruncatedItems  int  // candidates dropped due to budget
	TargetTruncated bool // true when the target's source exceeded the budget

	// UnattributedCallers / UnattributedCallees are the target's incoming and
	// outgoing references that are not graph edges (index.RepositoryIndex.
	// Unattributed): callers or callees the context may be missing because
	// they could not be resolved. 0 means the graph is complete for that
	// direction; it does not mean every caller fit in the budget.
	UnattributedCallers int
	UnattributedCallees int

	// UnresolvedCallees / OutsideCallees are the target's other outgoing
	// references that are no edge (index.RepositoryIndex.UnresolvedOutgoing):
	// ones no repository symbol can be the target of, and ones proven to refer
	// outside the repository. They cannot be missing context items, but with
	// UnattributedCallees they say whether every observed callee is shown.
	UnresolvedCallees int
	OutsideCallees    int
}

// Result holds the items selected within budget plus metadata.
type Result struct {
	Items       []Item
	Diagnostics []language.Diagnostic
	Stats       Stats
	// IndexDiagnostics summarizes the index's diagnostics when it has any
	// (nil otherwise): code lost to them is in no count of Stats.
	IndexDiagnostics *index.DiagnosticSummary
}

// candidate is an internal working type before source text is loaded.
type candidate struct {
	sym        symbol.Symbol
	reason     string
	confidence resolver.Confidence
	hopDepth   int
}
