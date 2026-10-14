package css

import (
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A stylesheet's overview (language.MapHinter) is the stylesheet, then what
// it offers to other rules and files — its @keyframes, @font-face families,
// @property registrations and CSS Modules classes; its custom properties,
// often hundreds of design tokens, are detail.
var _ language.MapHinter = (*Provider)(nil)

// MapRole implements language.MapHinter.
func (*Provider) MapRole(kind symbol.SymbolKind, _ string) language.MapRole {
	switch kind {
	case symbol.KindStylesheet:
		return language.MapRoleBuilt
	case symbol.KindKeyframes, symbol.KindFontFace, symbol.KindPropertyRegistration, symbol.KindCSSClass:
		return language.MapRoleExposed
	case symbol.KindCustomProperty:
		return language.MapRoleDetail
	}
	return language.MapRoleUnspecified
}

// DeclaredType implements language.MapHinter: CSS declares no typed
// instances.
func (*Provider) DeclaredType(symbol.SymbolKind, string) string { return "" }

// MemberScopeDir implements language.MapHinter: the provider states no
// member scopes.
func (*Provider) MemberScopeDir(string) (string, bool) { return "", false }
