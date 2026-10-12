package mcp

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/syntax"
)

// Rust end to end through the MCP surfaces, on the Cargo workspace fixture
// of the Rust provider (internal/languages/rust/testdata/repo).

func rustFixture() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "languages", "rust", "testdata", "repo")
}

func rsCall(t *testing.T, tool string, args map[string]interface{}) (string, bool) {
	t.Helper()
	res, text := callTool(t, rustFixture(), tool, args)
	return text, res.IsError
}

func rsEdges(t *testing.T, tool, symbol string) callersResult {
	t.Helper()
	text, isErr := rsCall(t, tool, map[string]interface{}{"symbol": symbol})
	if isErr {
		t.Fatalf("%s %s: %s", tool, symbol, text)
	}
	var out callersResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return out
}

func TestOneRegistrationInvariant_Rust(t *testing.T) {
	if syntax.DetectLanguage("src/lib.rs") != "rust" {
		t.Error("syntax detection does not route .rs to rust")
	}
	found := false
	for _, p := range defaultProviders() {
		found = found || p.Language() == "rust"
	}
	if !found {
		t.Error("defaultProviders() misses rust")
	}
	if _, ok := refProviderRegistry[".rs"]; !ok {
		t.Error("find_references registry misses .rs")
	}
	if lvl := languages.Registry().SupportLevelFor("rust"); lvl.String() != "graph" {
		t.Errorf("rust support level %s", lvl)
	}
	text, _ := rsCall(t, "get_language_support", map[string]interface{}{})
	var entries []languageSupportEntry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, e := range entries {
		if e.Language == "rust" {
			ok = e.Level == "graph" && containsExt(e.Extensions, ".rs")
		}
	}
	if !ok {
		t.Errorf("get_language_support does not report rust at graph with .rs: %s", text)
	}
}

// An inherent method, the trait's declaration and the trait
// implementation's method of one name are three symbols: a single-target
// tool never picks one, and each has its own callers.
func TestMCP_Rust_TraitImplIsNotInherent(t *testing.T) {
	text, isErr := rsCall(t, "get_callers", map[string]interface{}{"symbol": "save"})
	if !isErr || !strings.Contains(text, "Ambiguous") ||
		!strings.Contains(text, "crate::storage::Repository::save") ||
		!strings.Contains(text, "<crate::storage::Repository as crate::storage::Store>::save") ||
		!strings.Contains(text, "crate::storage::Store::save") {
		t.Fatalf("want an ambiguity listing the three declarations:\n%s", text)
	}
	out := rsEdges(t, "get_callers", "crate::storage::Repository::save")
	if len(out.Edges) != 1 || out.Edges[0].To != "<crate::storage::Repository as crate::storage::Store>::save" || out.Edges[0].Confidence != "exact" {
		t.Errorf("callers of the inherent save: %+v", out.Edges)
	}
	if out := rsEdges(t, "get_callers", "<crate::storage::Repository as crate::storage::Store>::save"); len(out.Edges) != 0 {
		t.Errorf("the trait implementation's save has callers %+v (a method call is never bound by name)", out.Edges)
	}
}

// Two crates' functions of one path are two symbols.
func TestMCP_Rust_CratesAreDistinct(t *testing.T) {
	text, isErr := rsCall(t, "get_callers", map[string]interface{}{"symbol": "helper"})
	if !isErr || !strings.Contains(text, "Ambiguous") ||
		!strings.Contains(text, "src/bin/tool.rs/crate::helper") || !strings.Contains(text, "crates/util/crate::helper") {
		t.Fatalf("want an ambiguity across crates:\n%s", text)
	}
	out := rsEdges(t, "get_callers", "src/bin/tool.rs/crate::helper")
	if len(out.Edges) != 1 || out.Edges[0].To != "src/bin/tool.rs/crate::main" {
		t.Errorf("callers of the binary's helper: %+v", out.Edges)
	}
}

func TestMCP_Rust_CalleesContextImpactRepomap(t *testing.T) {
	out := rsEdges(t, "get_callees", "crate::open")
	var got []string
	for _, e := range out.Edges {
		got = append(got, e.To+" "+e.Kind)
	}
	want := []string{"crate::log_line! calls", "crate::storage::Repository uses_type", "crate::storage::Repository::new calls"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("callees of crate::open %v, want %v", got, want)
	}

	text, isErr := rsCall(t, "get_context", map[string]interface{}{"symbol": "crate::storage::Repository::new"})
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{"Reason: target", "Symbol: crate::open", "Reason: caller"} {
		if !strings.Contains(text, want) {
			t.Errorf("get_context misses %q:\n%s", want, text)
		}
	}

	text, isErr = rsCall(t, "analyze_change_impact", map[string]interface{}{"symbol": "crate::storage::Repository::save"})
	if isErr || !strings.Contains(text, "<crate::storage::Repository as crate::storage::Store>::save") {
		t.Errorf("analyze_change_impact:\n%s", text)
	}

	text, isErr = rsCall(t, "get_repository_map", map[string]interface{}{})
	if isErr || !strings.Contains(text, "rust") || !strings.Contains(text, "src/storage") {
		t.Errorf("get_repository_map:\n%s", text)
	}
}

