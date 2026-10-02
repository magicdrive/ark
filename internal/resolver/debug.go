package resolver

import (
	"fmt"
	"strings"
)

// ResolutionDebug is a human-readable explanation of a Resolution, intended
// for debug output, CLI diagnostics, and optional MCP verbose mode.
// It is never included in normal analysis output.
type ResolutionDebug struct {
	// Reference is the raw reference name that was resolved.
	Reference string
	// Confidence is the overall confidence level.
	Confidence string
	// Unique is true when there is exactly one strong candidate.
	Unique bool
	// Candidates lists each candidate with its evidence.
	Candidates []CandidateDebug
}

// CandidateDebug describes one candidate in a resolution result.
type CandidateDebug struct {
	Qualified  string
	File       string
	Kind       string
	Confidence string
	Evidence   []string
}

// DebugResolution converts a Resolution into a human-readable debug form.
func DebugResolution(res Resolution) ResolutionDebug {
	d := ResolutionDebug{
		Reference:  res.ReferenceName,
		Confidence: res.Confidence.String(),
		Unique:     res.HasUniqueTarget(),
		Candidates: make([]CandidateDebug, len(res.Candidates)),
	}
	for i, c := range res.Candidates {
		ev := make([]string, len(c.Evidence))
		for j, e := range c.Evidence {
			if e.Detail != "" {
				ev[j] = fmt.Sprintf("%s: %s", e.Kind, e.Detail)
			} else {
				ev[j] = string(e.Kind)
			}
		}
		d.Candidates[i] = CandidateDebug{
			Qualified:  c.Qualified,
			File:       string(c.File),
			Kind:       string(c.Kind),
			Confidence: c.Confidence.String(),
			Evidence:   ev,
		}
	}
	return d
}

// FormatResolution returns a multi-line human-readable resolution explanation.
func FormatResolution(res Resolution) string {
	d := DebugResolution(res)
	var sb strings.Builder
	unique := "ambiguous"
	if d.Unique {
		unique = "unique"
	}
	fmt.Fprintf(&sb, "reference: %q  confidence: %s  (%s, %d candidate(s))\n",
		d.Reference, d.Confidence, unique, len(d.Candidates))
	for _, c := range d.Candidates {
		fmt.Fprintf(&sb, "  → %s (%s) in %s  [%s]\n", c.Qualified, c.Kind, c.File, c.Confidence)
		for _, e := range c.Evidence {
			fmt.Fprintf(&sb, "      evidence: %s\n", e)
		}
	}
	return sb.String()
}
