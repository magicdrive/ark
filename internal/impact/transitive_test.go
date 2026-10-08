package impact_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/impact"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

// A call chain Entry → Handle → Store → Write: changing Write must report
// Store as a direct dependent and Handle / Entry as transitive dependents with
// their real hop counts, bounded by maxDepth.
func TestAnalyze_TransitiveDependentsGo(t *testing.T) {
	dir := t.TempDir()
	src := `package app

func Entry()  { Handle() }
func Handle() { Store() }
func Store()  { Write() }
func Write()  {}
func Other()  {}
`
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	target := idx.FindSymbolsByQualified("Write")
	if len(target) != 1 {
		t.Fatalf("Write: %d symbols", len(target))
	}
	run := func(depth int) []string {
		res, err := impact.Analyze(context.Background(), idx, graph.New(idx), target[0].ID, depth)
		if err != nil || res == nil {
			t.Fatalf("Analyze: %v", err)
		}
		var out []string
		for _, e := range res.Entries {
			out = append(out, fmt.Sprintf("%s %s %d", e.Symbol.Qualified, e.Category, e.Distance))
		}
		sort.Strings(out)
		return out
	}
	for depth, want := range map[int][]string{
		1: {"Store direct_dependent 1"},
		2: {"Handle transitive_dependent 2", "Store direct_dependent 1"},
		3: {"Entry transitive_dependent 3", "Handle transitive_dependent 2", "Store direct_dependent 1"},
		9: {"Entry transitive_dependent 3", "Handle transitive_dependent 2", "Store direct_dependent 1"},
	} {
		if got := run(depth); !slices.Equal(got, want) {
			t.Errorf("maxDepth %d: %v, want %v", depth, got, want)
		}
	}
}

// Real Strong edges: a Go call into another file of the package resolves by
// directory (Strong). A transitive dependent's confidence is its path's
// weakest edge; a test caller stays in the test category at its distance.
func TestAnalyze_TransitivePathConfidenceGo(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"app.go":      "package app\n\nfunc Entry() { Handle() }\nfunc Handle() { Store() }\n",
		"store.go":    "package app\n\nfunc Store() { Write() }\nfunc Write() {}\n",
		"app_test.go": "package app\n\nimport \"testing\"\n\nfunc TestEntry(t *testing.T) { Entry() }\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	target := idx.FindSymbolsByQualified("Write")
	res, err := impact.Analyze(context.Background(), idx, graph.New(idx), target[0].ID, 4)
	if err != nil || res == nil {
		t.Fatalf("Analyze: %v", err)
	}
	var got []string
	for _, e := range res.Entries {
		got = append(got, fmt.Sprintf("%s %s %d %s", e.Symbol.Qualified, e.Category, e.Distance, e.Confidence))
	}
	sort.Strings(got)
	want := []string{
		"Entry transitive_dependent 3 strong",  // Entry -exact-> Handle -strong-> Store -exact-> Write: its own hop is Exact, its path is not
		"Handle transitive_dependent 2 strong", // Handle -strong-> Store
		"Store direct_dependent 1 exact",       // same file
		"TestEntry test 4 strong",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	// Evidence of a transitive entry is its own first hop only.
	for _, e := range res.Entries {
		if e.Symbol.Qualified == "Entry" {
			if len(e.Evidence) != 1 || e.Evidence[0].Kind != "same_lexical_scope" && e.Evidence[0].Kind != "same_file" || !strings.Contains(e.Evidence[0].Detail, `"Handle"`) {
				t.Errorf("Entry evidence %v, want only its own hop Entry -> Handle", e.Evidence)
			}
		}
	}
	if !slices.Equal(fileNames(res.AffectedFiles), []string{"app.go", "app_test.go", "store.go"}) {
		t.Errorf("affected files %v", res.AffectedFiles)
	}
}

func fileNames[T ~string](fs []T) []string {
	var out []string
	for _, f := range fs {
		out = append(out, string(f))
	}
	sort.Strings(out)
	return out
}
