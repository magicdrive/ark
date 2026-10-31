package repomap_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/yaml"
	"github.com/magicdrive/ark/internal/repomap"
)

// A package is named by the language of its code, not of the configuration
// beside it (language.Accompanying): a Go root package with action.yml and
// .golangci.yml, whose names sort before main.go, is go; a directory of
// YAML only is yaml.
func TestPackageLanguage_AccompanyingFilesDoNotName(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"action.yml":    "runs:\n  using: composite\n  steps: []\n",
		".golangci.yml": "linters: {}\n",
		"main.go":       "package main\n\nfunc main() {}\n",
		"conf/app.yaml": "a: 1\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{golang.NewProvider(), yaml.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	rm := repomap.Build(idx, dir, repomap.Options{DetailLevel: repomap.DetailNormal})
	got := map[string]string{}
	for _, p := range rm.Packages {
		got[p.Path] = p.Language
	}
	if got[""] != "go" || got["conf"] != "yaml" {
		t.Errorf("package languages %v, want the root go and conf yaml", got)
	}
}
