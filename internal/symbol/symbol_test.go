package symbol

import "testing"

func TestNewSymbolID_Deterministic(t *testing.T) {
	id1 := NewSymbolID("go", "internal/foo/bar.go", KindFunction, "Greet")
	id2 := NewSymbolID("go", "internal/foo/bar.go", KindFunction, "Greet")
	if id1 != id2 {
		t.Errorf("NewSymbolID not deterministic: %q != %q", id1, id2)
	}
}

func TestNewSymbolID_Distinct(t *testing.T) {
	cases := []struct{ lang, path string; kind SymbolKind; qualified string }{
		{"go", "a.go", KindFunction, "Foo"},
		{"go", "a.go", KindMethod, "Foo"},    // different kind
		{"go", "b.go", KindFunction, "Foo"},   // different path
		{"ts", "a.go", KindFunction, "Foo"},   // different lang
		{"go", "a.go", KindFunction, "Bar"},   // different qualified
	}
	seen := make(map[SymbolID]struct{})
	for _, c := range cases {
		id := NewSymbolID(c.lang, c.path, c.kind, c.qualified)
		if _, dup := seen[id]; dup {
			t.Errorf("collision for %+v: id=%q", c, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewSymbolID_NotEmpty(t *testing.T) {
	id := NewSymbolID("go", "main.go", KindFunction, "main")
	if id == "" {
		t.Error("NewSymbolID returned empty string")
	}
}
