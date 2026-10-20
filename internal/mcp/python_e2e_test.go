package mcp

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/syntax"
)

// Python end to end through the MCP surfaces, on the fixture of the Python
// provider (internal/languages/python/testdata/repo).

func pythonFixture() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "languages", "python", "testdata", "repo")
}

func pyCall(t *testing.T, tool string, args map[string]interface{}) (string, bool) {
	t.Helper()
	res, text := callTool(t, pythonFixture(), tool, args)
	return text, res.IsError
}

func pyEdges(t *testing.T, tool, symbol string) callersResult {
	t.Helper()
	text, isErr := pyCall(t, tool, map[string]interface{}{"symbol": symbol})
	if isErr {
		t.Fatalf("%s %s: %s", tool, symbol, text)
	}
	var out callersResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return out
}

func edgeSet(out callersResult) []string {
	var got []string
	for _, e := range out.Edges {
		got = append(got, e.To+" "+e.Kind+" "+e.Confidence)
	}
	sort.Strings(got)
	return got
}

func TestOneRegistrationInvariant_Python(t *testing.T) {
	if syntax.DetectLanguage("app/models.py") != "python" {
		t.Error("syntax detection does not route .py to python")
	}
	found := false
	for _, p := range defaultProviders() {
		found = found || p.Language() == "python"
	}
	if !found {
		t.Error("defaultProviders() misses python")
	}
	for _, ext := range []string{".py", ".pyw"} {
		if _, ok := refProviderRegistry[ext]; !ok {
			t.Errorf("find_references registry misses %s", ext)
		}
	}
	if lvl := languages.Registry().SupportLevelFor("python"); lvl.String() != "graph" {
		t.Errorf("python support level %s", lvl)
	}
	text, _ := pyCall(t, "get_language_support", map[string]interface{}{})
	var entries []languageSupportEntry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, e := range entries {
		if e.Language == "python" {
			ok = e.Level == "graph" && containsExt(e.Extensions, ".py") && containsExt(e.Extensions, ".pyw")
		}
	}
	if !ok {
		t.Errorf("get_language_support does not report python at graph with .py and .pyw: %s", text)
	}
}

// Two methods of one name are two symbols: a single-target tool never
// picks one, and each has its own callers.
func TestMCP_Python_MethodsOfOneNameAreDistinct(t *testing.T) {
	text, isErr := pyCall(t, "get_callers", map[string]interface{}{"symbol": "save"})
	if !isErr || !strings.Contains(text, "Ambiguous") || !strings.Contains(text, "Base.save") || !strings.Contains(text, "User.save") {
		t.Fatalf("want an ambiguity listing Base.save and User.save:\n%s", text)
	}
	if got := edgeSet(pyEdges(t, "get_callers", "Base.save")); strings.Join(got, ",") != "User.save called_by strong" {
		t.Errorf("callers of Base.save %v (super().save() in User.save)", got)
	}
	// user.save() in UserService.create: a local's type is not inferred —
	// no edge, and the reference is counted, never hidden.
	if out := pyEdges(t, "get_callers", "User.save"); len(out.Edges) != 0 || out.Unattributed == 0 {
		t.Errorf("callers of User.save: edges %v unattributed %d, want none and the untyped call unattributed", out.Edges, out.Unattributed)
	}
}

func TestMCP_Python_CalleesContextImpactRepomap(t *testing.T) {
	// One entry per callee: User is called (and named in the return
	// annotation), log is called through import app.util.
	got := edgeSet(pyEdges(t, "get_callees", "UserService.create"))
	want := []string{"User calls exact", "log calls exact"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("callees of UserService.create %v, want %v", got, want)
	}

	text, isErr := pyCall(t, "get_context", map[string]interface{}{"symbol": "Store.items"})
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{"Reason: target", "Symbol: Store.put", "Reason: caller"} {
		if !strings.Contains(text, want) {
			t.Errorf("get_context misses %q:\n%s", want, text)
		}
	}

	text, isErr = pyCall(t, "analyze_change_impact", map[string]interface{}{"symbol": "Base"})
	if isErr || !regexp.MustCompile(`(?m)^  User\s+app/models\.py:13\s+\[exact\]$`).MatchString(text) ||
		!regexp.MustCompile(`(?m)^  UserService\.create\s+app/service\.py:\d+\s+\[exact\]$`).MatchString(text) {
		t.Errorf("analyze_change_impact of Base (its subclass User, and transitively UserService.create):\n%s", text)
	}

	text, isErr = pyCall(t, "get_repository_map", map[string]interface{}{})
	if isErr || !strings.Contains(text, "python") || !strings.Contains(text, "app") {
		t.Errorf("get_repository_map:\n%s", text)
	}
}

