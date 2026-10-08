package terraform

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/impact"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestCorpus_External measures the provider on real Terraform repositories.
// It runs only when ARK_TERRAFORM_CORPUS lists directories (separated by the
// OS path-list separator); the repositories are not part of this one. For
// each it reports counts, the completeness partition, build time and heap
// high-water mark, checks determinism and the module-boundary property, and
// logs samples of unresolved references for manual false-positive review.
func TestCorpus_External(t *testing.T) {
	list := os.Getenv("ARK_TERRAFORM_CORPUS")
	if list == "" {
		t.Skip("ARK_TERRAFORM_CORPUS not set")
	}
	for _, dir := range filepath.SplitList(list) {
		t.Run(filepath.Base(filepath.Dir(dir))+"/"+filepath.Base(dir), func(t *testing.T) {
			measureCorpus(t, dir)
		})
	}
}

func measureCorpus(t *testing.T, dir string) {
	providers := []language.Provider{NewProvider()}
	runtime.GC()
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
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
		t.Fatal(err)
	}

	st := idx.Stats()
	kinds := map[string]int{}
	edgeKindRefs, sourceless := 0, 0
	for _, f := range idx.Files() {
		for _, r := range idx.ReferencesByFile(f) {
			kinds[string(r.Kind)]++
			if r.Kind == "value_reference" || r.Kind == "depends_on" {
				edgeKindRefs++
				if r.Container == "" {
					sourceless++
				}
			}
		}
	}
	var unattributed, unresolved, outside, noCandidate int
	reasons := map[string]int{}
	samples := map[string][]string{}
	for _, s := range idx.FindSymbols("") {
		_, out := idx.Unattributed(s.ID)
		unattributed += out
		un := idx.UnresolvedOutgoing(s.ID)
		unresolved += un.Unresolved
		outside += un.OutsideRepository
		noCandidate += un.Total
		for _, r := range un.References {
			name := r.Name
			if r.ReceiverExpr != "" {
				name = r.ReceiverExpr + "." + name
			}
			key := string(r.Reason)
			reasons[key+":"+rootOf(name)]++
			if len(samples[key]) < 12 {
				samples[key] = append(samples[key], fmt.Sprintf("%s @%s:%d", name, r.Location.File, r.Location.Range.Start.Line))
			}
		}
	}
	diags := 0
	for _, d := range idx.Diagnostics() {
		if d.Severity == language.SeverityError {
			diags++
		}
	}
	t.Logf("files=%d symbols=%d references=%d %v", st.Files, st.Symbols, st.References, kinds)
	t.Logf("edge-kind refs=%d: edges=%d unattributed=%d no-candidate=%d (unresolved=%d outside=%d) sourceless=%d",
		edgeKindRefs, st.Relations, unattributed, noCandidate, unresolved, outside, sourceless)
	t.Logf("index build %v, heap high-water %.1f MiB, error diagnostics %d", elapsed.Round(time.Millisecond), float64(peak.Load())/(1<<20), diags)
	var rk []string
	for k, n := range reasons {
		rk = append(rk, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(rk)
	t.Logf("no-candidate by reason:root: %s", strings.Join(rk, " "))
	for _, k := range []string{"unresolved", "outside_repository"} {
		t.Logf("sample %s: %s", k, strings.Join(samples[k], "; "))
	}

	// Impact over every symbol (maxDepth 3): entries by category, and the
	// deepest transitive dependent reached.
	g := graph.New(idx)
	cats := map[impact.Category]int{}
	maxDist := 0
	start = time.Now()
	for _, s := range idx.FindSymbols("") {
		res, err := impact.Analyze(context.Background(), idx, g, s.ID, 3)
		if err != nil || res == nil {
			t.Fatalf("impact %s: %v", s.Qualified, err)
		}
		for _, e := range res.Entries {
			cats[e.Category]++
			if e.Category == impact.CategoryTransitiveDependent && e.Distance > maxDist {
				maxDist = e.Distance
			}
		}
	}
	t.Logf("impact (all symbols, depth 3) in %v: direct_dependent=%d direct_dependency=%d transitive_dependent=%d possible_dependent=%d max transitive distance=%d",
		time.Since(start).Round(time.Millisecond), cats[impact.CategoryDirectDependent], cats[impact.CategoryDirectDependency],
		cats[impact.CategoryTransitiveDependent], cats[impact.CategoryPossibleDependent], maxDist)
	confs := map[string]int{}
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			confs[string(e.Kind)+"/"+e.Confidence.String()]++
		}
	}
	t.Logf("edges by kind/confidence (deduped): %v", confs)

	// The partition: every observed edge-kind reference is accounted for.
	// A reference without a container is sourceless (override files, a
	// duplicate declaration); it can still have been an unattributed one.
	if got := st.Relations + unattributed + noCandidate; got > edgeKindRefs || got < edgeKindRefs-sourceless {
		t.Errorf("partition: edges %d + unattributed %d + no-candidate %d = %d, refs %d (sourceless %d)",
			st.Relations, unattributed, noCandidate, got, edgeKindRefs, sourceless)
	}

	// Module boundary: an edge leaves its module directory only to a child
	// module's output through a member scope.
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			if e.Confidence != resolver.ConfidenceExact {
				t.Errorf("non-Exact edge %s -> %s (%s)", s.Qualified, to.Qualified, e.Confidence)
			}
			if path.Dir(string(s.Location.File)) != path.Dir(string(to.Location.File)) &&
				(to.Kind != symbol.KindOutput || !edgeVia(e, resolver.EvidenceMemberScope)) {
				t.Errorf("cross-module edge %s -> %s", s.Qualified, to.Qualified)
			}
		}
	}

	// Determinism.
	again, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatal(err)
	}
	if golden.IndexSnapshot(idx)+golden.CompletenessSnapshot(idx) != golden.IndexSnapshot(again)+golden.CompletenessSnapshot(again) {
		t.Error("non-deterministic index")
	}
}

// rootOf is the address root of a reference name (var, local, module,
// data, or the resource type).
func rootOf(name string) string {
	if i := strings.IndexByte(name, '.'); i > 0 {
		root := name[:i]
		switch root {
		case "var", "local", "module", "data", "ephemeral", "provider":
			return root
		}
		return "resource"
	}
	return name
}
