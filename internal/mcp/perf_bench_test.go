package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/perffixture"
)

// Performance benchmarks of the MCP tools on the synthetic repositories of
// internal/perffixture. Every measured call goes through CallTool — a
// request of its own, with its pinned root, policy snapshot and (for the
// index tools) the freshness check of the reused index — exactly as a
// client's call does; nothing is turned off for measuring.
//
// ARK_BENCH_FIXTURES (comma-separated spec names) limits the repositories.

// benchFixtures returns the specs to measure.
func benchFixtures(b *testing.B, def ...string) []perffixture.Spec {
	names := def
	if env := os.Getenv("ARK_BENCH_FIXTURES"); env != "" {
		names = strings.Split(env, ",")
	}
	var out []perffixture.Spec
	for _, n := range names {
		s, ok := perffixture.ByName(n)
		if !ok {
			b.Fatalf("unknown fixture %q", n)
		}
		out = append(out, s)
	}
	return out
}

func allFixtureNames() []string {
	var n []string
	for _, s := range perffixture.Specs {
		n = append(n, s.Name)
	}
	return n
}

// benchRepo writes spec's repository below a temporary directory of b.
func benchRepo(b *testing.B, spec perffixture.Spec) string {
	b.Helper()
	root := filepath.Join(b.TempDir(), "repo")
	st, err := perffixture.Write(root, spec)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("%s: %s", spec.Name, st)
	return root
}

// benchHandler builds the handler as `ark mcp-server --root root` does,
// with store as its extraction cache.
func benchHandler(b *testing.B, root string, store cache.Store) *ToolsHandler {
	_, serveOpt, err := commandline.ServerOptParse("bench", []string{"--root", root})
	if err != nil {
		b.Fatal(err)
	}
	return NewToolsHandlerWithCache(root, serveOpt.GeneralOption, store)
}

type benchCall struct {
	name string
	args map[string]interface{}
}

// benchCalls are the measured calls; the symbol F1_1_3 exists in every
// fixture (directory 1, file 1, function 3).
var benchCalls = []benchCall{
	{"list_files", map[string]interface{}{"path": "."}},
	{"get_directory_tree", map[string]interface{}{"path": "."}},
	{"get_project_stats", map[string]interface{}{"path": "."}},
	{"search_in_files", map[string]interface{}{"path": ".", "query": "F1_1_3"}},
	{"get_file_content", map[string]interface{}{"path": "d01/f001.go"}},
	{"find_symbol", map[string]interface{}{"pattern": "F1_1_3"}},
	{"find_references", map[string]interface{}{"path": ".", "name": "F1_1_3"}},
	{"search_code", map[string]interface{}{"path": ".", "query": "F1_1_3", "format": "json"}},
	{"get_callers", map[string]interface{}{"path": ".", "symbol": "F1_1_3"}},
	{"get_callees", map[string]interface{}{"path": ".", "symbol": "F1_1_3"}},
	{"get_relations", map[string]interface{}{"path": ".", "symbol": "F1_1_3"}},
	{"analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "F1_1_3"}},
	{"get_context", map[string]interface{}{"path": ".", "symbol": "F1_1_3"}},
	{"search_context", map[string]interface{}{"query": "Helper", "contextLimit": 5}},
	{"get_repository_map", map[string]interface{}{"path": "."}},
	{"get_diagnostics", map[string]interface{}{"path": "."}},
}

// mustCall calls a tool and fails b on an error result: a refused or failed
// call measures nothing worth knowing. It returns the output size.
func mustCall(b *testing.B, h *ToolsHandler, c benchCall) int {
	res, err := h.CallTool(c.name, c.args)
	if err != nil {
		b.Fatalf("%s: %v", c.name, err)
	}
	n := 0
	for _, ct := range res.Content {
		n += len(ct.Text)
	}
	if res.IsError {
		b.Fatalf("%s: error result: %.300s", c.name, res.Content[0].Text)
	}
	return n
}

// BenchmarkTools: each tool on a server whose index is built (warm): the
// cost of one client request, freshness check included.
func BenchmarkTools(b *testing.B) {
	for _, spec := range benchFixtures(b, allFixtureNames()...) {
		b.Run(spec.Name, func(b *testing.B) {
			root := benchRepo(b, spec)
			h := benchHandler(b, root, cache.NopStore{})
			for _, c := range benchCalls {
				mustCall(b, h, c) // warm: index built, verified
			}
			for _, c := range benchCalls {
				b.Run(c.name, func(b *testing.B) {
					out := mustCall(b, h, c)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						mustCall(b, h, c)
					}
					b.ReportMetric(float64(out), "outB")
				})
			}
		})
	}
}

