package zig

import (
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/golden"
)

// TestIndex_Deterministic: the fixture indexed twice gives byte-identical
// snapshots (symbols, references, resolutions, edges).
func TestIndex_Deterministic(t *testing.T) {
	a := golden.IndexSnapshot(buildIndex(t, fixtureRepo))
	b := golden.IndexSnapshot(buildIndex(t, fixtureRepo))
	if a != b {
		t.Fatal("two builds of one repository differ")
	}
	if !strings.Contains(a, "src/main.zig::main") {
		t.Fatal("snapshot misses the fixture")
	}
}
