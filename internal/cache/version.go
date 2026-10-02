package cache

// CurrentSchemaVersion is the cache schema version.
// Increment this to invalidate all existing cache entries on the next run.
const CurrentSchemaVersion = "1"

// IsCompatible reports whether a CacheKey was written with the current schema.
func IsCompatible(key CacheKey) bool {
	return key.SchemaVersion == CurrentSchemaVersion
}
