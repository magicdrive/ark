package java_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/cache"
	ctxengine "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestContextCorpus_Java measures Java context on a real repository, given by
// ARK_JAVA_CONTEXT_CORPUS (skipped without it). A deterministic sample of its
// declarations (every k-th by SymbolID, ARK_JAVA_CONTEXT_SAMPLE of them,
// default 3000) gets context at two budgets, and each context is checked as
// FuzzJavaContext checks it (contextViolations), plus one check of its own:
// each item's source contains the item's declared name, so that a range moved
// off its declaration shows. It logs the counts, token use, latency and heap
// growth, and a digest of every context's text: equal digests across
// GOMAXPROCS, repository paths and cache states (ARK_JAVA_CONTEXT_CACHE, a
// cache directory) are the determinism evidence.
func TestContextCorpus_Java(t *testing.T) {
	root := os.Getenv("ARK_JAVA_CONTEXT_CORPUS")
	if root == "" {
		t.Skip("ARK_JAVA_CONTEXT_CORPUS is not set")
	}
	sample := 3000
	if v := os.Getenv("ARK_JAVA_CONTEXT_SAMPLE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		sample = n
	}
	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	var idx *index.RepositoryIndex
	var err error
	if dir := os.Getenv("ARK_JAVA_CONTEXT_CACHE"); dir != "" {
		store, serr := cache.NewFileStore(dir)
		if serr != nil {
			t.Fatal(serr)
		}
		idx, err = index.NewWithCache(context.Background(), root, javaProviders(), store)
	} else {
		idx, err = index.New(context.Background(), root, javaProviders())
	}
	if err != nil {
		t.Fatal(err)
	}
	build := time.Since(start)
	var built runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&built)

	var all []symbol.Symbol
	for _, f := range idx.Files() {
		all = append(all, idx.SymbolsByFile(f)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	step := max(1, len(all)/max(1, sample))
	var targets []symbol.Symbol
	for i := 0; i < len(all) && len(targets) < sample; i += step {
		targets = append(targets, all[i])
	}

	files := map[source.FileID][]byte{}
	read := func(f source.FileID) ([]byte, bool) {
		if b, ok := files[f]; ok {
			return b, true
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(string(f))))
		if err != nil {
			return nil, false
		}
		files[f] = b
		return b, true
	}
	eng := ctxengine.New(idx, root)
	violations := map[string]int{}
	var examples []string
	digest := sha256.New()
	var latencies []time.Duration
	contexts, items, tokens, targetTruncated, truncatedItems := 0, 0, 0, 0, 0
	reasons := map[string]int{}
	confidences := map[string]int{}
	for _, budget := range []int{8000, 1000} {
		for _, target := range targets {
			req := ctxengine.Request{Target: target.ID, MaxTokens: budget, MaxDepth: 2}
			t0 := time.Now()
			res, err := eng.Build(context.Background(), req)
			latencies = append(latencies, time.Since(t0))
			if err != nil {
				t.Fatal(err)
			}
			contexts++
			vs := contextViolations(idx, read, target, req, res)
			for _, it := range res.Items {
				if !declaresName(it.Symbol, it.Source) {
					vs = append(vs, fmt.Sprintf("name: item %s (%s) at %s:%d does not show its name", it.Symbol.Qualified, it.Symbol.Kind, it.Symbol.Location.File, it.Symbol.Location.Range.Start.Line))
				}
				reasons[it.Reason]++
				confidences[it.Confidence.String()]++
			}
			for _, v := range vs {
				kind := v[:strings.IndexByte(v, ':')]
				violations[kind]++
				if len(examples) < 20 {
					examples = append(examples, fmt.Sprintf("%s@%d (budget %d): %s", target.Qualified, target.Location.Range.Start.Line, budget, v))
				}
			}
			items += len(res.Items)
			tokens += res.Stats.EstimatedTokens
			truncatedItems += res.Stats.TruncatedItems
			if res.Stats.TargetTruncated {
				targetTruncated++
			}
			fmt.Fprintf(digest, "%s@%d|%d\n%s\n", target.Qualified, target.Location.Range.Start.Line, budget, ctxengine.Format(res))
		}
	}
	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var total time.Duration
	for _, d := range latencies {
		total += d
	}
	pct := func(p float64) time.Duration { return latencies[int(p*float64(len(latencies)-1))] }
	keys := func(m map[string]int) string {
		var ks []string
		for k, v := range m {
			ks = append(ks, fmt.Sprintf("%s=%d", k, v))
		}
		sort.Strings(ks)
		return strings.Join(ks, " ")
	}
	t.Logf("corpus %s: %d files, %d symbols, %d references, %d relations; index build %v, heap %d MB",
		root, len(idx.Files()), len(all), idx.Stats().References, idx.Stats().Relations, build.Round(time.Millisecond), built.HeapAlloc>>20)
	t.Logf("contexts=%d (targets=%d x budgets 8000,1000) items=%d tokens=%d targetTruncated=%d truncatedItems=%d",
		contexts, len(targets), items, tokens, targetTruncated, truncatedItems)
	t.Logf("reasons: %s", keys(reasons))
	t.Logf("confidences: %s", keys(confidences))
	t.Logf("latency: total %v, p50 %v, p95 %v, p99 %v, max %v; heap after contexts %d MB (growth %+d MB)",
		total.Round(time.Millisecond), pct(0.5), pct(0.95), pct(0.99), latencies[len(latencies)-1],
		after.HeapAlloc>>20, (int64(after.HeapAlloc)-int64(built.HeapAlloc))>>20)
	t.Logf("violations: %s", keys(violations))
	t.Logf("digest=%x", digest.Sum(nil))
	for _, e := range examples {
		t.Log("  " + e)
	}
	if len(violations) > 0 {
		t.Errorf("%d kinds of violations", len(violations))
	}
}

// declaresName reports whether a declaration's source shows its name: a
// constructor its type's simple name; a local class of a name an earlier one
// of the file has (Splitr#2) that name; a synthetic name (<anonymous>, an
// initializer) has none to show.
func declaresName(s symbol.Symbol, src string) bool {
	name := s.Name
	if name == "<init>" {
		parent := s.ParentQualified
		name = parent[strings.LastIndexByte(parent, '.')+1:]
	}
	if i := strings.IndexByte(name, '#'); i > 0 {
		name = name[:i]
	}
	if strings.HasPrefix(name, "<") || name == "" {
		return true
	}
	return strings.Contains(src, name)
}
