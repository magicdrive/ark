package golden_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/css"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/html"
	"github.com/magicdrive/ark/internal/languages/java"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/rust"
	"github.com/magicdrive/ark/internal/languages/terraform"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/languages/zig"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"

	ctxengine "github.com/magicdrive/ark/internal/context"
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
		terraform.NewProvider(),
		rust.NewProvider(),
		java.NewProvider(),
		html.NewProvider(),
		css.NewProvider(),
		zig.NewProvider(),
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
	"ts_semantics",
	"terraform",
	"rust",
	"java",
	"html",
	"css",
	"zig",
}

// completenessLanguages also freeze what the graph does not show
// (golden.CompletenessSnapshot): for them the unresolved / outside /
// candidate partition is part of the behavior under review.
var completenessLanguages = []string{"terraform", "rust", "java", "html", "css", "zig"}

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

// TestCompletenessGolden freezes the completeness partition of the
// completenessLanguages fixtures.
func TestCompletenessGolden(t *testing.T) {
	for _, lang := range completenessLanguages {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join(testdataDir(), lang)
			idx, err := index.New(context.Background(), dir, allProviders())
			if err != nil {
				t.Fatalf("index.New: %v", err)
			}
			compareGolden(t, filepath.Join(dir, lang+".completeness.golden"), golden.CompletenessSnapshot(idx))
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

// contextTarget is one get_context request of the context golden: a
// declaration (pinned by its first line when it is an overload) and the
// request's options.
type contextTarget struct {
	qualified    string
	line         uint32
	maxTokens    int
	includeTests bool
}

// javaContextTargets cover the declarations Java context is certified on:
// methods, overloads of methods and constructors, a class, a record, an enum,
// an interface default method, a nested class, a duplicated name, an external
// namesake, tests on request, and budget pressure.
var javaContextTargets = []contextTarget{
	{qualified: "com.shop.service.UserService.create", maxTokens: 8000},
	{qualified: "com.shop.service.UserService.create", maxTokens: 120},
	{qualified: "com.shop.service.UserService.create", maxTokens: 8000, includeTests: true},
	{qualified: "com.shop.service.UserService.audit", line: 30, maxTokens: 8000},
	{qualified: "com.shop.service.UserService.audit", line: 34, maxTokens: 8000},
	{qualified: "com.shop.model.User.<init>", line: 7, maxTokens: 8000},
	{qualified: "com.shop.model.User.<init>", line: 11, maxTokens: 8000},
	{qualified: "com.shop.service.UserService.lookup", maxTokens: 8000},
	{qualified: "com.shop.service.UserService.lookupByName", maxTokens: 8000},
	{qualified: "com.shop.service.Coder.ofLong", maxTokens: 8000},
	{qualified: "com.shop.service.Coder.ofInt", maxTokens: 8000},
	{qualified: "com.shop.service.Coder.ofFlag", maxTokens: 8000},
	{qualified: "com.shop.repo.UserRepository.find", line: 10, maxTokens: 8000},
	{qualified: "com.shop.repo.UserRepository.find", line: 14, maxTokens: 8000},
	{qualified: "com.shop.model.User", maxTokens: 8000},
	{qualified: "com.shop.model.User", maxTokens: 5},
	{qualified: "com.shop.model.Money", maxTokens: 8000},
	{qualified: "com.shop.model.Status", maxTokens: 8000},
	{qualified: "com.shop.model.Named.label", maxTokens: 8000},
	{qualified: "com.shop.service.UserService.untyped", maxTokens: 8000},
	{qualified: "com.shop.service.UserService.setting", maxTokens: 8000},
	{qualified: "com.shop.service.UserService.Cache.get", maxTokens: 8000},
	{qualified: "com.shop.gateway.Gateway.fetch", maxTokens: 8000},
	{qualified: "com.shop.report.Ledger", maxTokens: 100},
	{qualified: "com.shop.report.LedgerReport.total", maxTokens: 300},
}

// TestContextGolden freezes get_context for Java: the selected items, their
// order, reasons, confidences, locations and source text, the budget and the
// completeness counts (go test ./internal/golden -update rewrites it).
func TestContextGolden(t *testing.T) {
	dir := filepath.Join(testdataDir(), "..", "..", "languages", "java", "testdata", "cq_java")
	idx, err := index.New(context.Background(), dir, allProviders())
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	eng := ctxengine.New(idx, dir)
	var b strings.Builder
	for i, c := range javaContextTargets {
		if i > 0 {
			b.WriteString("\n")
		}
		var target symbol.Symbol
		n := 0
		for _, s := range idx.FindSymbolsByQualified(c.qualified) {
			if c.line == 0 || s.Location.Range.Start.Line == c.line {
				target, n = s, n+1
			}
		}
		if n != 1 {
			t.Fatalf("%s@%d denotes %d declarations, want 1", c.qualified, c.line, n)
		}
		res, err := eng.Build(context.Background(), ctxengine.Request{
			Target: target.ID, MaxTokens: c.maxTokens, MaxDepth: 2, IncludeTests: c.includeTests,
		})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "# %s@%d maxTokens=%d includeTests=%t\n", c.qualified, target.Location.Range.Start.Line, c.maxTokens, c.includeTests)
		b.WriteString(golden.ContextSnapshot(res))
	}
	compareGolden(t, filepath.Join(testdataDir(), "java", "java.context.golden"), b.String())
}
