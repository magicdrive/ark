package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
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
// name using repository-index semantics (see lookupTargetCandidates).
func targetCandidatesFromIndex(idx *index.RepositoryIndex, name string) []symbol.Symbol {
	return lookupTargetCandidates(name, idx.FindSymbolsByQualified,
		func(n string) []symbol.Symbol {
			var out []symbol.Symbol
			for _, s := range idx.FindSymbols(n) {
				if s.Name == n { // exact short name only; FindSymbols is a prefix match
					out = append(out, s)
				}
			}
			return out
		},
		func(yield func(symbol.Symbol)) {
			for _, f := range idx.Files() {
				for _, s := range idx.SymbolsByFile(f) {
					yield(s)
				}
			}
		})
}

// lookupTargetCandidates resolves a user-supplied target name to candidate
// symbols. It is language-neutral: it compares the input with Symbol.Qualified
// and Symbol.Name only, and treats any non-identifier character as an identity
// segment boundary, so namespace ("\"), member (".") and module separators all
// work without a per-language rule. Stages, first non-empty wins:
//
//  1. exact Qualified, then exact Name, of the input as written (the
//     pre-existing behaviour, unchanged);
//  2. exact Qualified, then exact Name, of the input's alternative spellings: "::" written for
//     the "." member separator, and the input without leading separators
//     (e.g. a fully-qualified "\App\X");
//  3. boundary suffix: symbols whose Qualified ends with one of those
//     spellings at a segment boundary — "LoginScreenPolicy.showsSsoButton"
//     matches "App\Services\Auth\LoginScreenPolicy.showsSsoButton" but
//     "Service.run" never matches "FooService.run".
//
// Several matches are returned as they are: the caller reports ambiguity and
// never picks one.
func lookupTargetCandidates(
	name string,
	byQualified func(string) []symbol.Symbol,
	byName func(string) []symbol.Symbol,
	all func(yield func(symbol.Symbol)),
) []symbol.Symbol {
	if q := byQualified(name); len(q) > 0 {
		return q
	}
	if n := byName(name); len(n) > 0 {
		return n
	}
	spellings := targetSpellings(name)
	for _, sp := range spellings[1:] {
		if q := byQualified(sp); len(q) > 0 {
			return q
		}
		if n := byName(sp); len(n) > 0 {
			return n
		}
	}
	var out []symbol.Symbol
	all(func(s symbol.Symbol) {
		for _, sp := range spellings {
			if boundarySuffix(s.Qualified, sp) {
				out = append(out, s)
				return
			}
		}
	})
	return out
}

// targetSpellings returns name followed by its distinct alternative spellings.
func targetSpellings(name string) []string {
	out := []string{name}
	addUnique := func(s string) {
		if s == "" {
			return
		}
		for _, o := range out {
			if o == s {
				return
			}
		}
		out = append(out, s)
	}
	dotted := strings.ReplaceAll(name, "::", ".")
	addUnique(dotted)
	addUnique(strings.TrimLeftFunc(name, isSeparatorRune))
	addUnique(strings.TrimLeftFunc(dotted, isSeparatorRune))
	return out
}

// boundarySuffix reports whether suffix is a proper suffix of qualified that
// starts at an identity segment boundary: the suffix begins with a separator,
// or the character before it is one.
func boundarySuffix(qualified, suffix string) bool {
	if suffix == "" || len(qualified) <= len(suffix) || !strings.HasSuffix(qualified, suffix) {
		return false
	}
	return !isIdentByte(suffix[0]) || !isIdentByte(qualified[len(qualified)-len(suffix)-1])
}

// isIdentByte reports whether b can be part of an identifier. Bytes of
// multi-byte UTF-8 characters count as identifier bytes, so a boundary is only
// ever an ASCII separator.
func isIdentByte(b byte) bool {
	return b == '_' || b == '$' || b >= 0x80 ||
		('0' <= b && b <= '9') || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

func isSeparatorRune(r rune) bool {
	return r < 0x80 && !isIdentByte(byte(r))
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
	shown := cands
	if len(shown) > maxAmbiguousCandidates {
		shown = shown[:maxAmbiguousCandidates]
	}
	for _, s := range shown {
		fmt.Fprintf(&b, "  %s  (%s)  %s\n", s.Qualified, s.Kind, s.Location.File)
	}
	if rest := len(cands) - len(shown); rest > 0 {
		fmt.Fprintf(&b, "  ... and %d more\n", rest)
	}
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: b.String()}},
		IsError: true,
	}
}

// maxAmbiguousCandidates bounds the candidates listed in an ambiguity error;
// they are the first in the deterministic sortTargetCandidates order.
const maxAmbiguousCandidates = 20

// targetNotFoundResult is the error for a target name that matches no symbol.
// It is an error, never an empty result: "no such symbol" must not read as
// "a symbol with no relations".
func targetNotFoundResult(name, path string) *CallToolResult {
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("symbol %q not found in %s", name, path)}},
		IsError: true,
	}
}
