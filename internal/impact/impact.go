package impact

import (
	"context"
	"sort"

	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/testfiles"
)

// Category classifies an impacted symbol's relationship to the target.
type Category string

const (
	// CategoryTarget is the symbol being changed.
	CategoryTarget Category = "target"
	// CategoryDirectDependent is a symbol that directly calls the target.
	CategoryDirectDependent Category = "direct_dependent"
	// CategoryDirectDependency is a symbol that the target directly calls.
	CategoryDirectDependency Category = "direct_dependency"
	// CategoryTest is a test symbol that references the target or a direct dependent.
	CategoryTest Category = "test"
	// CategoryTransitiveDependent is a strong/exact transitive caller (beyond depth 1).
	CategoryTransitiveDependent Category = "transitive_dependent"
	// CategoryPossibleDependent is a candidate-confidence transitive caller.
	// It may or may not actually depend on the target.
	CategoryPossibleDependent Category = "possible_dependent"
)

// ImpactEntry is one symbol in the impact result with its category and evidence.
//
// For a direct entry (Distance 1) Confidence and Evidence are those of the one
// edge between the symbol and the target. For a transitive entry (Distance > 1)
// Confidence is that of the chosen path to the target — its weakest edge
// (graph.Hop) — while Evidence is only that of the path's first hop from the
// symbol: why the symbol refers to the next symbol on the path, not why it
// reaches the target. It is never a concatenation of the path's evidence.
type ImpactEntry struct {
	Symbol     symbol.Symbol
	Category   Category
	Confidence resolver.Confidence
	Evidence   []resolver.ResolutionEvidence
	Distance   int // hop count (1 = direct)
}

// ImpactResult is the full output of Analyze.
type ImpactResult struct {
	Target        symbol.Symbol
	Entries       []ImpactEntry   // sorted: category priority then SymbolID
	AffectedFiles []source.FileID // deduplicated, sorted; definite impacts only
	// UnresolvedCallees describes the target's own outgoing references that
	// resolve to no candidate (index.RepositoryIndex.UnresolvedOutgoing): its
	// dependencies the report cannot name.
	UnresolvedCallees index.UnresolvedSample
	// Unattributed is the number of references that may target the symbol but
	// are not resolved edges (index.RepositoryIndex.Unattributed, incoming).
	// 0 means the dependent list is complete as far as the index can tell.
	Unattributed int
	Diagnostics  []language.Diagnostic
}

