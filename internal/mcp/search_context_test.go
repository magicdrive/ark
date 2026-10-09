package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	arkctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
)

// searchFixture is a small Go repository with near-miss names, same-named
// symbols in different packages and kinds, a test file and a sibling directory
// whose name extends another's (auth / authz).
func searchFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.22\n",
		"internal/auth/service.go": `package auth

type AuthService struct{}

func (s *AuthService) AuthenticateUser(name string) bool {
	return validate(name)
}

func validate(name string) bool {
	return name != ""
}

func NewAuthService() *AuthService {
	return &AuthService{}
}
`,
		"internal/auth/service_test.go": `package auth

import "testing"

func TestAuthenticateUser(t *testing.T) {
	_ = NewAuthService().AuthenticateUser("x")
}
`,
		"internal/authz/policy.go": `package authz

type UserAuthenticator struct{}

func Authorize() bool {
	return true
}
`,
		"internal/users/user.go": `package users

type User struct {
	Name string
}

func getUser() User {
	return User{}
}

func GetUserProfile() User {
	return getUser()
}
`,
		"internal/legacy/user.go": `package legacy

type User struct {
	ID int
}

func User2() {}
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

func callSearch(t *testing.T, h *ToolsHandler, args map[string]interface{}) (string, bool) {
	t.Helper()
	res, err := h.CallTool("search_context", args)
	if err != nil {
		t.Fatalf("search_context %v: protocol error %v", args, err)
	}
	return res.Content[0].Text, res.IsError
}

func searchOK(t *testing.T, h *ToolsHandler, args map[string]interface{}) scResponse {
	t.Helper()
	text, isErr := callSearch(t, h, args)
	if isErr {
		t.Fatalf("search_context %v: error %s", args, text)
	}
	var r scResponse
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("search_context %v: invalid JSON: %v\n%s", args, err, text)
	}
	return r
}

func searchErr(t *testing.T, h *ToolsHandler, args map[string]interface{}) scError {
	t.Helper()
	text, isErr := callSearch(t, h, args)
	if !isErr {
		t.Fatalf("search_context %v: no error: %s", args, text)
	}
	var e scError
	if err := json.Unmarshal([]byte(text), &e); err != nil {
		t.Fatalf("error is not JSON: %s", text)
	}
	return e
}

func qualifiedNames(r scResponse) []string {
	var out []string
	for _, x := range r.Results {
		out = append(out, x.Symbol.QualifiedName+"@"+x.Symbol.Path)
	}
	return out
}

func TestSearchContext_RankedCandidates(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	r := searchOK(t, h, map[string]interface{}{"query": "auth", "limit": 20, "includeContext": false})
	got := qualifiedNames(r)
	want := []string{
		// prefix, fewest extra characters first
		"Authorize@internal/authz/policy.go",
		"AuthService@internal/auth/service.go",
		"AuthService.AuthenticateUser@internal/auth/service.go",
		// word boundary
		"NewAuthService@internal/auth/service.go",
		"UserAuthenticator@internal/authz/policy.go",
		"TestAuthenticateUser@internal/auth/service_test.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ranking:\n got %v\nwant %v", got, want)
	}
	if r.TotalMatches != len(want) || r.ReturnedMatches != len(want) || r.Truncated {
		t.Errorf("counts: total=%d returned=%d truncated=%v", r.TotalMatches, r.ReturnedMatches, r.Truncated)
	}
	for i, x := range r.Results {
		if x.Rank != i+1 || x.Context.Status != contextNotRequested || x.Symbol.ID == "" || x.Symbol.StartLine == 0 {
			t.Errorf("result %d malformed: %+v", i, x)
		}
	}
	for i, x := range r.Results {
		if want := map[bool]string{true: "prefix", false: "word_boundary"}[i < 3]; x.MatchType != want {
			t.Errorf("%s: matchType %s, want %s", x.Symbol.QualifiedName, x.MatchType, want)
		}
	}

	// limit truncates, and totalMatches stays exact.
	r = searchOK(t, h, map[string]interface{}{"query": "auth", "limit": 2, "includeContext": false})
	if r.TotalMatches != 6 || r.ReturnedMatches != 2 || !r.Truncated {
		t.Errorf("limit 2: total=%d returned=%d truncated=%v", r.TotalMatches, r.ReturnedMatches, r.Truncated)
	}

	// No match is a normal, empty result.
	r = searchOK(t, h, map[string]interface{}{"query": "nothingLikeThis"})
	if r.TotalMatches != 0 || len(r.Results) != 0 || r.Truncated {
		t.Errorf("no match: %+v", r)
	}
}

// Same-named symbols in different packages and of different kinds are
// separate candidates, each with its own context built from its own ID.
func TestSearchContext_SameNameNeverMerged(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	r := searchOK(t, h, map[string]interface{}{"query": "User", "limit": 20, "contextLimit": 20, "maxTokens": 20000})
	exact := map[string]scResult{}
	for _, x := range r.Results {
		if x.MatchType == "exact" {
			exact[x.Symbol.Path] = x
		}
	}
	if len(exact) != 2 {
		t.Fatalf("want two exact User candidates, got %v", qualifiedNames(r))
	}
	ids := map[string]bool{}
	for path, x := range exact {
		ids[x.Symbol.ID] = true
		if x.Context.Status != contextIncluded {
			t.Fatalf("%s: context %s", path, x.Context.Status)
		}
		var target *scContextItem
		for i := range x.Context.Items {
			if x.Context.Items[i].Reason == "target" {
				target = &x.Context.Items[i]
			}
		}
		if target == nil || target.Path != path || target.StartLine != x.Symbol.StartLine {
			t.Errorf("%s: context target is not this candidate: %+v", path, target)
		}
	}
	if len(ids) != 2 {
		t.Error("same-named candidates share an ID")
	}
}

type buildRecorder struct {
	calls []string
}

func (b *buildRecorder) hook(fail error) func(*index.RepositoryIndex, string, arkctx.Request) (*arkctx.Result, error) {
	return func(idx *index.RepositoryIndex, root string, req arkctx.Request) (*arkctx.Result, error) {
		b.calls = append(b.calls, string(req.Target))
		if fail != nil {
			return nil, fail
		}
		return arkctx.New(idx, root).Build(context.Background(), req)
	}
}

func TestSearchContext_IncludeContextFalseNeverRunsTheEngine(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	rec := &buildRecorder{}
	h.buildContext = rec.hook(nil)
	r := searchOK(t, h, map[string]interface{}{"query": "auth", "includeContext": false})
	if len(rec.calls) != 0 {
		t.Errorf("engine ran %d times with includeContext=false", len(rec.calls))
	}
	for _, x := range r.Results {
		if x.Context.Status != contextNotRequested || x.Context.Items != nil {
			t.Errorf("%s: context %+v", x.Symbol.QualifiedName, x.Context)
		}
	}
}

// Each candidate's context is built once, from its own ID, in rank order.
func TestSearchContext_OneBuildPerCandidate(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	rec := &buildRecorder{}
	h.buildContext = rec.hook(nil)
	r := searchOK(t, h, map[string]interface{}{"query": "auth", "limit": 20, "contextLimit": 20, "maxTokens": 50000})
	if len(rec.calls) != len(r.Results) {
		t.Fatalf("%d builds for %d candidates", len(rec.calls), len(r.Results))
	}
	for i, x := range r.Results {
		if rec.calls[i] != x.Symbol.ID {
			t.Errorf("build %d for %s, want candidate %s", i, rec.calls[i], x.Symbol.ID)
		}
		if x.Context.Status != contextIncluded {
			t.Errorf("%s: %s", x.Symbol.QualifiedName, x.Context.Status)
		}
	}
}

func TestSearchContext_EngineFailureIsContextUnavailable(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	h.buildContext = (&buildRecorder{}).hook(errors.New("boom"))
	r := searchOK(t, h, map[string]interface{}{"query": "AuthService"})
	if len(r.Results) < 2 {
		t.Fatalf("want several results: %v", qualifiedNames(r))
	}
	if c := r.Results[0].Context; c.Status != contextUnavailable || !strings.Contains(c.Reason, "boom") {
		t.Errorf("rank 1: %+v", c)
	}
	for _, x := range r.Results[1:] { // beyond the default contextLimit
		if x.Context.Status != contextNotRequested || x.Context.Reason != reasonContextLimit {
			t.Errorf("%s: %+v", x.Symbol.QualifiedName, x.Context)
		}
	}
}

// The context of a candidate is get_context's context of that symbol: same
// items in the same order with the same sources, reasons and confidence, and
// the same completeness counts, when the budget does not cut either.
func TestSearchContext_MatchesGetContext(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	r := searchOK(t, h, map[string]interface{}{"query": "AuthenticateUser", "limit": 1, "maxTokens": 20000})
	if len(r.Results) != 1 || r.Results[0].Context.Status != contextIncluded {
		t.Fatalf("unexpected: %+v", r)
	}
	res, err := h.CallTool("get_context", map[string]interface{}{
		"path": ".", "symbol": r.Results[0].Symbol.QualifiedName, "format": "json", "maxTokens": 8000,
		"filePattern": r.Results[0].Symbol.Path,
	})
	if err != nil || res.IsError {
		t.Fatalf("get_context: %v %+v", err, res)
	}
	var gc struct {
		Items []struct {
			Symbol, File, Reason, Confidence, Source string
			StartLine                                uint32
		}
		Stats arkctx.Stats
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &gc); err != nil {
		t.Fatal(err)
	}
	sc := r.Results[0].Context
	if len(gc.Items) != len(sc.Items) {
		t.Fatalf("items: get_context %d, search_context %d", len(gc.Items), len(sc.Items))
	}
	for i := range gc.Items {
		a, b := gc.Items[i], sc.Items[i]
		if a.Symbol != b.Symbol || a.File != b.Path || a.Reason != b.Reason || a.Confidence != b.Confidence || a.Source != b.Source || a.StartLine != b.StartLine {
			t.Errorf("item %d differs:\n get_context %+v\n search      %+v", i, a, b)
		}
	}
	st := sc.Stats
	if st.Candidates != gc.Stats.TotalCandidates || st.UnattributedCallers != gc.Stats.UnattributedCallers ||
		st.UnattributedCallees != gc.Stats.UnattributedCallees || st.UnresolvedCallees != gc.Stats.UnresolvedCallees ||
		st.OutsideCallees != gc.Stats.OutsideCallees {
		t.Errorf("stats differ: get_context %+v, search %+v", gc.Stats, st)
	}
}

// For every budget the response is valid JSON within the budget, or a
// budget_exceeded error when not even an empty result fits. Metadata is kept
// before context; omissions are marked.
func TestSearchContext_BudgetIsNeverExceeded(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	sawOmitted, sawPartial, sawDropped := false, false, false
	for budget := 1; budget <= 1500; budget += 7 {
		args := map[string]interface{}{"query": "u", "limit": 20, "contextLimit": 20, "maxTokens": budget}
		text, isErr := callSearch(t, h, args)
		if isErr {
			var e scError
			if json.Unmarshal([]byte(text), &e) != nil || e.Error != scErrBudgetExceeded {
				t.Fatalf("budget %d: unexpected error %s", budget, text)
			}
			continue
		}
		if n := arkctx.EstimateTokens(text); n > budget {
			t.Fatalf("budget %d: response is %d tokens", budget, n)
		}
		var r scResponse
		if err := json.Unmarshal([]byte(text), &r); err != nil {
			t.Fatalf("budget %d: invalid JSON", budget)
		}
		if r.Budget.EstimatedTokens < arkctx.EstimateTokens(text) || r.Budget.EstimatedTokens > budget {
			t.Errorf("budget %d: estimatedTokens %d for a %d-token response", budget, r.Budget.EstimatedTokens, arkctx.EstimateTokens(text))
		}
		if r.DroppedForBudget > 0 {
			sawDropped = true
			if !r.Truncated {
				t.Errorf("budget %d: dropped candidates but not truncated", budget)
			}
		}
		for _, x := range r.Results {
			switch x.Context.Status {
			case contextOmittedBudget:
				sawOmitted = true
			case contextIncluded:
				if x.Context.Stats.OmittedForBudget > 0 {
					sawPartial = true
				}
			default:
				t.Errorf("budget %d: status %s", budget, x.Context.Status)
			}
		}
	}
	if !sawOmitted || !sawPartial || !sawDropped {
		t.Errorf("budget sweep did not exercise omitted=%v partial=%v dropped=%v", sawOmitted, sawPartial, sawDropped)
	}
}

// An item whose source an earlier candidate already shows is referenced, not
// repeated.
func TestSearchContext_SharedItemsAreNotRepeated(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	r := searchOK(t, h, map[string]interface{}{"query": "getUser", "limit": 5, "contextLimit": 5, "maxTokens": 20000})
	sources := map[string]int{}
	refs := 0
	for _, x := range r.Results {
		for _, it := range x.Context.Items {
			key := it.Path + ":" + it.Symbol
			if it.Source != "" {
				sources[key]++
			}
			if it.SourceInResult != 0 {
				refs++
				if it.SourceInResult >= x.Rank {
					t.Errorf("%s refers forward to result %d", key, it.SourceInResult)
				}
			}
		}
	}
	for k, n := range sources {
		if n > 1 {
			t.Errorf("%s source shown %d times", k, n)
		}
	}
	if refs == 0 {
		t.Errorf("fixture shares no item between candidates: %v", qualifiedNames(r))
	}
}

func TestSearchContext_Paths(t *testing.T) {
	root := searchFixture(t)
	h := NewToolsHandler(root, createTestOption())
	paths := func(r scResponse) map[string]bool {
		out := map[string]bool{}
		for _, x := range r.Results {
			out[x.Symbol.Path] = true
		}
		return out
	}
	q := func(path string) map[string]interface{} {
		return map[string]interface{}{"query": "auth", "limit": 20, "includeContext": false, "path": path}
	}
	all := searchOK(t, h, map[string]interface{}{"query": "auth", "limit": 20, "includeContext": false})
	if all.Path != "." || all.TotalMatches != 6 {
		t.Fatalf("no path: %+v", all)
	}
	for _, p := range []string{"internal/auth", "internal/auth/", "./internal/auth", filepath.Join(root, "internal", "auth")} {
		r := searchOK(t, h, q(p))
		if r.Path != "internal/auth" || r.TotalMatches != 4 || paths(r)["internal/authz/policy.go"] {
			t.Errorf("path %q: scope %q total %d paths %v (authz must not match auth)", p, r.Path, r.TotalMatches, paths(r))
		}
	}
	for _, p := range []string{"internal/authz/policy.go", filepath.Join(root, "internal", "authz", "policy.go")} {
		r := searchOK(t, h, q(p))
		if r.TotalMatches != 2 || len(paths(r)) != 1 || !paths(r)["internal/authz/policy.go"] {
			t.Errorf("file %q: %v", p, qualifiedNames(r))
		}
	}
	for _, c := range []struct{ path, want string }{
		{filepath.Dir(root), "outside the server root"},
		{"../", "outside the server root"},
		{"internal/../../x", "outside the server root"},
		{"internal/missing", "does not exist"},
	} {
		e := searchErr(t, h, q(c.path))
		if e.Error != scErrInvalidPath || !strings.Contains(e.Message, c.want) {
			t.Errorf("path %q: %+v", c.path, e)
		}
	}
	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
			t.Fatal(err)
		}
		if e := searchErr(t, h, q("escape")); e.Error != scErrInvalidPath {
			t.Errorf("symlink escape: %+v", e)
		}
		// An in-root symlink to a directory searches its target.
		if err := os.Symlink(filepath.Join(root, "internal", "auth"), filepath.Join(root, "authlink")); err != nil {
			t.Fatal(err)
		}
		if r := searchOK(t, h, q("authlink")); r.Path != "internal/auth" || r.TotalMatches != 4 {
			t.Errorf("in-root symlink scope: %q %d", r.Path, r.TotalMatches)
		}
	}
}

func TestSearchContext_InvalidParameters(t *testing.T) {
	h := NewToolsHandler(searchFixture(t), createTestOption())
	for _, c := range []struct {
		args map[string]interface{}
		kind string
	}{
		{map[string]interface{}{}, scErrInvalidQuery},
		{map[string]interface{}{"query": 3.0}, scErrInvalidQuery},
		{map[string]interface{}{"query": ""}, scErrInvalidQuery},
		{map[string]interface{}{"query": "  \t"}, scErrInvalidQuery},
		{map[string]interface{}{"query": strings.Repeat("x", 257)}, scErrInvalidQuery},
		{map[string]interface{}{"query": "a", "limit": 0.0}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "limit": 21.0}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "limit": 2.5}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "limit": "5"}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "maxTokens": 0.0}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "maxTokens": 100001.0}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "includeContext": "yes"}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "path": 1.0}, scErrInvalidParameters},
		{map[string]interface{}{"query": "a", "maxTokens": 5.0}, scErrBudgetExceeded},
	} {
		if e := searchErr(t, h, c.args); e.Error != c.kind || e.Message == "" {
			t.Errorf("%v: got %+v, want %s", c.args, e, c.kind)
		}
	}
	// Limits are inclusive.
	searchOK(t, h, map[string]interface{}{"query": strings.Repeat("x", 256), "limit": 20.0, "maxTokens": 100000.0})
	searchOK(t, h, map[string]interface{}{"query": "a", "limit": 1.0})
}

// Same inputs give byte-identical output: repeated calls, a fresh handler
// (cold index), and a fresh handler over a warm persistent extraction cache.
func TestSearchContext_Deterministic(t *testing.T) {
	root := searchFixture(t)
	args := map[string]interface{}{"query": "user", "limit": 10, "maxTokens": 3000}
	first, _ := callSearch(t, NewToolsHandler(root, createTestOption()), args)
	h := NewToolsHandler(root, createTestOption())
	for range 3 {
		if again, _ := callSearch(t, h, args); again != first {
			t.Fatal("repeated call differs")
		}
	}
	storeDir := t.TempDir()
	for range 2 { // cold, then warm persistent cache
		store, err := cache.NewFileStore(storeDir)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := callSearch(t, NewToolsHandlerWithCache(root, createTestOption(), store), args); got != first {
			t.Fatal("output differs with the persistent cache")
		}
	}
}

func TestSearchContext_IndexDiagnosticsReported(t *testing.T) {
	root := searchFixture(t)
	if err := os.WriteFile(filepath.Join(root, "internal", "broken.go"), []byte("package x\nfunc Broken( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := searchOK(t, NewToolsHandler(root, createTestOption()), map[string]interface{}{"query": "auth", "includeContext": false})
	if r.IndexDiagnostics == nil || r.IndexDiagnostics.Files == 0 {
		t.Errorf("index diagnostics missing: %+v", r.IndexDiagnostics)
	}
}
