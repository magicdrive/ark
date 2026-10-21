package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
)

// End-to-end MCP validation of Java context over the real pipeline. The
// fixture is the context-quality repository of the java provider
// (testdata/cq_java), so the MCP answers are checked against the same
// semantic truth as its scenarios.

func javaCQFixture(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "languages", "java", "testdata", "cq_java"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMCP_Java_LanguageSupportMatchesRegistry(t *testing.T) {
	res, text := callAt(t, javaCQFixture(t), "get_language_support", map[string]interface{}{})
	mustOK(t, res, "get_language_support", text)
	level := languages.Registry().SupportLevelFor(language.Language("java"))
	i := strings.Index(text, `"language": "java"`)
	if i < 0 {
		t.Fatalf("java missing from get_language_support:\n%s", text)
	}
	seg := text[i:]
	if j := strings.Index(seg, `"extensions"`); j > 0 {
		seg = seg[:j]
	}
	if !strings.Contains(seg, `"level": "`+level.String()+`"`) {
		t.Errorf("java: tool level does not match the registry level %q:\n%s", level, seg)
	}
}

// symbolIDAt returns the symbolId an ambiguity listing gives the declaration
// at file:line.
func symbolIDAt(t *testing.T, listing, fileLine string) string {
	t.Helper()
	m := regexp.MustCompile(regexp.QuoteMeta(fileLine) + `\s+symbolId=([0-9a-f]+)`).FindStringSubmatch(listing)
	if m == nil {
		t.Fatalf("no symbolId for %s in:\n%s", fileLine, listing)
	}
	return m[1]
}

func TestMCP_Java_Context(t *testing.T) {
	dir := javaCQFixture(t)

	// A method: the overloads and constructor it calls, by their own source;
	// never the other overload, a same-name method of another type, or a test.
	res, text := callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "UserService.create"})
	mustOK(t, res, "get_context", text)
	contains(t, "get_context", text,
		"### src/main/java/com/shop/service/UserService.java:22-28", `@Audited("create")`,
		"### src/main/java/com/shop/model/User.java:7-9", "public User(String name) {",
		"### src/main/java/com/shop/util/Texts.java:6-8", "public static String normalize(String s) {",
		"Symbol: com.shop.repo.UserRepository.save")
	excludes(t, "get_context", text, "### src/main/java/com/shop/model/User.java:11-13",
		"OrderRepository", "OrderService", "UnrelatedService", "UserServiceTest", "void audit(Order o)")

	// Overloads are never picked by name: every surface lists them, and a
	// symbolId selects one declaration and its own context.
	for _, tool := range []string{"get_context", "analyze_change_impact", "get_callers", "get_callees"} {
		res, text := callAt(t, dir, tool, map[string]interface{}{"path": ".", "symbol": "UserService.audit"})
		if !res.IsError || !strings.Contains(text, "Ambiguous") || strings.Count(text, "symbolId=") != 2 {
			t.Errorf("%s(UserService.audit) must list both overloads; isError=%v:\n%s", tool, res.IsError, text)
		}
	}
	_, listing := callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "User.<init>"})
	noArg := symbolIDAt(t, listing, "src/main/java/com/shop/model/User.java:11")
	res, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "User.<init>", "symbolId": noArg})
	mustOK(t, res, "get_context(symbolId)", text)
	contains(t, "get_context(symbolId)", text,
		"### src/main/java/com/shop/model/User.java:11-13\nSymbol: com.shop.model.User.<init>\nReason: target",
		`this("anonymous");`,
		// this("anonymous") calls the one-argument constructor.
		"### src/main/java/com/shop/model/User.java:7-9\nSymbol: com.shop.model.User.<init>\nReason: direct callee\nConfidence: exact")

	// Tests are left out unless asked for; a test caller keeps its own
	// (strong) confidence.
	res, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "UserService.create", "includeTests": true})
	mustOK(t, res, "get_context(includeTests)", text)
	contains(t, "get_context(includeTests)", text,
		"Symbol: com.shop.service.UserServiceTest.creates\nReason: caller\nConfidence: strong")

	// No fabrication: the unknown receiver's save, a name declared in two
	// source roots and an external namesake are completeness counts only.
	for sym, absent := range map[string][]string{
		"UserService.untyped": {"UserRepository.save", "OrderRepository.save"},
		"UserService.setting": {"Config.java", "com.shop.shared.Config"},
		"Gateway.fetch":       {"com.shop.model.User", "Entity.touch"},
	} {
		res, text := callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": sym})
		mustOK(t, res, "get_context "+sym, text)
		excludes(t, "get_context "+sym, text, absent...)
	}
	_, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "UserService.setting"})
	contains(t, "get_context UserService.setting", text, "unattributed: 0 callers, 2 callees")

	// JSON keeps each item's confidence: the on-demand import's edges stay
	// strong. A field and a method may share a name (User.name): they are
	// two declarations, told apart by their ranges and kinds.
	res, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "Reports.title", "format": "json"})
	mustOK(t, res, "get_context(json)", text)
	var out struct {
		Items []struct {
			Symbol, File, Kind, Reason, Confidence, Source string
			StartLine, EndLine                             int
		}
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("get_context(json): %v\n%s", err, text)
	}
	got := map[string]string{}
	for _, it := range out.Items {
		got[fmt.Sprintf("%s@%d %s %s", it.Symbol, it.StartLine, it.Kind, it.Reason)] = it.Confidence
	}
	want := map[string]string{
		"com.shop.service.Reports.title@6 method target":     "exact",
		"com.shop.model.User.name@19 method direct callee":   "strong",
		"com.shop.model.User@3 class direct callee":          "strong",
		"com.shop.model.User.name@4 field transitive callee": "exact",
		"com.shop.model.Entity@3 class transitive callee":    "exact",
		"com.shop.model.Named@3 interface transitive callee": "exact",
	}
	if len(got) != len(want) {
		t.Errorf("Reports.title context %v, want %v", got, want)
	}
	for s, c := range want {
		if got[s] != c {
			t.Errorf("%s: confidence %q, want %q (all: %v)", s, got[s], c, got)
		}
	}
}