// A reverse edge says what the reference is: a subclass is extended_by, an
// annotation's user type_used_by, a call called_by.
func TestMCP_Python_ReverseKinds(t *testing.T) {
	kinds := func(symbol string) map[string]string {
		out := pyEdges(t, "get_callers", symbol)
		m := map[string]string{}
		for _, e := range out.Edges {
			if prev, ok := m[e.To]; ok {
				m[e.To] = prev + "," + e.Kind
				continue
			}
			m[e.To] = e.Kind
		}
		return m
	}
	if got := kinds("Base")["User"]; got != "extended_by" {
		t.Errorf("Base's subclass User is %q, want extended_by", got)
	}
	store := kinds("Store")
	if store["UserService.__init__"] != "type_used_by" || store["main"] != "called_by" {
		t.Errorf("Store's users: %v (want UserService.__init__ type_used_by, main called_by)", store)
	}
}

// A name a package re-exports (from .service import UserService in
// app/__init__.py) is the declaration it names: its callers include the
// script that imports it from the package.
func TestMCP_Python_ReExport(t *testing.T) {
	out := pyEdges(t, "get_callers", "UserService")
	if got := edgeSet(out); strings.Join(got, ",") != "main called_by exact" {
		t.Errorf("callers of UserService %v, want main through app/__init__.py", got)
	}
	if text, isErr := pyCall(t, "get_callers", map[string]interface{}{"symbol": "NoSuchName"}); !isErr {
		t.Errorf("an unknown name was found: %s", text)
	}
}

func TestMCP_Python_FindTools(t *testing.T) {
	text, _ := pyCall(t, "find_symbol", map[string]interface{}{"pattern": "UserService"})
	if !strings.Contains(text, "app/service.py") || !strings.Contains(text, `"kind": "class"`) {
		t.Errorf("find_symbol:\n%s", text)
	}
	text, _ = pyCall(t, "search_code", map[string]interface{}{"kind": "method", "language": "python"})
	if !strings.Contains(text, "Found 10 symbol(s)") {
		t.Errorf("search_code kind=method:\n%s", text)
	}
	h := &ToolsHandler{rootDir: pythonFixture()}
	res, err := h.CallTool("get_symbols", map[string]interface{}{"path": "app/models.py"})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].Text, "validate") || !strings.Contains(res.Content[0].Text, `"receiver": "User"`) {
		t.Errorf("get_symbols: %v %+v", err, res)
	}
	text, _ = pyCall(t, "find_references", map[string]interface{}{"path": ".", "name": "User"})
	if !strings.Contains(text, "app/service.py") || !strings.Contains(text, `"kind": "type_use"`) || !strings.Contains(text, `"kind": "call"`) {
		t.Errorf("find_references:\n%s", text)
	}
	text, isErr := pyCall(t, "get_diagnostics", map[string]interface{}{})
	if isErr || !strings.Contains(text, `"errors": 0`) {
		t.Errorf("get_diagnostics:\n%s", text)
	}
}

// Secret masking covers Python sources through every reading tool.
func TestMCP_Python_SecretsMasked(t *testing.T) {
	root := t.TempDir()
	aws := "AKIA" + "IOSFODNN7EXAMPLE"
	gh := "ghp_" + strings.Repeat("c", 36)
	writeTree(t, root, map[string]string{
		"app/settings.py": "AWS_KEY = \"" + aws + "\"\n\n\ndef token():\n    api_token = \"" + gh + "\"\n    return api_token\n\n\ndef use_it():\n    return token()\n",
	})
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	for _, c := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"get_file_content", map[string]interface{}{"path": "app/settings.py"}},
		{"get_symbol", map[string]interface{}{"path": "app/settings.py", "name": "token"}},
		{"get_symbol", map[string]interface{}{"path": "app/settings.py", "name": "AWS_KEY"}},
		{"get_context", map[string]interface{}{"path": ".", "symbol": "use_it"}},
		{"get_files_arklite", map[string]interface{}{"paths": []interface{}{"app/settings.py"}}},
	} {
		text, isErr := callText(t, h, c.tool, c.args)
		if isErr {
			t.Fatalf("%s %v: %s", c.tool, c.args, text)
		}
		if strings.Contains(text, aws) || strings.Contains(text, gh) {
			t.Errorf("%s %v leaks a secret:\n%s", c.tool, c.args, text)
		}
		if !strings.Contains(text, "MASKED") {
			t.Errorf("%s %v shows no masked value:\n%s", c.tool, c.args, text)
		}
	}
}

// get_relations lists a class's users by what each reference is, with the
// evidence that resolved it (the alias import Backend included).
func TestMCP_Python_Relations(t *testing.T) {
	h := NewToolsHandler(pythonFixture(), nil)
	got := relationsFor(t, h, "Store")
	want := []string{
		`called_by UserService.__init__ type_use exact | "Backend" bound by import (app/service.py ← ".storage" → app/storage.py exports "Store" → app/service.py ← ".storage.Store")`,
		`called_by main call exact | "Store" bound by import (scripts/run.py ← "app.storage" → app/storage.py exports "Store" → scripts/run.py ← "app.storage.Store")`,
	}
	if lines := relationLines(got.Relations); strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("relations of Store:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if got.Unattributed != 0 {
		t.Errorf("unattributed %d, want 0", got.Unattributed)
	}
}
