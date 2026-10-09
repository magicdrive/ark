package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	arkctx "github.com/magicdrive/ark/internal/context"
)

// contextLimit separates how many candidates are returned (limit) from how
// many get context. These tests pin that it only spends budget: it never
// changes the ranking, never drops a candidate and never runs the engine more
// than contextLimit times.

// rankingFixture holds the cases where the symbol an agent needs is not rank 1:
//
//	A  same prefix:            Auth, AuthService, AuthenticateUser, UserAuthenticator
//	B  same name, namespaces:  Admin\UserService, PublicApi\UserService, Internal\UserService
//	C  similar function names: getUser, getUserById, getUserProfile
//	D  shared ID:              two Go init functions in one file
func rankingFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/rk\n\ngo 1.22\n",
		"auth/auth.go": `package auth

type Auth interface{}

type AuthService struct{}

func AuthenticateUser(name string) bool {
	return checkPassword(name)
}

func checkPassword(name string) bool {
	return name != ""
}

type UserAuthenticator struct{}
`,
		"php/Admin/UserService.php":     "<?php\nnamespace Admin;\n\nclass UserService\n{\n    public function ban(): void {}\n}\n",
		"php/PublicApi/UserService.php": "<?php\nnamespace PublicApi;\n\nclass UserService\n{\n    public function show(): void {}\n}\n",
		"php/Internal/UserService.php":  "<?php\nnamespace Internal;\n\nclass UserService\n{\n    public function sync(): void {}\n}\n",
		"users/users.go": `package users

type User struct{ ID int }

func getUser() User {
	return User{}
}

func getUserById(id int) User {
	return User{ID: id}
}

func getUserProfile(id int) string {
	return formatProfile(getUserById(id))
}

func formatProfile(u User) string {
	return "profile"
}
`,
		"boot/boot.go": `package boot

func init() { first() }

func init() { second() }

func first()  {}
func second() {}
`,
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type rankKey struct {
	Rank      int
	ID        string
	Qualified string
	Path      string
	Line      uint32
	MatchType string
}

func ranking(r scResponse) []rankKey {
	var out []rankKey
	for _, x := range r.Results {
		out = append(out, rankKey{x.Rank, x.Symbol.ID, x.Symbol.QualifiedName, x.Symbol.Path, x.Symbol.StartLine, x.MatchType})
	}
	return out
}

func statuses(r scResponse) string {
	var s []string
	for _, x := range r.Results {
		st := x.Context.Status
		if x.Context.Reason == reasonContextLimit {
			st += "/limit"
		}
		s = append(s, st)
	}
	return strings.Join(s, ",")
}