// BenchmarkIndexCold: the first index-based request of a server (a new
// handler each iteration): the build and its verification, with the
// extraction cache off, and on with the cache already filled.
func BenchmarkIndexCold(b *testing.B) {
	call := benchCall{"search_code", map[string]interface{}{"path": ".", "query": "F1_1_3", "format": "json"}}
	for _, spec := range benchFixtures(b, "small", "medium-shallow", "medium-nested-rules", "large", "bigfiles") {
		b.Run(spec.Name, func(b *testing.B) {
			root := benchRepo(b, spec)
			b.Run("nocache", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					mustCall(b, benchHandler(b, root, cache.NopStore{}), call)
				}
			})
			b.Run("filecache", func(b *testing.B) {
				store, err := cache.NewFileStore(filepath.Join(b.TempDir(), "cache"))
				if err != nil {
					b.Fatal(err)
				}
				mustCall(b, benchHandler(b, root, store), call) // fills the cache
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					mustCall(b, benchHandler(b, root, store), call)
				}
			})
		})
	}
}

// BenchmarkIndexInvalidate: a source edited between requests: the
// freshness check finds the change and the index is rebuilt.
func BenchmarkIndexInvalidate(b *testing.B) {
	call := benchCall{"search_code", map[string]interface{}{"path": ".", "query": "F1_1_3", "format": "json"}}
	for _, spec := range benchFixtures(b, "small", "medium-shallow") {
		b.Run(spec.Name, func(b *testing.B) {
			root := benchRepo(b, spec)
			h := benchHandler(b, root, cache.NopStore{})
			mustCall(b, h, call)
			edited := filepath.Join(root, "d00", "f000.go")
			orig, err := os.ReadFile(edited)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := os.WriteFile(edited, append(orig, fmt.Sprintf("\n// edit %d\n", i)...), 0o644); err != nil {
					b.Fatal(err)
				}
				mustCall(b, h, call)
			}
		})
	}
}

// BenchmarkParallel: concurrent requests on one server (warm).
func BenchmarkParallel(b *testing.B) {
	calls := []benchCall{
		{"search_code", map[string]interface{}{"path": ".", "query": "F1_1_3", "format": "json"}},
		{"list_files", map[string]interface{}{"path": "."}},
		{"get_file_content", map[string]interface{}{"path": "d01/f001.go"}},
	}
	for _, spec := range benchFixtures(b, "small", "medium-shallow") {
		b.Run(spec.Name, func(b *testing.B) {
			root := benchRepo(b, spec)
			h := benchHandler(b, root, cache.NopStore{})
			for _, c := range calls {
				mustCall(b, h, c)
			}
			for _, c := range calls {
				b.Run(c.name, func(b *testing.B) {
					b.ReportAllocs()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							res, err := h.CallTool(c.name, c.args)
							if err != nil || res.IsError {
								b.Errorf("%s: %v", c.name, err)
								return
							}
						}
					})
				})
			}
		})
	}
}

// BenchmarkSyntaxCold: find_symbol and find_references as a new server's
// first request: every file walked, read and parsed (no parse memo yet).
func BenchmarkSyntaxCold(b *testing.B) {
	calls := []benchCall{
		{"find_symbol", map[string]interface{}{"pattern": "F1_1_3"}},
		{"find_symbol", map[string]interface{}{"pattern": "^F", "maxResults": 20}},
		{"find_references", map[string]interface{}{"path": ".", "name": "F1_1_3"}},
	}
	for _, spec := range benchFixtures(b, "small", "medium-shallow", "bigfiles") {
		b.Run(spec.Name, func(b *testing.B) {
			root := benchRepo(b, spec)
			for i, c := range calls {
				b.Run(fmt.Sprintf("%s-%d", c.name, i), func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						mustCall(b, benchHandler(b, root, cache.NopStore{}), c)
					}
				})
			}
		})
	}
}

// BenchmarkServerStart: a new server's first request (no index involved).
func BenchmarkServerStart(b *testing.B) {
	for _, spec := range benchFixtures(b, "small", "medium-shallow") {
		b.Run(spec.Name, func(b *testing.B) {
			root := benchRepo(b, spec)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				mustCall(b, benchHandler(b, root, cache.NopStore{}), benchCall{"get_file_content", map[string]interface{}{"path": "go.mod"}})
			}
		})
	}
}
