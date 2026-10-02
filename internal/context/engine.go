package context

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Engine builds context results from a RepositoryIndex.
type Engine struct {
	idx        *index.RepositoryIndex
	root       string
	ranker     Ranker
	readSource func(root string, fileID source.FileID, startLine, endLine uint32) (string, error)
}

// New creates an Engine with the default ranker.
func New(idx *index.RepositoryIndex, root string) *Engine {
	return NewWithRanker(idx, root, DefaultRanker{})
}

// NewWithRanker creates an Engine with a custom ranker.
func NewWithRanker(idx *index.RepositoryIndex, root string, ranker Ranker) *Engine {
	return &Engine{
		idx:        idx,
		root:       root,
		ranker:     ranker,
		readSource: readSourceLines,
	}
}

// Build collects, ranks, and selects context items within the token budget.
func (e *Engine) Build(ctx context.Context, req Request) (*Result, error) {
	if req.MaxTokens <= 0 {
		req.MaxTokens = 8000
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}

	target, ok := e.idx.GetSymbol(req.Target)
	if !ok {
		return &Result{Stats: Stats{BudgetTokens: req.MaxTokens}}, nil
	}

	// Collect candidates.
	candidates := e.collectCandidates(target, req)

	// Score all candidates (need source length for large-source penalty).
	type scoredCandidate struct {
		c      candidate
		score  float64
		bdMap  map[string]float64
		srcLen int
	}
	scored := make([]scoredCandidate, 0, len(candidates))
	for _, c := range candidates {
		// Quick length estimate using line range (no I/O yet).
		lines := int(c.sym.Location.Range.End.Line) - int(c.sym.Location.Range.Start.Line) + 1
		approxBytes := lines * 40 // rough average chars per line
		sc := e.ranker.Rank(c, approxBytes)
		scored = append(scored, scoredCandidate{c: c, score: sc.Total, bdMap: sc.Breakdown, srcLen: approxBytes})
	}

	// Sort: score desc, then SymbolID asc as tiebreak for determinism.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].c.sym.ID < scored[j].c.sym.ID
	})

	totalCandidates := len(scored)
	usedTokens := 0
	var items []Item
	truncated := 0

	// Track which source ranges are already covered to avoid duplication.
	type rangeKey struct {
		file  source.FileID
		start uint32
		end   uint32
	}
	covered := make(map[rangeKey]bool)

	for _, sc := range scored {
		rk := rangeKey{
			file:  sc.c.sym.Location.File,
			start: sc.c.sym.Location.Range.Start.Line,
			end:   sc.c.sym.Location.Range.End.Line,
		}
		if covered[rk] {
			continue
		}

		src, err := e.readSource(e.root, sc.c.sym.Location.File,
			sc.c.sym.Location.Range.Start.Line,
			sc.c.sym.Location.Range.End.Line)
		if err != nil {
			// Non-fatal: skip this item.
			continue
		}

		tokens := EstimateTokens(src)
		if usedTokens+tokens > req.MaxTokens {
			truncated++
			continue // keep trying smaller items below
		}

		covered[rk] = true
		usedTokens += tokens
		conf := sc.c.confidence
		if sc.c.reason == "target" {
			conf = resolver.ConfidenceExact
		}
		items = append(items, Item{
			Symbol:     sc.c.sym,
			Source:     src,
			Reason:     sc.c.reason,
			Score:      sc.score,
			Confidence: conf,
			Tokens:     tokens,
		})
	}

	return &Result{
		Items: items,
		Stats: Stats{
			TotalCandidates: totalCandidates,
			SelectedItems:   len(items),
			EstimatedTokens: usedTokens,
			BudgetTokens:    req.MaxTokens,
			TruncatedItems:  truncated,
		},
	}, nil
}

// collectCandidates gathers symbols related to target up to req.MaxDepth.
func (e *Engine) collectCandidates(target symbol.Symbol, req Request) []candidate {
	seen := make(map[symbol.SymbolID]bool)
	var result []candidate

	add := func(sym symbol.Symbol, reason string, conf resolver.Confidence, depth int) {
		if seen[sym.ID] {
			return
		}
		seen[sym.ID] = true
		result = append(result, candidate{
			sym:        sym,
			reason:     reason,
			confidence: conf,
			hopDepth:   depth,
		})
	}

	add(target, "target", resolver.ConfidenceExact, 0)

	// Direct callees.
	for _, edge := range e.idx.GetCallees(target.ID) {
		if sym, ok := e.idx.GetSymbol(edge.To); ok {
			add(sym, "direct callee", edge.Confidence, 1)
		}
	}

	// Types used by this symbol.
	for _, ref := range e.idx.ReferencesByContainer(target.ID) {
		if ref.Kind != reference.KindTypeUse {
			continue
		}
		syms := e.idx.FindSymbols(ref.Name)
		for _, sym := range syms {
			add(sym, "type dependency", resolver.ConfidenceStrong, 1)
		}
	}

	// Direct callers (lower priority).
	for _, edge := range e.idx.GetCallers(target.ID) {
		if sym, ok := e.idx.GetSymbol(edge.From); ok {
			add(sym, "caller", edge.Confidence, 1)
		}
	}

	// Depth > 1: expand callees of callees.
	if req.MaxDepth >= 2 {
		// snapshot current set to avoid modifying while iterating
		depth1 := make([]symbol.SymbolID, 0)
		for _, c := range result {
			if c.hopDepth == 1 && c.reason == "direct callee" {
				depth1 = append(depth1, c.sym.ID)
			}
		}
		for _, id := range depth1 {
			for _, edge := range e.idx.GetCallees(id) {
				if sym, ok := e.idx.GetSymbol(edge.To); ok {
					add(sym, "transitive callee", edge.Confidence, 2)
				}
			}
		}
	}

	return result
}

// readSourceLines reads lines [startLine, endLine] (1-based, inclusive) from a file.
func readSourceLines(root string, fileID source.FileID, startLine, endLine uint32) (string, error) {
	path := filepath.Join(root, string(fileID))
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	lineNum := uint32(1)
	for scanner.Scan() {
		if lineNum >= startLine && lineNum <= endLine {
			lines = append(lines, scanner.Text())
		}
		if lineNum > endLine {
			break
		}
		lineNum++
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}
