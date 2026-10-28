package zig

import (
	"context"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestCorpus measures the provider on a real repository, opt-in:
//
//	ARK_ZIG_CORPUS=/path/to/repo go test ./internal/languages/zig -run TestCorpus -v
//
// It reports what the evaluation of a Zig repository needs (files, parse
// errors, symbols, references by resolution, imports, edges, time,
// memory) and, with ARK_ZIG_SAMPLE=n, prints n random graph edges with the
// source line of the reference, for a person to check by hand. It never
// runs anything of the repository.
func TestCorpus(t *testing.T) {
	root := os.Getenv("ARK_ZIG_CORPUS")
	if root == "" {
		t.Skip("ARK_ZIG_CORPUS not set")
	}
	p := NewProvider()

	var paths []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "zig-cache" || d.Name() == "zig-out") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".zig") && d.Type().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)

	// Extraction and resolution alone, file by file.
	start := time.Now()
	var fis []resolver.FileIndex
	parseErrorFiles, diags := 0, 0
	symbols := 0
	kinds := map[symbol.SymbolKind]int{}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		ex, err := p.Extract(context.Background(), source.FileID(filepath.ToSlash(rel)), src)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if len(ex.Diagnostics) > 0 {
			parseErrorFiles++
			diags += len(ex.Diagnostics)
			for _, d := range ex.Diagnostics {
				t.Logf("diagnostic %s:%d:%d %s", rel, d.Location.Range.Start.Line, d.Location.Range.Start.Column, d.Code)
			}
		}
		symbols += len(ex.Symbols)
		for _, s := range ex.Symbols {
			kinds[s.Kind]++
		}
		fis = append(fis, index.NewFileIndex(p, source.FileID(filepath.ToSlash(rel)), ex))
	}
	extractTime := time.Since(start)
	refs := map[reference.ReferenceID]reference.Reference{}
	for _, fi := range fis {
		for _, r := range fi.References {
			refs[r.ID] = r
		}
	}
	start = time.Now()
	results := resolver.New(fis).Resolve()
	resolveTime := time.Since(start)
	byConf := map[string]int{}
	imports := map[string]int{}
	speculativeDropped := 0
	for _, res := range results {
		r := refs[res.ReferenceID]
		if r.Speculative && len(res.Candidates) == 0 {
			speculativeDropped++
			continue
		}
		c := res.Confidence.String()
		if res.OutsideRepository {
			c = "outside"
		}
		byConf[c]++
		if r.Kind == reference.KindImport {
			switch {
			case len(res.Candidates) == 1 && res.Confidence >= resolver.ConfidenceStrong:
				imports["resolved"]++
			case strings.HasSuffix(r.Name, ".zig"):
				imports["unresolved file"]++
				t.Logf("unresolved file import %s:%d %q", r.Location.File, r.Location.Range.Start.Line, r.Name)
			default:
				imports["named module / zon"]++
			}
		}
	}

	// The index, as the MCP server builds it.
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start = time.Now()
	idx, err := index.New(context.Background(), root, []language.Provider{p})
	if err != nil {
		t.Fatal(err)
	}
	indexTime := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	edges := 0
	edgeKinds := map[string]int{}
	edgeConf := map[string]int{}
	type edgeSample struct{ from, to symbol.Symbol }
	var all []edgeSample
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			edges++
			edgeKinds[string(e.Kind)]++
			edgeConf[e.Confidence.String()]++
			to, _ := idx.GetSymbol(e.To)
			if c := os.Getenv("ARK_ZIG_SAMPLE_CONFIDENCE"); (c == "" || c == e.Confidence.String()) &&
				(os.Getenv("ARK_ZIG_SAMPLE_KIND") == "" || os.Getenv("ARK_ZIG_SAMPLE_KIND") == string(e.Kind)) {
				all = append(all, edgeSample{s, to})
			}
		}
	}

	t.Logf("files=%d parse_error_files=%d diagnostics=%d symbols=%d nodes=%d edges=%d", len(paths), parseErrorFiles, diags, symbols, len(idx.FindSymbols("")), edges)
	t.Logf("symbol kinds: %v", kinds)
	t.Logf("references=%d by resolution: %v (speculative without a declaration, dropped: %d)", len(refs), byConf, speculativeDropped)
	t.Logf("imports: %v", imports)
	t.Logf("edges by kind: %v by confidence: %v", edgeKinds, edgeConf)
	t.Logf("time: extract=%s resolve=%s index=%s heap_alloc_delta=%dMB total_alloc_delta=%dMB", extractTime, resolveTime, indexTime,
		(int64(after.HeapAlloc)-int64(before.HeapAlloc))/(1<<20), (after.TotalAlloc-before.TotalAlloc)/(1<<20))

	if os.Getenv("ARK_ZIG_INCREMENTAL") != "" {
		corpusIncremental(t, root, paths, edgeLines(idx))
	}

	if n, _ := strconv.Atoi(os.Getenv("ARK_ZIG_SAMPLE")); n > 0 {
		rng := rand.New(rand.NewSource(1))
		rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		if n > len(all) {
			n = len(all)
		}
		var b strings.Builder
		for _, e := range all[:n] {
			fmt.Fprintf(&b, "%s  ->  %s\n", e.from.Qualified, e.to.Qualified)
			for _, r := range idx.ReferencesByContainer(e.from.ID) {
				if r.Name == e.to.Name || r.NameQualified == e.to.Qualified {
					fmt.Fprintf(&b, "    %s:%d  %s\n", r.Location.File, r.Location.Range.Start.Line, lineOf(filepath.Join(root, string(r.Location.File)), int(r.Location.Range.Start.Line)))
					break
				}
			}
		}
		t.Logf("sample of %d edges (seed 1):\n%s", n, b.String())
	}
}

func lineOf(path string, line int) string {
	src, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// corpusIncremental measures a build with an empty extraction cache, one
// from the entries it wrote, and one after an edit of one file (only that
// file is extracted again), and checks that the cache changes no edge. The
// edit appends a comment to the first file and is undone.
func corpusIncremental(t *testing.T, root string, paths []string, want []string) {
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	build := func(what string) []string {
		start := time.Now()
		idx, err := index.NewWithCache(context.Background(), root, []language.Provider{NewProvider()}, store)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("incremental: %s build %s", what, time.Since(start))
		return edgeLines(idx)
	}
	cold := build("cold (empty cache)")
	warm := build("warm (every file cached)")
	edited := paths[0]
	orig, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(edited, orig, 0o644)
	if err := os.WriteFile(edited, append(append([]byte(nil), orig...), []byte("\n// edited\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	after := build("after editing one file")
	same := func(a, b []string) bool { return strings.Join(a, "\n") == strings.Join(b, "\n") }
	if !same(cold, want) || !same(warm, want) || !same(after, want) {
		t.Errorf("the cache changed the graph: cold %d, warm %d, after edit %d, uncached %d edges", len(cold), len(warm), len(after), len(want))
	}
}
