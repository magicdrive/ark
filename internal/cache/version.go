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
// ConfidenceCap follows the same rule (PHP: "php-8").
const CurrentSchemaVersion = "2"

// IsCompatible reports whether a CacheKey was written with the current schema.
func IsCompatible(key CacheKey) bool {
	return key.SchemaVersion == CurrentSchemaVersion
}
