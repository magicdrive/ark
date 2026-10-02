// Package context builds token-budgeted, relevance-ranked source context
// from a RepositoryIndex for use by LLMs and agents.
package context

import (
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Request specifies what context to build and how much to spend.
type Request struct {
	Target       symbol.SymbolID
	MaxTokens    int  // hard token budget
	MaxDepth     int  // graph traversal depth (default 2)
	IncludeTests bool
}

// Item is one piece of context: a symbol and its source text.
type Item struct {
	Symbol     symbol.Symbol
	Source     string // source text for the symbol's range
	Reason     string // why this was included ("target", "direct callee", …)
	Score      float64
	Confidence resolver.Confidence
	Tokens     int // estimated tokens for Source
}

// Stats describes budget usage for a context build.
type Stats struct {
	TotalCandidates int
	SelectedItems   int
	EstimatedTokens int
	BudgetTokens    int
	TruncatedItems  int // candidates dropped due to budget
}

// Result holds the items selected within budget plus metadata.
type Result struct {
	Items       []Item
	Diagnostics []language.Diagnostic
	Stats       Stats
}

// candidate is an internal working type before source text is loaded.
type candidate struct {
	sym        symbol.Symbol
	reason     string
	score      float64
	confidence resolver.Confidence
	hopDepth   int
}
