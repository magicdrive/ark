package search

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Format returns a human-readable summary of the search result.
func Format(r *Result, q Query) string {
	if len(r.Matches) == 0 {
		return "No matches found.\n"
	}

	var sb strings.Builder
	desc := buildQueryDesc(q)
	if r.Truncated {
		fmt.Fprintf(&sb, "Found %d symbols (showing %d) matching %s\n\n", r.TotalCount, len(r.Matches), desc)
	} else {
		fmt.Fprintf(&sb, "Found %d symbol(s) matching %s\n\n", r.TotalCount, desc)
	}

	for _, m := range r.Matches {
		if m.Symbol != nil {
			sym := m.Symbol
			exported := ""
			if sym.Exported {
				exported = " [exported]"
			}
			fmt.Fprintf(&sb, "%s:%d  %s  %s%s\n",
				sym.Location.File, sym.Location.Range.Start.Line,
				sym.Kind, sym.Qualified, exported)
		}
	}
	return sb.String()
}

// FormatJSON returns a JSON encoding of the result.
func FormatJSON(r *Result) ([]byte, error) {
	type jsonMatch struct {
		Kind   string      `json:"kind"`
		Symbol interface{} `json:"symbol,omitempty"`
		File   string      `json:"file"`
		Line   uint32      `json:"line"`
	}
	out := make([]jsonMatch, 0, len(r.Matches))
	for _, m := range r.Matches {
		jm := jsonMatch{
			Kind: string(m.Kind),
			File: string(m.File),
			Line: m.Line,
		}
		if m.Symbol != nil {
			jm.Symbol = m.Symbol
		}
		out = append(out, jm)
	}
	return json.MarshalIndent(map[string]interface{}{
		"totalCount": r.TotalCount,
		"truncated":  r.Truncated,
		"matches":    out,
	}, "", "  ")
}

func buildQueryDesc(q Query) string {
	var parts []string
	if q.Kind != "" {
		parts = append(parts, fmt.Sprintf("kind=%s", q.Kind))
	}
	if q.NamePattern != "" {
		parts = append(parts, fmt.Sprintf("name~%q", q.NamePattern))
	}
	if q.Exported != nil {
		if *q.Exported {
			parts = append(parts, "exported=true")
		} else {
			parts = append(parts, "exported=false")
		}
	}
	if q.CallsName != "" {
		parts = append(parts, fmt.Sprintf("calls~%q", q.CallsName))
	}
	if q.UsesType != "" {
		parts = append(parts, fmt.Sprintf("uses_type~%q", q.UsesType))
	}
	if q.Language != "" {
		parts = append(parts, fmt.Sprintf("language=%s", q.Language))
	}
	if len(parts) == 0 {
		return "(no filters)"
	}
	return strings.Join(parts, ", ")
}
