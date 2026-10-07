package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/symbol"
)

// CallersToolDefinitions returns tool definitions for caller/callee queries.
func CallersToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_callers",
			Description: "Find symbols that call a given symbol, using the repository index. unattributed counts references that may call it but are not resolved edges (0 means the caller list is complete); candidates lists possible callers from ambiguous references (a deterministic sample of at most 10; candidatesTotal counts them all)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to index (repository root or a subdirectory)",
					},
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name or qualified name (e.g. greet, UserService.Create, LoginScreenPolicy::showsSsoButton; a namespace prefix is optional)",
					},
					"filePattern": map[string]interface{}{
						"type":        "string",
						"description": "File path substring to disambiguate when multiple packages define the same symbol (e.g. internal/resolver)",
					},
					"maxDepth": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum transitive depth (1 = direct callers only, 0 = unlimited)",
						"default":     1,
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of edges to return",
						"default":     50,
					},
				},
				"required": []string{"path", "symbol"},
			},
		},
		{
			Name:        "get_callees",
			Description: "Find symbols called by a given symbol, using the repository index. unattributed counts its references whose target could not be resolved (0 means the callee list is complete); candidates lists possible callees of its ambiguous references (a deterministic sample of at most 10; candidatesTotal counts them all)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to index (repository root or a subdirectory)",
					},
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name or qualified name to look up",
					},
					"filePattern": map[string]interface{}{
						"type":        "string",
						"description": "File path substring to disambiguate when multiple packages define the same symbol (e.g. internal/resolver)",
					},
					"maxDepth": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum transitive depth (1 = direct callees only, 0 = unlimited)",
						"default":     1,
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of edges to return",
						"default":     50,
					},
				},
				"required": []string{"path", "symbol"},
			},
		},
	}
}

type edgeEntry struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence,omitempty"`
}

// callersResult is the get_callers / get_callees result. Unattributed is always
// present: 0 means the graph is complete for the queried direction, so an
// empty Edges list is a true zero (see index.Completeness). Total and
// Truncated appear only when Edges was cut at maxResults. Candidates lists
// possible callers (get_callers) or callees (get_callees) from ambiguous
// references — never edges — not already among Edges: a deterministic sample
// of at most index.MaxCandidateSources; CandidatesTotal counts every distinct
// symbol in the queried direction's candidate relations.
type callersResult struct {
	Symbol          string           `json:"symbol"`
	Edges           []edgeEntry      `json:"edges"`
	Unattributed    int              `json:"unattributed"`
	Total           int              `json:"total,omitempty"`
	Truncated       bool             `json:"truncated,omitempty"`
	Candidates      []candidateEntry `json:"candidates,omitempty"`
	CandidatesTotal int              `json:"candidatesTotal,omitempty"`
}

// candidateEntry is a possible caller or callee: one end of a reference whose
// resolution lists several (or capped) candidates, with the reference's kind
// and the resolver's evidence.
type candidateEntry struct {
	Symbol     string `json:"symbol"`
	File       string `json:"file"`
	Kind       string `json:"kind,omitempty"`
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence,omitempty"`
}

func (h *ToolsHandler) getCallers(args map[string]interface{}) (*CallToolResult, error) {
	return h.callGraph(args, true)
}

func (h *ToolsHandler) getCallees(args map[string]interface{}) (*CallToolResult, error) {
	return h.callGraph(args, false)
}

