package rust

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestCorpus_External measures the provider on real Rust packages. It runs
// only when ARK_RUST_CORPUS lists directories (separated by the OS path-list
// separator); the packages are not part of this repository. For each it
// reports counts, the resolution partition, build time and heap high-water
// mark, checks determinism, that every edge is Exact or Strong and stays in
// its crate, and logs a sample of edges for manual review
// (ARK_RUST_CORPUS_SAMPLE edges per package).
func TestCorpus_External(t *testing.T) {
	list := os.Getenv("ARK_RUST_CORPUS")
	if list == "" {
		t.Skip("ARK_RUST_CORPUS not set")
	}
	var total corpusTotals
	for _, dir := range filepath.SplitList(list) {
		if dir == "" {
			continue
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			total.add(measureCorpus(t, dir))
		})
	}
	t.Logf("TOTAL packages=%d files=%d bytes=%d symbols=%d refs=%d edges=%d (through aliases %d) identity=%d edgeRelations=%d candidateCallees=%d unresolved=%d outside=%d time=%v maxPeakHeap=%dMB diagnostics=%d",
		total.packages, total.files, total.bytes, total.symbols, total.refs, total.edges, total.throughAlias, total.withIdentity, total.exact, total.candidate, total.unresolved, total.outside, total.elapsed, total.peak>>20, total.diags)
}

type corpusTotals struct {
	packages, files, bytes, symbols, refs, edges, withIdentity int
	exact, candidate, unresolved, outside, diags               int
	throughAlias                                               int // edges resolved through a re-export
	elapsed                                                    time.Duration
	peak                                                       uint64
}

func (c *corpusTotals) add(o corpusTotals) {
	c.packages++
	c.files += o.files
	c.bytes += o.bytes
	c.symbols += o.symbols
	c.refs += o.refs
	c.edges += o.edges
	c.withIdentity += o.withIdentity
	c.exact += o.exact
	c.candidate += o.candidate
	c.unresolved += o.unresolved
	c.outside += o.outside
	c.diags += o.diags
	c.throughAlias += o.throughAlias
	c.elapsed += o.elapsed
	if o.peak > c.peak {
		c.peak = o.peak
	}
}

func measureCorpus(t *testing.T, dir string) corpusTotals {
	providers := []language.Provider{NewProvider()}
	runtime.GC()
	var peak atomic.Uint64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var ms runtime.MemStats
		for {
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak.Load() {
				peak.Store(ms.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	idx, err := index.New(context.Background(), dir, providers)
	elapsed := time.Since(start)
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	var out corpusTotals
	out.elapsed, out.peak = elapsed, peak.Load()
	for _, f := range idx.Files() {
		out.files++
		if st, err := os.Stat(filepath.Join(dir, string(f))); err == nil {
			out.bytes += int(st.Size())
		}
		for _, r := range idx.ReferencesByFile(f) {
			out.refs++
			if r.NameQualified != "" {
				out.withIdentity++
			}
		}
	}
	out.diags = len(idx.Diagnostics())
	var edges []string
	for _, s := range idx.FindSymbols("") {
		out.symbols++
		u := idx.UnresolvedOutgoing(s.ID)
		out.unresolved += u.Unresolved
		out.outside += u.OutsideRepository
		out.candidate += idx.CandidateCalleeSample(s.ID).Total
		for _, e := range idx.GetCallees(s.ID) {
			out.edges++
			out.exact++
			to, _ := idx.GetSymbol(e.To)
			if e.Confidence != resolver.ConfidenceExact && e.Confidence != resolver.ConfidenceStrong {
				t.Errorf("edge %s -> %s is %s", s.Qualified, to.Qualified, e.Confidence)
			}
			if crateOf(s.Qualified) != crateOf(to.Qualified) {
				t.Errorf("edge %s -> %s crosses crates", s.Qualified, to.Qualified)
			}
			edges = append(edges, fmt.Sprintf("%s -%s-> %s (%s:%d)", s.Qualified, e.Kind, to.Qualified, s.Location.File, s.Location.Range.Start.Line))
		}
	}
	// The index's alias lookup agrees with resolution through aliases.
	targetOf := map[reference.ReferenceID]symbol.SymbolID{}
	for _, s := range idx.FindSymbols("") {
		for _, r := range idx.ReferencesByTarget(s.ID) {
			targetOf[r.ID] = s.ID
		}
	}
	for _, f := range idx.Files() {
		for _, r := range idx.ReferencesByFile(f) {
			tgt, ok := targetOf[r.ID]
			if !ok || r.NameQualified == "" || len(idx.FindSymbolsByQualified(r.NameQualified)) > 0 {
				continue
			}
			out.throughAlias++
			if got := idx.SymbolsThroughAliases(r.NameQualified); len(got) != 1 || got[0].ID != tgt {
				t.Errorf("%s: alias lookup %v, resolution %s", r.NameQualified, got, tgt)
			}
		}
	}
	again, err := index.New(context.Background(), dir, providers)
	if err != nil || golden.IndexSnapshot(again) != golden.IndexSnapshot(idx) {
		t.Errorf("non-deterministic index (%v)", err)
	}
	t.Logf("files=%d bytes=%d symbols=%d refs=%d identity=%d edges=%d edgeRelations=%d candidateCallees=%d unresolved=%d outside=%d diagnostics=%d time=%v peakHeap=%dMB",
		out.files, out.bytes, out.symbols, out.refs, out.withIdentity, out.edges, out.exact, out.candidate, out.unresolved, out.outside, out.diags, elapsed, out.peak>>20)
	if n := sampleSize(); n > 0 && len(edges) > 0 {
		sort.Strings(edges)
		r := rand.New(rand.NewSource(int64(len(edges))))
		r.Shuffle(len(edges), func(i, j int) { edges[i], edges[j] = edges[j], edges[i] })
		if len(edges) > n {
			edges = edges[:n]
		}
		t.Logf("sample edges:\n  %s", strings.Join(edges, "\n  "))
	}
	return out
}

func sampleSize() int {
	var n int
	fmt.Sscanf(os.Getenv("ARK_RUST_CORPUS_SAMPLE"), "%d", &n)
	return n
}
