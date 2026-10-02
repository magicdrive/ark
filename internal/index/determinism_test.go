package index_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// TestIndex_DeterministicRepeated verifies that building the same fixture
// multiple times produces identical symbol sets, file lists, and edge sets.
// This catches nondeterminism from map iteration and insertion-order dependence.
func TestIndex_DeterministicRepeated(t *testing.T) {
	const runs = 5
	dir := fixtureDir(t, "simple_go")
	providers := benchmarkProviders()

	type snapshot struct {
		files   []string
		symbols []string
		statsStr string
	}

	snapshots := make([]snapshot, runs)
	for i := range runs {
		idx, err := index.New(context.Background(), dir, providers)
		if err != nil {
			t.Fatalf("run %d: index.New: %v", i, err)
		}

		files := idx.Files()
		syms := idx.FindSymbols("")
		st := idx.Stats()

		fNames := make([]string, len(files))
		for j, f := range files {
			fNames[j] = string(f)
		}
		sNames := make([]string, len(syms))
		for j, s := range syms {
			sNames[j] = string(s.ID) + ":" + s.Qualified
		}

		snapshots[i] = snapshot{
			files:    fNames,
			symbols:  sNames,
			statsStr: fmt.Sprintf("files=%d syms=%d rels=%d", st.Files, st.Symbols, st.Relations),
		}
	}

	base := snapshots[0]
	for i := 1; i < runs; i++ {
		run := snapshots[i]
		if run.statsStr != base.statsStr {
			t.Errorf("run %d: stats differ:\n  base: %s\n  got:  %s", i, base.statsStr, run.statsStr)
		}
		if len(run.files) != len(base.files) {
			t.Errorf("run %d: file count differs: %d vs %d", i, len(run.files), len(base.files))
		} else {
			for j := range base.files {
				if run.files[j] != base.files[j] {
					t.Errorf("run %d: file[%d] differs: %q vs %q", i, j, run.files[j], base.files[j])
				}
			}
		}
		if len(run.symbols) != len(base.symbols) {
			t.Errorf("run %d: symbol count differs: %d vs %d", i, len(run.symbols), len(base.symbols))
		} else {
			for j := range base.symbols {
				if run.symbols[j] != base.symbols[j] {
					t.Errorf("run %d: symbol[%d] differs: %q vs %q", i, j, run.symbols[j], base.symbols[j])
				}
			}
		}
	}
}
