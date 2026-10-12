package repomap

import (
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// fakeHinter states every role by kind and every scope "DIR/x." as DIR.
type fakeHinter struct{}

func (fakeHinter) MapRole(kind symbol.SymbolKind, _ string) language.MapRole {
	switch kind {
	case symbol.KindModule:
		return language.MapRoleComposed
	case symbol.KindResource:
		return language.MapRoleBuilt
	case symbol.KindOutput:
		return language.MapRoleExposed
	}
	return language.MapRoleUnspecified
}
func (fakeHinter) DeclaredType(symbol.SymbolKind, string) string { return "" }
func (fakeHinter) MemberScopeDir(scope string) (string, bool) {
	dir, _ := filepath.Split(scope)
	return filepath.ToSlash(filepath.Clean(dir)), dir != ""
}

// Only a declaration that composes a unit depends on it: a member scope on a
// declaration of any other role, or of none, states no dependency, nor does
// a scope the provider reads as no directory, nor members outside.
func TestRolePolicy_DependencyOnlyFromComposition(t *testing.T) {
	dep := rolePolicy(fakeHinter{}).dependency
	for _, tc := range []struct {
		s    symbol.Symbol
		want string
		ok   bool
	}{
		{symbol.Symbol{Kind: symbol.KindModule, MemberScope: "a/b/x."}, filepath.FromSlash("a/b"), true},
		{symbol.Symbol{Kind: symbol.KindResource, MemberScope: "a/b/x."}, "", false},
		{symbol.Symbol{Kind: symbol.KindOutput, MemberScope: "a/b/x."}, "", false},
		{symbol.Symbol{Kind: symbol.KindClass, MemberScope: "a/b/x."}, "", false},
		{symbol.Symbol{Kind: symbol.KindModule, MemberScope: "x."}, "", false},
		{symbol.Symbol{Kind: symbol.KindModule}, "", false},
		{symbol.Symbol{Kind: symbol.KindModule, MembersOutside: true}, "", false},
	} {
		if got, ok := dep(tc.s); got != tc.want || ok != tc.ok {
			t.Errorf("dependency(%v %q) = %q %v, want %q %v", tc.s.Kind, tc.s.MemberScope, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := defaultPolicy.dependency(symbol.Symbol{Kind: symbol.KindModule, MemberScope: "a/x."}); ok {
		t.Error("defaultPolicy states a dependency")
	}
}
