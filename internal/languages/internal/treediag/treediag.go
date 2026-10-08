// Package treediag turns what a Tree-sitter parse reports about a file into
// language.Diagnostics, the same way for every provider: a parse that yields
// no tree, and the ERROR / MISSING nodes of one that does. The region such a
// node covers is not analyzed as written (a provider skips or partially
// recovers it), so the diagnostic is how a consumer learns the file is not
// fully observed.
//
// It states the parser's verdict and nothing more. In particular it does not
// say the source is invalid: the grammar may reject valid code (real
// repositories show this), and then the diagnostic is still true about
// Ark's analysis — that region is not analyzed — while a "syntax error"
// would be false about the source.
package treediag

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Codes of the diagnostics this package produces (language.Diagnostic.Code).
const (
	CodeParseFailed = "parse_failed" // the parser returned no tree: nothing is extracted
	CodeParseError  = "parse_error"  // an ERROR or MISSING node: the region is not analyzed as written
)

// MaxParseErrors bounds the parse-error diagnostics of one file.
const MaxParseErrors = 5

// ParseFailed is the diagnostic of a parse that returned no tree.
func ParseFailed(file source.FileID, err error) language.Diagnostic {
	return language.Diagnostic{
		Severity: language.SeverityError,
		Code:     CodeParseFailed,
		Message:  "parse failed: " + err.Error(),
		Location: source.Location{File: file},
	}
}

// ParseErrors returns a diagnostic for each of the first MaxParseErrors
// ERROR or MISSING nodes under root, in source order (outermost first). It
// descends only into subtrees that contain an error, iteratively, so its cost
// and depth are bounded by the tree.
func ParseErrors(root *ts.Node, lang *ts.Language, file source.FileID) []language.Diagnostic {
	if root == nil || !root.HasError() {
		return nil
	}
	var out []language.Diagnostic
	stack := []*ts.Node{root}
	for len(stack) > 0 && len(out) < MaxParseErrors {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.IsError() || n.IsMissing() {
			msg := "parse error: this region is not analyzed as written (the parser rejected it; the source itself may be valid)"
			if n.IsMissing() {
				msg = "parse error: the parser expected " + n.Type(lang) + " here; the surrounding code is not analyzed as written (the source itself may be valid)"
			}
			out = append(out, language.Diagnostic{
				Severity: language.SeverityError,
				Code:     CodeParseError,
				Message:  msg,
				Location: location(n, file),
			})
			continue
		}
		if !n.HasError() {
			continue
		}
		for i := n.ChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

func location(n *ts.Node, file source.FileID) source.Location {
	return source.Location{
		File: file,
		Range: source.Range{
			Start: source.Position{Line: n.StartPoint().Row + 1, Column: n.StartPoint().Column + 1},
			End:   source.Position{Line: n.EndPoint().Row + 1, Column: n.EndPoint().Column + 1},
		},
	}
}
