package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magicdrive/ark/internal/mcp"
)

type node struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Children []node `json:"children,omitempty"`
}

func TestHandleStructureJSON(t *testing.T) {
	allowed := []string{
		"main.go",
		"internal/util/helper.go",
		"internal/util/convert.go",
		"README.md",
	}

	h := mcp.HandleStructureJSON(allowed)
	r := httptest.NewRequest(http.MethodGet, "/mcp/structure.json", nil)
	w := httptest.NewRecorder()
	h(w, r)

	res := w.Result()
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var root node
	if err := json.NewDecoder(res.Body).Decode(&root); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if root.Type != "dir" || root.Name != "root" {
		t.Errorf("unexpected root: %+v", root)
	}

	found := false
	for _, c := range root.Children {
		if c.Name == "internal" && c.Type == "dir" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find 'internal' dir in root children: %+v", root.Children)
	}
}
