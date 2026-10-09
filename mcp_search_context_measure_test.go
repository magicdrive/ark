package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestMeasureSearchContext measures search_context against this repository
// through the real binary. It is a measurement, not a gate: run it with
//
//	ARK_SEARCH_MEASURE=1 go test -run TestMeasureSearchContext -v .
//
// Tokens are context.EstimateTokens' len/4 estimate of each tool's text.
// Context Engine build counts are measured in-process by
// internal/mcp TestMeasureSearchContextBuilds (the binary does not expose them).

type measureTask struct{ query, want, wantFile string }

// measureTasks: the first eight are the earlier task set (the symbol needed
// is rank 1); the last four need a symbol that is not rank 1.
var measureTasks = []measureTask{
	{"resolveToolPath", "ToolsHandler.resolveToolPath", "internal/mcp/tools_syntax.go"},
	{"canonicalDir", "ToolsHandler.canonicalDir", "internal/mcp/tools.go"},
	{"EstimateTok", "EstimateTokens", "internal/context/tokens.go"},
	{"split_words", "SplitWords", "internal/search/symbols.go"},
	{"ambiguousTarget", "ambiguousTargetResult", "internal/mcp/target_lookup.go"},
	{"rootMismatch", "rootMismatchWarning", "internal/mcp/server.go"},
	{"lookupTarget", "lookupTargetCandidates", "internal/mcp/target_lookup.go"},
	{"Fingerprint", "RepositoryIndex.Fingerprint", "internal/index/index.go"},
	// Below rank 1.
	{"Format", "Format", "internal/search/format.go"},
	{"canonical", "canonicalPath", "internal/mcp/tools.go"},
	{"Normalize", "ServeOption.Normalize", "internal/commandline/server_option.go"},
	{"GetSymbol", "ToolsHandler.getSymbol", "internal/mcp/tools_syntax.go"},
}

type measureConfig struct {
	name string
	args map[string]any // nil: A (find_symbol → get_context)
}

var measureConfigs = []measureConfig{
	{"A find_symbol→get_context", nil},
	{"B limit5 ctx5 4000 (old default)", map[string]any{"limit": 5, "contextLimit": 5, "maxTokens": 4000}},
	{"C limit5 ctx1 4000 (new default)", map[string]any{"limit": 5, "contextLimit": 1, "maxTokens": 4000}},
	{"D limit3 ctx1 2000", map[string]any{"limit": 3, "contextLimit": 1, "maxTokens": 2000}},
}

type taskOutcome struct {
	ok           bool
	calls        int
	tokens       int // all tool responses of the task
	firstTokens  int // the first response only
	latency      time.Duration
	rank         int // rank of the needed symbol in search_context (0: absent / A)
	followUpCall int // calls after the first one
}

