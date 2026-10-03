package context

import (
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Score holds a total ranking score and a per-factor breakdown for debugging.
type Score struct {
	Total     float64
	Breakdown map[string]float64
}

// Ranker scores a candidate for inclusion in context.
type Ranker interface {
	Rank(c candidate, sourceLen int) Score
}

// DefaultRanker is the deterministic built-in ranker.
// Numbers are tunable; the structure (explainable, additive) is the invariant.
type DefaultRanker struct{}

func (DefaultRanker) Rank(c candidate, sourceLen int) Score {
	bd := make(map[string]float64)

	switch c.reason {
	case "target":
		bd["target"] = 100
	case "direct callee", "extends", "implements", "uses_trait":
		// Typed structural relations are scored exactly like a direct callee
		// (confidence-tiered); only the reason label differs, so ordering is
		// unchanged relative to pre-PHP-7 behavior.
		switch c.confidence {
		case resolver.ConfidenceExact:
			bd["direct_callee_exact"] = 40
		case resolver.ConfidenceStrong:
			bd["direct_callee_strong"] = 30
		default:
			bd["direct_callee_candidate"] = 10
		}
	case "type dependency":
		bd["type_dependency"] = 25
	case "caller":
		bd["caller"] = 20
	case "test relation":
		bd["test_relation"] = 15
	default:
		bd["other"] = 5
	}

	// Exported symbols are more likely to be interface boundaries.
	if c.sym.Exported {
		bd["exported"] = 5
	}

	// Penalise hop distance.
	if c.hopDepth > 0 {
		bd["distance_penalty"] = float64(-10 * c.hopDepth)
	}

	// Penalise large symbols (over ~500 tokens = ~2000 bytes).
	if sourceLen > 2000 {
		excess := float64(sourceLen-2000) / 100.0
		bd["large_source_penalty"] = -0.5 * excess
	}

	// Function/method kinds are higher value than variables.
	switch c.sym.Kind {
	case symbol.KindFunction, symbol.KindMethod, symbol.KindConstructor:
		bd["kind_bonus"] = 3
	}

	total := 0.0
	for _, v := range bd {
		total += v
	}
	return Score{Total: total, Breakdown: bd}
}
