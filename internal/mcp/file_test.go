package mcp_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/mcp"
)

func TestHandleFile(t *testing.T) {
	tmp := t.TempDir()
	writeTestFile(t, tmp, "main.go", "package main\nfunc main() {}")
	writeTestFile(t, tmp, "deny.go", "package main\nfunc deny() {}")

	allowed := []string{"main.go"}
	h := mcp.HandleFile(tmp, allowed)

	t.Run("allowed file", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/file?path=main.go", nil)
		w := httptest.NewRecorder()
		h(w, r)

		res := w.Result()
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(body), "@main.go") {
			t.Error("missing file header in response")
		}
	})

	t.Run("missing path", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/file", nil)
		w := httptest.NewRecorder()
		h(w, r)

		res := w.Result()
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", res.StatusCode)
		}
	})

	t.Run("forbidden file", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/file?path=deny.go", nil)
		w := httptest.NewRecorder()
		h(w, r)

		res := w.Result()
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", res.StatusCode)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/mcp/file?path=not_exist.go", nil)
		w := httptest.NewRecorder()
		h(w, r)

		res := w.Result()
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for not allowed file, got %d", res.StatusCode)
		}
	})
}

