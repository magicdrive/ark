package golden_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/source"
)

// update regenerates the committed .golden files instead of comparing against
// them: `go test ./internal/golden -update`.
var update = flag.Bool("update", false, "update golden files")

// allProviders is the full set of language providers under baseline freeze.
// When the registry (Phase 1) lands, this list should be derived from it.
func allProviders() []language.Provider {
	return []language.Provider{
		golang.NewProvider(),
		typescript.NewProvider(),
		typescript.NewTSXProvider(),
		javascript.NewProvider(),
		python.NewProvider(),
	}
}

// baselineLanguages enumerates the fixture directories under testdata/.
// Each entry is a self-contained single-language repository.
var baselineLanguages = []string{
	"go",
	"typescript",
	"tsx",
	"javascript",
	"python",
}

func testdataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata")
}

func providerForExt() map[string]language.Provider {
	m := make(map[string]language.Provider)
	for _, p := range allProviders() {
		for _, ext := range p.Extensions() {
			m[ext] = p
		}
	}
	return m
}

// sourceFiles returns the fixture source files for a language dir, sorted,
// relative to the dir.
func sourceFiles(t *testing.T, dir string) []string {
	t.Helper()
	extMap := providerForExt()
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if _, ok := extMap[strings.ToLower(filepath.Ext(path))]; ok {
			rel, _ := filepath.Rel(dir, path)
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(files)
	return files
}

// TestExtractionGolden freezes per-file provider extraction (symbols,
// references, imports, diagnostics) for each baseline language.
func TestExtractionGolden(t *testing.T) {
	extMap := providerForExt()
	for _, lang := range baselineLanguages {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join(testdataDir(), lang)
			var b strings.Builder
			for _, rel := range sourceFiles(t, dir) {
				src, err := os.ReadFile(filepath.Join(dir, rel))
				if err != nil {
					t.Fatalf("read %s: %v", rel, err)
				}
				p := extMap[strings.ToLower(filepath.Ext(rel))]
				ext, err := p.Extract(context.Background(), source.FileID(rel), src)
				if err != nil {
					t.Fatalf("extract %s: %v", rel, err)
				}
				b.WriteString(golden.ExtractionSnapshot(source.FileID(rel), ext))
				b.WriteString("\n")
			}
			compareGolden(t, filepath.Join(dir, lang+".extract.golden"), b.String())
		})
	}
}

// TestIndexGolden freezes the resolved repository index (symbols, references,
// edges with confidence) for each baseline language.
func TestIndexGolden(t *testing.T) {
	for _, lang := range baselineLanguages {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join(testdataDir(), lang)
			idx, err := index.New(context.Background(), dir, allProviders())
			if err != nil {
				t.Fatalf("index.New: %v", err)
			}
			compareGolden(t, filepath.Join(dir, lang+".index.golden"), golden.IndexSnapshot(idx))
		})
	}
}

// TestIndexDeterminism asserts the Definition of Done from Phase 0: indexing
// the same repository many times yields a byte-for-byte identical snapshot.
func TestIndexDeterminism(t *testing.T) {
	runs := 100
	if testing.Short() {
		// The DoD is 100 identical builds; -short keeps the heavy -race pass fast.
		runs = 10
	}
	for _, lang := range baselineLanguages {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join(testdataDir(), lang)
			var want string
			for i := range runs {
				idx, err := index.New(context.Background(), dir, allProviders())
				if err != nil {
					t.Fatalf("run %d: index.New: %v", i, err)
				}
				got := golden.IndexSnapshot(idx)
				if i == 0 {
					want = got
					continue
				}
				if got != want {
					t.Fatalf("run %d: non-deterministic index snapshot", i)
				}
			}
		})
	}
}

// compareGolden compares got against the file at path, or rewrites it when
// -update is set.
func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}
	if string(want) != got {
		t.Errorf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", path, string(want), got)
	}
}
