package toml

import (
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A TOML file's overview (language.MapHinter) is what it defines — the file
// itself, a manifest's package — then the units the package composes (its
// dependencies); its tables and keys are detail.
var _ language.MapHinter = (*Provider)(nil)

// MapRole implements language.MapHinter.
func (*Provider) MapRole(kind symbol.SymbolKind, _ string) language.MapRole {
	switch kind {
	case symbol.KindDocument, symbol.KindPackage:
		return language.MapRoleBuilt
	case symbol.KindDependency:
		return language.MapRoleComposed
	case symbol.KindTable, symbol.KindKey:
		return language.MapRoleDetail
	}
	return language.MapRoleUnspecified
}

// DeclaredType implements language.MapHinter: TOML declares no typed
// instances.
func (*Provider) DeclaredType(symbol.SymbolKind, string) string { return "" }

// MemberScopeDir implements language.MapHinter: the provider states no
// member scopes.
func (*Provider) MemberScopeDir(string) (string, bool) { return "", false }

// Accompanies implements language.Accompanying: TOML files (a Cargo
// manifest, a pyproject.toml, tool settings) are the configuration of the
// code beside them.
func (*Provider) Accompanies() bool { return true }

// The provider reads no hidden directory (language.HiddenDirReader):
// .cargo holds config.toml, but also credentials.toml, a registry token.
