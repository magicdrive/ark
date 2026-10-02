package reference

import (
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

func TestNewReferenceID_Deterministic(t *testing.T) {
	loc := source.Location{
		File: source.FileID("service/user.go"),
		Range: source.Range{
			Start: source.Position{Line: 10, Column: 5},
			End:   source.Position{Line: 10, Column: 9},
		},
	}
	id1 := NewReferenceID("go", loc.File, KindCall, "Save", loc)
	id2 := NewReferenceID("go", loc.File, KindCall, "Save", loc)
	if id1 != id2 {
		t.Errorf("NewReferenceID is non-deterministic: %q vs %q", id1, id2)
	}
}

func TestNewReferenceID_Distinct(t *testing.T) {
	file := source.FileID("service/user.go")
	loc1 := source.Location{File: file, Range: source.Range{Start: source.Position{Line: 10, Column: 5}}}
	loc2 := source.Location{File: file, Range: source.Range{Start: source.Position{Line: 20, Column: 5}}}

	id1 := NewReferenceID("go", file, KindCall, "Save", loc1)
	id2 := NewReferenceID("go", file, KindCall, "Save", loc2)
	if id1 == id2 {
		t.Error("NewReferenceID should differ for different locations")
	}
}

func TestNewReferenceID_DifferentKinds(t *testing.T) {
	file := source.FileID("service/user.go")
	loc := source.Location{File: file, Range: source.Range{Start: source.Position{Line: 5, Column: 1}}}

	idCall := NewReferenceID("go", file, KindCall, "Foo", loc)
	idType := NewReferenceID("go", file, KindTypeUse, "Foo", loc)
	if idCall == idType {
		t.Error("NewReferenceID should differ for different kinds")
	}
}
