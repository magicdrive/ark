package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Namesake declarations through the MCP tools: two Go init functions in
// boot/boot.go (rankingFixture), init@3 calls first(), init@5 calls second().
// A name that several declarations share is ambiguous to every target tool;
// symbolId selects one, and nothing ever mixes the two.

var symbolIDLine = regexp.MustCompile(`init  \(function\)  boot/boot\.go:(\d+)  symbolId=([0-9a-f]+)`)

// initIDs returns line → symbolId from get_context's ambiguity listing.
func initIDs(t *testing.T, h *ToolsHandler) map[string]string {
	t.Helper()
	res, err := h.CallTool("get_context", map[string]interface{}{"path": ".", "symbol": "init"})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].Text, "Ambiguous symbol \"init\" — 2 matches") {
		t.Fatalf("get_context(init) must report two candidates: %v %+v", err, res)
	}
	out := map[string]string{}
	for _, m := range symbolIDLine.FindAllStringSubmatch(res.Content[0].Text, -1) {
		out[m[1]] = m[2]
	}
	if len(out) != 2 || out["3"] == "" || out["5"] == "" || out["3"] == out["5"] {
		t.Fatalf("listing must name both declarations with distinct IDs:\n%s", res.Content[0].Text)
	}
	return out
}

func idToolText(t *testing.T, h *ToolsHandler, name string, args map[string]interface{}) (string, bool) {
	t.Helper()
	res, err := h.CallTool(name, args)
	if err != nil {
		t.Fatalf("%s: protocol error %v", name, err)
	}
	return res.Content[0].Text, res.IsError
}

func TestSymbolIdentity_GetContextSelectsOneDeclaration(t *testing.T) {
	h := NewToolsHandler(rankingFixture(t), createTestOption())
	ids := initIDs(t, h)
	for line, want := range map[string]struct{ has, hasNot string }{"3": {"first", "second"}, "5": {"second", "first"}} {
		text, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "init", "symbolId": ids[line]})
		if isErr {
			t.Fatalf("init@%s: %s", line, text)
		}
		if !strings.Contains(text, "### boot/boot.go:"+line+"-") || !strings.Contains(text, "Symbol: "+want.has+"\n") || strings.Contains(text, "Symbol: "+want.hasNot+"\n") {
			t.Errorf("init@%s context mixes declarations:\n%s", line, text)
		}
	}
	// filePattern cannot separate namesakes of one file.
	if _, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "init", "filePattern": "boot.go"}); !isErr {
		t.Error("filePattern picked one of two same-file declarations")
	}
}

func TestSymbolIdentity_SymbolIDIsValidated(t *testing.T) {
	h := NewToolsHandler(rankingFixture(t), createTestOption())
	ids := initIDs(t, h)
	for _, c := range []struct {
		args map[string]interface{}
		want string
	}{
		{map[string]interface{}{"symbol": "init", "symbolId": "0000000000000000"}, "is not a symbol of the index"},
		{map[string]interface{}{"symbol": "getUser", "symbolId": ids["3"]}, `names init (boot/boot.go:3), not "getUser"`},
		{map[string]interface{}{"symbol": "init", "symbolId": ids["3"], "filePattern": "users/"}, "does not match filePattern"},
		{map[string]interface{}{"symbol": "init", "symbolId": 3.0}, "symbolId must be a string"},
	} {
		c.args["path"] = "."
		for _, tool := range []string{"get_context", "get_callers", "get_callees", "get_relations", "analyze_change_impact"} {
			text, isErr := idToolText(t, h, tool, c.args)
			if !isErr || !strings.Contains(text, c.want) {
				t.Errorf("%s %v: %s", tool, c.args, text)
			}
		}
	}
	// An empty symbolId is no selector: name lookup applies.
	if _, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "getUserProfile", "symbolId": ""}); isErr {
		t.Error("empty symbolId broke name lookup")
	}
}

// The graph tools see two declarations with their own edges.
func TestSymbolIdentity_GraphToolsKeepDeclarationsApart(t *testing.T) {
	h := NewToolsHandler(rankingFixture(t), createTestOption())
	ids := initIDs(t, h)
	for line, c := range map[string]struct{ callee, other string }{"3": {"first", "second"}, "5": {"second", "first"}} {
		out, text, _ := callGraphTool(t, h.rootDir, "get_callees", map[string]interface{}{"path": ".", "symbol": "init", "symbolId": ids[line]})
		if len(out.Edges) != 1 || out.Edges[0].To != c.callee {
			t.Errorf("get_callees init@%s: %s", line, text)
		}
		for _, tool := range []string{"get_relations", "analyze_change_impact"} {
			text, isErr := idToolText(t, h, tool, map[string]interface{}{"path": ".", "symbol": "init", "symbolId": ids[line]})
			if isErr || !strings.Contains(text, c.callee) || strings.Contains(text, `"`+c.other+`"`) {
				t.Errorf("%s init@%s:\n%s", tool, line, text)
			}
		}
	}
	for callee, line := range map[string]string{"first": "3", "second": "5"} {
		out, text, _ := callGraphTool(t, h.rootDir, "get_callers", map[string]interface{}{"path": ".", "symbol": callee})
		if len(out.Edges) != 1 || out.Edges[0].To != "init" || *out.Unattributed != 0 {
			t.Errorf("get_callers %s: %s", callee, text)
		}
		if res, _ := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": callee}); !strings.Contains(res, "### boot/boot.go:"+line+"-") ||
			strings.Count(res, "Symbol: init\n") != 1 {
			t.Errorf("get_context %s must show its one caller init@%s:\n%s", callee, line, res)
		}
	}
}

