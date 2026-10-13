package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/source"
)

// find_references reports each reference's ID ("id"): the index's own ID of
// that reference (reference.NewReferenceID over language, file, kind, name and
// span), an opaque value for telling the listed references apart. It is no
// symbolId: no tool takes it as one.
func TestFindReferences_IDContract(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "g"), 0o755); err != nil {
		t.Fatal(err)
	}
	// F(1).F().F(): three calls of F starting at one position.
	src := "package g\n\ntype V struct{}\n\nfunc (v *V) F() *V { return v }\n\nfunc F(x int) *V { return &V{} }\n\nfunc use() {\n\tF(1).F().F()\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "g", "g.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, text := callAt(t, dir, "find_references", map[string]interface{}{"path": ".", "name": "F"})
	if res.IsError {
		t.Fatalf("find_references: %s", text)
	}
	var out struct {
		Results []map[string]json.RawMessage `json:"results"`
		Count   int                          `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%v:\n%s", err, text)
	}
	// The response's shape: these keys and no others.
	required := []string{"id", "name", "kind", "language", "file", "startLine", "startColumn"}
	known := map[string]bool{"container": true, "receiverExpr": true, "isCall": true}
	for _, k := range required {
		known[k] = true
	}
	ids := map[string]bool{}
	for _, r := range out.Results {
		for _, k := range required {
			if _, ok := r[k]; !ok {
				t.Errorf("a result without %q: %v", k, r)
			}
		}
		for k := range r {
			if !known[k] {
				t.Errorf("a result with an unknown key %q", k)
			}
		}
		var id string
		if err := json.Unmarshal(r["id"], &id); err != nil || len(id) != 16 {
			t.Errorf("id %s is not a 16-digit hex string", r["id"])
		}
		if ids[id] {
			t.Errorf("two listed references share id %s:\n%s", id, text)
		}
		ids[id] = true
	}
	if out.Count != 3 || len(out.Results) != 3 {
		t.Fatalf("want the three calls of F, got %d:\n%s", out.Count, text)
	}

	// Each id is the index's ID of that reference.
	idx, err := index.New(context.Background(), dir, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	indexIDs := map[string]bool{}
	for _, r := range idx.ReferencesByFile(source.FileID("g/g.go")) {
		if r.Name == "F" {
			indexIDs[string(r.ID)] = true
		}
	}
	for id := range ids {
		if !indexIDs[id] {
			t.Errorf("find_references id %s is no reference ID of the index (%v)", id, indexIDs)
		}
	}

	// A reference ID is no symbolId.
	h := &ToolsHandler{rootDir: dir}
	for id := range ids {
		r, err := h.CallTool("get_callers", map[string]interface{}{"path": ".", "symbol": "F", "symbolId": id})
		if err != nil {
			t.Fatal(err)
		}
		var msg string
		for _, c := range r.Content {
			msg += c.Text
		}
		if !r.IsError || !strings.Contains(msg, "is not a symbol of the index") {
			t.Errorf("a reference ID was taken as a symbolId: %s", msg)
		}
		break
	}
}