func TestMCP_Rust_FindTools(t *testing.T) {
	text, _ := rsCall(t, "find_symbol", map[string]interface{}{"pattern": "Repository"})
	if !strings.Contains(text, "src/storage/mod.rs") || !strings.Contains(text, `"kind": "struct"`) {
		t.Errorf("find_symbol:\n%s", text)
	}
	text, _ = rsCall(t, "search_code", map[string]interface{}{"kind": "impl"})
	if !strings.Contains(text, "Found 6 symbol(s)") {
		t.Errorf("search_code kind=impl:\n%s", text)
	}
	text, _ = rsCall(t, "search_code", map[string]interface{}{"kind": "macro"})
	if !strings.Contains(text, "Found 1 symbol(s)") || !strings.Contains(text, "log_line") {
		t.Errorf("search_code kind=macro:\n%s", text)
	}
	h := &ToolsHandler{rootDir: rustFixture()}
	res, err := h.CallTool("get_symbols", map[string]interface{}{"path": "src/storage/mod.rs"})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].Text, "Repository") || !strings.Contains(res.Content[0].Text, "Store") {
		t.Errorf("get_symbols: %v %+v", err, res)
	}
	text, isErr := rsCall(t, "get_diagnostics", map[string]interface{}{})
	if isErr || !strings.Contains(text, `"errors": 0`) {
		t.Errorf("get_diagnostics:\n%s", text)
	}
}

// Secret masking covers Rust sources through every reading tool: a secret
// in a literal is masked in file content, a symbol's source, context and the
// arklite bundle.
func TestMCP_Rust_SecretsMasked(t *testing.T) {
	root := t.TempDir()
	aws := "AKIA" + "IOSFODNN7EXAMPLE"
	gh := "ghp_" + strings.Repeat("b", 36)
	writeTree(t, root, map[string]string{
		"Cargo.toml": "[package]\nname = \"s\"\n",
		"src/lib.rs": "pub const AWS_KEY: &str = \"" + aws + "\";\n\npub fn token() -> &'static str {\n    let api_token = \"" + gh + "\";\n    api_token\n}\n\npub fn use_it() -> &'static str { token() }\n",
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
		{"get_file_content", map[string]interface{}{"path": "src/lib.rs"}},
		{"get_symbol", map[string]interface{}{"path": "src/lib.rs", "name": "token"}},
		{"get_symbol", map[string]interface{}{"path": "src/lib.rs", "name": "AWS_KEY"}},
		{"get_context", map[string]interface{}{"path": ".", "symbol": "crate::use_it"}},
		{"get_files_arklite", map[string]interface{}{"paths": []interface{}{"src/lib.rs"}}},
	} {
		text, isErr := callText(t, h, c.tool, c.args)
		if isErr {
			t.Fatalf("%s %v: %s", c.tool, c.args, text)
		}
		if strings.Contains(text, aws) || strings.Contains(text, gh) {
			t.Errorf("%s %v leaks a secret:\n%s", c.tool, c.args, text)
		}
		if !strings.Contains(text, "MASKED") {
			t.Errorf("%s %v shows no masked value (the secret's line is not in the output):\n%s", c.tool, c.args, text)
		}
	}
}

// A reverse edge says what the reference is: a trait's implementations are
// implemented_by, a type's users type_used_by — never called_by.
func TestMCP_Rust_ReverseKinds(t *testing.T) {
	kinds := func(symbol string) map[string]string {
		out := rsEdges(t, "get_callers", symbol)
		m := map[string]string{}
		for _, e := range out.Edges {
			m[e.To] = e.Kind
		}
		return m
	}
	if got := kinds("crate::storage::Store")["<crate::storage::Repository as crate::storage::Store>"]; got != "implemented_by" {
		t.Errorf("Store's implementation is %q, want implemented_by", got)
	}
	repo := kinds("crate::storage::Repository")
	if repo["crate::open"] != "type_used_by" || repo["crate::storage::Repository::new"] != "called_by" {
		t.Errorf("Repository's users: %v (want crate::open type_used_by, its construction in new called_by)", repo)
	}
	for to, k := range kinds("crate::model::Named") {
		if k == "called_by" {
			t.Errorf("%s is called_by of a trait", to)
		}
	}
}

// A re-exported name (`pub use storage::Repository as Repo;` in the crate
// root) is looked up as what it names.
func TestMCP_Rust_ReExportLookup(t *testing.T) {
	out := rsEdges(t, "get_callers", "crate::Repo")
	callers := map[string]bool{}
	for _, e := range out.Edges {
		callers[e.To] = true
	}
	if !callers["crate::open"] || !callers["crate::model::repository"] {
		t.Fatalf("crate::Repo's callers %v, want Repository's (crate::open, crate::model::repository)", callers)
	}
	text, isErr := rsCall(t, "get_context", map[string]interface{}{"symbol": "crate::Repo::new"})
	if isErr || !strings.Contains(text, "Symbol: crate::storage::Repository::new") {
		t.Errorf("get_context through the re-export:\n%s", text)
	}
	if text, isErr := rsCall(t, "get_callers", map[string]interface{}{"symbol": "crate::NoSuchName"}); !isErr {
		t.Errorf("an unknown name was found: %s", text)
	}
}
