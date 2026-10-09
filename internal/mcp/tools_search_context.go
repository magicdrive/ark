package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	arkctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/search"
	"github.com/magicdrive/ark/internal/symbol"
)

// search_context: discovery for an agent that does not know a symbol's exact
// name. It ranks the served repository's indexed symbols against a partial
// identifier (search.MatchSymbols) and, within one token budget for the whole
// response, attaches get_context's context for the top candidates.
//
// Contract:
//   - Candidates are search results, not resolution. Each is an independent
//     symbol; none is chosen as "the" answer, same-named symbols are never
//     merged, and the rank is never fed back into target lookup, the resolver
//     or confidence. Context is built from each candidate's SymbolID, never
//     by re-resolving the query text.
//   - The index always covers the whole server root (shared with get_context
//     on "."); path only narrows which symbols are searched, at path-component
//     boundaries.
//   - limit bounds the candidates returned; contextLimit (default 1) bounds
//     how many of them, from rank 1 down, get context. contextLimit only
//     spends budget: it does not mark rank 1 as the answer, and the other
//     candidates keep their rank, ID and location (context "not_requested",
//     reason "context_limit").
//   - maxTokens bounds the whole response text (context.EstimateTokens over
//     the serialized JSON): candidate metadata first, then context for the
//     candidates within contextLimit in rank order while budget remains.
//     Anything left out is marked; the response never exceeds the budget.
//   - includeContext=false, or contextLimit=0, never runs the Context Engine;
//     otherwise it runs at most contextLimit times, once per candidate.

const (
	searchContextDefaultLimit = 5
	// searchContextDefaultContextLimit: context for rank 1 only, by default;
	// the other candidates are still returned.
	searchContextDefaultContextLimit = 1
	searchContextMaxLimit            = 20
	searchContextDefaultMaxTokens    = 4000
	searchContextMaxMaxTokens        = 100000
	// searchContextMinContextTokens is the least remaining budget worth a
	// context build: below it not even a target header fits.
	searchContextMinContextTokens = 32
)

// Context statuses of a search_context result.
const (
	contextIncluded      = "included"
	contextNotRequested  = "not_requested"
	contextOmittedBudget = "omitted_budget"
	contextUnavailable   = "unavailable"

	// reasonContextLimit: a not_requested result beyond contextLimit.
	reasonContextLimit = "context_limit"
	// reasonSharedID: the candidate's SymbolID also names another
	// declaration, so the index cannot attribute context to this one.
	reasonSharedID = "the symbol's ID is shared with another declaration in the same file; context cannot be attributed to this one"
)

// Error kinds of a search_context failure.
const (
	scErrInvalidParameters = "invalid_parameters"
	scErrInvalidQuery      = "invalid_query"
	scErrInvalidPath       = "invalid_path"
	scErrIndexUnavailable  = "index_unavailable"
	scErrBudgetExceeded    = "budget_exceeded"
)

