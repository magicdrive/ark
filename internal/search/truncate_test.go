package search_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/search"
)

func TestSearch_Truncation(t *testing.T) {
	idx := buildIndex(t)

	// Ask for only 2 results from a set that has more.
	r, err := search.Search(context.Background(), idx, search.Query{}, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(r.Matches) > 2 {
		t.Errorf("expected at most 2 matches, got %d", len(r.Matches))
	}

	// Only check truncation when there are actually more than 2 symbols.
	if r.TotalCount > 2 {
		if !r.Truncated {
			t.Errorf("expected Truncated=true when TotalCount=%d > 2", r.TotalCount)
		}
		if len(r.Matches) != 2 {
			t.Errorf("expected exactly 2 matches, got %d", len(r.Matches))
		}
	}
}

func TestSearch_NoTruncationWhenUnderLimit(t *testing.T) {
	idx := buildIndex(t)
	r, err := search.Search(context.Background(), idx, search.Query{}, 1000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if r.Truncated {
		t.Error("expected Truncated=false with large limit")
	}
	if r.TotalCount != len(r.Matches) {
		t.Errorf("TotalCount %d != len(Matches) %d", r.TotalCount, len(r.Matches))
	}
}
