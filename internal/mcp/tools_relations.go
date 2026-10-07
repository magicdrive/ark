package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// RelationsToolDefinitions returns tool definitions for relation tools.
func RelationsToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "get_relations",
			Description: "Get symbols that a given symbol calls or is called by, resolved from source",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File or directory path to analyse",
					},
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "Qualified symbol name to inspect (e.g. UserService.Create)",
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
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence,omitempty"`
}

// relationsResult is the get_relations result. Unattributed counts the
// references into or out of the symbol that are not resolved (Strong+) edges —
// candidate relations listed above included — so 0 means the relations are
// complete (see index.Completeness). Total and Truncated appear only when
// Relations was cut at maxResults.
type relationsResult struct {
	Symbol       string          `json:"symbol"`
	Relations    []relationEntry `json:"relations"`
	Unattributed int             `json:"unattributed"`
	Total        int             `json:"total,omitempty"`
	Truncated    bool            `json:"truncated,omitempty"`
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
	// Same provider set as repository indexing: the full canonical registry.
	providers := defaultProviders()

	fileIndexes, err := buildFileIndexes(fullPath, providers)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	res := resolver.New(fileIndexes)
	resolutions := res.Resolve()

	// Resolve exactly one target symbol. A short name matching several symbols
	// must NOT merge their relations into one answer — return ambiguity instead.
	tl := resolveTarget(targetCandidatesFromFileIndexes(fileIndexes, symName), filePattern)
	if !tl.Found {
		return targetNotFoundResult(symName, path), nil
	}
	if tl.Ambiguous {
		return ambiguousTargetResult(symName, tl.Candidates), nil
	}
	targetIDs := map[symbol.SymbolID]bool{tl.Symbol.ID: true}

	// Build relation entries.
	var relations []relationEntry
	seen := make(map[string]bool)
	locate := newRefLocator(fileIndexes)

	for _, resolution := range resolutions {
		if resolution.Confidence == resolver.ConfidenceUnresolved {
			continue
		}
		// Find ref in file indexes.
		loc, ok := locate[resolution.ReferenceID]
		if !ok {
			continue
		}
		refObj := loc.ref

		// "calls": the container symbol calls one of our targets.
		for _, cand := range resolution.Candidates {
			if !targetIDs[cand.SymbolID] {
				continue
			}
			if refObj.Container == "" {
				continue
			}
			key := "calls:" + string(loc.file) + ":" + refObj.Container + ":" + cand.Qualified
			if seen[key] {
				continue
			}
			seen[key] = true
			ev := ""
			if len(resolution.Evidence) > 0 {
				ev = resolution.Evidence[0].Detail
			}
			relations = append(relations, relationEntry{
				Direction:  "called_by",
				Name:       cand.Name,
				Qualified:  refObj.Container,
				File:       string(loc.file),
				Kind:       string(refObj.Kind),
				Confidence: resolution.Confidence.String(),
				Evidence:   ev,
			})
		}

		// "called_by": our target symbol contains this reference.
		// The containing symbol is identified by (file, qualified): a reference
		// belongs to the target only when it sits in the target's own file.
		for _, fi := range fileIndexes {
			if fi.FileID != loc.file {
				continue
			}
			for _, sym := range fi.Symbols {
				if !targetIDs[sym.ID] {
					continue
				}
				if refObj.Container != sym.Qualified {
					continue
				}
				for _, cand := range resolution.Candidates {
					key := "calledby:" + sym.Qualified + ":" + cand.Qualified
					if seen[key] {
						continue
					}
					seen[key] = true
					ev := ""
					if len(resolution.Evidence) > 0 {
						ev = resolution.Evidence[0].Detail
					}
					relations = append(relations, relationEntry{
						Direction:  "calls",
						Name:       cand.Name,
						Qualified:  cand.Qualified,
						File:       string(cand.File),
						Kind:       string(refObj.Kind),
						Confidence: resolution.Confidence.String(),
						Evidence:   ev,
					})
				}
			}
		}
	}

	// Deterministic order.
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].Direction != relations[j].Direction {
			return relations[i].Direction < relations[j].Direction
		}
		return relations[i].Qualified < relations[j].Qualified
	})
	out := relationsResult{Symbol: symName}
	c := relationsCompleteness(fileIndexes, resolutions, locate)
	out.Unattributed = c.Incoming(tl.Symbol.ID) + c.Outgoing(tl.Symbol.ID)
	if len(relations) > maxResults {
		out.Total = len(relations)
		out.Truncated = true
		relations = relations[:maxResults]
	}
	if relations == nil {
		relations = []relationEntry{}
	}
	out.Relations = relations
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return &CallToolResult{Content: []Content{{Type: "text", Text: string(b)}}}, nil
}

// buildFileIndexes walks path and extracts symbols + references for every supported file.
func buildFileIndexes(root string, providers []language.Provider) ([]resolver.FileIndex, error) {
	extMap := make(map[string]language.Provider)
	for _, p := range providers {
		for _, ext := range p.Extensions() {
			extMap[ext] = p
		}
	}

	var indexes []resolver.FileIndex
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Same skip rule as repository indexing (hidden dirs, vendor,
			// node_modules); the scan root itself is always entered.
			if path != root && index.SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		p, ok := extMap[ext]
		if !ok {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		fileID := source.FileID(rel)
		extraction, err := p.Extract(context.Background(), fileID, src)
		if err != nil {
			return nil // partial failure: skip file
		}

		// Same conversion as repository indexing: one source of truth for
		// symbol identity, containment and reference evidence.
		indexes = append(indexes, index.NewFileIndex(string(p.Language()), fileID, extraction))
		return nil
	})
	return indexes, err
}

// refLocator maps a ReferenceID to the reference and the file that contains it.
type refLocator map[reference.ReferenceID]locatedRef

type locatedRef struct {
	ref  *reference.Reference
	file source.FileID
}

func newRefLocator(files []resolver.FileIndex) refLocator {
	m := make(refLocator)
	for i := range files {
		for j := range files[i].References {
			m[files[i].References[j].ID] = locatedRef{ref: &files[i].References[j], file: files[i].FileID}
		}
	}
	return m
}

// relationsCompleteness applies index.Completeness to get_relations' own
// resolutions, with the same container identification as repository indexing
// (a container is the single symbol of its file carrying the qualified name).
func relationsCompleteness(files []resolver.FileIndex, resolutions []resolver.Resolution, locate refLocator) *index.Completeness {
	byName := make(map[string][]symbol.Symbol)
	containers := make(map[source.FileID]map[string][]symbol.SymbolID)
	for _, fi := range files {
		byQual := make(map[string][]symbol.SymbolID)
		for _, s := range fi.Symbols {
			byName[s.Name] = append(byName[s.Name], s)
			if !slices.Contains(byQual[s.Qualified], s.ID) {
				byQual[s.Qualified] = append(byQual[s.Qualified], s.ID)
			}
		}
		containers[fi.FileID] = byQual
	}
	c := index.NewCompleteness(byName)
	for _, res := range resolutions {
		loc, ok := locate[res.ReferenceID]
		if !ok {
			continue
		}
		var src symbol.SymbolID
		hasSrc := false
		if loc.ref.Container != "" {
			if ids := containers[loc.file][loc.ref.Container]; len(ids) == 1 {
				src, hasSrc = ids[0], true
			}
		}
		c.Observe(*loc.ref, res, src, hasSrc)
	}
	return c
}