// SearchContextToolDefinitions returns the search_context tool definition.
func SearchContextToolDefinitions() []Tool {
	return []Tool{
		{
			Name: "search_context",
			Description: "Discover symbols from a partial or approximate identifier (e.g. 'auth', 'getUser', 'user_profile', 'Service.create') " +
				"and get token-budgeted context for the top candidates in one call: up to limit candidates (default 5), " +
				"context for the first contextLimit of them (default 1). Use it when you do not know a symbol's exact name; " +
				"use get_context when you do, and find_symbol for a regex over declarations in files. " +
				"Candidates are ranked by how closely the name matches the query (matchType: exact, exact_case_insensitive, prefix, " +
				"word_boundary, qualified, substring) — a rank is not evidence of what any code refers to, and every candidate is a separate symbol: " +
				"check the other candidates before assuming rank 1 is the one you need. For another candidate's context, raise contextLimit or call get_context " +
				"with its qualifiedName and filePattern=its path. maxTokens bounds the whole response; context that does not fit is marked omitted_budget.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": fmt.Sprintf("Identifier, qualified name or part of one (plain text, not a regex; at most %d bytes)", search.MaxQueryBytes),
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Only search symbols in this file or directory (relative to the server root, or absolute inside it). Default: the whole server root",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum number of candidates to return (1-%d)", searchContextMaxLimit),
						"default":     searchContextDefaultLimit,
						"minimum":     1,
						"maximum":     searchContextMaxLimit,
					},
					"contextLimit": map[string]interface{}{
						"type":        "integer",
						"description": fmt.Sprintf("How many candidates, from rank 1 down, get context (0-%d, at most limit). 0 = metadata only. Rank is not correctness: the others are still returned", searchContextMaxLimit),
						"default":     searchContextDefaultContextLimit,
						"minimum":     0,
						"maximum":     searchContextMaxLimit,
					},
					"maxTokens": map[string]interface{}{
						"type":        "integer",
						"description": "Estimated token budget for the whole response (len(text)/4 approximation, as get_context)",
						"default":     searchContextDefaultMaxTokens,
						"minimum":     1,
						"maximum":     searchContextMaxMaxTokens,
					},
					"includeContext": map[string]interface{}{
						"type":        "boolean",
						"description": "Attach context for the top candidates within the budget. false returns candidate metadata only (faster)",
						"default":     true,
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// --- response ---------------------------------------------------------------

type scResponse struct {
	Query string `json:"query"`
	// Path is the searched scope, relative to the server root ("." = all).
	Path            string `json:"path"`
	TotalMatches    int    `json:"totalMatches"`
	ReturnedMatches int    `json:"returnedMatches"`
	// Truncated: fewer candidates are returned than matched (limit or budget).
	Truncated bool `json:"truncated"`
	// DroppedForBudget counts candidates within limit left out because their
	// metadata did not fit the budget.
	DroppedForBudget int `json:"droppedForBudget,omitempty"`
	// ContextLimit is the number of top candidates context was requested for
	// (0 when includeContext is false).
	ContextLimit     int                      `json:"contextLimit"`
	Budget           scBudget                 `json:"budget"`
	Results          []scResult               `json:"results"`
	IndexDiagnostics *index.DiagnosticSummary `json:"indexDiagnostics,omitempty"`
}

type scBudget struct {
	MaxTokens int `json:"maxTokens"`
	// EstimatedTokens is an upper bound of this response's estimated tokens.
	EstimatedTokens int `json:"estimatedTokens"`
}

type scResult struct {
	Rank      int       `json:"rank"`
	MatchType string    `json:"matchType"`
	Symbol    scSymbol  `json:"symbol"`
	Context   scContext `json:"context"`
}

type scSymbol struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualifiedName"`
	Kind          string `json:"kind"`
	Language      string `json:"language"`
	Path          string `json:"path"`
	StartLine     uint32 `json:"startLine"`
	EndLine       uint32 `json:"endLine"`
	Signature     string `json:"signature,omitempty"`
}

type scContext struct {
	Status string          `json:"status"`
	Reason string          `json:"reason,omitempty"`
	Items  []scContextItem `json:"items,omitempty"`
	Stats  *scContextStats `json:"stats,omitempty"`
}

// scContextItem is one get_context item. An item whose source an earlier
// result already shows carries SourceInResult (that result's rank) instead of
// repeating the source.
type scContextItem struct {
	Symbol         string `json:"symbol"`
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	StartLine      uint32 `json:"startLine"`
	EndLine        uint32 `json:"endLine"`
	Reason         string `json:"reason"`
	Confidence     string `json:"confidence"`
	Source         string `json:"source,omitempty"`
	SourceInResult int    `json:"sourceInResult,omitempty"`
}

// scContextStats carries the Context Engine's completeness signals unchanged
// (see context.Stats) plus what was left out for the budget.
type scContextStats struct {
	Candidates int `json:"candidates"`
	// OmittedForBudget: related items left out — by the engine for its
	// allocation, or afterwards to fit the response budget.
	OmittedForBudget    int `json:"omittedForBudget"`
	UnattributedCallers int `json:"unattributedCallers"`
	UnattributedCallees int `json:"unattributedCallees"`
	UnresolvedCallees   int `json:"unresolvedCallees"`
	OutsideCallees      int `json:"outsideCallees"`
}

type scError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func searchContextError(kind, format string, a ...any) *CallToolResult {
	b, _ := marshalCompact(scError{Error: kind, Message: fmt.Sprintf(format, a...)})
	return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}, IsError: true}
}

