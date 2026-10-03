package languages_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
)

// TestRegistry_CanonicalSet pins the canonical supported-language set and order.
// tsx is a first-class language in the registry (its exclusion from MCP
// indexing is a separate, MCP-local compatibility concern).
func TestRegistry_CanonicalSet(t *testing.T) {
	want := []language.Language{"go", "typescript", "tsx", "javascript", "python", "php"}
	got := languages.Registry().Languages()
	if len(got) != len(want) {
		t.Fatalf("Languages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Languages()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRegistry_DescriptorIntegrity verifies each descriptor is coherent: the
// extensions match the provider's own Extensions(), and a grammar exists.
func TestRegistry_DescriptorIntegrity(t *testing.T) {
	for _, d := range languages.Registry().Descriptors() {
		if d.Provider == nil {
			t.Errorf("%s: nil provider", d.Language)
			continue
		}
		if d.Provider.Language() != d.Language {
			t.Errorf("%s: provider.Language()=%q mismatch", d.Language, d.Provider.Language())
		}
		pexts := d.Provider.Extensions()
		if len(pexts) != len(d.Extensions) {
			t.Errorf("%s: descriptor extensions %v != provider extensions %v", d.Language, d.Extensions, pexts)
		}
		getter, ok := languages.Grammar(d.Language)
		if !ok || getter == nil {
			t.Errorf("%s: missing grammar", d.Language)
			continue
		}
		if getter() == nil {
			t.Errorf("%s: grammar getter returned nil", d.Language)
		}
	}
}

func TestRegistry_TSXIsReferences(t *testing.T) {
	if lvl := languages.Registry().SupportLevelFor("tsx"); lvl != language.SupportLevelReferences {
		t.Errorf("tsx SupportLevel = %v, want references", lvl)
	}
}

// TestRegistry_PHPIsSymbols pins PHP-2: PHP is registered at SupportLevelSymbols
// (top-level symbol extraction) and maps the .php extension.
func TestRegistry_PHPIsSymbols(t *testing.T) {
	reg := languages.Registry()
	if lvl := reg.SupportLevelFor("php"); lvl != language.SupportLevelSymbols {
		t.Errorf("php SupportLevel = %v, want symbols", lvl)
	}
	if d, ok := reg.DetectByFilename("index.php"); !ok || d.Language != "php" {
		t.Errorf("DetectByFilename(index.php) = %v,%v; want php", d.Language, ok)
	}
}