// search_context ranks names and never merges overloads: each one gets its
// own context, built from its own SymbolID, identical to get_context's.
func TestMCP_Java_SearchContextKeepsOverloadsApart(t *testing.T) {
	dir := javaCQFixture(t)
	res, text := callAt(t, dir, "search_context", map[string]interface{}{"path": ".", "query": "normalize", "contextLimit": 2, "maxTokens": 4000})
	mustOK(t, res, "search_context", text)
	var out struct {
		Results []struct {
			Symbol struct {
				ID, QualifiedName, Signature string
				StartLine                    int
			}
			Context struct {
				Status string
				Items  []struct {
					Symbol, Reason string
					StartLine      int
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("search_context: %v\n%s", err, text)
	}
	if len(out.Results) != 2 || out.Results[0].Symbol.ID == out.Results[1].Symbol.ID {
		t.Fatalf("want two overloads with distinct IDs:\n%s", text)
	}
	for _, r := range out.Results {
		if r.Context.Status != "included" || len(r.Context.Items) == 0 ||
			r.Context.Items[0].Reason != "target" || r.Context.Items[0].StartLine != r.Symbol.StartLine {
			t.Errorf("%s%s at %d: context is not its own: %+v", r.Symbol.QualifiedName, r.Symbol.Signature, r.Symbol.StartLine, r.Context)
		}
		_, ctx := callAt(t, dir, "get_context", map[string]interface{}{
			"path": ".", "symbol": "Texts.normalize", "symbolId": r.Symbol.ID, "format": "json",
		})
		var gc struct {
			Items []struct {
				Symbol, Reason string
				StartLine      int
			}
		}
		if err := json.Unmarshal([]byte(ctx), &gc); err != nil {
			t.Fatalf("get_context: %v\n%s", err, ctx)
		}
		if len(gc.Items) != len(r.Context.Items) {
			t.Errorf("%d: search_context has %d items, get_context %d", r.Symbol.StartLine, len(r.Context.Items), len(gc.Items))
			continue
		}
		for i := range gc.Items {
			if gc.Items[i] != r.Context.Items[i] {
				t.Errorf("%d: item %d differs: %+v vs %+v", r.Symbol.StartLine, i, r.Context.Items[i], gc.Items[i])
			}
		}
	}
}
