package source

// FileID is a repository-relative path used as a stable file identifier.
type FileID string

// Position is a 1-based line/column coordinate within a file.
type Position struct {
	Line   uint32
	Column uint32
}

// Range is a half-open [Start, End) region within a file.
type Range struct {
	Start Position
	End   Position
}

// Location combines a file identity with a source range.
type Location struct {
	File  FileID
	Range Range
}