// The parameter table of the contract, with the engine's build count.
func TestSearchContext_ContextLimitSemantics(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	rec := &buildRecorder{}
	h.buildContext = rec.hook(nil)
	base := searchOK(t, h, map[string]interface{}{"query": "auth", "limit": 5, "includeContext": false})
	if len(base.Results) != 5 || base.TotalMatches != 6 {
		t.Fatalf("fixture: %v", qualifiedNames(base))
	}
	lim := "not_requested/limit"
	for _, c := range []struct {
		args       map[string]interface{}
		builds     int
		effective  int
		statusList string
	}{
		{map[string]interface{}{"includeContext": false}, 0, 0, "not_requested,not_requested,not_requested,not_requested,not_requested"},
		{map[string]interface{}{"includeContext": false, "contextLimit": 0}, 0, 0, "not_requested,not_requested,not_requested,not_requested,not_requested"},
		{map[string]interface{}{"includeContext": false, "contextLimit": 3}, 0, 0, "not_requested,not_requested,not_requested,not_requested,not_requested"},
		{map[string]interface{}{"contextLimit": 0}, 0, 0, strings.Repeat(lim+",", 4) + lim},
		{map[string]interface{}{}, 1, 1, "included," + strings.Repeat(lim+",", 3) + lim},
		{map[string]interface{}{"contextLimit": 1}, 1, 1, "included," + strings.Repeat(lim+",", 3) + lim},
		{map[string]interface{}{"contextLimit": 3}, 3, 3, "included,included,included," + lim + "," + lim},
		{map[string]interface{}{"contextLimit": 5}, 5, 5, "included,included,included,included,included"},
	} {
		rec.calls = nil
		args := map[string]interface{}{"query": "auth", "limit": 5, "maxTokens": 20000}
		for k, v := range c.args {
			args[k] = v
		}
		r := searchOK(t, h, args)
		if len(rec.calls) != c.builds {
			t.Errorf("%v: %d builds, want %d", c.args, len(rec.calls), c.builds)
		}
		if r.ContextLimit != c.effective {
			t.Errorf("%v: contextLimit %d, want %d", c.args, r.ContextLimit, c.effective)
		}
		if got := statuses(r); got != c.statusList {
			t.Errorf("%v: statuses\n got %s\nwant %s", c.args, got, c.statusList)
		}
		// The ranking, IDs and locations never depend on contextLimit.
		if !reflect.DeepEqual(ranking(r), ranking(base)) {
			t.Errorf("%v: ranking changed", c.args)
		}
		if r.TotalMatches != base.TotalMatches || r.ReturnedMatches != 5 || !r.Truncated {
			t.Errorf("%v: counts %d/%d truncated=%v", c.args, r.ReturnedMatches, r.TotalMatches, r.Truncated)
		}
		// Builds are for the top candidates, in rank order, once each.
		for i, id := range rec.calls {
			if id != r.Results[i].Symbol.ID {
				t.Errorf("%v: build %d for %s, want rank %d", c.args, i, id, i+1)
			}
		}
	}

	// Fewer candidates than contextLimit: one build per candidate, no more.
	rec.calls = nil
	r := searchOK(t, h, map[string]interface{}{"query": "AuthService", "limit": 5, "contextLimit": 5, "maxTokens": 20000})
	if len(r.Results) >= 5 || len(rec.calls) != len(r.Results) {
		t.Errorf("fewer candidates than contextLimit: %d results, %d builds", len(r.Results), len(rec.calls))
	}
}

func TestSearchContext_ContextLimitValidation(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	for _, args := range []map[string]interface{}{
		{"contextLimit": -1.0},
		{"contextLimit": 21.0, "limit": 20.0},
		{"contextLimit": 1.5},
		{"contextLimit": "1"},
		{"contextLimit": true},
		{"contextLimit": 3.0, "limit": 2.0}, // more than limit: refused, not clamped
		{"contextLimit": 6.0},               // default limit is 5
	} {
		args["query"] = "auth"
		if e := searchErr(t, h, args); e.Error != scErrInvalidParameters || !strings.Contains(e.Message, "contextLimit") {
			t.Errorf("%v: %+v", args, e)
		}
	}
	for _, args := range []map[string]interface{}{
		{"contextLimit": 0.0},
		{"contextLimit": 2.0, "limit": 2.0},
		{"contextLimit": 20.0, "limit": 20.0},
		{"contextLimit": 3.0, "limit": 3.0, "includeContext": false},
	} {
		args["query"] = "auth"
		searchOK(t, h, args)
	}
}

