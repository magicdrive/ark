package golang

import (
	"reflect"
	"strconv"
	"testing"
)

// fidelityPartitions is the number of parallel subtests that share the
// independent items of one repository-wide test (part/0 ...): fixed, so that
// the subtests are the same on every machine; -parallel bounds how many run
// at once.
const fidelityPartitions = 8

// inPartitions calls check for every item index in [0, n) exactly once, from
// fidelityPartitions parallel subtests (item i in part i%fidelityPartitions,
// in increasing order), and returns when all of them have finished. check
// runs on its subtest's goroutine with its subtest's t: it may call t.Fatal.
// Results the caller needs it stores by index, to read in item order after.
// A caller's checks over all items (a minimum count) hold only when every
// part ran: -run selecting one part (Test/part/3) runs an eighth of them.
func inPartitions(t *testing.T, n int, check func(t *testing.T, i int)) {
	t.Helper()
	t.Run("part", func(t *testing.T) {
		for p := range fidelityPartitions {
			t.Run(strconv.Itoa(p), func(t *testing.T) {
				t.Parallel()
				for i := p; i < n; i += fidelityPartitions {
					check(t, i)
				}
			})
		}
	})
}

// inPartitions visits every index exactly once, whatever n is relative to
// the number of parts.
func TestInPartitions_EveryItemOnce(t *testing.T) {
	for _, n := range []int{0, 1, fidelityPartitions - 1, fidelityPartitions, fidelityPartitions + 1, 100} {
		seen := make([]int, n)
		inPartitions(t, n, func(t *testing.T, i int) { seen[i]++ })
		for i, c := range seen {
			if c != 1 {
				t.Errorf("n=%d: item %d checked %d times", n, i, c)
			}
		}
	}
}

// Merging per-directory reports in directory order gives the report one
// sequential walk fills: the counts summed, each kind's examples in order.
func TestFidelityReport_MergeIsTheSequentialReport(t *testing.T) {
	dirs := []fidelityReport{{checked: 2, held: 1, generic: 3}, {}, {checked: 1, generic: 1}}
	dirs[0].add("missing call", "a.go:1")
	dirs[0].add("FALSE call (no such call)", "a.go:2")
	dirs[2].add("missing call", "c.go:1")
	dirs[2].add("missing call", "c.go:2")
	var want fidelityReport
	want.checked, want.held, want.generic = 3, 1, 4
	for _, w := range []string{"a.go:1", "c.go:1", "c.go:2"} {
		want.add("missing call", w)
	}
	want.add("FALSE call (no such call)", "a.go:2")
	var got fidelityReport
	for i := range dirs {
		got.merge(&dirs[i])
	}
	if got.checked != want.checked || got.held != want.held || got.generic != want.generic || !reflect.DeepEqual(got.problems, want.problems) {
		t.Errorf("merged %+v, want %+v", got, want)
	}
}
