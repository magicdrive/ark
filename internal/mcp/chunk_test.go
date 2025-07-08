package mcp_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/mcp"
)

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
}

func TestHandleChunks(t *testing.T) {
	tmp := t.TempDir()
	writeTestFile(t, tmp, "a.go", strings.Repeat("a", 400))
	writeTestFile(t, tmp, "b.go", strings.Repeat("b", 400))
	writeTestFile(t, tmp, "c.go", strings.Repeat("c", 400))

	allowed := []string{"a.go", "b.go", "c.go"}
	r := httptest.NewRequest(http.MethodGet, "/mcp/chunks?max_chunk_size=100", nil)
	w := httptest.NewRecorder()

	h := mcp.HandleChunks(tmp, allowed)
	h.ServeHTTP(w, r)

	res := w.Result()
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", res.StatusCode)
	}
	var index mcp.ChunkIndex
	if err := json.NewDecoder(res.Body).Decode(&index); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(index.Chunks) == 0 {
		t.Error("expected non-zero chunks")
	}
}

func TestHandleChunkByID(t *testing.T) {
	tmp := t.TempDir()
	writeTestFile(t, tmp, "x.go", "package main\nfunc main() {}")
	writeTestFile(t, tmp, "y.go", "package main\nfunc y() {}")

	allowed := []string{"x.go", "y.go"}
	r := httptest.NewRequest(http.MethodGet, "/mcp/chunk/1?max_chunk_size=100", nil)
	w := httptest.NewRecorder()

	h := mcp.HandleChunkByID(tmp, allowed)
	h.ServeHTTP(w, r)

	res := w.Result()
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	text := string(body)
	if !strings.Contains(text, "@x.go") {
		t.Errorf("expected chunk content for x.go, got: %s", text)
	}
}