// Every token budget, for every contextLimit shape: valid JSON within budget,
// or budget_exceeded when not even an empty result fits. Candidates within
// contextLimit are included or omitted_budget; those beyond it keep their
// context_limit reason whatever the budget.
func TestSearchContext_BudgetSweepByContextLimit(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	for _, cfg := range []map[string]interface{}{
		{"limit": 20, "contextLimit": 0},
		{"limit": 20, "contextLimit": 1},
		{"limit": 20, "contextLimit": 3},
		{"limit": 20, "contextLimit": 20},
		{"limit": 20, "includeContext": false},
		{"limit": 2, "contextLimit": 2}, // fewer slots than matches
	} {
		seen := map[string]bool{}
		for budget := 1; budget <= 1200; budget++ {
			args := map[string]interface{}{"query": "u", "maxTokens": budget}
			for k, v := range cfg {
				args[k] = v
			}
			text, isErr := callSearch(t, h, args)
			if isErr {
				var e scError
				if json.Unmarshal([]byte(text), &e) != nil || e.Error != scErrBudgetExceeded {
					t.Fatalf("%v budget %d: %s", cfg, budget, text)
				}
				seen["budget_exceeded"] = true
				continue
			}
			if n := arkctx.EstimateTokens(text); n > budget {
				t.Fatalf("%v budget %d: response is %d tokens", cfg, budget, n)
			}
			var r scResponse
			if err := json.Unmarshal([]byte(text), &r); err != nil {
				t.Fatalf("%v budget %d: invalid JSON", cfg, budget)
			}
			if r.ReturnedMatches != len(r.Results) || r.Truncated != (r.ReturnedMatches < r.TotalMatches) {
				t.Fatalf("%v budget %d: inconsistent counts %+v", cfg, budget, r)
			}
			for _, x := range r.Results {
				within := x.Rank <= r.ContextLimit
				switch st := x.Context.Status; {
				case !within && cfg["includeContext"] == false:
					if st != contextNotRequested || x.Context.Reason != "" {
						t.Fatalf("%v budget %d rank %d: %+v", cfg, budget, x.Rank, x.Context)
					}
				case !within:
					if st != contextNotRequested || x.Context.Reason != reasonContextLimit {
						t.Fatalf("%v budget %d rank %d: %+v", cfg, budget, x.Rank, x.Context)
					}
				case st == contextIncluded || st == contextOmittedBudget:
					seen[st] = true
				default:
					t.Fatalf("%v budget %d rank %d: %+v", cfg, budget, x.Rank, x.Context)
				}
			}
			if r.DroppedForBudget > 0 {
				seen["dropped"] = true
			}
		}
		if !seen["budget_exceeded"] || (cfg["limit"] == 20 && !seen["dropped"]) {
			t.Errorf("%v: sweep did not reach %v", cfg, seen)
		}
		if cl, _ := cfg["contextLimit"].(int); cl > 0 && (!seen[contextIncluded] || !seen[contextOmittedBudget]) {
			t.Errorf("%v: sweep did not reach %v", cfg, seen)
		}
	}
}

