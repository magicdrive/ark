package html

import (
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A directory of documents' overview (language.MapHinter) is its documents;
// the elements with an id, often many per page, are detail.
var _ language.MapHinter = (*Provider)(nil)

// MapRole implements language.MapHinter.
func (*Provider) MapRole(kind symbol.SymbolKind, _ string) language.MapRole {
	switch kind {
	case symbol.KindDocument:
		return language.MapRoleBuilt
	case symbol.KindElement:
		return language.MapRoleDetail
	}
	return language.MapRoleUnspecified
}

// DeclaredType implements language.MapHinter: HTML declares no typed
// instances.
func (*Provider) DeclaredType(symbol.SymbolKind, string) string { return "" }

// MemberScopeDir implements language.MapHinter: the provider states no
// member scopes.
func (*Provider) MemberScopeDir(string) (string, bool) { return "", false }
