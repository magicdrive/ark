package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// get_relations reads the shared repository index, and its JSON keeps every
// relation the resolver established: the reference kind (call, construction,
// type_use, read, ...), confidence and evidence, both directions, resolved and
// candidate. Sharing a source of truth does not require a lossy representation.

func relationsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/Svc.php": `<?php
namespace App;

class Svc
{
    public function target(Repo $repo, $view)
    {
        $repo->save();
        $view->render();
        $d = new Dto();
        return Config::LIMIT;
    }
}
`,
		"app/Repo.php":    "<?php\nnamespace App;\nclass Repo { public function save() {} }\n",
		"app/Dto.php":     "<?php\nnamespace App;\nclass Dto {}\n",
		"app/Config.php":  "<?php\nnamespace App;\nclass Config { const LIMIT = 10; }\n",
		"app/AB.php":      "<?php\nnamespace App;\nclass A { public function render() {} }\nclass B { public function render() {} }\nclass Other { public function target() {} }\n",
		"app/Callers.php": "<?php\nnamespace App;\nclass TypedCaller { public function run(Svc $s) { return $s->target(new Repo(), null); } }\nclass UntypedCaller { public function run($s) { return $s->target(null, null); } }\n",
		// A property and a method share the name Cmd.cache: the code calling
		// save() is no one identified symbol.
		"app/Cmd.php": "<?php\nnamespace App;\nclass Cmd\n{\n    protected $cache;\n    public function cache(Repo $r) { return $r->save(); }\n}\n",
	})
	return root
}

type relationsOut struct {
	Relations                []relationEntry `json:"relations"`
	CandidateCallers         int             `json:"candidateCallers"`
	CandidateCallees         int             `json:"candidateCallees"`
	CandidateCallerRelations int             `json:"candidateCallerRelations"`
	CandidateCalleeRelations int             `json:"candidateCalleeRelations"`
	Unattributed             int             `json:"unattributed"`
}

func relationsFor(t *testing.T, h *ToolsHandler, symbol string) relationsOut {
	t.Helper()
	text, isErr := callText(t, h, "get_relations", map[string]interface{}{"path": ".", "symbol": symbol, "maxResults": float64(500)})
	if isErr {
		t.Fatalf("get_relations(%s): %s", symbol, text)
	}
	var out relationsOut
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func relationLines(rs []relationEntry) []string {
	var out []string
	for _, r := range rs {
		line := fmt.Sprintf("%s %s %s %s | %s", r.Direction, r.Qualified, r.Kind, r.Confidence, r.Evidence)
		if r.SourceUnidentified {
			line += " [source unidentified]"
		}
		out = append(out, line)
	}
	return out
}

// The Phase 5 observable relations, kept exactly.
func TestRelations_ObservableContract(t *testing.T) {
	h := NewToolsHandler(relationsRepo(t), nil)
	got := relationsFor(t, h, `App\Svc.target`)
	want := []string{
		`called_by App\TypedCaller.run call exact | receiver type "App\\Svc"`,
		`called_by App\UntypedCaller.run call candidate | symbol "target" found in same package as app/Callers.php`,
		`calls App\A.render call candidate | symbol "render" found in same package as app/Svc.php`,
		`calls App\B.render call candidate | symbol "render" found in same package as app/Svc.php`,
		`calls App\Config.LIMIT read exact | receiver type "App\\Config"`,
		`calls App\Dto construction exact | "App\\Dto" is declared at app/Dto.php`,
		`calls App\Repo type_use exact | "App\\Repo" is declared at app/Repo.php`,
		`calls App\Repo.save call exact | receiver type "App\\Repo"`,
	}
	if lines := relationLines(got.Relations); !reflect.DeepEqual(lines, want) {
		t.Errorf("relations:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if got.CandidateCallers != 1 || got.CandidateCallees != 2 || got.Unattributed != 2 {
		t.Errorf("candidateCallers=%d candidateCallees=%d unattributed=%d, want 1 2 2", got.CandidateCallers, got.CandidateCallees, got.Unattributed)
	}
	// The other ends see the same relations from their side.
	if lines := relationLines(relationsFor(t, h, `App\Config.LIMIT`).Relations); !reflect.DeepEqual(lines, []string{`called_by App\Svc.target read exact | receiver type "App\\Config"`}) {
		t.Errorf("Config.LIMIT: %v", lines)
	}
	if lines := relationLines(relationsFor(t, h, `App\A.render`).Relations); !reflect.DeepEqual(lines, []string{`called_by App\Svc.target call candidate | symbol "render" found in same package as app/Svc.php`}) {
		t.Errorf("A.render: %v", lines)
	}
}

// Ambiguity is evidence: an ambiguous call out of a symbol lists its possible
// targets in get_relations and get_callees — never only a count.
func TestRelations_OutgoingCandidatesAreListed(t *testing.T) {
	h := NewToolsHandler(relationsRepo(t), nil)
	var outgoing []string
	for _, r := range relationsFor(t, h, `App\Svc.target`).Relations {
		if r.Direction == "calls" && r.Confidence == "candidate" {
			outgoing = append(outgoing, r.Qualified+" "+r.Kind)
		}
	}
	if !reflect.DeepEqual(outgoing, []string{`App\A.render call`, `App\B.render call`}) {
		t.Errorf("get_relations outgoing candidates = %v: the ambiguous call collapsed", outgoing)
	}
	out, text, _ := callGraphTool(t, h.rootDir, "get_callees", map[string]interface{}{"path": ".", "symbol": `App\Svc.target`})
	var cands []string
	for _, c := range out.Candidates {
		cands = append(cands, c.Symbol+" "+c.Kind+" "+c.Confidence)
	}
	if !reflect.DeepEqual(cands, []string{`App\A.render call candidate`, `App\B.render call candidate`}) || *out.Unattributed != 1 {
		t.Errorf("get_callees candidates = %v, unattributed %d:\n%s", cands, *out.Unattributed, text)
	}
	// Candidates never become edges, and the Context Engine never uses them.
	for _, e := range out.Edges {
		if strings.HasSuffix(e.To, ".render") {
			t.Errorf("candidate promoted to an edge: %v", e)
		}
	}
	ctx, _ := callText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": `App\Svc.target`, "maxTokens": float64(8000)})
	for _, no := range []string{`App\A.render`, `App\B.render`, `App\UntypedCaller.run`} {
		if strings.Contains(ctx, "Symbol: "+no) {
			t.Errorf("get_context promoted candidate %s", no)
		}
	}
}

// A relation from code that is no one symbol is listed by that code's name,
// marked, never attributed to one of the same-named symbols, and counted as
// unattributed.
func TestRelations_UnidentifiedSource(t *testing.T) {
	h := NewToolsHandler(relationsRepo(t), nil)
	got := relationsFor(t, h, `App\Repo.save`)
	want := []string{
		`called_by App\Cmd.cache call exact | receiver type "App\\Repo" [source unidentified]`,
		`called_by App\Svc.target call exact | receiver type "App\\Repo"`,
	}
	if lines := relationLines(got.Relations); !reflect.DeepEqual(lines, want) {
		t.Errorf("relations:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	out, _, _ := callGraphTool(t, h.rootDir, "get_callers", map[string]interface{}{"path": ".", "symbol": `App\Repo.save`})
	if len(out.Edges) != 1 || *out.Unattributed != 1 {
		t.Errorf("get_callers: edges %v unattributed %d, want only Svc.target and 1 unattributed", out.Edges, *out.Unattributed)
	}
}

// Candidate samples hold at most MaxCandidateSources relations per direction,
// chosen deterministically; the totals count them all.
func TestRelations_CandidateSampleBounded(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"app/P.php": "<?php\nnamespace App;\nclass P { public function ping() {} }\nclass Q { public function ping() {} }\n",
	}
	var calls []string
	for i := 0; i < 12; i++ {
		files[fmt.Sprintf("app/C%02d.php", i)] = fmt.Sprintf("<?php\nnamespace App;\nclass C%02d { public function run($x) { return $x->ping(); } }\n", i)
		calls = append(calls, fmt.Sprintf("$x->m%02d();", i))
		files[fmt.Sprintf("app/M%02d.php", i)] = fmt.Sprintf("<?php\nnamespace App;\nclass M%02dA { public function m%02d() {} }\nclass M%02dB { public function m%02d() {} }\n", i, i, i, i)
	}
	files["app/T.php"] = "<?php\nnamespace App;\nclass T { public function fan($x) { " + strings.Join(calls, " ") + " } }\n"
	writeTree(t, root, files)
	h := NewToolsHandler(root, nil)

	in := relationsFor(t, h, `App\P.ping`)
	if len(in.Relations) != index.MaxCandidateSources || in.CandidateCallers != 12 || in.CandidateCallerRelations != 12 {
		t.Errorf("P.ping: %d relations, candidateCallers %d; want %d of 12", len(in.Relations), in.CandidateCallers, index.MaxCandidateSources)
	}
	// The first sources in resolution order (files sorted): C00 … C09.
	for i, r := range in.Relations {
		if want := fmt.Sprintf(`App\C%02d.run`, i); r.Qualified != want {
			t.Errorf("sample[%d] = %s, want %s", i, r.Qualified, want)
		}
	}
	out := relationsFor(t, h, `App\T.fan`)
	if len(out.Relations) != index.MaxCandidateSources || out.CandidateCallees != 24 || out.CandidateCalleeRelations != 24 || out.Unattributed != 12 {
		t.Errorf("T.fan: %d relations, candidateCallees %d, unattributed %d; want %d of 24, 12", len(out.Relations), out.CandidateCallees, out.Unattributed, index.MaxCandidateSources)
	}
	callers, _, _ := callGraphTool(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": `App\P.ping`})
	callees, _, _ := callGraphTool(t, root, "get_callees", map[string]interface{}{"path": ".", "symbol": `App\T.fan`})
	if len(callers.Candidates) != index.MaxCandidateSources || len(callees.Candidates) != index.MaxCandidateSources {
		t.Errorf("get_callers %d / get_callees %d candidates, want %d each", len(callers.Candidates), len(callees.Candidates), index.MaxCandidateSources)
	}
	for _, tool := range []string{"get_callers", "get_callees"} {
		sym := map[string]string{"get_callers": `App\P.ping`, "get_callees": `App\T.fan`}[tool]
		text, _ := callText(t, h, tool, map[string]interface{}{"path": ".", "symbol": sym})
		if want := map[string]string{"get_callers": `"candidatesTotal": 12`, "get_callees": `"candidatesTotal": 24`}[tool]; !strings.Contains(text, want) {
			t.Errorf("%s: missing %s", tool, want)
		}
	}
	// Deterministic across fresh builds.
	first, _ := callText(t, NewToolsHandler(root, nil), "get_relations", map[string]interface{}{"path": ".", "symbol": `App\T.fan`})
	for i := 0; i < 3; i++ {
		if again, _ := callText(t, NewToolsHandler(root, nil), "get_relations", map[string]interface{}{"path": ".", "symbol": `App\T.fan`}); again != first {
			t.Fatal("candidate sample is not deterministic")
		}
	}
}

// Candidate relations are identified by symbol and reference kind: `new Foo()`
// and `Foo()` are two relations with each Foo, repeated references one
// relation with a reference count. The totals count distinct symbols and
// distinct relations, so nothing collapses unseen.
func TestRelations_CandidateRelationIdentity(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.js":   "export class Foo {}\n",
		"b.js":   "export class Foo {}\n",
		"use.js": "export function useIt() {\n  const f = new Foo();\n  Foo();\n  Foo();\n}\n",
	})
	h := NewToolsHandler(root, nil)
	text, isErr := callText(t, h, "get_relations", map[string]interface{}{"path": ".", "symbol": "useIt"})
	if isErr {
		t.Fatal(text)
	}
	var out struct {
		Relations                []relationEntry `json:"relations"`
		CandidateCallees         int             `json:"candidateCallees"`
		CandidateCalleeRelations int             `json:"candidateCalleeRelations"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range out.Relations {
		got = append(got, fmt.Sprintf("%s %s %s %s refs=%d", r.Direction, r.File, r.Kind, r.Confidence, r.References))
	}
	want := []string{
		"calls a.js call candidate refs=2",
		"calls a.js construction candidate refs=1",
		"calls b.js call candidate refs=2",
		"calls b.js construction candidate refs=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("relations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if out.CandidateCallees != 2 || out.CandidateCalleeRelations != 4 {
		t.Errorf("candidateCallees=%d candidateCalleeRelations=%d, want 2 symbols, 4 relations", out.CandidateCallees, out.CandidateCalleeRelations)
	}
	// get_callees lists the same relations and totals.
	ct, _ := callText(t, h, "get_callees", map[string]interface{}{"path": ".", "symbol": "useIt"})
	var callees struct {
		Edges                   []edgeEntry      `json:"edges"`
		Candidates              []candidateEntry `json:"candidates"`
		CandidatesTotal         int              `json:"candidatesTotal"`
		CandidateRelationsTotal int              `json:"candidateRelationsTotal"`
	}
	if err := json.Unmarshal([]byte(ct), &callees); err != nil {
		t.Fatal(err)
	}
	if len(callees.Candidates) != 4 || callees.CandidatesTotal != 2 || callees.CandidateRelationsTotal != 4 {
		t.Errorf("get_callees: %d candidates, candidatesTotal %d, candidateRelationsTotal %d; want 4, 2, 4:\n%s", len(callees.Candidates), callees.CandidatesTotal, callees.CandidateRelationsTotal, ct)
	}
	// Candidates stay candidates: never edges.
	if len(callees.Edges) != 0 {
		t.Errorf("candidate relations became edges: %v", callees.Edges)
	}
}
