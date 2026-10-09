package cache

// CurrentSchemaVersion is the cache schema version.
// Increment this to invalidate all existing cache entries on the next run.
//
//	"1": symbols / references / imports / diagnostics.
//	"2": + module bindings, exports, ModuleScoped, ReferenceDraft.ReceiverType.
//
// ReferenceDraft.NameQualified / ReceiverTypeQualified are optional (omitempty)
// and were added without a schema bump: entries written before they existed
// decode with them empty, which is exactly what a provider that does not emit
// them produces. A provider that starts emitting them changes its own output
// and must bump its Provider.CacheVersion (PHP: "php-7"). ReferenceDraft.
// ConfidenceCap and SymbolDraft.Visibility follow the same rule (PHP: "php-9"),
// as does ReferenceDraft.Dynamic (PHP: "php-10") and ReferenceDraft.
// TargetKinds (Go: "go-6"). Changing which references
// carry evidence is an extraction change too (PHP: "php-11"). The Terraform
// fields — SymbolDraft.MemberScope / ParameterScope / MembersOutside,
// ReferenceDraft.IdentityInRepository / NamedArgument and Extraction.
// IdentityOnly — follow the same rule: only
// the Terraform provider emits them, and it has no entries that predate them.
// Diagnostic.Code is optional the same way; every provider that started
// stating codes bumped its CacheVersion (go-3, javascript 2, python 2, ts-3,
// php-12, terraform-5), and again when tsparse began recovering trees the
// production parser route misparsed (go-4, javascript 3, python 3, ts-4,
// php-13, terraform-6), and when it added the forest route and the Go / TypeScript
// extractors began reading ambiguous generic syntax by the languages' rules
// (go-5, javascript 4, python 4, ts-5, php-14, terraform-7), and when those
// readings took only what a file proves (go-6, ts-6).
//
// SymbolIDs are not cached: the index derives them from the cached drafts
// (index.NewFileIndex), so a change of identity scheme (declaration ordinals)
// needs no bump — an entry can never carry an old ID — and a build that finds
// two declarations sharing an ID (index/identity.go) fails the same way warm
// as cold.
const CurrentSchemaVersion = "2"

// IsCompatible reports whether a CacheKey was written with the current schema.
func IsCompatible(key CacheKey) bool {
	return key.SchemaVersion == CurrentSchemaVersion
}
