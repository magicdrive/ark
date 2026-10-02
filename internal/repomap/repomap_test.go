package repomap_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/repomap"
)

func fixtureProviders() []language.Provider {
	return []language.Provider{golang.NewProvider()}
}

func fixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "simple_go")
}

func buildFixtureIndex(t *testing.T) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), fixtureDir(), fixtureProviders())
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

func TestBuildBasic(t *testing.T) {
	idx := buildFixtureIndex(t)
	rm := repomap.Build(idx, fixtureDir(), repomap.Options{DetailLevel: repomap.DetailVerbose})

	if rm.TotalFiles == 0 {
		t.Error("expected TotalFiles > 0")
	}
	if len(rm.Packages) == 0 {
		t.Error("expected at least one package")
	}
}

func TestBuildDeterministic(t *testing.T) {
	idx := buildFixtureIndex(t)
	opts := repomap.Options{DetailLevel: repomap.DetailNormal}

	rm1 := repomap.Build(idx, fixtureDir(), opts)
	rm2 := repomap.Build(idx, fixtureDir(), opts)

	if rm1.Format() != rm2.Format() {
		t.Error("Build is not deterministic: two calls produced different output")
	}
}

func TestBuildContainsExpectedSymbols(t *testing.T) {
	idx := buildFixtureIndex(t)
	rm := repomap.Build(idx, fixtureDir(), repomap.Options{DetailLevel: repomap.DetailVerbose})

	formatted := rm.Format()
	// The fixture has greet and greetUser functions; at least one should appear.
	found := false
	for _, pkg := range rm.Packages {
		for _, sym := range pkg.Symbols {
			if sym.Name == "greet" || sym.Name == "greetUser" || sym.Name == "main" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected greet/greetUser/main in map; got:\n%s", formatted)
	}
}

func TestFormatNotEmpty(t *testing.T) {
	idx := buildFixtureIndex(t)
	rm := repomap.Build(idx, fixtureDir(), repomap.Options{DetailLevel: repomap.DetailNormal})
	out := rm.Format()
	if len(out) == 0 {
		t.Error("Format() returned empty string")
	}
}

func TestMaxSymbolsRespected(t *testing.T) {
	idx := buildFixtureIndex(t)
	rm := repomap.Build(idx, fixtureDir(), repomap.Options{
		DetailLevel: repomap.DetailVerbose,
		MaxSymbols:  1,
	})
	for _, pkg := range rm.Packages {
		if len(pkg.Symbols) > 1 {
			t.Errorf("package %q has %d symbols, want ≤ 1", pkg.Path, len(pkg.Symbols))
		}
	}
}