// Analyze returns the likely impact of changing targetID.
//
// Categories are returned with explicit confidence levels.
// Heuristic (candidate-confidence) results are labelled CategoryPossibleDependent,
// not CategoryTransitiveDependent — callers must not treat them as guaranteed impacts.
func Analyze(
	ctx context.Context,
	idx *index.RepositoryIndex,
	g *graph.Graph,
	targetID symbol.SymbolID,
	maxDepth int,
) (*ImpactResult, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	target, ok := idx.GetSymbol(targetID)
	if !ok {
		return nil, nil
	}

	result := &ImpactResult{
		Target:      target,
		Diagnostics: idx.Diagnostics(),
	}

	seen := make(map[symbol.SymbolID]bool)
	seen[targetID] = true

	// Direct callees (depth 1).
	for _, edge := range idx.GetCallees(targetID) {
		sym, ok := idx.GetSymbol(edge.To)
		if !ok {
			continue
		}
		if seen[edge.To] {
			continue
		}
		// Skip testdata symbols — they are fixture artifacts, not real production dependencies.
		if isTestFile(string(sym.Location.File)) {
			continue
		}
		seen[edge.To] = true
		result.Entries = append(result.Entries, ImpactEntry{
			Symbol:     sym,
			Category:   CategoryDirectDependency,
			Confidence: edge.Confidence,
			Evidence:   edge.Evidence,
			Distance:   1,
		})
	}

	// Direct callers (depth 1), split into test vs non-test.
	// GetCallers returns EdgeCalledBy edges: From=target, To=caller.
	for _, edge := range idx.GetCallers(targetID) {
		symID := edge.To
		sym, ok := idx.GetSymbol(symID)
		if !ok {
			continue
		}
		if seen[symID] {
			continue
		}
		seen[symID] = true

		cat := CategoryDirectDependent
		if isTestFile(string(sym.Location.File)) {
			cat = CategoryTest
		}
		result.Entries = append(result.Entries, ImpactEntry{
			Symbol:     sym,
			Category:   cat,
			Confidence: edge.Confidence,
			Evidence:   edge.Evidence,
			Distance:   1,
		})
	}

	// Possible direct callers: symbols whose reference to the target is
	// ambiguous (Candidate). They are never definite impacts.
	for _, symID := range idx.CandidateCallers(targetID) {
		if seen[symID] {
			continue
		}
		sym, ok := idx.GetSymbol(symID)
		if !ok {
			continue
		}
		seen[symID] = true
		result.Entries = append(result.Entries, ImpactEntry{
			Symbol:     sym,
			Category:   CategoryPossibleDependent,
			Confidence: resolver.ConfidenceCandidate,
			Distance:   1,
		})
	}
	result.Unattributed, _ = idx.Unattributed(targetID)

	// Transitive callers (depth > 1).
	// TransitiveCallerHops returns EdgeCalledBy edges (From=callee,
	// To=caller), each caller once, with its hop count and the confidence of
	// the path that reaches it.
	if maxDepth > 1 {
		for _, hop := range g.TransitiveCallerHops(targetID, maxDepth) {
			edge := hop.Edge
			symID := edge.To
			if seen[symID] {
				continue
			}
			sym, ok := idx.GetSymbol(symID)
			if !ok {
				continue
			}
			seen[symID] = true

			cat := CategoryTransitiveDependent
			if hop.Confidence == resolver.ConfidenceCandidate ||
				hop.Confidence == resolver.ConfidenceUnresolved {
				cat = CategoryPossibleDependent
			}
			if isTestFile(string(sym.Location.File)) {
				cat = CategoryTest
			}
			result.Entries = append(result.Entries, ImpactEntry{
				Symbol:     sym,
				Category:   cat,
				Confidence: hop.Confidence,
				Evidence:   edge.Evidence,
				Distance:   hop.Depth,
			})
		}
	}

	result.UnresolvedCallees = idx.UnresolvedOutgoing(targetID)

	sortEntries(result.Entries)
	result.AffectedFiles = affectedFiles(target, result.Entries)

	return result, nil
}

// isTestFile reports whether path is a test file or test fixture data; both
// are classified by internal/testfiles alone.
func isTestFile(path string) bool {
	return testfiles.IsTestFile(path) || testfiles.IsTestData(path)
}

// categoryOrder defines display/sort priority.
var categoryOrder = map[Category]int{
	CategoryTarget:              0,
	CategoryDirectDependent:     1,
	CategoryDirectDependency:    2,
	CategoryTest:                3,
	CategoryTransitiveDependent: 4,
	CategoryPossibleDependent:   5,
}

func sortEntries(entries []ImpactEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		oi := categoryOrder[entries[i].Category]
		oj := categoryOrder[entries[j].Category]
		if oi != oj {
			return oi < oj
		}
		if entries[i].Distance != entries[j].Distance {
			return entries[i].Distance < entries[j].Distance
		}
		return entries[i].Symbol.ID < entries[j].Symbol.ID
	})
}

func affectedFiles(target symbol.Symbol, entries []ImpactEntry) []source.FileID {
	seen := map[source.FileID]bool{target.Location.File: true}
	for _, e := range entries {
		if e.Category == CategoryPossibleDependent {
			continue // possible, not affected
		}
		seen[e.Symbol.Location.File] = true
	}
	out := make([]source.FileID, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
