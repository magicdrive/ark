package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// RelationsToolDefinitions returns tool definitions for relation tools.
func RelationsToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_relations",
			Description: "Get everything a symbol calls, uses or reads and everything that does so to it, with reference kind (call, construction, type_use, read, ...), confidence and evidence. Ambiguous references appear as candidate relations, one per symbol and reference kind with its reference count (a deterministic sample of at most 10 per direction; candidateCallers / candidateCallees count the symbols, candidateCallerRelations / candidateCalleeRelations the relations). unattributed counts references into or out of the symbol that may be a repository relation but are not resolved graph edges (0 means no repository relation is missing). Outgoing references no repository symbol can be the target of are counted as unresolved, those proven to refer outside the repository as outsideRepository, and unresolvedReferences lists the outgoing references with no candidate (first 10 in source order; unresolvedReferencesTotal counts them). indexDiagnostics appears when the index has diagnostics (see get_diagnostics): references in code they cover are in no count",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to index (repository root or a subdirectory)",
					},
					"symbolId": symbolIDProperty,
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name or qualified name (e.g. UserService.Create; a namespace prefix is optional)",
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of relations to return",
						"default":     50,
					},
					"filePattern": map[string]interface{}{
						"type":        "string",
						"description": "Narrow an ambiguous symbol name by file-path substring",
					},
				},
				"required": []string{"path", "symbol"},
			},
		},
	}
}

type relationEntry struct {
	Direction  string `json:"direction"` // "calls" or "called_by"
	Name       string `json:"name"`
	Qualified  string `json:"qualified"`
	File       string `json:"file"`
	Kind       string `json:"kind"` // reference kind: call, construction, type_use, read, ...
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence,omitempty"`
	// SourceUnidentified marks a called_by relation from enclosing code that
	// is not one symbol: Qualified is that code's name, which no symbol of
	// File carries or several do. Never attributed to one of them.
	SourceUnidentified bool `json:"sourceUnidentified,omitempty"`
	// References, on a candidate relation, is the number of candidate
	// references it stands for (same symbol, same kind); Evidence is the
	// first one's.
	References int `json:"references,omitempty"`
}

// relationsResult is the get_relations result, in three layers:
//
//   - resolved relations: graph edges (Strong or better, one target) and
//     uniquely resolved references without graph semantics (read, write);
//   - candidate relations (confidence "candidate"): references that name the
//     symbol among several possible targets, or name several possible targets
//     from it — one per other symbol and reference kind, with the number of
//     references it stands for, at most index.MaxCandidateSources per
//     direction, chosen deterministically. CandidateCallers / CandidateCallees
//     count the distinct symbols, CandidateCallerRelations /
//     CandidateCalleeRelations the distinct relations: more relations than
//     listed means some were left out;
//   - relations marked sourceUnidentified: references into the symbol from
//     enclosing code that is no one symbol (resolved ones all listed,
//     candidate ones sampled like the rest);
//   - Unattributed: the references into or out of the symbol that are not
//     resolved graph edges — candidate and unidentified-source relations of
//     graph kinds included — so 0 means the graph relations are complete (see
//     index.Completeness);
//   - Unresolved / OutsideRepository / UnresolvedReferences: the outgoing
//     references that are neither edges nor unattributed, and a sample of
//     every outgoing reference with no candidate (index.UnresolvedSample),
//     as in get_callees.
//
// Total and Truncated appear only when Relations was cut at maxResults.
type relationsResult struct {
	Symbol           string          `json:"symbol"`
	Relations        []relationEntry `json:"relations"`
	CandidateCallers int             `json:"candidateCallers,omitempty"`
	CandidateCallees int             `json:"candidateCallees,omitempty"`
	// Distinct candidate relations (symbol + reference kind) per direction.
	CandidateCallerRelations int  `json:"candidateCallerRelations,omitempty"`
	CandidateCalleeRelations int  `json:"candidateCalleeRelations,omitempty"`
	Unattributed             int  `json:"unattributed"`
	Unresolved               int  `json:"unresolved"`
	OutsideRepository        int  `json:"outsideRepository"`
	Total                    int  `json:"total,omitempty"`
	Truncated                bool `json:"truncated,omitempty"`

	UnresolvedReferences      []unresolvedRefEntry `json:"unresolvedReferences,omitempty"`
	UnresolvedReferencesTotal int                  `json:"unresolvedReferencesTotal,omitempty"`

	// IndexDiagnostics: see callersResult.IndexDiagnostics.
	IndexDiagnostics *index.DiagnosticSummary `json:"indexDiagnostics,omitempty"`
}

func (h *ToolsHandler) getRelations(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	symName, ok := args["symbol"].(string)
	if !ok {
		return nil, fmt.Errorf("symbol parameter is required")
	}
	maxResults := 50
	if v, ok := args["maxResults"].(float64); ok && v > 0 {
		maxResults = int(v)
	}
	filePattern := ""
	if v, ok := args["filePattern"].(string); ok {
		filePattern = v
	}

	fullPath, _, pathErr := h.resolveToolPath(path)
	if pathErr != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: pathErr.Error()}},
			IsError: true,
		}, nil
	}
	// The repository index is the single source of truth shared with
	// get_callers / get_callees / get_context.
	idx, err := h.buildIndex(context.Background(), fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error building index: %v", err)}},
			IsError: true,
		}, nil
	}

	// Resolve exactly one target symbol. A short name matching several symbols
	// must NOT merge their relations into one answer — return ambiguity instead.
	tl, errRes := lookupToolTarget(idx, args, symName, filePattern, path)
	if errRes != nil {
		return errRes, nil
	}
	target := tl.Symbol
	relations, totals := relationsOf(idx, target)
	out := relationsResult{
		Symbol:                   symName,
		CandidateCallers:         totals.callers,
		CandidateCallees:         totals.callees,
		CandidateCallerRelations: totals.callerRelations,
		CandidateCalleeRelations: totals.calleeRelations,
	}
	incoming, outgoing := idx.Unattributed(target.ID)
	out.Unattributed = incoming + outgoing
	un := idx.UnresolvedOutgoing(target.ID)
	out.Unresolved = un.Unresolved
	out.OutsideRepository = un.OutsideRepository
	out.UnresolvedReferences = unresolvedRefEntries(un)
	out.UnresolvedReferencesTotal = un.Total
	if len(relations) > maxResults {
		out.Total = len(relations)
		out.Truncated = true
		relations = relations[:maxResults]
	}
	out.Relations = relations
	out.IndexDiagnostics = indexDiagnosticsNote(idx)
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}}, nil
}

