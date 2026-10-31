package markdown

import (
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A Markdown file's overview (language.MapHinter) is the document and the
// sections it is made of; its code blocks and HTML anchors are detail.
var _ language.MapHinter = (*Provider)(nil)

// MapRole implements language.MapHinter.
func (*Provider) MapRole(kind symbol.SymbolKind, _ string) language.MapRole {
	switch kind {
	case symbol.KindDocument:
		return language.MapRoleBuilt
	case symbol.KindSection:
		return language.MapRoleExposed
	case symbol.KindCodeBlock, symbol.KindAnchor:
		return language.MapRoleDetail
	}
	return language.MapRoleUnspecified
}

// DeclaredType implements language.MapHinter: Markdown declares no typed
// instances.
func (*Provider) DeclaredType(symbol.SymbolKind, string) string { return "" }

// MemberScopeDir implements language.MapHinter: the provider states no
// member scopes.
func (*Provider) MemberScopeDir(string) (string, bool) { return "", false }

// Accompanies implements language.Accompanying: Markdown files document the
// code beside them; a package is named by the language of its code.
func (*Provider) Accompanies() bool { return true }

// ReadsHiddenDir implements language.HiddenDirReader: GitHub's .github
// holds documents of the repository (templates, release notes, the
// contributing guide) that its other documents link to. A Markdown file is
// IdentityOnly, so reading it changes no name of any other file.
func (*Provider) ReadsHiddenDir(name string) bool { return name == ".github" }
