package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
)

// End-to-end MCP validation over the real TypeScript / TSX pipeline. The
// fixtures are the context-quality repositories of the typescript provider, so
// every surface is checked against the same semantic truth.

func tsFixture(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "languages", "typescript", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// callAt calls a tool with explicit arguments (path is not forced).
func callAt(t *testing.T, dir, tool string, args map[string]interface{}) (*CallToolResult, string) {
	t.Helper()
	h := &ToolsHandler{rootDir: dir}
	res, err := h.CallTool(tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var text string
	for _, c := range res.Content {
		text += c.Text
	}
	return res, text
}

func mustOK(t *testing.T, res *CallToolResult, tool, text string) {
	t.Helper()
	if res.IsError {
		t.Fatalf("%s returned an error:\n%s", tool, text)
	}
}

func contains(t *testing.T, tool, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("%s output missing %q:\n%s", tool, w, text)
		}
	}
}

func excludes(t *testing.T, tool, text string, nos ...string) {
	t.Helper()
	for _, n := range nos {
		if strings.Contains(text, n) {
			t.Errorf("%s output must not contain %q:\n%s", tool, n, text)
		}
	}
}

func TestMCP_TS_LanguageSupportMatchesRegistry(t *testing.T) {
	res, text := callAt(t, tsFixture(t, "cq_ts"), "get_language_support", map[string]interface{}{})
	mustOK(t, res, "get_language_support", text)
	for _, lang := range []string{"typescript", "tsx"} {
		level := languages.Registry().SupportLevelFor(language.Language(lang))
		i := strings.Index(text, `"language": "`+lang+`"`)
		if i < 0 {
			t.Fatalf("%s missing from get_language_support:\n%s", lang, text)
		}
		seg := text[i:]
		if j := strings.Index(seg, `"extensions"`); j > 0 {
			seg = seg[:j]
		}
		if !strings.Contains(seg, `"level": "`+level.String()+`"`) {
			t.Errorf("%s: tool level does not match the registry level %q:\n%s", lang, level, seg)
		}
	}
}

