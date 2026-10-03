package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// targetLookup is the result of resolving a user-supplied MCP target symbol name
// to exactly one semantic symbol. It is the MCP-adapter-layer counterpart to the
// repository reference Resolver: it answers "which symbol did the agent mean?",
// never "what does this reference point to?".
//
// Invariant: Ark must never silently choose among multiple viable target
// symbols. When Candidates holds more than one symbol, Ambiguous is true and the
// caller must stop and return candidate evidence — never pick Candidates[0].
type targetLookup struct {
	Symbol     symbol.Symbol
	Candidates []symbol.Symbol
	Found      bool
	Ambiguous  bool
}

// targetCandidatesFromIndex gathers candidate target symbols for a user-supplied
// name using repository-index semantics: an exact qualified match first, then an
// exact short-name match. It is language-neutral — no punctuation heuristics —
// so PHP's "\" namespace separator and member "." work without special cases.
func targetCandidatesFromIndex(idx *index.RepositoryIndex, name string) []symbol.Symbol {
	if q := idx.FindSymbolsByQualified(name); len(q) > 0 {
		return q
	}
	var out []symbol.Symbol
	for _, s := range idx.FindSymbols(name) {
		if s.Name == name { // exact short name only; FindSymbols is a prefix match
			out = append(out, s)
		}
	}
	return out
}

// targetCandidatesFromFileIndexes applies the same qualified-first / short-name
// policy over resolver.FileIndex symbols (used by get_relations, which builds
// FileIndexes rather than a RepositoryIndex).
func targetCandidatesFromFileIndexes(files []resolver.FileIndex, name string) []symbol.Symbol {
	var qualified, short []symbol.Symbol
	for _, fi := range files {
		for _, s := range fi.Symbols {
			if s.Qualified == name {
				qualified = append(qualified, s)
			} else if s.Name == name {
				short = append(short, s)
			}
		}
	}
	if len(qualified) > 0 {
		return qualified
	}
	return short
}

// resolveTarget narrows candidates by filePattern (if any), de-duplicates and
// orders them deterministically, then classifies the result. It NEVER picks one
// among several viable targets.
func resolveTarget(cands []symbol.Symbol, filePattern string) targetLookup {
	if filePattern != "" {
		var f []symbol.Symbol
		for _, s := range cands {
			if strings.Contains(string(s.Location.File), filePattern) {
				f = append(f, s)
			}
		}
		cands = f
	}

	seen := make(map[symbol.SymbolID]bool, len(cands))
	uniq := make([]symbol.Symbol, 0, len(cands))
	for _, s := range cands {
		if !seen[s.ID] {
			seen[s.ID] = true
			uniq = append(uniq, s)
		}
	}
	sortTargetCandidates(uniq)

	switch len(uniq) {
	case 0:
		return targetLookup{}
	case 1:
		return targetLookup{Symbol: uniq[0], Found: true}
	default:
		return targetLookup{Candidates: uniq, Found: true, Ambiguous: true}
	}
}

// sortTargetCandidates orders candidates by a stable semantic key so ambiguity
// output is deterministic regardless of map/index iteration order.
func sortTargetCandidates(s []symbol.Symbol) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Qualified != s[j].Qualified {
			return s[i].Qualified < s[j].Qualified
		}
		if s[i].Location.File != s[j].Location.File {
			return s[i].Location.File < s[j].Location.File
		}
		if s[i].Kind != s[j].Kind {
			return s[i].Kind < s[j].Kind
		}
		return s[i].ID < s[j].ID
	})
}

// ambiguousTargetResult formats a deterministic, actionable ambiguity error.
// Candidates are evidence, not a ranking: no candidate is marked "likely".
func ambiguousTargetResult(name string, cands []symbol.Symbol) *CallToolResult {
	var b strings.Builder
	fmt.Fprintf(&b, "Ambiguous symbol %q — %d matches found. "+
		"Narrow with filePattern, or retry with one of the qualified names below:\n", name, len(cands))
	for _, s := range cands {
		fmt.Fprintf(&b, "  %s  (%s)  %s\n", s.Qualified, s.Kind, s.Location.File)
	}
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: b.String()}},
		IsError: true,
	}
}
