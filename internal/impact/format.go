package impact

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Format returns a human-readable impact report.
func Format(r *ImpactResult) string {
	if r == nil {
		return "(no result)"
	}
	var b strings.Builder

	fmt.Fprintf(&b, "Impact analysis: %s\n\n", r.Target.Qualified)

	fmt.Fprintf(&b, "Target:\n  %s  %s:%d\n\n",
		r.Target.Qualified,
		r.Target.Location.File,
		r.Target.Location.Range.Start.Line)

	writeSection := func(cat Category, label string) {
		var entries []ImpactEntry
		for _, e := range r.Entries {
			if e.Category == cat {
				entries = append(entries, e)
			}
		}
		fmt.Fprintf(&b, "%s:\n", label)
		if len(entries) == 0 {
			fmt.Fprintf(&b, "  (none)\n")
		} else {
			for _, e := range entries {
				fmt.Fprintf(&b, "  %-40s %s:%d  [%s]\n",
					e.Symbol.Qualified,
					e.Symbol.Location.File,
					e.Symbol.Location.Range.Start.Line,
					e.Confidence.String())
			}
		}
		fmt.Fprintln(&b)
	}

	writeSection(CategoryDirectDependent, "Direct dependents (callers)")
	writeSection(CategoryDirectDependency, "Direct dependencies (callees)")
	writeSection(CategoryTest, "Tests")
	writeSection(CategoryTransitiveDependent, "Transitive dependents")
	writeSection(CategoryPossibleDependent, "Possible dependents (low confidence)")

	fmt.Fprintf(&b, "Affected files (%d):\n", len(r.AffectedFiles))
	for _, f := range r.AffectedFiles {
		fmt.Fprintf(&b, "  %s\n", f)
	}

	if len(r.Unresolved) > 0 {
		fmt.Fprintf(&b, "\nUnresolved references (%d):\n", len(r.Unresolved))
		for _, ref := range r.Unresolved {
			fmt.Fprintf(&b, "  %q at %s:%d  (no candidates found)\n",
				ref.Name,
				ref.Location.File,
				ref.Location.Range.Start.Line)
		}
	}

	return b.String()
}

// FormatJSON returns a JSON-encoded impact result.
func FormatJSON(r *ImpactResult) ([]byte, error) {
	type entry struct {
		Symbol     string `json:"symbol"`
		File       string `json:"file"`
		Line       uint32 `json:"line"`
		Category   string `json:"category"`
		Confidence string `json:"confidence"`
		Distance   int    `json:"distance"`
	}
	type out struct {
		Target        string   `json:"target"`
		TargetFile    string   `json:"target_file"`
		TargetLine    uint32   `json:"target_line"`
		Entries       []entry  `json:"entries"`
		AffectedFiles []string `json:"affected_files"`
		UnresolvedCount int    `json:"unresolved_count"`
	}

	o := out{
		Target:        r.Target.Qualified,
		TargetFile:    string(r.Target.Location.File),
		TargetLine:    r.Target.Location.Range.Start.Line,
		UnresolvedCount: len(r.Unresolved),
	}
	for _, e := range r.Entries {
		o.Entries = append(o.Entries, entry{
			Symbol:     e.Symbol.Qualified,
			File:       string(e.Symbol.Location.File),
			Line:       e.Symbol.Location.Range.Start.Line,
			Category:   string(e.Category),
			Confidence: e.Confidence.String(),
			Distance:   e.Distance,
		})
	}
	for _, f := range r.AffectedFiles {
		o.AffectedFiles = append(o.AffectedFiles, string(f))
	}
	return json.MarshalIndent(o, "", "  ")
}