// A search_context candidate's ID hands off to get_context with path ".".
func TestSymbolIdentity_SearchContextHandOff(t *testing.T) {
	h := NewToolsHandler(rankingFixture(t), createTestOption())
	r := searchOK(t, h, map[string]interface{}{"query": "init", "contextLimit": 0})
	for _, x := range r.Results {
		text, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": x.Symbol.QualifiedName, "symbolId": x.Symbol.ID})
		if isErr || !strings.Contains(text, "### boot/boot.go:"+itoaU(x.Symbol.StartLine)+"-") {
			t.Errorf("hand-off of init@%d: %s", x.Symbol.StartLine, text)
		}
	}
	// IDs depend on the indexed path: under another path they are unknown,
	// reported as such rather than matched loosely.
	text, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": "boot", "symbol": "init", "symbolId": r.Results[0].Symbol.ID})
	if !isErr || !strings.Contains(text, "IDs depend on the indexed path") {
		t.Errorf("foreign-root ID: %s", text)
	}
}

func itoaU(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

// An ambiguity listing's own advice works where qualified names carry a
// namespace: Java overloads share the qualified name p.A.f, which the name
// A.f finds without its package, and only a symbolId tells them apart. The
// listed name with a listed ID selects that declaration in every target
// tool; an ID of a declaration the name does not denote is still refused.
func TestSymbolIdentity_ListedNameSelectsWithItsIDs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "src", "p", "A.java"),
		"package p;\n\nclass A {\n    void f(int x) { g(); }\n    void f(String s) { h(); }\n    void g() {}\n    void h() {}\n}\n"); err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(dir, createTestOption())
	listing, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "A.f"})
	if !isErr || !strings.Contains(listing, "Ambiguous") {
		t.Fatalf("A.f must be ambiguous:\n%s", listing)
	}
	ids := map[string]string{}
	for _, m := range regexp.MustCompile(`p\.A\.f  \(method\)  src/p/A\.java:(\d+)  symbolId=([0-9a-f]+)`).FindAllStringSubmatch(listing, -1) {
		ids[m[1]] = m[2]
	}
	if len(ids) != 2 {
		t.Fatalf("listing must give both overloads' IDs:\n%s", listing)
	}
	for line, callee := range map[string]string{"4": "p.A.g", "5": "p.A.h"} {
		for _, name := range []string{"A.f", "f", "p.A.f"} {
			for _, tool := range []string{"get_context", "get_callers", "get_callees", "get_relations", "analyze_change_impact"} {
				text, isErr := idToolText(t, h, tool, map[string]interface{}{"path": ".", "symbol": name, "symbolId": ids[line]})
				if isErr {
					t.Errorf("%s(%s, symbolId of line %s): %s", tool, name, line, text)
				}
			}
			text, _ := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": name, "symbolId": ids[line]})
			if !strings.Contains(text, "### src/p/A.java:"+line+"-"+line+"\nSymbol: p.A.f\nReason: target") || !strings.Contains(text, "Symbol: "+callee+"\n") {
				t.Errorf("get_context(%s, line %s) is not that overload's context:\n%s", name, line, text)
			}
		}
	}
	if text, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "A.g", "symbolId": ids["4"]}); !isErr || !strings.Contains(text, `names p.A.f (src/p/A.java:4), not "A.g"`) {
		t.Errorf("an ID of another name must be refused: %s", text)
	}
	if text, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "A.f", "symbolId": ids["4"], "filePattern": "q/"}); !isErr || !strings.Contains(text, "does not match filePattern") {
		t.Errorf("filePattern must still apply: %s", text)
	}
}

// A name another language declares exactly (a Go method A.f) ends the
// repository-wide lookup before the Java overloads; narrowed to the Java
// files, the lookup lists them, and the listed name, filePattern and symbolId
// select one.
func TestSymbolIdentity_ListedIDsUnderFilePattern(t *testing.T) {
	dir := t.TempDir()
	for path, src := range map[string]string{
		"src/p/A.java": "package p;\n\nclass A {\n    void f(int x) { g(); }\n    void f(String s) { h(); }\n    void g() {}\n    void h() {}\n}\n",
		"g/a.go":       "package g\n\ntype A struct{}\n\nfunc (A) f() {}\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeFile(filepath.Join(dir, path), src); err != nil {
			t.Fatal(err)
		}
	}
	h := NewToolsHandler(dir, createTestOption())
	listing, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "A.f", "filePattern": ".java"})
	if !isErr || strings.Count(listing, "symbolId=") != 2 {
		t.Fatalf("A.f in the Java files must list both overloads:\n%s", listing)
	}
	m := regexp.MustCompile(`src/p/A\.java:5  symbolId=([0-9a-f]+)`).FindStringSubmatch(listing)
	if m == nil {
		t.Fatalf("no symbolId for line 5:\n%s", listing)
	}
	text, isErr := idToolText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "A.f", "filePattern": ".java", "symbolId": m[1]})
	if isErr || !strings.Contains(text, "### src/p/A.java:5-5\nSymbol: p.A.f\nReason: target") {
		t.Errorf("the listed name, filePattern and symbolId must select the overload:\n%s", text)
	}
}
