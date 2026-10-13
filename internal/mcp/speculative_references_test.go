package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// find_references lists what the syntax records, and resolves nothing: a
// Speculative reference (a selector that may be a field read, kept by the
// index only once resolved to a declaration) is not listed — a field write
// is, as before.
func TestFindReferences_SpeculativeSelectorsAreNotListed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	// T is declared in another file: the provider cannot tell the read
	// from a method value, so it records it Speculatively.
	files := map[string]string{
		"t.go": "package p\n\ntype T struct{ field int }\n",
		"p.go": "package p\n\nfunc Use(t T) {\n\t_ = t.field\n\tt.field = 1\n}\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, "p", name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, text := callAt(t, dir, "find_references", map[string]interface{}{"path": ".", "name": "field"})
	if !strings.Contains(text, `"kind": "write"`) {
		t.Errorf("the field write is not listed:\n%s", text)
	}
	if strings.Contains(text, `"kind": "value_reference"`) {
		t.Errorf("the speculative field read is listed:\n%s", text)
	}
}
