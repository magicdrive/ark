package language

import (
	"reflect"
	"testing"
)

func TestTypeExpr_Helpers(t *testing.T) {
	for _, tc := range []struct{ in, deref, elem string }{
		{"T", "T", ""},
		{"*T", "T", ""},
		{"[]*q.T", "[]*q.T", "*q.T"},
		{"map[string]T", "map[string]T", "T"},
		{"map[K]map[]V", "map[K]map[]V", "map[]V"},
		{"map[[]K]V", "map[[]K]V", "V"},
		{"*[]T", "[]T", ""},
	} {
		if got := TypeDeref(tc.in); got != tc.deref {
			t.Errorf("TypeDeref(%q) = %q, want %q", tc.in, got, tc.deref)
		}
		if got := TypeElem(tc.in); got != tc.elem {
			t.Errorf("TypeElem(%q) = %q, want %q", tc.in, got, tc.elem)
		}
	}
	for in, want := range map[string][3]string{
		"T":     {"", "T", "true"},
		"q.T":   {"q", "T", "true"},
		"*T":    {"", "", "false"},
		"[]T":   {"", "", "false"},
		"a.b.c": {"", "", "false"},
		".T":    {"", "", "false"},
		"":      {"", "", "false"},
	} {
		q, n, ok := TypeNamed(in)
		if got := [3]string{q, n, map[bool]string{true: "true", false: "false"}[ok]}; got != want {
			t.Errorf("TypeNamed(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestChainSteps(t *testing.T) {
	steps, ok := ChainSteps("decl:pkg.New|call:1|member:items|elem|member:Run")
	want := []ChainStep{{Op: "decl", Arg: "pkg.New"}, {Op: "call", Arg: "1", Call: 1}, {Op: "member", Arg: "items"}, {Op: "elem"}, {Op: "member", Arg: "Run"}}
	if !ok || !reflect.DeepEqual(steps, want) {
		t.Errorf("got %+v %v", steps, ok)
	}
	for _, bad := range []string{"", "member:x", "call:0", "elem", "type:", "decl:", "type:T|decl:x", "decl:f|call:-1", "decl:f|call:x", "decl:f|elem:x", "decl:f|member:", "decl:f|bogus:1"} {
		if _, ok := ChainSteps(bad); ok {
			t.Errorf("ChainSteps(%q) accepted a malformed chain", bad)
		}
	}
}
