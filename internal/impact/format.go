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

	fmt.Fprintf(&b, "Unattributed references: %d\n\n", r.Unattributed)

	fmt.Fprintf(&b, "Affected files (%d):\n", len(r.AffectedFiles))
	for _, f := range r.AffectedFiles {
		fmt.Fprintf(&b, "  %s\n", f)
	}

	un := r.UnresolvedCallees
	fmt.Fprintf(&b, "\nUnresolved outgoing references: %d unresolved, %d outside repository\n",
		un.Unresolved, un.OutsideRepository)
	for _, ref := range un.References {
		recv := ""
		if ref.ReceiverExpr != "" {
			recv = ", receiver " + ref.ReceiverExpr
		}
		fmt.Fprintf(&b, "  %q (%s) at %s:%d  [%s%s]\n",
			ref.Name, ref.Kind, ref.Location.File, ref.Location.Range.Start.Line, ref.Reason, recv)
	}
	if un.Truncated() {
		fmt.Fprintf(&b, "  ... and %d more\n", un.Total-len(un.References))
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
	type unresolvedRef struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Receiver string `json:"receiver,omitempty"`
		File     string `json:"file"`
		Line     uint32 `json:"line"`
		Reason   string `json:"reason"`
	}
	type out struct {
		Target        string   `json:"target"`
		TargetFile    string   `json:"target_file"`
		TargetLine    uint32   `json:"target_line"`
		Entries       []entry  `json:"entries"`
		AffectedFiles []string `json:"affected_files"`
		Unattributed  int      `json:"unattributed"`
		// The target's outgoing references with no candidate
		// (index.UnresolvedSample): unresolved_count those no repository
		// symbol can be the target of, outside_repository_count those proven
		// external; unresolved_references the first of all of them.
		UnresolvedCount           int             `json:"unresolved_count"`
		OutsideRepositoryCount    int             `json:"outside_repository_count"`
		UnresolvedReferences      []unresolvedRef `json:"unresolved_references,omitempty"`
		UnresolvedReferencesTotal int             `json:"unresolved_references_total,omitempty"`
	}

	o := out{
		Target:       r.Target.Qualified,
		TargetFile:   string(r.Target.Location.File),
		TargetLine:   r.Target.Location.Range.Start.Line,
		Unattributed: r.Unattributed,

		UnresolvedCount:           r.UnresolvedCallees.Unresolved,
		OutsideRepositoryCount:    r.UnresolvedCallees.OutsideRepository,
		UnresolvedReferencesTotal: r.UnresolvedCallees.Total,
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
	for _, ref := range r.UnresolvedCallees.References {
		o.UnresolvedReferences = append(o.UnresolvedReferences, unresolvedRef{
			Name:     ref.Name,
			Kind:     string(ref.Kind),
			Receiver: ref.ReceiverExpr,
			File:     string(ref.Location.File),
			Line:     ref.Location.Range.Start.Line,
			Reason:   string(ref.Reason),
		})
	}
	for _, f := range r.AffectedFiles {
		o.AffectedFiles = append(o.AffectedFiles, string(f))
	}
	return json.MarshalIndent(o, "", "  ")
}
