package terraform

import (
	"path"
	"strings"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A Terraform module's overview is what it builds (language.MapHinter): its
// managed and ephemeral resources, the modules it calls and the data sources
// it reads come before its outputs, provider configurations and input
// variables; locals, terraform and check blocks are detail. Exported marks a
// module's interface (variables and outputs), which is not what matters to
// its architecture.
var _ language.MapHinter = (*Provider)(nil)

// MapRole implements language.MapHinter.
func (p *Provider) MapRole(kind symbol.SymbolKind, name string) language.MapRole {
	switch {
	case kind == symbol.KindResource:
		return language.MapRoleBuilt
	case kind == symbol.KindModule:
		return language.MapRoleComposed
	case kind == symbol.KindDataSource:
		return language.MapRoleRead
	case kind == symbol.KindOutput:
		return language.MapRoleExposed
	case kind == symbol.KindConfiguration && strings.HasPrefix(name, "provider."):
		return language.MapRoleConfigured
	case kind == symbol.KindConfiguration:
		return language.MapRoleDetail // terraform, check
	case kind == symbol.KindVariable && strings.HasPrefix(name, "var."):
		return language.MapRoleParameter
	case kind == symbol.KindVariable && strings.HasPrefix(name, "local."):
		return language.MapRoleDetail
	}
	return language.MapRoleUnspecified
}

// DeclaredType implements language.MapHinter: a resource's type
// (ResourceAddress), "data.TYPE" for a data source and "ephemeral.TYPE" for
// an ephemeral resource, so the three modes of one type stay apart.
func (p *Provider) DeclaredType(kind symbol.SymbolKind, name string) string {
	mode, typ, _, ok := ResourceAddress(kind, name)
	switch {
	case !ok:
		return ""
	case mode == ModeData:
		return "data." + typ
	case mode == ModeEphemeral:
		return "ephemeral." + typ
	}
	return typ
}

// MemberScopeDir implements language.MapHinter. A module call whose source is
// a local directory states its child's outputs as "DIR/output." ("output."
// for the root; moduleMembers), DIR clean, relative and inside the root; any
// other source states no scope, and a scope of any other form names nothing.
func (p *Provider) MemberScopeDir(scope string) (string, bool) {
	if scope == "output." {
		return "", true
	}
	dir, ok := strings.CutSuffix(scope, "/output.")
	if !ok || dir == "." || path.IsAbs(dir) || path.Clean(dir) != dir || dir == ".." || strings.HasPrefix(dir, "../") {
		return "", false
	}
	return dir, true
}
