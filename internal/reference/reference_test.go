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

// Two references of one name and kind starting at one position (Go
// f(x).Value.f(): the call of f, and the call of the method f over the whole
// selector) have distinct IDs: one's resolution is never joined to the other.
func TestNewReferenceID_SameStartDistinctSpans(t *testing.T) {
	file := source.FileID("slog/attr_test.go")
	start := source.Position{Line: 25, Column: 7}
	inner := source.Location{File: file, Range: source.Range{Start: start, End: source.Position{Line: 25, Column: 12}}}
	outer := source.Location{File: file, Range: source.Range{Start: start, End: source.Position{Line: 25, Column: 34}}}
	if NewReferenceID("go", file, KindCall, "Int64", inner) == NewReferenceID("go", file, KindCall, "Int64", outer) {
		t.Error("references of one start but different spans share an ID")
	}
}

// Every ingredient of the ID tells references apart on its own: changing any
// one of language, file, kind, name, start or end gives another ID.
func TestNewReferenceID_EveryIngredientCounts(t *testing.T) {
	type in struct {
		lang string
		file source.FileID
		kind ReferenceKind
		name string
		rg   source.Range
	}
	base := in{"go", "a/a.go", KindCall, "F", source.Range{Start: source.Position{Line: 3, Column: 5}, End: source.Position{Line: 3, Column: 9}}}
	id := func(x in) ReferenceID {
		return NewReferenceID(x.lang, x.file, x.kind, x.name, source.Location{File: x.file, Range: x.rg})
	}
	variants := map[string]func(*in){
		"language":    func(x *in) { x.lang = "javascript" },
		"file":        func(x *in) { x.file = "a/b.go" },
		"kind":        func(x *in) { x.kind = KindTypeUse },
		"name":        func(x *in) { x.name = "G" },
		"startLine":   func(x *in) { x.rg.Start.Line = 2 },
		"startColumn": func(x *in) { x.rg.Start.Column = 6 },
		"endLine":     func(x *in) { x.rg.End.Line = 4 },
		"endColumn":   func(x *in) { x.rg.End.Column = 10 },
	}
	want := id(base)
	for what, change := range variants {
		x := base
		change(&x)
		if id(x) == want {
			t.Errorf("another %s gives the same ID", what)
		}
	}
}

// The container tells two references of one kind and name at one place
// apart (content a YAML alias repeats in two steps); none keeps the ID of
// NewReferenceID.
func TestNewReferenceIDIn_Container(t *testing.T) {
	loc := source.Location{File: "w.yml", Range: source.Range{Start: source.Position{Line: 3, Column: 5}, End: source.Position{Line: 3, Column: 9}}}
	a := NewReferenceIDIn("yaml", "w.yml", KindCall, "x", "w.yml::a::s", loc)
	b := NewReferenceIDIn("yaml", "w.yml", KindCall, "x", "w.yml::b::s", loc)
	if a == b {
		t.Error("two containers give one ID")
	}
	if a != NewReferenceIDIn("yaml", "w.yml", KindCall, "x", "w.yml::a::s", loc) {
		t.Error("not deterministic")
	}
	if NewReferenceIDIn("yaml", "w.yml", KindCall, "x", "", loc) != NewReferenceID("yaml", "w.yml", KindCall, "x", loc) {
		t.Error("no container must give NewReferenceID's ID")
	}
}
