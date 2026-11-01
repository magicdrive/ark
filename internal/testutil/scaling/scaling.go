// Package scaling checks, in tests, that a cost grows linearly with its
// input: the time of four times the input against a bound well under the
// sixteen times a quadratic cost would take, and the allocations likewise.
// Tests only.
//
// A time is the wall clock's, measured where other packages' tests run at
// the same time (go test ./... starts a binary per package, as many as the
// machine has CPUs): a load that comes and goes slows a run now and then.
// So the two sizes run alternately, a few times each, and their best runs
// are compared — a load slows both alike, not the large input's alone, as
// measuring all of one size and then all of the other did — and a ratio
// above the bound is measured again, up to a few attempts in all. A
// quadratic cost exceeds the bound in every attempt; a burst of load does
// not last through all of them.
//
// Allocations are counted, not timed: their ratio does not depend on the
// machine's load at all. A cost that allocates nothing per step is left to
// the time.
package scaling

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

const (
	// rounds is the runs of each size in an attempt; each size's best run
	// counts (a load only ever adds time).
	rounds = 3
	// attempts is the measurements of the ratio before one above the bound
	// fails the test.
	attempts = 3
)

// Linear reports through t.Errorf that the cost of large is more than bound
// times the cost of small in every attempt: small and large run alternately,
// rounds times each per attempt, and their best runs are compared. bound
// stays well below the ratio a quadratic cost has at the inputs' sizes (16
// at four times the input), so that a quadratic cost exceeds it every time.
// what names the inputs in the report.
func Linear(t testing.TB, what string, bound float64, small, large func()) {
	t.Helper()
	linear(t, what, bound, small, large, sinceStart)
}

var start = time.Now()

// sinceStart reads the wall clock (monotonic) as a duration.
func sinceStart() time.Duration { return time.Since(start) }

func linear(t testing.TB, what string, bound float64, small, large func(), clock func() time.Duration) {
	t.Helper()
	timed := func(run func()) time.Duration {
		t0 := clock()
		run()
		return clock() - t0
	}
	var tries []string
	for range attempts {
		bestSmall, bestLarge := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
		for range rounds {
			bestSmall = min(bestSmall, timed(small))
			bestLarge = min(bestLarge, timed(large))
		}
		ratio := float64(bestLarge) / float64(max(bestSmall, 1))
		tries = append(tries, fmt.Sprintf("%v / %v (x%.1f)", bestSmall, bestLarge, ratio))
		if ratio <= bound {
			t.Logf("%s: %s", what, strings.Join(tries, ", "))
			return
		}
	}
	t.Errorf("%s: took more than %g times as long in every attempt, %s: the cost is not linear", what, bound, strings.Join(tries, ", "))
}

// LinearAllocs reports through t.Errorf that large allocates more than bound
// times as often as small (testing.AllocsPerRun: the average of runs after
// one to warm up). The count is the same under any load, so it is measured
// once. what names the inputs in the report.
func LinearAllocs(t testing.TB, what string, bound float64, small, large func()) {
	t.Helper()
	a, b := testing.AllocsPerRun(1, small), testing.AllocsPerRun(1, large)
	ratio := b / max(a, 1)
	t.Logf("%s: %.0f / %.0f allocations (x%.2f)", what, a, b, ratio)
	if ratio > bound {
		t.Errorf("%s: %.0f / %.0f allocations, %.2f times as many (at most %g): the cost is not linear", what, a, b, ratio, bound)
	}
}
