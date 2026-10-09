package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// search_context end to end: the real binary, launched as a client would,
// over stdio JSON-RPC.

func searchWorkspace(t *testing.T) (proj, other, launcher string) {
	t.Helper()
	base := t.TempDir()
	proj = filepath.Join(base, "proj")
	other = filepath.Join(base, "other")
	launcher = filepath.Join(base, "launcher")
	files := map[string]string{
		filepath.Join(proj, "go.mod"): "module example.com/proj\n\ngo 1.22\n",
		filepath.Join(proj, "internal", "auth", "service.go"): `package auth

type AuthService struct{}

func (s *AuthService) AuthenticateUser(name string) bool {
	return validate(name)
}

func validate(name string) bool {
	return name != ""
}
`,
		filepath.Join(proj, "internal", "authz", "policy.go"): `package authz

type UserAuthenticator struct{}

func Authorize() bool {
	return true
}
`,
		filepath.Join(other, "secret.go"): "package other\n\nfunc AuthSecret() {}\n",
	}
	for p, c := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	return proj, other, launcher
}

type searchResult struct {
	TotalMatches    int `json:"totalMatches"`
	ReturnedMatches int `json:"returnedMatches"`
	Path            string
	ContextLimit    int `json:"contextLimit"`
	Budget          struct{ MaxTokens, EstimatedTokens int }
	Results         []struct {
		Rank      int
		MatchType string
		Symbol    struct{ ID, QualifiedName, Path string }
		Context   struct {
			Status string
			Reason string
			Items  []struct{ Reason, Source string }
		}
	}
}

func decodeSearch(t *testing.T, text string) searchResult {
	t.Helper()
	var r searchResult
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("invalid search_context JSON: %v\n%s", err, text)
	}
	return r
}

func TestMCPSearchContext_EndToEnd(t *testing.T) {
	bin := buildArk(t)
	proj, other, launcher := searchWorkspace(t)
	for _, launch := range []struct{ name, cwd, root string }{
		{"dot root from project", proj, "./"},
		{"absolute root from elsewhere", launcher, proj},
	} {
		t.Run(launch.name, func(t *testing.T) {
			c := startServer(t, launch.cwd, t.TempDir(), bin, "mcp-server", "--root", launch.root, "--no-cache")
			c.handshake()
			if resp := c.call("tools/list", map[string]any{}); !strings.Contains(string(resp.Result), `"search_context"`) {
				t.Fatal("tools/list does not list search_context")
			}

			// Partial identifier → several ranked candidates with context.
			text, isErr := c.tool("search_context", map[string]any{"query": "auth", "limit": 3})
			if isErr {
				t.Fatalf("search_context: %s", text)
			}
			r := decodeSearch(t, text)
			if r.TotalMatches != 4 || r.ReturnedMatches != 3 || len(r.Results) != 3 {
				t.Fatalf("counts: total=%d returned=%d", r.TotalMatches, r.ReturnedMatches)
			}
			if r.Results[0].Symbol.QualifiedName != "Authorize" || r.Results[0].Context.Status != "included" ||
				len(r.Results[0].Context.Items) == 0 || r.Results[0].Context.Items[0].Source == "" {
				t.Errorf("top candidate: %+v", r.Results[0])
			}
			if len(text)/4 > r.Budget.MaxTokens {
				t.Errorf("response exceeds its budget")
			}
			// Default contextLimit 1: the other candidates stay, metadata only.
			if r.ContextLimit != 1 {
				t.Errorf("default contextLimit %d", r.ContextLimit)
			}
			for _, x := range r.Results[1:] {
				if x.Context.Status != "not_requested" || x.Context.Reason != "context_limit" || x.Symbol.ID == "" {
					t.Errorf("rank %d: %+v", x.Rank, x)
				}
			}
			// contextLimit=3: same ranking, context for all three.
			text3, _ := c.tool("search_context", map[string]any{"query": "auth", "limit": 3, "contextLimit": 3})
			r3 := decodeSearch(t, text3)
			for i, x := range r3.Results {
				if x.Symbol.ID != r.Results[i].Symbol.ID || x.Rank != r.Results[i].Rank || x.Context.Status != "included" {
					t.Errorf("contextLimit=3 rank %d: %+v", x.Rank, x)
				}
			}
			if text, isErr := c.tool("search_context", map[string]any{"query": "auth", "limit": 2, "contextLimit": 3}); !isErr || !strings.Contains(text, `"invalid_parameters"`) {
				t.Errorf("contextLimit > limit accepted: %s", text)
			}
			for _, x := range r.Results {
				if strings.Contains(x.Symbol.Path, "other") {
					t.Errorf("candidate outside the root: %+v", x.Symbol)
				}
			}

			// Metadata only.
			text, _ = c.tool("search_context", map[string]any{"query": "auth", "includeContext": false})
			for _, x := range decodeSearch(t, text).Results {
				if x.Context.Status != "not_requested" || len(x.Context.Items) != 0 {
					t.Errorf("includeContext=false: %+v", x)
				}
			}

			// In-root absolute directory scope; auth does not include authz.
			text, isErr = c.tool("search_context", map[string]any{"query": "auth", "path": filepath.Join(proj, "internal", "auth"), "includeContext": false})
			if isErr {
				t.Fatalf("absolute in-root path: %s", text)
			}
			if r := decodeSearch(t, text); r.Path != "internal/auth" || r.TotalMatches != 2 {
				t.Errorf("scoped search: path %q total %d", r.Path, r.TotalMatches)
			}

			// Out-of-root absolute path and ../ are refused.
			for _, p := range []string{other, "../other"} {
				text, isErr = c.tool("search_context", map[string]any{"query": "auth", "path": p})
				if !isErr || !strings.Contains(text, `"invalid_path"`) || !strings.Contains(text, "outside the server root") {
					t.Errorf("path %q not refused: %s", p, text)
				}
			}

			// Deterministic: identical input, identical bytes.
			first, _ := c.tool("search_context", map[string]any{"query": "user", "limit": 5, "maxTokens": 2000})
			for range 3 {
				if again, _ := c.tool("search_context", map[string]any{"query": "user", "limit": 5, "maxTokens": 2000}); again != first {
					t.Fatal("search_context output differs between identical calls")
				}
			}

			// Existing tools are unaffected.
			if text, isErr := c.tool("get_context", map[string]any{"path": ".", "symbol": "AuthService.AuthenticateUser"}); isErr || !strings.Contains(text, "validate") {
				t.Errorf("get_context: %s", text)
			}
			if text, isErr := c.tool("find_symbol", map[string]any{"pattern": "^Auth"}); isErr || !strings.Contains(text, "AuthService") {
				t.Errorf("find_symbol: %s", text)
			}
		})
	}
}