func TestMeasureSearchContext(t *testing.T) {
	if os.Getenv("ARK_SEARCH_MEASURE") == "" {
		t.Skip("set ARK_SEARCH_MEASURE=1 to measure")
	}
	bin := buildArk(t)
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tok := func(s string) int { return len(s) / 4 }

	// Cold vs warm per configuration: a fresh server per configuration, so
	// the first call of each builds the index (no persistent cache).
	t.Logf("cold vs warm (first call builds the index; warm = median of 7):")
	for _, cfg := range measureConfigs[1:] {
		c := startServer(t, repo, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
		c.handshake()
		args := withQuery(cfg.args, "Format")
		start := time.Now()
		text, _ := c.tool("search_context", args)
		cold := time.Since(start)
		var ds []time.Duration
		for range 7 {
			s := time.Now()
			c.tool("search_context", args)
			ds = append(ds, time.Since(s))
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		t.Logf("  %-34s cold %8v  warm %8v  response %5d tokens", cfg.name, cold.Round(time.Millisecond), ds[3].Round(100*time.Microsecond), tok(text))
	}

	// Tasks, on one warm server.
	c := startServer(t, repo, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	c.tool("search_context", map[string]any{"query": "warmup", "includeContext": false})
	timed := func(name string, args map[string]any) (string, time.Duration, bool) {
		start := time.Now()
		text, isErr := c.tool(name, args)
		return text, time.Since(start), isErr
	}
	getContext := func(tk measureTask) (string, time.Duration) {
		gc, d, _ := timed("get_context", map[string]any{"path": ".", "symbol": tk.want, "filePattern": tk.wantFile, "maxTokens": 4000})
		return gc, d
	}
	isTargetOf := func(gc string, tk measureTask) bool {
		return strings.Contains(gc, "### "+tk.wantFile+":") && strings.Contains(gc, "Symbol: "+tk.want+"\nReason: target")
	}

	results := map[string][]taskOutcome{}
	for _, cfg := range measureConfigs {
		for _, tk := range measureTasks {
			var o taskOutcome
			if cfg.args == nil {
				// A: find_symbol takes a regex; the agent passes the fragment.
				fs, d, _ := timed("find_symbol", map[string]any{"pattern": tk.query})
				o = taskOutcome{calls: 1, tokens: tok(fs), firstTokens: tok(fs), latency: d}
				if strings.Contains(fs, tk.wantFile) {
					gc, d2 := getContext(tk)
					o.calls++
					o.followUpCall++
					o.tokens += tok(gc)
					o.latency += d2
					o.ok = isTargetOf(gc, tk)
				}
			} else {
				sc, d, isErr := timed("search_context", withQuery(cfg.args, tk.query))
				if isErr {
					t.Fatalf("%s %s: %s", cfg.name, tk.query, sc)
				}
				o = taskOutcome{calls: 1, tokens: tok(sc), firstTokens: tok(sc), latency: d}
				var r searchResult
				if err := json.Unmarshal([]byte(sc), &r); err != nil {
					t.Fatal(err)
				}
				for _, x := range r.Results {
					if x.Symbol.QualifiedName != tk.want || x.Symbol.Path != tk.wantFile {
						continue
					}
					o.rank = x.Rank
					if x.Context.Status == "included" {
						o.ok = true
						break
					}
					// The candidate is listed without context: one get_context.
					gc, d2 := getContext(tk)
					o.calls++
					o.followUpCall++
					o.tokens += tok(gc)
					o.latency += d2
					o.ok = isTargetOf(gc, tk)
				}
			}
			results[cfg.name] = append(results[cfg.name], o)
		}
	}

	t.Logf("per task: ok/calls/total tokens (rank of the needed symbol)")
	header := "  query           "
	for _, cfg := range measureConfigs {
		header += " | " + cfg.name[:1]
	}
	t.Logf("%s", header)
	for i, tk := range measureTasks {
		line := "  " + pad(tk.query, 16)
		for _, cfg := range measureConfigs {
			o := results[cfg.name][i]
			line += " | " + pad(fmtOutcome(o), 20)
		}
		t.Logf("%s", line)
	}

	t.Logf("totals (all 12 tasks; rank-1 subset = first 8; below-rank-1 subset = last 4):")
	t.Logf("  %-34s %8s %6s %8s %10s %10s | %8s %8s | %8s %8s %9s", "config", "ok", "calls", "tokens", "1st-resp", "latency", "r1 tok", "r1 calls", "rN tok", "rN calls", "follow-up")
	for _, cfg := range measureConfigs {
		var all, r1, rn taskOutcome
		okAll := 0
		for i, o := range results[cfg.name] {
			if o.ok {
				okAll++
			}
			add(&all, o)
			if i < 8 {
				add(&r1, o)
			} else {
				add(&rn, o)
			}
		}
		t.Logf("  %-34s %5d/%-2d %6d %8d %10d %10v | %8d %8d | %8d %8d %9d", cfg.name, okAll, len(measureTasks), all.calls, all.tokens, all.firstTokens,
			all.latency.Round(time.Millisecond), r1.tokens, r1.calls, rn.tokens, rn.calls, rn.followUpCall)
	}
}

func withQuery(args map[string]any, q string) map[string]any {
	out := map[string]any{"query": q}
	for k, v := range args {
		out[k] = v
	}
	return out
}

func add(sum *taskOutcome, o taskOutcome) {
	sum.calls += o.calls
	sum.tokens += o.tokens
	sum.firstTokens += o.firstTokens
	sum.latency += o.latency
	sum.followUpCall += o.followUpCall
}

func fmtOutcome(o taskOutcome) string {
	ok := "ok"
	if !o.ok {
		ok = "FAIL"
	}
	s := ok + "/" + itoa(o.calls) + "/" + itoa(o.tokens)
	if o.rank > 0 {
		s += " (r" + itoa(o.rank) + ")"
	}
	return s
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
