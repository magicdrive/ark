package v61

import (
	"fmt"
	"regexp"
	"slices"
)

// An Amendment is one intended change of the recommended masking since
// v6.1.0: v6.1.0's pattern at Index (Was, checked against defaultPatterns)
// replaced by Now. Now matches wherever Was matches, at least as far, and
// every match of Now holds one of Triggers: text with no trigger is masked
// exactly as v6.1.0 masked it. (Where a match runs further than Was's, a
// later pattern sees less of the line — see Why.) Amendments are never
// removed; a later change of the same rule is another amendment.
type Amendment struct {
	Version  string   // the Ark version that made the change
	Rule     string   // the built-in rule's stable ID (secrets/rules.go)
	Index    int      // the pattern's index in defaultPatterns
	Was, Now string   // the expression before and after
	Triggers []string // every match of Now holds one of these
	Why      string
}

// Amendments are the intended changes, in order.
var Amendments = []Amendment{
	{
		Version: "6.11.0",
		Rule:    "slack-token",
		Index:   20,
		Was:     `xox[baprs]-([0-9a-zA-Z]{10,48})?`,
		Now: `(?:xoxe\.)?xox[baprs]-(?:(?:[0-9]+-)+(?:[0-9A-Za-z]{20,}|[0-9A-Fa-f]{10,}|[0-9]+)|[0-9A-Za-z]{10,})?` +
			`|(?:xoxe\.)?xapp-[0-9]+-[0-9A-Z]+-[0-9]+-[0-9A-Fa-f]+` +
			`|xoxe-[0-9]+-[0-9A-Za-z]{20,}` +
			`|xoxo-[0-9]+-[0-9]+-[0-9]+-[0-9A-Fa-f]+` +
			`|xwfp-[0-9A-Za-z]{10,}(?:-[0-9A-Za-z]{20,})*`,
		Triggers: []string{"xox", "xapp-", "xwfp-"},
		Why: "v6.1.0 masked a Slack token only to its first \"-\" and at most 48 characters: " +
			"xox" + "b-123456789012-1234567890123-<secret> kept its secret section. The token is " +
			"masked whole, and the app-level, refresh, legacy and workflow tokens too. A word " +
			"glued to a token by \"-\" (xox" + "b-1234567890-password=x) is not the token's: a token's " +
			"last section is long, hex or numeric, so what v6.1.0 masked after it is still masked.",
	},
}

// amendedPatterns are defaultPatterns with every amendment applied.
var amendedPatterns = func() []*regexp.Regexp {
	out := slices.Clone(defaultPatterns)
	for _, a := range Amendments {
		if a.Index < 0 || a.Index >= len(out) || out[a.Index].String() != a.Was {
			panic(fmt.Sprintf("v61: amendment %s (%s): pattern %d is not %q", a.Rule, a.Version, a.Index, a.Was))
		}
		out[a.Index] = regexp.MustCompile(a.Now)
	}
	return out
}()

// AmendedRuleSet is DefaultRuleSet with the amendments applied.
func AmendedRuleSet() *RuleSet {
	r := DefaultRuleSet()
	r.FullMaskPatterns = slices.Clone(amendedPatterns)
	return r
}

// MaskAllInAmended is MaskAllIn with the amendments applied.
func MaskAllInAmended(content string, syntax Syntax) string {
	return maskLinesWith(AmendedRuleSet(), content, syntax, false)
}

// MaskCompactInAmended is MaskCompactIn with the amendments applied.
func MaskCompactInAmended(content string, syntax Syntax) string {
	return maskLinesWith(AmendedRuleSet(), content, syntax, true)
}
