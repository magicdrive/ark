package scaling

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// recorder is a testing.TB that keeps what a check reports.
type recorder struct {
	testing.TB
	errors []string
}

func (r *recorder) Helper()             {}
func (r *recorder) Logf(string, ...any) {}
func (r *recorder) Errorf(format string, a ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, a...))
}

// script is a fake clock: each run advances it by the next of its costs.
type script struct {
	now   time.Duration
	order []string
}

func (s *script) run(name string, costs func(k int) time.Duration) func() {
	k := 0
	return func() {
		s.order = append(s.order, name)
		s.now += costs(k)
		k++
	}
}

func (s *script) clock() time.Duration { return s.now }

func check(t *testing.T, small, large func(k int) time.Duration) (*recorder, *script) {
	t.Helper()
	r, s := &recorder{TB: t}, &script{}
	linear(r, "x", 10, s.run("s", small), s.run("l", large), s.clock)
	return r, s
}

func cost(d time.Duration) func(int) time.Duration { return func(int) time.Duration { return d } }

// A linear cost passes in the first attempt; the sizes alternate.
func TestLinear_Linear(t *testing.T) {
	r, s := check(t, cost(10*time.Millisecond), cost(45*time.Millisecond))
	if len(r.errors) != 0 {
		t.Errorf("reported %v", r.errors)
	}
	if got := strings.Join(s.order, ""); got != "slslsl" {
		t.Errorf("runs %q, want the sizes alternately, three times each", got)
	}
}

// A quadratic cost fails, and only after every attempt exceeded the bound.
func TestLinear_Quadratic(t *testing.T) {
	r, s := check(t, cost(10*time.Millisecond), cost(160*time.Millisecond))
	if len(r.errors) != 1 || !strings.Contains(r.errors[0], "every attempt") || strings.Count(r.errors[0], "x16.0") != attempts {
		t.Errorf("reported %q, want one failure naming three ratios of 16", r.errors)
	}
	if len(s.order) != 2*rounds*attempts {
		t.Errorf("%d runs, want %d", len(s.order), 2*rounds*attempts)
	}
}

// A quadratic cost whose large runs are now and then fast (a best run of
// each attempt) still fails: the best of each size is compared.
func TestLinear_QuadraticNotRescuedByASmallLoad(t *testing.T) {
	r, _ := check(t, func(k int) time.Duration { return 10*time.Millisecond + time.Duration(k%2)*20*time.Millisecond },
		func(k int) time.Duration { return 160*time.Millisecond + time.Duration(k%3)*time.Second })
	if len(r.errors) != 1 {
		t.Errorf("reported %q, want a failure", r.errors)
	}
}

// A burst of load that slows every large run of one attempt is measured
// again, and passes.
func TestLinear_BurstIsRetried(t *testing.T) {
	r, s := check(t, cost(10*time.Millisecond), func(k int) time.Duration {
		if k < rounds {
			return 300 * time.Millisecond
		}
		return 42 * time.Millisecond
	})
	if len(r.errors) != 0 {
		t.Errorf("reported %v", r.errors)
	}
	if len(s.order) != 4*rounds {
		t.Errorf("%d runs, want two attempts", len(s.order))
	}
}

// A burst within one round leaves the attempt's best runs alone.
func TestLinear_BurstOfOneRun(t *testing.T) {
	r, s := check(t, cost(10*time.Millisecond), func(k int) time.Duration {
		if k == 0 {
			return time.Second
		}
		return 42 * time.Millisecond
	})
	if len(r.errors) != 0 || len(s.order) != 2*rounds {
		t.Errorf("reported %v after %d runs", r.errors, len(s.order))
	}
}

func TestLinearAllocs(t *testing.T) {
	var sink [][]byte
	alloc := func(n int) func() {
		return func() {
			for range n {
				sink = append(sink[:0], make([]byte, 64))
			}
		}
	}
	quadratic := func(n int) func() { return alloc(n * n) }
	r := &recorder{TB: t}
	LinearAllocs(r, "linear", 4.5, alloc(100), alloc(400))
	if len(r.errors) != 0 {
		t.Errorf("linear: reported %v", r.errors)
	}
	LinearAllocs(r, "quadratic", 4.5, quadratic(20), quadratic(80))
	if len(r.errors) != 1 {
		t.Errorf("quadratic: reported %v, want a failure", r.errors)
	}
	_ = sink
}
