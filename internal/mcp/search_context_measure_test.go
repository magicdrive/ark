package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMeasureSearchContextBuilds counts Context Engine builds per
// search_context configuration over this repository, for the task queries of
// the root package's TestMeasureSearchContext. Measurement only:
//
//	ARK_SEARCH_MEASURE=1 go test -run TestMeasureSearchContextBuilds -v ./internal/mcp
func TestMeasureSearchContextBuilds(t *testing.T) {
	if os.Getenv("ARK_SEARCH_MEASURE") == "" {
		t.Skip("set ARK_SEARCH_MEASURE=1 to measure")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(repo, createTestOption())
	rec := &buildRecorder{}
	h.buildContext = rec.hook(nil)
	queries := []string{"resolveToolPath", "canonicalDir", "EstimateTok", "split_words", "ambiguousTarget", "rootMismatch",
		"lookupTarget", "Fingerprint", "Format", "canonical", "Normalize", "GetSymbol"}
	for _, cfg := range []struct {
		name string
		args map[string]interface{}
	}{
		{"B limit5 ctx5 4000", map[string]interface{}{"limit": 5, "contextLimit": 5, "maxTokens": 4000}},
		{"C limit5 ctx1 4000", map[string]interface{}{"limit": 5, "contextLimit": 1, "maxTokens": 4000}},
		{"D limit3 ctx1 2000", map[string]interface{}{"limit": 3, "contextLimit": 1, "maxTokens": 2000}},
		{"includeContext=false", map[string]interface{}{"limit": 5, "includeContext": false}},
	} {
		rec.calls = nil
		candidates, included, omitted := 0, 0, 0
		for _, q := range queries {
			args := map[string]interface{}{"query": q}
			for k, v := range cfg.args {
				args[k] = v
			}
			text, isErr := callSearch(t, h, args)
			if isErr {
				t.Fatalf("%s %s: %s", cfg.name, q, text)
			}
			var r scResponse
			if err := json.Unmarshal([]byte(text), &r); err != nil {
				t.Fatal(err)
			}
			candidates += len(r.Results)
			for _, x := range r.Results {
				switch x.Context.Status {
				case contextIncluded:
					included++
				case contextOmittedBudget:
					omitted++
				}
			}
		}
		t.Logf("%-22s %2d queries: %3d candidates returned, %3d engine builds, %3d contexts included, %2d omitted_budget",
			cfg.name, len(queries), candidates, len(rec.calls), included, omitted)
	}
}
