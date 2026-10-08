package context

import (
	"encoding/json"
	"fmt"
	"github.com/magicdrive/ark/internal/index"
	"strings"
)

// Format serialises a Result into a human-readable text block suitable
// for pasting directly into an LLM prompt.
func Format(result *Result) string {
	if result == nil || len(result.Items) == 0 {
		return "(no context items)"
	}

	var sb strings.Builder
	for i, item := range result.Items {
		if i > 0 {
			sb.WriteString("\n")
		}
		file := string(item.Symbol.Location.File)
		start := item.Symbol.Location.Range.Start.Line
		end := item.Symbol.Location.Range.End.Line
		fmt.Fprintf(&sb, "### %s:%d-%d\n", file, start, end)
		fmt.Fprintf(&sb, "Symbol: %s\n", item.Symbol.Qualified)
		fmt.Fprintf(&sb, "Reason: %s\n", item.Reason)
		if item.Confidence > 0 {
			fmt.Fprintf(&sb, "Confidence: %s\n", item.Confidence.String())
		}
		sb.WriteString("\n")
		sb.WriteString(item.Source)
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "\n--- stats: %d/%d items, ~%d tokens (budget %d), unattributed: %d callers, %d callees; unresolved callees: %d, outside repository: %d ---\n",
		result.Stats.SelectedItems, result.Stats.TotalCandidates,
		result.Stats.EstimatedTokens, result.Stats.BudgetTokens,
		result.Stats.UnattributedCallers, result.Stats.UnattributedCallees,
		result.Stats.UnresolvedCallees, result.Stats.OutsideCallees)
	if d := result.IndexDiagnostics; d != nil {
		fmt.Fprintf(&sb, "--- index diagnostics: %d file(s), %d error(s), %d warning(s); code lost to them is in no count above (get_diagnostics) ---\n",
			d.Files, d.Errors, d.Warnings)
	}

	return sb.String()
}

// jsonItem is the JSON representation of an Item.
type jsonItem struct {
	Symbol         string             `json:"symbol"`
	File           string             `json:"file"`
	StartLine      uint32             `json:"startLine"`
	EndLine        uint32             `json:"endLine"`
	Kind           string             `json:"kind"`
	Reason         string             `json:"reason"`
	Confidence     string             `json:"confidence"`
	Score          float64            `json:"score"`
	ScoreBreakdown map[string]float64 `json:"scoreBreakdown,omitempty"`
	Tokens         int                `json:"tokens"`
	Source         string             `json:"source"`
}

type jsonResult struct {
	Items []jsonItem `json:"items"`
	Stats Stats      `json:"stats"`

	IndexDiagnostics *index.DiagnosticSummary `json:"indexDiagnostics,omitempty"`
}

// FormatJSON serialises a Result as JSON.
func FormatJSON(result *Result) ([]byte, error) {
	if result == nil {
		return json.Marshal(jsonResult{Items: []jsonItem{}, Stats: Stats{}})
	}

	out := jsonResult{
		Stats:            result.Stats,
		IndexDiagnostics: result.IndexDiagnostics,
		Items:            make([]jsonItem, len(result.Items)),
	}
	for i, item := range result.Items {
		out.Items[i] = jsonItem{
			Symbol:         item.Symbol.Qualified,
			File:           string(item.Symbol.Location.File),
			StartLine:      item.Symbol.Location.Range.Start.Line,
			EndLine:        item.Symbol.Location.Range.End.Line,
			Kind:           string(item.Symbol.Kind),
			Reason:         item.Reason,
			Confidence:     item.Confidence.String(),
			Score:          item.Score,
			ScoreBreakdown: item.ScoreBreakdown,
			Tokens:         item.Tokens,
			Source:         item.Source,
		}
	}
	return json.MarshalIndent(out, "", "  ")
}
