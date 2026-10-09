package mcp

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// A forced SymbolID collision through the MCP handlers (the binary has no
// way to inject one, by design: production IDs are not configurable). Every
// tool that answers from the index refuses with the collision report instead
// of answering from a merged ID; tools that read files keep working.
func TestIdentityCollision_IndexToolsRefuse(t *testing.T) {
	root := rankingFixture(t)
	h := NewToolsHandler(root, createTestOption())
	forced := func(lang, path string, kind symbol.SymbolKind, qualified string, ordinal int) symbol.SymbolID {
		if slices.Contains([]string{"users/users.go|getUser", "users/users.go|formatProfile"}, path+"|"+qualified) {
			return "c0111de0c0111de0"
		}
		return symbol.NewDeclarationID(lang, path, kind, qualified, ordinal)
	}
	h.newIndex = func(ctx context.Context, root string, providers []language.Provider, store cache.Store) (*index.RepositoryIndex, error) {
		return index.NewWithIDs(ctx, root, providers, store, forced)
	}
	for _, c := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"search_context", map[string]interface{}{"query": "getUser"}},
		{"get_context", map[string]interface{}{"path": ".", "symbol": "getUser"}},
		{"get_callers", map[string]interface{}{"path": ".", "symbol": "formatProfile"}},
		{"get_callees", map[string]interface{}{"path": ".", "symbol": "getUserProfile"}},
		{"get_relations", map[string]interface{}{"path": ".", "symbol": "getUser"}},
		{"analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "getUser"}},
		{"search_code", map[string]interface{}{"path": ".", "namePattern": "getUser"}},
		{"get_repository_map", map[string]interface{}{"path": "."}},
		{"get_diagnostics", map[string]interface{}{"path": "."}},
	} {
		res, err := h.CallTool(c.tool, c.args)
		if err != nil {
			t.Errorf("%s: protocol error %v", c.tool, err)
			continue
		}
		text := res.Content[0].Text
		if !res.IsError || !strings.Contains(text, "SymbolID collision") || !strings.Contains(text, "c0111de0c0111de0") {
			t.Errorf("%s answered over a collision:\n%.400s", c.tool, text)
		}
		if strings.Contains(text, "func getUser") || strings.Contains(text, "func formatProfile") {
			t.Errorf("%s returned source over a collision", c.tool)
		}
	}
	// File tools do not depend on SymbolIDs.
	for _, c := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"get_file_content", map[string]interface{}{"path": "users/users.go"}},
		{"get_symbols", map[string]interface{}{"path": "users/users.go"}},
		{"find_symbol", map[string]interface{}{"pattern": "getUser"}},
	} {
		if res, err := h.CallTool(c.tool, c.args); err != nil || res.IsError {
			t.Errorf("%s failed: %v %+v", c.tool, err, res)
		}
	}
	// search_context classifies it as the index being unavailable.
	if e := searchErr(t, h, map[string]interface{}{"query": "getUser"}); e.Error != scErrIndexUnavailable {
		t.Errorf("search_context error kind %q", e.Error)
	}
}
