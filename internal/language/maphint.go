package language

import "github.com/magicdrive/ark/internal/symbol"

// MapRole is a declaration's place in an overview of its unit (a package, a
// module): what a reader who orients in the repository should see first. It
// is language knowledge, stated by a provider (MapHinter); the repository map
// ranks by it without knowing the language.
type MapRole int

const (
	// MapRoleUnspecified: no role stated; the declaration is mapped as any
	// language's, by exportedness.
	MapRoleUnspecified MapRole = iota
	// MapRoleBuilt is what the unit builds or defines (a Terraform resource).
	MapRoleBuilt
	// MapRoleComposed is another unit it composes (a Terraform module call).
	MapRoleComposed
	// MapRoleRead is existing state it reads (a Terraform data source).
	MapRoleRead
	// MapRoleExposed is what it exposes to its users (a Terraform output).
	MapRoleExposed
	// MapRoleConfigured configures something it uses (a Terraform provider).
	MapRoleConfigured
	// MapRoleParameter is an input it takes (a Terraform variable).
	MapRoleParameter
	// MapRoleDetail is internal detail, for verbose overviews only (a
	// Terraform local value, a terraform block).
	MapRoleDetail
)

// MapHinter is implemented by a provider whose declarations an overview
// should rank by what they are rather than by exportedness — a language in
// which Exported marks an interface (Terraform: variables and outputs) but
// not what matters to the unit's architecture. Every method answers from a
// symbol this provider extracted (its Kind, its Name, the MemberScope it
// stated) and from nothing else.
type MapHinter interface {
	// MapRole returns a declaration's role.
	MapRole(kind symbol.SymbolKind, name string) MapRole
	// DeclaredType returns the type of the instance a declaration declares
	// — a Terraform resource's type, "data.TYPE" for a data source — or ""
	// when it declares none. Declarations of one type are alike to a
	// reader: an overview shows one of each before a second of any.
	DeclaredType(kind symbol.SymbolKind, name string) string
	// MemberScopeDir returns the repository-relative directory, slash
	// separated ("" for the root), of the unit whose declarations a
	// MemberScope this provider stated names: the unit a declaration
	// composes, statically. ok is false for a scope that names none.
	MemberScopeDir(scope string) (dir string, ok bool)
}
