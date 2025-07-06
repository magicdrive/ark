package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/mcp"
)

func TestHandleSearch(t *testing.T) {
	tmp := t.TempDir()
	writeTestFile(t, tmp, "hello.go", "package main\n// hello world\nfunc main() {}")
	writeTestFile(t, tmp, "utils.go", "package utils\nfunc Help() string { return \"support\" }")

	allowed := []string{"hello.go", "utils.go"}
	h := mcp.HandleSearch(tmp, allowed)

	t.Run("search hit", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/search?q=hello", nil)
		w := httptest.NewRecorder()
		h(w, r)
		res := w.Result()
		defer res.Body.Close()

		if res.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", res.StatusCode)
		}

		var results []mcp.SearchResult
		if err := json.NewDecoder(res.Body).Decode(&results); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(results) == 0 {
			t.Error("expected at least one result")
		}
		if results[0].Path != "hello.go" || !strings.Contains(results[0].Snippet, "hello") {
			t.Errorf("unexpected search result: %+v", results[0])
		}
	})

	t.Run("no match", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/search?q=notfound", nil)
		w := httptest.NewRecorder()
		h(w, r)
		res := w.Result()
		defer res.Body.Close()

		if res.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", res.StatusCode)
		}

		var results []mcp.SearchResult
		if err := json.NewDecoder(res.Body).Decode(&results); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("missing query", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/search", nil)
		w := httptest.NewRecorder()
		h(w, r)

		if w.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Result().StatusCode)
		}
	})

	t.Run("regex rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/search?q=.*", nil)
		w := httptest.NewRecorder()
		h(w, r)

		if w.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Result().StatusCode)
		}
	})
}
