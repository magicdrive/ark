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
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/testfiles"
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
	unattributedCallers, unattributedCallees := e.idx.Unattributed(target.ID)

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
	targetTruncated := false

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
		isTarget := sc.c.reason == "target"
		if usedTokens+tokens > req.MaxTokens {
			if isTarget {
				// Target is always included even when it exceeds the budget.
				targetTruncated = true
			} else {
				truncated++
				continue // keep trying smaller items below
			}
		}

		covered[rk] = true
		usedTokens += tokens
		conf := sc.c.confidence
		if isTarget {
			conf = resolver.ConfidenceExact
		}
		items = append(items, Item{
			Symbol:         sc.c.sym,
			Source:         src,
			Reason:         sc.c.reason,
			Score:          sc.score,
			ScoreBreakdown: sc.bdMap,
			Confidence:     conf,
			Tokens:         tokens,
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
			TargetTruncated: targetTruncated,

			UnattributedCallers: unattributedCallers,
			UnattributedCallees: unattributedCallees,
		},
	}, nil
}

// reasonForEdge maps a graph EdgeKind to a context reason label. Typed relation
// edges keep their semantic meaning; all other outgoing edges (calls, type use,
// imports) remain the generic "direct callee". Language-neutral: it switches on
// graph semantics, not on any provider's syntax.
func reasonForEdge(k index.EdgeKind) string {
	switch k {
	case index.EdgeExtends:
		return "extends"
	case index.EdgeImplements:
		return "implements"
	case index.EdgeUsesTrait:
		return "uses_trait"
	default:
		return "direct callee"
	}
}

// collectCandidates gathers symbols related to target up to req.MaxDepth.
func (e *Engine) collectCandidates(target symbol.Symbol, req Request) []candidate {
	seen := make(map[symbol.SymbolID]bool)
	var result []candidate

	add := func(sym symbol.Symbol, reason string, conf resolver.Confidence, depth int) {
		if seen[sym.ID] {
			return
		}
		// Filter test files unless IncludeTests is set. The target is always added
		// regardless (reason == "target" check happens at the call site below).
		if !req.IncludeTests && reason != "target" && isTestFile(string(sym.Location.File)) {
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

	// Direct callees. Typed relation edges (extends/implements/uses_trait) carry
	// their semantic meaning into the reason label via the graph EdgeKind; this
	// is language-neutral (no provider-specific logic).
	for _, edge := range e.idx.GetCallees(target.ID) {
		if sym, ok := e.idx.GetSymbol(edge.To); ok {
			add(sym, reasonForEdge(edge.Kind), edge.Confidence, 1)
		}
	}

	// Type dependencies arrive through the graph: a resolved type reference is an
	// EdgeUsesType edge and is collected with the direct callees above. The
	// engine deliberately does not look type names up itself — a name-based
	// search here would be a second resolver that bypasses the resolver's
	// ambiguity, import and module-scope evidence (e.g. pulling in a repository
	// class that merely shares its name with an externally imported type).

	// Direct callers (lower priority). GetCallers returns the reverse edges
	// stored for target: From is the target itself and To is the caller (see
	// index.EdgeCalledBy), so the caller is edge.To. Only graph edges are used
	// — unique Strong/Exact resolutions; candidate callers are possible
	// callers, not context, and are reported through Stats instead.
	for _, edge := range e.idx.GetCallers(target.ID) {
		if sym, ok := e.idx.GetSymbol(edge.To); ok {
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

// isTestFile reports whether the given repo-relative path is a test file by
// its language's conventions (internal/testfiles).
func isTestFile(path string) bool { return testfiles.IsTestFile(path) }

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