// marshalCompact encodes v as compact JSON without HTML escaping, so source
// text ("<-", "&&", "<T>") costs no more tokens than it has.
func marshalCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// setCounts derives ReturnedMatches and Truncated from Results.
func (r *scResponse) setCounts() {
	r.ReturnedMatches = len(r.Results)
	r.Truncated = r.ReturnedMatches < r.TotalMatches
}

func responseTokens(r *scResponse) int {
	b, err := marshalCompact(r)
	if err != nil {
		return math.MaxInt
	}
	return arkctx.EstimateTokens(string(b))
}

// --- parameters ---------------------------------------------------------------

// intParam reads an optional integer argument within [lo, hi].
func intParam(args map[string]interface{}, name string, def, lo, hi int) (int, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return def, nil
	}
	var f float64
	switch v := raw.(type) {
	case float64: // JSON numbers
		f = v
	case int:
		f = float64(v)
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if f != math.Trunc(f) {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if f < float64(lo) || f > float64(hi) {
		return 0, fmt.Errorf("%s must be between %d and %d, got %v", name, lo, hi, f)
	}
	return int(f), nil
}

func boolParam(args map[string]interface{}, name string, def bool) (bool, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return def, nil
	}
	b, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return b, nil
}

// --- handler --------------------------------------------------------------------

// contextBuild builds one candidate's context. It is the Context Engine unless
// a test replaces it (ToolsHandler.buildContext).
func (h *ToolsHandler) contextBuild(idx *index.RepositoryIndex, req arkctx.Request) (*arkctx.Result, error) {
	if h.buildContext != nil {
		return h.buildContext(idx, h.rootDir, req)
	}
	return arkctx.New(idx, h.rootDir).Build(context.Background(), req)
}

