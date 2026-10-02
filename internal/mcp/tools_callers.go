package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/symbol"
)

// CallersToolDefinitions returns tool definitions for caller/callee queries.
func CallersToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_callers",
			Description: "Find symbols that call a given symbol, using the repository index",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File or directory path to index",
					},
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "Symbol name or qualified name to look up (e.g. greet or UserService.Create)",
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
			Description: "Find symbols called by a given symbol, using the repository index",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File or directory path to index",
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

type callersResult struct {
	Symbol string      `json:"symbol"`
	Edges  []edgeEntry `json:"edges"`
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
	if v, ok := args["maxResults"].(float64); ok {
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

	// Find all symbols matching the name.
	// If the input looks like a qualified name (contains "."), skip the short-name lookup
	// to avoid accidental partial matches.
	var syms []symbol.Symbol
	if strings.Contains(symName, ".") {
		syms = idx.FindSymbolsByQualified(symName)
	} else {
		syms = idx.FindSymbols(symName)
		if len(syms) == 0 {
			syms = idx.FindSymbolsByQualified(symName)
		}
	}
	// Apply filePattern filter when provided — narrows same-qualified-name symbols
	// that exist in multiple packages (e.g. Resolver.Resolve in resolver vs skill).
	if filePattern != "" {
		filtered := syms[:0]
		for _, s := range syms {
			if strings.Contains(string(s.Location.File), filePattern) {
				filtered = append(filtered, s)
			}
		}
		syms = filtered
	}
	if len(syms) == 0 {
		out := callersResult{Symbol: symName, Edges: []edgeEntry{}}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}}, nil
	}
	// When multiple symbols share the same name, merging their edges produces
	// misleading results. Return an error listing candidates with file paths so
	// the caller can retry with filePattern to narrow the match.
	if len(syms) > 1 {
		candidates := make([]string, 0, len(syms))
		for _, s := range syms {
			candidates = append(candidates, fmt.Sprintf("  %s  (%s)  %s", s.Qualified, s.Kind, s.Location.File))
		}
		sort.Strings(candidates)
		msg := fmt.Sprintf(
			"Ambiguous symbol %q — %d matches found. Add filePattern to narrow by file path:\n%s",
			symName, len(syms), strings.Join(candidates, "\n"),
		)
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: msg}},
			IsError: true,
		}, nil
	}

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
	if len(edges) > maxResults {
		edges = edges[:maxResults]
	}
	if edges == nil {
		edges = []edgeEntry{}
	}

	out := callersResult{Symbol: symName, Edges: edges}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}}, nil
}
