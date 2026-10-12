package repomap

import (
	"math"
	"path/filepath"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/symbol"
)

// A policy is how the map treats one language's symbols: which are
// candidates for a package's listing, how they rank, how a listing spreads
// over types of declaration, and which package-to-package relations the
// declarations state beyond imports. The engine (Build) applies the policy
// of each symbol's language. It knows no language: a provider that states
// what its declarations are (language.MapHinter) gets rolePolicy, any other
// defaultPolicy — the map as it always was.
type policy struct {
	// candidate reports whether s may be listed at detail.
	candidate func(s symbol.Symbol, detail DetailLevel) bool
	// score ranks a candidate within its package; higher first.
	score func(se SymbolEntry, s symbol.Symbol, pkg PackageEntry) int
	// group is the key a listing spreads over: candidates of one non-empty
	// group are listed one at a time, best first, before any group's second
	// (pickSymbols). "" never limits.
	group func(s symbol.Symbol) string
	// dependency returns the package directory a declaration makes its
	// package depend on, as the provider stated it.
	dependency func(s symbol.Symbol) (dir string, ok bool)
}

// defaultPolicy is the map of a language whose exported declarations are its
// API: the exported ones are listed, structure first (scoreSymbol).
var defaultPolicy = policy{
	candidate: func(s symbol.Symbol, detail DetailLevel) bool {
		return detail == DetailVerbose || s.Exported
	},
	score: func(se SymbolEntry, _ symbol.Symbol, pkg PackageEntry) int {
		return scoreSymbol(se, pkg)
	},
	group:      func(symbol.Symbol) string { return "" },
	dependency: func(symbol.Symbol) (string, bool) { return "", false },
}

// policyFor returns the policy of a language.
func policyFor(lang string) policy {
	if p, ok := languages.Registry().Provider(language.Language(lang)); ok {
		if h, ok := p.(language.MapHinter); ok {
			return rolePolicy(h)
		}
	}
	return defaultPolicy
}

// roleScore ranks the roles a provider states: what a unit builds, composes
// and reads orients a reader before what it exposes and takes.
var roleScore = map[language.MapRole]int{
	language.MapRoleBuilt:      60,
	language.MapRoleComposed:   55,
	language.MapRoleRead:       30,
	language.MapRoleExposed:    15,
	language.MapRoleConfigured: 10,
	language.MapRoleParameter:  0,
	language.MapRoleDetail:     -20,
}

// rolePolicy maps a language whose provider states its declarations' roles:
// a declaration of a stated role is a candidate whatever its exportedness
// (detail only in verbose maps), one of no stated role as in defaultPolicy.
// The role ranks first; references count sublinearly, so a parameter every
// declaration uses does not outrank what the unit builds. A listing takes one
// declaration of each declared type before a second of any. A declaration
// that composes another unit (MapRoleComposed) and whose member scope names
// that unit's directory is a dependency on it; nothing else is.
func rolePolicy(h language.MapHinter) policy {
	return policy{
		candidate: func(s symbol.Symbol, detail DetailLevel) bool {
			switch h.MapRole(s.Kind, s.Name) {
			case language.MapRoleUnspecified:
				return defaultPolicy.candidate(s, detail)
			case language.MapRoleDetail:
				return detail == DetailVerbose
			}
			return true
		},
		score: func(se SymbolEntry, s symbol.Symbol, pkg PackageEntry) int {
			role := h.MapRole(s.Kind, s.Name)
			if role == language.MapRoleUnspecified {
				return scoreSymbol(se, pkg)
			}
			score := roleScore[role]
			score += int(10 * math.Log2(1+float64(se.InboundEdges)))
			score += int(5 * math.Log2(1+float64(se.OutboundEdges)))
			if pkg.IsGenerated {
				score -= 20
			}
			if pkg.IsTest {
				score -= 10
			}
			return score
		},
		group: func(s symbol.Symbol) string {
			return h.DeclaredType(s.Kind, s.Name)
		},
		dependency: func(s symbol.Symbol) (string, bool) {
			if s.MemberScope == "" || s.MembersOutside || h.MapRole(s.Kind, s.Name) != language.MapRoleComposed {
				return "", false
			}
			dir, ok := h.MemberScopeDir(s.MemberScope)
			if !ok || dir == "" {
				return "", ok // the root's package key is ""
			}
			return filepath.FromSlash(dir), true
		},
	}
}

// pickSymbols returns at most max of entries, which are sorted best first,
// in that order: the best of each group first (policy.group), then the next
// best of any. With no group it is the first max entries.
func pickSymbols(entries []SymbolEntry, max int) []SymbolEntry {
	if len(entries) <= max {
		return entries
	}
	chosen := make([]bool, len(entries))
	n := 0
	seen := map[string]bool{}
	for i, e := range entries {
		if n == max {
			break
		}
		if e.group != "" {
			if seen[e.group] {
				continue
			}
			seen[e.group] = true
		}
		chosen[i] = true
		n++
	}
	for i := range entries {
		if n == max {
			break
		}
		if !chosen[i] {
			chosen[i] = true
			n++
		}
	}
	out := make([]SymbolEntry, 0, max)
	for i, e := range entries {
		if chosen[i] {
			out = append(out, e)
		}
	}
	return out
}
