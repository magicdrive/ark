package source

import "testing"

func TestLocationZeroValue(t *testing.T) {
	var loc Location
	if loc.File != "" {
		t.Errorf("zero FileID should be empty, got %q", loc.File)
	}
	if loc.Range.Start.Line != 0 || loc.Range.Start.Column != 0 {
		t.Error("zero Position should have Line=0 Column=0")
	}
}

func TestLocationEquality(t *testing.T) {
	a := Location{
		File:  "internal/foo/bar.go",
		Range: Range{Start: Position{1, 1}, End: Position{10, 1}},
	}
	b := a
	if a != b {
		t.Error("identical locations should be equal")
	}
}