// candidateTotals counts a symbol's candidate relations: distinct symbols
// (sources) and distinct relations, per direction.
type candidateTotals struct {
	callers, callees, callerRelations, calleeRelations int
}

// relationsOf returns target's relations in deterministic order and the
// totals of its candidate relations.
func relationsOf(idx *index.RepositoryIndex, target symbol.Symbol) ([]relationEntry, candidateTotals) {
	relations := []relationEntry{}
	seen := make(map[relationEntry]bool)
	// add records the relation between the target and other: other calls the
	// target (called_by) or the target calls other (calls).
	add := func(direction string, other symbol.SymbolID, kind reference.ReferenceKind, conf resolver.Confidence, ev string, refs int) {
		sym, ok := idx.GetSymbol(other)
		if !ok {
			return
		}
		e := relationEntry{Direction: direction, Name: sym.Name, Qualified: sym.Qualified, File: string(sym.Location.File),
			Kind: string(kind), Confidence: conf.String(), Evidence: ev, References: refs}
		if direction == "called_by" {
			e.Name = target.Name
		}
		if !seen[e] {
			seen[e] = true
			relations = append(relations, e)
		}
	}
	first := func(ev []resolver.ResolutionEvidence) string {
		if len(ev) > 0 {
			return ev[0].Detail
		}
		return ""
	}

	// Resolved graph relations. A caller's relation is its forward edge to
	// the target, which carries the reference kind.
	for _, rev := range idx.GetCallers(target.ID) {
		for _, fwd := range idx.GetCallees(rev.To) {
			if fwd.To == target.ID {
				add("called_by", fwd.From, edgeRefKind(fwd), fwd.Confidence, first(fwd.Evidence), 0)
			}
		}
	}
	for _, fwd := range idx.GetCallees(target.ID) {
		add("calls", fwd.To, edgeRefKind(fwd), fwd.Confidence, first(fwd.Evidence), 0)
	}
	// Resolved relations without graph semantics (read, write, ...).
	for _, r := range idx.NonEdgeRelationsTo(target.ID) {
		add("called_by", r.Source, r.Kind, r.Confidence, first(r.Evidence), 0)
	}
	for _, r := range idx.NonEdgeRelationsFrom(target.ID) {
		add("calls", r.Target, r.Kind, r.Confidence, first(r.Evidence), 0)
	}
	// Candidate relations: ambiguity is evidence, in both directions.
	callers, callees := idx.CandidateCallerSample(target.ID), idx.CandidateCalleeSample(target.ID)
	neIn, neOut := idx.NonEdgeCandidates(target.ID)
	for _, s := range []index.CandidateSample{callers, neIn} {
		for _, r := range s.Relations {
			add("called_by", r.Symbol, r.Kind, r.Confidence, r.Evidence.Detail, r.References)
		}
	}
	for _, s := range []index.CandidateSample{callees, neOut} {
		for _, r := range s.Relations {
			add("calls", r.Symbol, r.Kind, r.Confidence, r.Evidence.Detail, r.References)
		}
	}

	// Relations from enclosing code that is no one symbol: listed by its
	// name and file, never attributed to a symbol.
	unid := idx.UnidentifiedSources(target.ID)
	for _, list := range [][]index.UnidentifiedSourceRelation{unid.Resolved, unid.Candidates} {
		for _, r := range list {
			e := relationEntry{Direction: "called_by", Name: target.Name, Qualified: r.Container, File: string(r.File),
				Kind: string(r.Kind), Confidence: r.Confidence.String(), Evidence: r.Evidence.Detail, SourceUnidentified: true, References: r.References}
			if !seen[e] {
				seen[e] = true
				relations = append(relations, e)
			}
		}
	}

	// Deterministic order.
	sort.Slice(relations, func(i, j int) bool {
		a, b := relations[i], relations[j]
		if a.Direction != b.Direction {
			return a.Direction < b.Direction
		}
		if a.Qualified != b.Qualified {
			return a.Qualified < b.Qualified
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Confidence != b.Confidence {
			return a.Confidence < b.Confidence
		}
		if a.SourceUnidentified != b.SourceUnidentified {
			return !a.SourceUnidentified
		}
		return a.Evidence < b.Evidence
	})

	return relations, candidateTotals{
		callers:         callers.Total + neIn.Total + unid.CandidatesTotal,
		callees:         callees.Total + neOut.Total,
		callerRelations: callers.RelationsTotal + neIn.RelationsTotal + unid.CandidateRelationsTotal,
		calleeRelations: callees.RelationsTotal + neOut.RelationsTotal,
	}
}

// edgeRefKind is the reference kind an edge was built from, or its graph kind
// for an edge that does not record one.
func edgeRefKind(e index.GraphEdge) reference.ReferenceKind {
	if e.RefKind != "" {
		return e.RefKind
	}
	return reference.ReferenceKind(e.Kind)
}
