package xml

import (
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// An XML file's overview (language.MapHinter) is what it builds — a Maven
// project, a document — then the artifacts a project composes; its
// elements, attributes, namespaces and IDs are detail.
var _ language.MapHinter = (*Provider)(nil)

// MapRole implements language.MapHinter.
func (*Provider) MapRole(kind symbol.SymbolKind, _ string) language.MapRole {
	switch kind {
	case symbol.KindPackage, symbol.KindDocument:
		return language.MapRoleBuilt
	case symbol.KindDependency:
		return language.MapRoleComposed
	case symbol.KindElement, symbol.KindAttribute, symbol.KindNamespace, symbol.KindAnchor:
		return language.MapRoleDetail
	}
	return language.MapRoleUnspecified
}

// DeclaredType implements language.MapHinter: XML declares no typed
// instances.
func (*Provider) DeclaredType(symbol.SymbolKind, string) string { return "" }

// MemberScopeDir implements language.MapHinter: the provider states no
// member scopes.
func (*Provider) MemberScopeDir(string) (string, bool) { return "", false }

// Accompanies implements language.Accompanying: XML files (a POM, Spring or
// Android resources) are the build and configuration of the code beside
// them.
func (*Provider) Accompanies() bool { return true }
