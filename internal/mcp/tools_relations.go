package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

type relationsResult struct {
	Symbol    string          `json:"symbol"`
	Relations []relationEntry `json:"relations"`
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

	// Find the target symbol(s) matching symName.
	targetIDs := make(map[symbol.SymbolID]bool)
	for _, fi := range fileIndexes {
		for _, sym := range fi.Symbols {
			if sym.Name == symName || sym.Qualified == symName {
				targetIDs[sym.ID] = true
			}
		}
	}

	// Build relation entries.
	var relations []relationEntry
	seen := make(map[string]bool)

	for _, resolution := range resolutions {
		if resolution.Confidence == resolver.ConfidenceUnresolved {
			continue
		}
		// Find ref in file indexes.
		refObj := findReference(fileIndexes, resolution.ReferenceID)
		if refObj == nil {
			continue
		}

		// "calls": the container symbol calls one of our targets.
		for _, cand := range resolution.Candidates {
			if !targetIDs[cand.SymbolID] {
				continue
			}
			if refObj.Container == "" {
				continue
			}
			key := "calls:" + refObj.Container + ":" + cand.Qualified
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
				File:       string(refObj.Location.File),
				Kind:       string(refObj.Kind),
				Confidence: resolution.Confidence.String(),
				Evidence:   ev,
			})
		}

		// "called_by": our target symbol contains this reference.
		for _, fi := range fileIndexes {
			for _, sym := range fi.Symbols {
				if !targetIDs[sym.ID] {
					continue
				}
				if refObj.Container != sym.Qualified && refObj.Container != sym.Name {
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
	if len(relations) > maxResults {
		relations = relations[:maxResults]
	}

	out := relationsResult{Symbol: symName, Relations: relations}
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
		if err != nil || d.IsDir() {
			return err
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

		var syms []symbol.Symbol
		for _, draft := range extraction.Symbols {
			qualified := draft.Qualified
			if qualified == "" {
				qualified = draft.Name
			}
			syms = append(syms, symbol.Symbol{
				ID:        symbol.NewSymbolID(string(p.Language()), string(fileID), draft.Kind, qualified),
				Name:      draft.Name,
				Qualified: qualified,
				Kind:      draft.Kind,
				Language:  string(p.Language()),
				Location:  draft.Location,
				Receiver:  draft.Receiver,
				Signature: draft.Signature,
				Exported:  draft.Exported,
			})
		}

		var refs []reference.Reference
		for _, rd := range extraction.References {
			loc := rd.Location
			refs = append(refs, reference.Reference{
				ID:           reference.NewReferenceID(string(p.Language()), fileID, reference.ReferenceKind(rd.Kind), rd.Name, loc),
				Name:         rd.Name,
				Kind:         reference.ReferenceKind(rd.Kind),
				Language:     string(p.Language()),
				Location:     loc,
				Container:    rd.Container,
				ReceiverExpr: rd.ReceiverExpr,
				IsCall:       rd.IsCall,
			})
		}

		indexes = append(indexes, resolver.FileIndex{
			FileID:     fileID,
			Language:   string(p.Language()),
			Symbols:    syms,
			References: refs,
			Imports:    extraction.Imports,
		})
		return nil
	})
	return indexes, err
}

// findReference locates a Reference by ID across all file indexes.
func findReference(files []resolver.FileIndex, id reference.ReferenceID) *reference.Reference {
	for i := range files {
		for j := range files[i].References {
			if files[i].References[j].ID == id {
				return &files[i].References[j]
			}
		}
	}
	return nil
}
