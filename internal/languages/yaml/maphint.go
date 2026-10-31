package yaml

import (
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A YAML file's overview (language.MapHinter) is what it builds — a GitHub
// Actions workflow and its jobs, an action, a data document — then the
// events a workflow is triggered by; its steps, keys and anchors are detail.
var _ language.MapHinter = (*Provider)(nil)

// MapRole implements language.MapHinter.
func (*Provider) MapRole(kind symbol.SymbolKind, _ string) language.MapRole {
	switch kind {
	case symbol.KindWorkflow, symbol.KindJob, symbol.KindAction, symbol.KindDocument:
		return language.MapRoleBuilt
	case symbol.KindTrigger:
		return language.MapRoleExposed
	case symbol.KindStep, symbol.KindKey, symbol.KindAnchor:
		return language.MapRoleDetail
	}
	return language.MapRoleUnspecified
}

// DeclaredType implements language.MapHinter: YAML declares no typed
// instances.
func (*Provider) DeclaredType(symbol.SymbolKind, string) string { return "" }

// MemberScopeDir implements language.MapHinter: the provider states no
// member scopes.
func (*Provider) MemberScopeDir(string) (string, bool) { return "", false }

// ReadsHiddenDir implements language.HiddenDirReader: GitHub's .github holds
// the workflows (.github/workflows) and actions a repository's CI runs.
func (*Provider) ReadsHiddenDir(name string) bool { return name == ".github" }

// Accompanies implements language.Accompanying: YAML files are the
// configuration of the code beside them.
func (*Provider) Accompanies() bool { return true }