// Case A–C: the needed symbol is not rank 1. It is still returned with its
// ID and location, the wording claims nothing about rank 1, and its context
// is one step away — by raising contextLimit or through get_context.
func TestSearchContext_NeededSymbolBelowRankOne(t *testing.T) {
	h := NewToolsHandler(rankingFixture(t), createTestOption())
	for _, c := range []struct {
		name, query, want string
	}{
		{"A same prefix", "auth", "AuthenticateUser"},
		{"B namespaces", "UserService", `Internal\UserService`},
		{"C similar names", "getUser", "getUserProfile"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := searchOK(t, h, map[string]interface{}{"query": c.query})
			var hit *scResult
			for i := range r.Results {
				if r.Results[i].Symbol.QualifiedName == c.want {
					hit = &r.Results[i]
				}
			}
			if hit == nil {
				t.Fatalf("%s not returned: %v", c.want, qualifiedNames(r))
			}
			if hit.Rank == 1 {
				t.Fatalf("fixture: %s is rank 1, the case needs it lower", c.want)
			}
			if hit.Symbol.ID == "" || hit.Symbol.Path == "" || hit.Symbol.StartLine == 0 {
				t.Errorf("candidate lost its identity: %+v", hit.Symbol)
			}
			if hit.Context.Status != contextNotRequested || hit.Context.Reason != reasonContextLimit {
				t.Errorf("rank %d context: %+v", hit.Rank, hit.Context)
			}
			if strings.Contains(strings.ToLower(searchText(t, h, c.query)), "best") {
				t.Error("the response calls a candidate the best")
			}

			// Expansion 1: raise contextLimit to the candidate's rank.
			r2 := searchOK(t, h, map[string]interface{}{"query": c.query, "contextLimit": hit.Rank})
			if !reflect.DeepEqual(ranking(r2), ranking(r)) {
				t.Error("raising contextLimit changed the ranking")
			}
			got := r2.Results[hit.Rank-1]
			if got.Context.Status != contextIncluded || len(got.Context.Items) == 0 ||
				got.Context.Items[0].Path != hit.Symbol.Path {
				t.Errorf("contextLimit=%d did not give its context: %+v", hit.Rank, got.Context)
			}

			// Expansion 2: get_context with the candidate's qualified name
			// and path.
			res, err := h.CallTool("get_context", map[string]interface{}{
				"path": ".", "symbol": hit.Symbol.QualifiedName, "filePattern": hit.Symbol.Path,
			})
			if err != nil || res.IsError {
				t.Fatalf("get_context: %v %+v", err, res)
			}
			want := fmt.Sprintf("### %s:%d-", hit.Symbol.Path, hit.Symbol.StartLine)
			if !strings.Contains(res.Content[0].Text, want) || !strings.Contains(res.Content[0].Text, "Reason: target") {
				t.Errorf("get_context is not about the candidate:\n%s", res.Content[0].Text)
			}
		})
	}

	// B: the same short name in three namespaces stays three candidates, and
	// get_context on the short name reports the ambiguity instead of picking.
	r := searchOK(t, h, map[string]interface{}{"query": "UserService", "includeContext": false})
	qs := map[string]bool{}
	for _, x := range r.Results {
		qs[x.Symbol.QualifiedName] = true
	}
	for _, q := range []string{`Admin\UserService`, `PublicApi\UserService`, `Internal\UserService`} {
		if !qs[q] {
			t.Errorf("%s missing: %v", q, qualifiedNames(r))
		}
	}
	res, _ := h.CallTool("get_context", map[string]interface{}{"path": ".", "symbol": "UserService"})
	if !res.IsError || !strings.Contains(res.Content[0].Text, "Ambiguous") {
		t.Errorf("get_context on the short name: %+v", res)
	}
}

// Case D, the known limit: two declarations with the same kind and qualified
// name in one file (Go init functions) share a SymbolID, and the index merges
// them. search_context returns both, never folded, and builds no context for
// either, since the index cannot say which one it would describe.
func TestSearchContext_SharedSymbolIDIsNotAttributed(t *testing.T) {
	h := NewToolsHandler(rankingFixture(t), createTestOption())
	rec := &buildRecorder{}
	h.buildContext = rec.hook(nil)
	r := searchOK(t, h, map[string]interface{}{"query": "init", "contextLimit": 2})
	if len(r.Results) != 2 || r.TotalMatches != 2 {
		t.Fatalf("want both init declarations: %v", ranking(r))
	}
	a, b := r.Results[0], r.Results[1]
	if a.Symbol.ID != b.Symbol.ID || a.Symbol.StartLine == b.Symbol.StartLine {
		t.Fatalf("fixture: want one ID, two locations: %+v / %+v", a.Symbol, b.Symbol)
	}
	for _, x := range r.Results {
		if x.Context.Status != contextUnavailable || x.Context.Reason != reasonSharedID {
			t.Errorf("line %d: %+v", x.Symbol.StartLine, x.Context)
		}
	}
	if len(rec.calls) != 0 {
		t.Errorf("%d builds for declarations with a shared ID", len(rec.calls))
	}
}

func searchText(t *testing.T, h *ToolsHandler, query string) string {
	t.Helper()
	text, _ := callSearch(t, h, map[string]interface{}{"query": query})
	return text
}
