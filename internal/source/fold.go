package source

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// FoldPath is the form of a slash path under which a case-insensitive,
// normalization-insensitive file system (macOS's default) opens the same
// file: Unicode NFC, then full case folding (σ, ς and Σ; K and the Kelvin
// sign alike). Two paths of one fold may name one file there; on a
// case-sensitive file system they may name two.
func FoldPath(p string) string {
	return norm.NFC.String(foldCaser.String(norm.NFC.String(p)))
}

var foldCaser = cases.Fold()