func TestMCP_TS_Workflow(t *testing.T) {
	dir := tsFixture(t, "cq_ts")
	const (
		userSave  = "UserRepository.save"
		orderSave = "OrderRepository.save"
	)

	// find_symbol: members are first-class symbols with containment.
	res, text := callAt(t, dir, "find_symbol", map[string]interface{}{"pattern": "save", "path": "."})
	mustOK(t, res, "find_symbol", text)
	contains(t, "find_symbol", text, `"receiver": "UserRepository"`, `"receiver": "OrderRepository"`, "src/repo/user-repository.ts")

	// get_symbol: bare unique name and qualified name.
	res, text = callAt(t, dir, "get_symbol", map[string]interface{}{"path": "src/service/user-service.ts", "name": "create"})
	mustOK(t, res, "get_symbol", text)
	contains(t, "get_symbol", text, `"parent": "UserService"`, "this.repo.save(u)")
	res, text = callAt(t, dir, "get_symbol", map[string]interface{}{"path": "src/service/user-service.ts", "name": "UserService.create"})
	mustOK(t, res, "get_symbol(qualified)", text)
	contains(t, "get_symbol(qualified)", text, "this.repo.save(u)")

	// find_references: type references carry the qualified container.
	res, text = callAt(t, dir, "find_references", map[string]interface{}{"path": ".", "name": "UserRepository"})
	mustOK(t, res, "find_references", text)
	contains(t, "find_references", text, `"container": "UserService.constructor"`, `"container": "typedHandler"`)

	// get_relations: the typed receiver resolves; the same-name method does not.
	res, text = callAt(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": "UserService.create"})
	mustOK(t, res, "get_relations", text)
	contains(t, "get_relations", text, `"qualified": "`+userSave+`"`, `"qualified": "UserService.audit"`, `"confidence": "exact"`)
	excludes(t, "get_relations", text, orderSave)

	// get_callees
	res, text = callAt(t, dir, "get_callees", map[string]interface{}{"path": ".", "symbol": "UserService.create"})
	mustOK(t, res, "get_callees", text)
	contains(t, "get_callees", text, `"to": "`+userSave+`"`, `"to": "User"`)
	excludes(t, "get_callees", text, orderSave)

	// get_callers: typed callers only; the untyped receiver is not a caller —
	// it is reported as a possible (candidate) caller, never as an edge.
	res, text = callAt(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": userSave})
	mustOK(t, res, "get_callers", text)
	contains(t, "get_callers", text, `"to": "UserService.create"`, `"to": "typedHandler"`)
	edges, cands := callersEdgesAndCandidates(t, text)
	excludes(t, "get_callers edges", edges, "untypedHandler")
	contains(t, "get_callers candidates", cands, `"untypedHandler"`, `"confidence": "candidate"`)

	// get_context: required context present, unrelated / same-name absent.
	res, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "UserService.create"})
	mustOK(t, res, "get_context", text)
	contains(t, "get_context", text, "UserRepository.save", "this.repo.save(u)")
	excludes(t, "get_context", text, "OrderRepository", "UnrelatedService", "OrderService")

	// analyze_change_impact
	res, text = callAt(t, dir, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": userSave})
	mustOK(t, res, "analyze_change_impact", text)
	contains(t, "analyze_change_impact", text, "UserService.create", "typedHandler")
	excludes(t, "analyze_change_impact", text, "OrderRepository")
	// The untyped receiver is a possible dependent only: never a definite one,
	// (its file is affected anyway, through the definite typedHandler).
	definite, possible := impactDefiniteAndPossible(text)
	excludes(t, "analyze_change_impact definite", definite, "untypedHandler")
	contains(t, "analyze_change_impact possible", possible, "untypedHandler", "[candidate]")

	// get_repository_map / search_code
	res, text = callAt(t, dir, "get_repository_map", map[string]interface{}{"path": "."})
	mustOK(t, res, "get_repository_map", text)
	contains(t, "get_repository_map", text, "typescript", "UserService", "UserRepository")
	res, text = callAt(t, dir, "search_code", map[string]interface{}{"path": ".", "kind": "method", "language": "typescript"})
	mustOK(t, res, "search_code", text)
	contains(t, "search_code", text, "UserService.create", userSave, orderSave)
}

// Candidate zero must never be reintroduced at the MCP boundary: a short name
// that denotes several symbols is rejected with the candidates listed.
func TestMCP_TS_AmbiguousNamesAreRejected(t *testing.T) {
	dir := tsFixture(t, "cq_ts")
	for _, tool := range []string{"get_context", "analyze_change_impact", "get_relations", "get_callers", "get_callees"} {
		res, text := callAt(t, dir, tool, map[string]interface{}{"path": ".", "symbol": "save"})
		if !res.IsError || !strings.Contains(text, "Ambiguous") {
			t.Errorf("%s(save) must reject the ambiguous name; got isError=%v:\n%s", tool, res.IsError, text)
			continue
		}
		contains(t, tool, text, "UserRepository.save", "OrderRepository.save")
	}
	// A qualified name disambiguates.
	res, text := callAt(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": "OrderRepository.save"})
	mustOK(t, res, "get_callers(qualified)", text)
	edges, _ := callersEdgesAndCandidates(t, text)
	excludes(t, "get_callers(qualified) edges", edges, "typedHandler", "UserService.create")
}

func TestMCP_GetSymbol_AmbiguousInFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.ts"),
		[]byte("export class A { save() { return 1; } }\nexport class B { save() { return 2; } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, text := callAt(t, dir, "get_symbol", map[string]interface{}{"path": "a.ts", "name": "save"})
	if !res.IsError || !strings.Contains(text, "Ambiguous") {
		t.Fatalf("two same-name members in one file must be ambiguous, got isError=%v:\n%s", res.IsError, text)
	}
	contains(t, "get_symbol", text, "A.save", "B.save")
	for _, q := range []string{"A.save", "B.save"} {
		res, text = callAt(t, dir, "get_symbol", map[string]interface{}{"path": "a.ts", "name": q})
		mustOK(t, res, "get_symbol "+q, text)
	}
	_, text = callAt(t, dir, "get_symbol", map[string]interface{}{"path": "a.ts", "name": "B.save"})
	contains(t, "get_symbol B.save", text, "return 2")
}

func TestMCP_TSX_Workflow(t *testing.T) {
	dir := tsFixture(t, "cq_tsx")

	res, text := callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "UserPage"})
	mustOK(t, res, "get_context", text)
	contains(t, "get_context", text, "UserCard", "useUser", "Button")
	excludes(t, "get_context", text, "legacy", "Unrelated")

	// Two components named UserCard: ambiguity unless narrowed.
	res, text = callAt(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": "UserCard"})
	if !res.IsError || !strings.Contains(text, "Ambiguous") {
		t.Fatalf("ambiguous UserCard must be rejected, got:\n%s", text)
	}
	res, text = callAt(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": "UserCard", "filePattern": "src/components"})
	mustOK(t, res, "get_callers(filePattern)", text)
	contains(t, "get_callers", text, `"to": "UserPage"`)
	excludes(t, "get_callers", text, "legacy")

	// Intrinsic elements are never repository dependencies.
	res, text = callAt(t, dir, "get_callees", map[string]interface{}{"path": ".", "symbol": "UserPage"})
	mustOK(t, res, "get_callees", text)
	excludes(t, "get_callees", text, `"to": "span"`, `"to": "div"`, `"to": "button"`)
}

// get_relations must scan the same files as repository indexing: node_modules
// and hidden directories are never part of the repository.
func TestMCP_GetRelations_SkipsNodeModulesAndHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"src/a.ts":                  "export function helper() {}\nexport function main() { helper(); }\n",
		"node_modules/pkg/index.ts": "export function main() {}\n",
		".hidden/b.ts":              "export function main() {}\n",
		"vendor/v.ts":               "export function main() {}\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, text := callAt(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": "main"})
	if res.IsError || strings.Contains(text, "Ambiguous") {
		t.Fatalf("node_modules / hidden / vendor duplicates must not make main ambiguous:\n%s", text)
	}
	contains(t, "get_relations", text, `"qualified": "helper"`)
}

// callersEdgesAndCandidates splits a get_callers / get_callees result into the
// JSON of its resolved edges and of its candidate (possible) callers.
func callersEdgesAndCandidates(t *testing.T, text string) (edges, candidates string) {
	t.Helper()
	var out struct {
		Edges      json.RawMessage `json:"edges"`
		Candidates json.RawMessage `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("callers result is not JSON: %v\n%s", err, text)
	}
	return string(out.Edges), string(out.Candidates)
}

// impactDefiniteAndPossible splits analyze_change_impact text output into the
// definite part (every section except possible dependents, affected files
// included) and the possible-dependents section.
func impactDefiniteAndPossible(text string) (definite, possible string) {
	const head = "Possible dependents (low confidence):\n"
	i := strings.Index(text, head)
	if i < 0 {
		return text, ""
	}
	rest := text[i+len(head):]
	j := strings.Index(rest, "\n\n")
	if j < 0 {
		j = len(rest)
	}
	return text[:i] + rest[j:], rest[:j]
}
