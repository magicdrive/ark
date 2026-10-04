package setup

import "os"

// replaceFile atomically replaces dst with src (both on the same filesystem).
//
// This is the single platform boundary for the final rename step of an atomic
// config write. It is deliberately kept tiny so the surrounding merge logic
// stays platform-neutral (task §13).
//
// On Unix, rename(2) atomically replaces an existing destination.
//
// On Windows, Go's os.Rename maps to MoveFileEx(..., MOVEFILE_REPLACE_EXISTING),
// which also replaces an existing destination on the same volume (the temp file
// is created in the destination's directory, so this holds). The known Windows
// caveat is that the replace fails with a sharing violation if another process
// currently holds the destination open — in that case Ark returns an error and
// leaves the original file intact rather than truncating it.
//
// If a future Windows requirement cannot be met with the standard library, this
// is the one place to add a //go:build-tagged implementation (replace_windows.go)
// without touching the rest of the setup package.
func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
