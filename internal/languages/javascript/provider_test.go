package javascript

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

func TestExtractJavaScriptSymbols(t *testing.T) {
	src := readFixture(t, "basic.js")
	p := NewProvider()

	ext, err := p.Extract(context.Background(), source.FileID("basic.js"), src)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	want := map[string]symbol.SymbolKind{
		"UserService":     symbol.KindClass,
		"User":            symbol.KindClass,
		"formatUser":      symbol.KindFunction,
		"DEFAULT_TIMEOUT": symbol.KindConstant,
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
}

func TestExtractJavaScriptReferences(t *testing.T) {
	src := readFixture(t, "basic.js")
	p := NewProvider()

	ext, err := p.Extract(context.Background(), source.FileID("basic.js"), src)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	hasCall := false
	hasConstruction := false

	for _, r := range ext.References {
		switch r.Kind {
		case string(reference.KindCall):
			hasCall = true
		case string(reference.KindConstruction):
			hasConstruction = true
		}
	}

	if !hasCall {
		t.Error("expected at least one call reference")
	}
	if !hasConstruction {
		t.Error("expected at least one construction (new) reference")
	}
}

func TestJavaScriptDeterminism(t *testing.T) {
	src := readFixture(t, "basic.js")
	p := NewProvider()
	fileID := source.FileID("basic.js")

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

func TestJavaScriptBrokenSyntax(t *testing.T) {
	src := readFixture(t, "broken.js")
	p := NewProvider()

	ext, err := p.Extract(context.Background(), source.FileID("broken.js"), src)
	if err != nil {
		t.Fatalf("Extract must not return error on broken syntax: %v", err)
	}
	_ = ext
}
