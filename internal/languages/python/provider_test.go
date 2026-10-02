package python

import (
	"context"
	"os"
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestExtractPythonSymbols(t *testing.T) {
	src := readFixture(t, "basic.py")
	p := NewProvider()

	ext, err := p.Extract(context.Background(), source.FileID("basic.py"), src)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	want := map[string]symbol.SymbolKind{
		"UserService": symbol.KindClass,
		"User":        symbol.KindClass,
		"format_user": symbol.KindFunction,
	}

	got := make(map[string]symbol.SymbolKind)
	for _, d := range ext.Symbols {
		got[d.Name] = d.Kind
	}

	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("symbol %q: kind = %q, want %q", name, got[name], kind)
		}
	}

	// Private helper should also be extracted (Python just marks exported=false)
	foundPrivate := false
	for _, d := range ext.Symbols {
		if d.Name == "_private_helper" {
			foundPrivate = true
			if d.Exported {
				t.Error("_private_helper should not be exported")
			}
		}
	}
	if !foundPrivate {
		t.Error("expected _private_helper in symbols")
	}
}

func TestExtractPythonReferences(t *testing.T) {
	src := readFixture(t, "basic.py")
	p := NewProvider()

	ext, err := p.Extract(context.Background(), source.FileID("basic.py"), src)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	hasCall := false
	hasImport := false

	for _, r := range ext.References {
		if r.Kind == string(reference.KindCall) {
			hasCall = true
		}
		if r.Kind == string(reference.KindImport) {
			hasImport = true
		}
	}
	for range ext.Imports {
		hasImport = true
	}

	if !hasCall {
		t.Error("expected at least one call reference")
	}
	if !hasImport {
		t.Error("expected at least one import reference")
	}
}

func TestPythonDeterminism(t *testing.T) {
	src := readFixture(t, "basic.py")
	p := NewProvider()
	fileID := source.FileID("basic.py")

	ext1, err := p.Extract(context.Background(), fileID, src)
	if err != nil {
		t.Fatalf("first Extract error: %v", err)
	}
	ext2, err := p.Extract(context.Background(), fileID, src)
	if err != nil {
		t.Fatalf("second Extract error: %v", err)
	}

	if len(ext1.Symbols) != len(ext2.Symbols) {
		t.Fatalf("non-deterministic symbol count: %d vs %d", len(ext1.Symbols), len(ext2.Symbols))
	}
	for i := range ext1.Symbols {
		if ext1.Symbols[i].Name != ext2.Symbols[i].Name {
			t.Errorf("non-deterministic symbol at %d: %q vs %q", i, ext1.Symbols[i].Name, ext2.Symbols[i].Name)
		}
	}
}

func TestPythonBrokenSyntax(t *testing.T) {
	src := readFixture(t, "broken.py")
	p := NewProvider()

	ext, err := p.Extract(context.Background(), source.FileID("broken.py"), src)
	if err != nil {
		t.Fatalf("Extract must not return error on broken syntax: %v", err)
	}
	_ = ext
}
