package context

import (
	"math"
	"testing"

	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// A score is the same float on every call. The breakdown is a map; summing it
// in iteration order (random per call) made totals with fractional factors
// differ in the last bit, so near-equal candidates swapped places between
// identical requests.
func TestDefaultRanker_TotalIsBitIdentical(t *testing.T) {
	c := candidate{
		sym:        symbol.Symbol{Kind: symbol.KindMethod, Exported: true},
		reason:     "direct callee",
		confidence: resolver.ConfidenceStrong,
		hopDepth:   1,
	}
	for _, size := range []int{2001, 2137, 2240, 3333, 5049} {
		want := math.Float64bits(DefaultRanker{}.Rank(c, size).Total)
		for range 500 {
			if got := math.Float64bits(DefaultRanker{}.Rank(c, size).Total); got != want {
				t.Fatalf("size %d: total %v then %v", size, math.Float64frombits(want), math.Float64frombits(got))
			}
		}
	}
}
