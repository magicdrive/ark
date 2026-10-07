package search

import (
	"context"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/testfiles"
)

// MatchKind identifies whether a Match wraps a Symbol or a Reference.
type MatchKind string

const (
	MatchSymbol    MatchKind = "symbol"
	MatchReference MatchKind = "reference"
)

// Match is a single search result.
type Match struct {
	Kind      MatchKind
	Symbol    *symbol.Symbol
	Reference *reference.Reference
	File      source.FileID
	Line      uint32 // 1-based start line for sorting
}

// Result holds all matches for a query.
type Result struct {
	Matches    []Match
	TotalCount int  // total before truncation
	Truncated  bool // true when TotalCount > maxResults
}

// Search executes q against idx and returns at most maxResults matches.
// Results are ordered deterministically by (FileID, Line, symbol name).
func Search(ctx context.Context, idx *index.RepositoryIndex, q Query, maxResults int) (*Result, error) {
	if maxResults <= 0 {
		maxResults = 50
	}

	var matches []Match

	// Collect all symbols, then filter.
	var candidates []symbol.Symbol
	if q.Kind != "" {
		candidates = idx.SymbolsByKind(q.Kind)
	} else {
		// All symbols: iterate over all files.
		for _, fid := range idx.Files() {
			candidates = append(candidates, idx.SymbolsByFile(fid)...)
		}
	}

	for i := range candidates {
		sym := candidates[i]

		if !matchesSymbol(sym, q) {
			continue
		}
		if !matchesFile(string(sym.Location.File), sym.Language, q, idx) {
			continue
		}
		if !matchesReferenceFilters(sym, q, idx) {
			continue
		}

		matches = append(matches, Match{
			Kind:   MatchSymbol,
			Symbol: &candidates[i],
			File:   sym.Location.File,
			Line:   sym.Location.Range.Start.Line,
		})
	}

	// Deterministic sort: file → line → qualified name.
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.File != b.File {
			return string(a.File) < string(b.File)
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		nameA := symbolName(a)
		nameB := symbolName(b)
		return nameA < nameB
	})

	total := len(matches)
	truncated := false
	if total > maxResults {
		matches = matches[:maxResults]
		truncated = true
	}

	return &Result{
		Matches:    matches,
		TotalCount: total,
		Truncated:  truncated,
	}, nil
}

func symbolName(m Match) string {
	if m.Symbol != nil {
		return m.Symbol.Qualified
	}
	return ""
}

// matchesSymbol applies symbol-level predicates.
func matchesSymbol(sym symbol.Symbol, q Query) bool {
	if q.NamePattern != "" {
		if !strings.Contains(strings.ToLower(sym.Name), strings.ToLower(q.NamePattern)) {
			return false
		}
	}
	if q.Exported != nil && sym.Exported != *q.Exported {
		return false
	}
	return true
}

// matchesFile applies file-level predicates.
// isGenerated is approximated by checking for "generated" in file path.
func matchesFile(fileID, lang string, q Query, _ *index.RepositoryIndex) bool {
	lower := strings.ToLower(fileID)

	if q.Language != "" && !strings.EqualFold(lang, q.Language) {
		return false
	}
	if q.FilePattern != "" && !strings.Contains(lower, strings.ToLower(q.FilePattern)) {
		return false
	}
	if q.ExcludeTest && (testfiles.IsTestFile(fileID) || testfiles.IsTestData(fileID)) {
		return false
	}
	if q.ExcludeGenerated {
		if strings.Contains(lower, "generated") ||
			strings.Contains(lower, "vendor/") ||
			strings.Contains(lower, "/vendor/") {
			return false
		}
	}
	return true
}

// matchesReferenceFilters checks CallsName and UsesType against the symbol's references.
func matchesReferenceFilters(sym symbol.Symbol, q Query, idx *index.RepositoryIndex) bool {
	if q.CallsName == "" && q.UsesType == "" {
		return true
	}

	refs := idx.ReferencesByContainer(sym.ID)

	if q.CallsName != "" {
		found := false
		for _, r := range refs {
			if r.Kind == reference.KindCall &&
				strings.Contains(strings.ToLower(r.Name), strings.ToLower(q.CallsName)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if q.UsesType != "" {
		found := false
		for _, r := range refs {
			if r.Kind == reference.KindTypeUse &&
				strings.Contains(strings.ToLower(r.Name), strings.ToLower(q.UsesType)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