func (h *ToolsHandler) searchContext(args map[string]interface{}) (*CallToolResult, error) {
	rawQuery, ok := args["query"].(string)
	if !ok {
		return searchContextError(scErrInvalidQuery, "query is required and must be a string"), nil
	}
	query, err := search.NormalizeQuery(rawQuery)
	if err != nil {
		return searchContextError(scErrInvalidQuery, "%v", err), nil
	}
	limit, err := intParam(args, "limit", searchContextDefaultLimit, 1, searchContextMaxLimit)
	if err != nil {
		return searchContextError(scErrInvalidParameters, "%v", err), nil
	}
	maxTokens, err := intParam(args, "maxTokens", searchContextDefaultMaxTokens, 1, searchContextMaxMaxTokens)
	if err != nil {
		return searchContextError(scErrInvalidParameters, "%v", err), nil
	}
	contextLimit, err := intParam(args, "contextLimit", searchContextDefaultContextLimit, 0, searchContextMaxLimit)
	if err != nil {
		return searchContextError(scErrInvalidParameters, "%v", err), nil
	}
	if contextLimit > limit {
		return searchContextError(scErrInvalidParameters, "contextLimit (%d) must not exceed limit (%d)", contextLimit, limit), nil
	}
	includeContext, err := boolParam(args, "includeContext", true)
	if err != nil {
		return searchContextError(scErrInvalidParameters, "%v", err), nil
	}
	if !includeContext {
		contextLimit = 0 // includeContext=false always wins
	}
	scope := "."
	if raw, present := args["path"]; present && raw != nil {
		p, ok := raw.(string)
		if !ok {
			return searchContextError(scErrInvalidParameters, "path must be a string"), nil
		}
		if p != "" {
			scope = p
		}
	}
	scopeRel, err := h.searchScope(scope)
	if err != nil {
		return searchContextError(scErrInvalidPath, "%v", err), nil
	}

	idx, err := h.buildIndex(context.Background(), h.rootDir)
	if err != nil {
		return searchContextError(scErrIndexUnavailable, "building the index failed: %v", err), nil
	}

	var syms []symbol.Symbol
	for _, f := range idx.Files() {
		if !inScope(string(f), scopeRel) {
			continue
		}
		syms = append(syms, idx.SymbolsByFile(f)...)
	}
	matches := search.MatchSymbols(query, syms)
	// A SymbolID is derived from (language, file, kind, qualified name), so
	// two such declarations in one file share it and the index merges them.
	// Their context would mix both: it is never built for them.
	idCount := make(map[symbol.SymbolID]int, len(syms))
	for _, s := range syms {
		idCount[s.ID]++
	}

	resp := &scResponse{
		Query:        query,
		Path:         scopeRel,
		TotalMatches: len(matches),
		ContextLimit: contextLimit,
		Budget:       scBudget{MaxTokens: maxTokens, EstimatedTokens: maxTokens},
		Results:      []scResult{},
	}
	if s := idx.DiagnosticSummary(); !s.Empty() {
		resp.IndexDiagnostics = &s
	}
	for i, m := range matches {
		if i == limit {
			break
		}
		// Within contextLimit, omitted_budget holds the place until a context
		// fits; beyond it, the candidate is metadata only by request.
		ctx := scContext{Status: contextOmittedBudget}
		switch {
		case !includeContext:
			ctx = scContext{Status: contextNotRequested}
		case i >= contextLimit:
			ctx = scContext{Status: contextNotRequested, Reason: reasonContextLimit}
		case idCount[m.Symbol.ID] > 1:
			ctx = scContext{Status: contextUnavailable, Reason: reasonSharedID}
		}
		resp.Results = append(resp.Results, scResult{
			Rank:      i + 1,
			MatchType: string(m.Type),
			Symbol:    toSCSymbol(m.Symbol),
			Context:   ctx,
		})
	}

	// Candidate metadata comes first: drop from the end until it fits. The
	// counts are part of the measured text, so they are kept current.
	resp.setCounts()
	for responseTokens(resp) > maxTokens && len(resp.Results) > 0 {
		resp.Results = resp.Results[:len(resp.Results)-1]
		resp.DroppedForBudget++
		resp.setCounts()
	}
	if n := responseTokens(resp); n > maxTokens {
		return searchContextError(scErrBudgetExceeded,
			"maxTokens %d is too small for even an empty result (about %d tokens); raise maxTokens", maxTokens, n), nil
	}

	if contextLimit > 0 {
		h.attachContexts(idx, resp, maxTokens, contextLimit)
	}

	// EstimatedTokens was held at maxTokens while fitting, so the size measured
	// now bounds the final text, whose number has no more digits.
	resp.Budget.EstimatedTokens = responseTokens(resp)
	b, err := marshalCompact(resp)
	if err != nil {
		return nil, err
	}
	return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}}, nil
}

// searchScope resolves the path argument through the common path gate and
// returns it relative to the indexed root, in the index's FileID form. The
// index is built over the symlink-resolved root, so the scope is taken from
// the resolved forms too.
func (h *ToolsHandler) searchScope(p string) (string, error) {
	full, _, err := h.resolveToolPath(p)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(full); err != nil {
		return "", fmt.Errorf("path %q does not exist", p)
	}
	realFull, err1 := filepath.EvalSymlinks(full)
	realRoot, err2 := filepath.EvalSymlinks(h.rootDir)
	if err1 != nil || err2 != nil {
		return "", fmt.Errorf("path %q cannot be resolved", p)
	}
	rel, ok := relInside(realRoot, realFull)
	if !ok {
		return "", outsideRootError(p, h.rootDir)
	}
	return filepath.ToSlash(rel), nil
}

// inScope reports whether file (a FileID) is scope or lies below it, at a
// path-component boundary: "internal/auth" holds "internal/auth/x.go" but not
// "internal/authz/x.go".
func inScope(file, scope string) bool {
	if scope == "." {
		return true
	}
	file = filepath.ToSlash(file)
	return file == scope || strings.HasPrefix(file, scope+"/")
}