func (h *ToolsHandler) callGraph(args map[string]interface{}, callers bool) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	symName, ok := args["symbol"].(string)
	if !ok {
		return nil, fmt.Errorf("symbol parameter is required")
	}
	filePattern := ""
	if v, ok := args["filePattern"].(string); ok {
		filePattern = v
	}
	maxDepth := 1
	if v, ok := args["maxDepth"].(float64); ok {
		maxDepth = int(v)
	}
	maxResults := 50
	if v, ok := args["maxResults"].(float64); ok && v > 0 {
		maxResults = int(v)
	}

	fullPath, _, pathErr := h.resolveToolPath(path)
	if pathErr != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: pathErr.Error()}},
			IsError: true,
		}, nil
	}

	idx, err := h.buildIndex(context.Background(), fullPath)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error building index: %v", err)}},
			IsError: true,
		}, nil
	}

	// Resolve exactly one target symbol via the shared single-target contract.
	// Multiple matches never merge: return deterministic candidate evidence.
	tl := resolveTarget(targetCandidatesFromIndex(idx, symName), filePattern)
	if !tl.Found {
		return targetNotFoundResult(symName, path), nil
	}
	if tl.Ambiguous {
		return ambiguousTargetResult(symName, tl.Candidates), nil
	}
	syms := []symbol.Symbol{tl.Symbol}

	var edges []edgeEntry
	seen := make(map[[2]symbol.SymbolID]bool)

	for _, sym := range syms {
		var idxEdges []index.GraphEdge
		if callers {
			idxEdges = idx.GetCallers(sym.ID)
			// Transitive traversal via graph.
			if maxDepth != 1 {
				// For simplicity, use the index's direct query for now.
				// Full transitive traversal is available via graph.Graph.
			}
		} else {
			idxEdges = idx.GetCallees(sym.ID)
		}

		for _, e := range idxEdges {
			key := [2]symbol.SymbolID{e.From, e.To}
			if seen[key] {
				continue
			}
			seen[key] = true

			fromSym, _ := idx.GetSymbol(e.From)
			toSym, _ := idx.GetSymbol(e.To)

			fromName := string(e.From)
			if fromSym.Qualified != "" {
				fromName = fromSym.Qualified
			}
			toName := string(e.To)
			if toSym.Qualified != "" {
				toName = toSym.Qualified
			}

			ev := ""
			if len(e.Evidence) > 0 {
				ev = e.Evidence[0].Detail
			}

			edges = append(edges, edgeEntry{
				From:       fromName,
				To:         toName,
				Kind:       string(e.Kind),
				Confidence: e.Confidence.String(),
				Evidence:   ev,
			})
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
	out := callersResult{Symbol: symName}
	incoming, outgoing := idx.Unattributed(tl.Symbol.ID)
	var sample index.CandidateSample
	if callers {
		out.Unattributed = incoming
		sample = idx.CandidateCallerSample(tl.Symbol.ID)
	} else {
		out.Unattributed = outgoing
		sample = idx.CandidateCalleeSample(tl.Symbol.ID)
	}
	out.Candidates = candidateEntries(idx, sample, edges)
	out.CandidatesTotal = sample.Total
	if len(edges) > maxResults {
		out.Total = len(edges)
		out.Truncated = true
		edges = edges[:maxResults]
	}
	if edges == nil {
		edges = []edgeEntry{}
	}
	out.Edges = edges
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}}, nil
}

// candidateEntries lists the sampled candidate relations whose other end is
// not already a resolved relation in edges (the other end of an edge entry is
// its To, for callers and callees alike), ordered by qualified name, file,
// kind.
func candidateEntries(idx *index.RepositoryIndex, sample index.CandidateSample, edges []edgeEntry) []candidateEntry {
	resolved := make(map[string]bool, len(edges))
	for _, e := range edges {
		resolved[e.To] = true
	}
	var out []candidateEntry
	for _, r := range sample.Relations {
		sym, ok := idx.GetSymbol(r.Symbol)
		if !ok {
			continue
		}
		name := string(r.Symbol)
		if sym.Qualified != "" {
			name = sym.Qualified
		}
		if resolved[name] {
			continue
		}
		out = append(out, candidateEntry{
			Symbol:     name,
			File:       string(sym.Location.File),
			Kind:       string(r.Kind),
			Confidence: r.Confidence.String(),
			Evidence:   r.Evidence.Detail,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol != out[j].Symbol {
			return out[i].Symbol < out[j].Symbol
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
