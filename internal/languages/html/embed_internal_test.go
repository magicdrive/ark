package html

import (
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// shift maps a location with no position (a parse failure's) to the start
// of the region (Reviewer B, R8-2), line 1 by the region's column, and later
// lines by its line.
func TestShift(t *testing.T) {
	e := &extractor{file: "s.html", lines: newLines([]byte("ab\ncd<script>x\ny"))}
	off := len("ab\ncd<script>")
	cases := []struct{ in, want source.Position }{
		{source.Position{}, source.Position{Line: 2, Column: 11}},
		{source.Position{Line: 1, Column: 1}, source.Position{Line: 2, Column: 11}},
		{source.Position{Line: 1, Column: 2}, source.Position{Line: 2, Column: 12}},
		{source.Position{Line: 2, Column: 1}, source.Position{Line: 3, Column: 1}},
	}
	for _, c := range cases {
		got := e.shift(source.Location{Range: source.Range{Start: c.in, End: c.in}}, off)
		if got.Range.Start != c.want || got.File != "s.html" {
			t.Errorf("shift %v = %v, want %v", c.in, got.Range.Start, c.want)
		}
	}
}