func toSCSymbol(s symbol.Symbol) scSymbol {
	return scSymbol{
		ID:            string(s.ID),
		Name:          s.Name,
		QualifiedName: s.Qualified,
		Kind:          string(s.Kind),
		Language:      s.Language,
		Path:          filepath.ToSlash(string(s.Location.File)),
		StartLine:     s.Location.Range.Start.Line,
		EndLine:       s.Location.Range.End.Line,
		Signature:     s.Signature,
	}
}

// attachContexts builds context for the first contextLimit results in rank
// order (the others stay metadata only), each from its
// own SymbolID and each at most once, giving every build what the response
// has left. A context is kept only as far as the whole response still fits:
// related items are dropped from the end of the engine's ranking, and if even
// the target does not fit the result stays omitted_budget.
func (h *ToolsHandler) attachContexts(idx *index.RepositoryIndex, resp *scResponse, maxTokens, contextLimit int) {
	shownIn := map[symbol.SymbolID]int{} // symbol → rank of the result showing its source
	for i := range min(contextLimit, len(resp.Results)) {
		r := &resp.Results[i]
		if r.Context.Status != contextOmittedBudget {
			continue // decided before any build (shared ID)
		}
		remaining := maxTokens - responseTokens(resp)
		if remaining < searchContextMinContextTokens {
			continue
		}
		res, err := h.contextBuild(idx, arkctx.Request{Target: symbol.SymbolID(r.Symbol.ID), MaxTokens: remaining})
		if err != nil {
			r.Context = scContext{Status: contextUnavailable, Reason: err.Error()}
			continue
		}
		if len(res.Items) == 0 {
			r.Context = scContext{Status: contextUnavailable, Reason: "the symbol's source could not be read"}
			continue
		}
		if res.Stats.TargetTruncated {
			continue // the target alone exceeds what is left
		}
		items := make([]scContextItem, 0, len(res.Items))
		ids := make([]symbol.SymbolID, 0, len(res.Items))
		target := -1
		for _, it := range res.Items {
			ci := scContextItem{
				Symbol:     it.Symbol.Qualified,
				Kind:       string(it.Symbol.Kind),
				Path:       filepath.ToSlash(string(it.Symbol.Location.File)),
				StartLine:  it.Symbol.Location.Range.Start.Line,
				EndLine:    it.Symbol.Location.Range.End.Line,
				Reason:     it.Reason,
				Confidence: it.Confidence.String(),
				Source:     it.Source,
			}
			if rank, ok := shownIn[it.Symbol.ID]; ok {
				ci.Source, ci.SourceInResult = "", rank
			}
			if it.Reason == "target" {
				target = len(items)
			}
			items = append(items, ci)
			ids = append(ids, it.Symbol.ID)
		}
		// The context must be about this candidate: its target is the
		// candidate's own declaration, not another one behind the same ID.
		if target < 0 || items[target].Path != r.Symbol.Path || items[target].StartLine != r.Symbol.StartLine {
			r.Context = scContext{Status: contextUnavailable, Reason: "the context the index returned is not about this declaration"}
			continue
		}
		stats := &scContextStats{
			Candidates:          res.Stats.TotalCandidates,
			OmittedForBudget:    res.Stats.TruncatedItems,
			UnattributedCallers: res.Stats.UnattributedCallers,
			UnattributedCallees: res.Stats.UnattributedCallees,
			UnresolvedCallees:   res.Stats.UnresolvedCallees,
			OutsideCallees:      res.Stats.OutsideCallees,
		}
		r.Context = scContext{Status: contextIncluded, Items: items, Stats: stats}
		for responseTokens(resp) > maxTokens {
			drop := len(r.Context.Items) - 1
			if drop == target {
				drop--
			}
			if drop < 0 {
				break
			}
			r.Context.Items = append(r.Context.Items[:drop], r.Context.Items[drop+1:]...)
			ids = append(ids[:drop], ids[drop+1:]...)
			if drop < target {
				target--
			}
			stats.OmittedForBudget++
		}
		if responseTokens(resp) > maxTokens {
			r.Context = scContext{Status: contextOmittedBudget}
			continue
		}
		for j, it := range r.Context.Items {
			if it.SourceInResult == 0 {
				if _, ok := shownIn[ids[j]]; !ok {
					shownIn[ids[j]] = r.Rank
				}
			}
		}
	}
}
