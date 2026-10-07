package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Target lookup and result semantics of the graph tools: a target is found by
// the names people write, never chosen among several, and an answer says
// whether it is complete ("Unknown is not empty").

func phpSemanticsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/Services/LoginScreenPolicy.php": `<?php
namespace App\Services;

class LoginScreenPolicy
{
    public function showsSsoButton() { return true; }
    public function isSsoOnly() { return $this->showsSsoButton(); }
    public function unused() { return 1; }
}
`,
		"app/Models/Clinic.php": `<?php
namespace App\Models;

class Clinic
{
    public function showsSsoButton() { return false; }
}
`,
		// Laravel 6 style: untyped property injected by the constructor → the
		// constructor parameter type proves the receiver (Exact, private).
		"app/Http/LoginController.php": `<?php
namespace App\Http;

use App\Services\LoginScreenPolicy;

class LoginController
{
    private $policy;
    public function __construct(LoginScreenPolicy $policy) { $this->policy = $policy; }
    public function showLoginForm() { return $this->policy->showsSsoButton(); }
}
`,
		// The property is reassigned outside the constructor → no evidence: the
		// call is a Candidate.
		"app/Http/LegacyController.php": `<?php
namespace App\Http;

use App\Services\LoginScreenPolicy;

class LegacyController
{
    private $policy;
    public function __construct(LoginScreenPolicy $policy) { $this->policy = $policy; }
    public function swap($policy) { $this->policy = $policy; }
    public function render() { return $this->policy->showsSsoButton(); }
}
`,
		// Typed property → an Exact edge.
		"app/Http/TypedController.php": `<?php
namespace App\Http;

use App\Services\LoginScreenPolicy;

class TypedController
{
    private LoginScreenPolicy $policy;
    public function __construct(LoginScreenPolicy $policy) { $this->policy = $policy; }
    public function show() { return $this->policy->showsSsoButton(); }
}
`,
		// Inherited member → the call is Unresolved (same name, no candidate).
		"app/Services/Base.php": `<?php
namespace App\Services;

abstract class Base
{
    protected function common() { return 1; }
}
`,
		"app/Services/Child.php": `<?php
namespace App\Services;

class Child extends Base
{
    public function run() { return $this->common(); }
}
`,
		// A vendor type with a same-named member: proven outside the repository.
		"app/Models/Request.php": `<?php
namespace App\Models;

class Request
{
    public function input() { return 1; }
}
`,
		"app/Http/VendorUser.php": `<?php
namespace App\Http;

use Illuminate\Http\Request;

class VendorUser
{
    public function handle(Request $r) { return $r->input(); }
}
`,
		"app/Admin/UserService.php":   "<?php\nnamespace App\\Admin;\n\nclass UserService\n{\n    public function create() { return 1; }\n}\n",
		"app/Api/UserService.php":     "<?php\nnamespace App\\Api;\n\nclass UserService\n{\n    public function create() { return 2; }\n}\n",
		"app/Services/FooService.php": "<?php\nnamespace App\\Services;\n\nclass FooService\n{\n    public function run() { return 1; }\n}\n",
		// Top-level script code: a reference with no enclosing symbol.
		"bootstrap.php": "<?php\n$p = new \\App\\Services\\LoginScreenPolicy();\n",
	})
	return root
}

type graphResult struct {
	Edges        []edgeEntry      `json:"edges"`
	Relations    []relationEntry  `json:"relations"`
	Unattributed *int             `json:"unattributed"`
	Total        int              `json:"total"`
	Truncated    bool             `json:"truncated"`
	Candidates   []candidateEntry `json:"candidates"`
}

func callGraphTool(t *testing.T, dir, tool string, args map[string]interface{}) (graphResult, string, bool) {
	t.Helper()
	res, text := callAt(t, dir, tool, args)
	if res.IsError {
		return graphResult{}, text, true
	}
	var out graphResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s: result is not JSON: %v\n%s", tool, err, text)
	}
	if out.Unattributed == nil {
		t.Fatalf("%s: unattributed must always be present:\n%s", tool, text)
	}
	return out, text, false
}

func edgeTargets(edges []edgeEntry) []string {
	out := []string{}
	for _, e := range edges {
		out = append(out, e.To+" ["+e.Confidence+"]")
	}
	return out
}

func candidateNames(cs []candidateEntry) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Symbol+" ["+c.Confidence+"]")
	}
	return out
}

func TestTargetLookup_NaturalNames(t *testing.T) {
	dir := phpSemanticsRepo(t)
	const policySso = `App\Services\LoginScreenPolicy.showsSsoButton`
	cases := []struct {
		name, input string
		want        string // resolved target, or "" for not found
		ambiguous   []string
	}{
		{name: "exact qualified", input: policySso, want: policySso},
		{name: "exact name (unique)", input: "isSsoOnly", want: `App\Services\LoginScreenPolicy.isSsoOnly`},
		{name: "exact name (ambiguous)", input: "showsSsoButton",
			ambiguous: []string{`App\Models\Clinic.showsSsoButton`, policySso}},
		{name: "unique boundary suffix", input: "LoginScreenPolicy.showsSsoButton", want: policySso},
		{name: "namespace boundary suffix", input: `Services\LoginScreenPolicy.showsSsoButton`, want: policySso},
		{name: "Class::method", input: "LoginScreenPolicy::showsSsoButton", want: policySso},
		{name: "fully qualified with leading separator", input: `\App\Services\LoginScreenPolicy`, want: `App\Services\LoginScreenPolicy`},
		{name: "ambiguous boundary suffix", input: "UserService.create",
			ambiguous: []string{`App\Admin\UserService.create`, `App\Api\UserService.create`}},
		{name: "non-boundary suffix", input: "Service.run"},
		{name: "non-boundary suffix (class part)", input: "Policy.showsSsoButton"},
		{name: "not found", input: "DefinitelyDoesNotExist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, tool := range []string{"get_callers", "get_callees", "get_relations", "analyze_change_impact", "get_context"} {
				res, text := callAt(t, dir, tool, map[string]interface{}{"path": ".", "symbol": tc.input})
				switch {
				case len(tc.ambiguous) > 0:
					if !res.IsError || !strings.Contains(text, "Ambiguous") {
						t.Fatalf("%s(%q): want ambiguity error, got isError=%v:\n%s", tool, tc.input, res.IsError, text)
					}
					contains(t, tool, text, tc.ambiguous...)
				case tc.want == "":
					if !res.IsError || !strings.Contains(text, "not found") {
						t.Fatalf("%s(%q): want not-found error, got isError=%v:\n%s", tool, tc.input, res.IsError, text)
					}
				default:
					if res.IsError {
						t.Fatalf("%s(%q): unexpected error:\n%s", tool, tc.input, text)
					}
				}
			}
			if tc.want == "" {
				return
			}
			// The resolved target is the intended symbol (impact names it).
			_, text := callAt(t, dir, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": tc.input})
			if !strings.Contains(text, "Impact analysis: "+tc.want+"\n") {
				t.Errorf("%q resolved to the wrong target:\n%s", tc.input, text)
			}
		})
	}
}

// Existing exact lookups in other languages are unchanged.
func TestTargetLookup_OtherLanguagesUnchanged(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go/svc.go":     "package svc\n\ntype UserService struct{}\n\nfunc (s UserService) Create() {}\n\nfunc Run() { var s UserService; s.Create() }\n",
		"ts/repo.ts":    "export class Repository {\n  find() { return 1; }\n}\nexport function use(r: Repository) { return r.find(); }\n",
		"go/other.go":   "package svc\n\ntype OtherService struct{}\n\nfunc (s OtherService) Create() {}\n",
		"ts/service.ts": "export class Service {\n  find() { return 2; }\n}\n",
	})
	for input, want := range map[string]string{
		"UserService.Create": "UserService.Create",
		"Repository.find":    "Repository.find",
		"Run":                "Run",
	} {
		_, text := callAt(t, root, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": input})
		if !strings.Contains(text, "Impact analysis: "+want+"\n") {
			t.Errorf("%q: want target %s:\n%s", input, want, text)
		}
	}
	for _, input := range []string{"Create", "find"} {
		res, text := callAt(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": input})
		if !res.IsError || !strings.Contains(text, "Ambiguous") {
			t.Errorf("%q must stay ambiguous, got isError=%v:\n%s", input, res.IsError, text)
		}
	}
	// "Service.find" must not reach "Repository.find" or "UserService.Create".
	res, text := callAt(t, root, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "Service.find"})
	if res.IsError || !strings.Contains(text, "Impact analysis: Service.find\n") {
		t.Errorf("Service.find: want the exact Service.find, got isError=%v:\n%s", res.IsError, text)
	}
}

func TestGetCallers_ResultSemantics(t *testing.T) {
	dir := phpSemanticsRepo(t)
	cases := []struct {
		name             string
		symbol           string
		maxResults       int
		wantEdges        []string
		wantUnattributed int
		wantCandidates   []string
		wantTruncated    bool
		wantTotal        int
	}{
		{name: "true zero", symbol: "LoginScreenPolicy.unused", wantEdges: []string{}, wantUnattributed: 0, wantCandidates: []string{}},
		{name: "exact + candidate", symbol: "LoginScreenPolicy.showsSsoButton",
			wantEdges: []string{
				`App\Http\LoginController.showLoginForm [exact]`,
				`App\Http\TypedController.show [exact]`,
				`App\Services\LoginScreenPolicy.isSsoOnly [exact]`,
			},
			wantUnattributed: 1,
			wantCandidates:   []string{`App\Http\LegacyController.render [candidate]`}},
		{name: "candidate only", symbol: "Clinic.showsSsoButton", wantEdges: []string{}, wantUnattributed: 1,
			wantCandidates: []string{`App\Http\LegacyController.render [candidate]`}},
		{name: "unresolved same-name only", symbol: "Base.common", wantEdges: []string{}, wantUnattributed: 1, wantCandidates: []string{}},
		{name: "outside-repository same name is not unattributed", symbol: `App\Models\Request.input`,
			wantEdges: []string{}, wantUnattributed: 0, wantCandidates: []string{}},
		{name: "sourceless reference counts", symbol: `App\Services\LoginScreenPolicy`, maxResults: 1,
			wantEdges: []string{`App\Http\LegacyController.__construct [exact]`}, wantUnattributed: 1, wantCandidates: []string{},
			wantTruncated: true, wantTotal: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]interface{}{"path": ".", "symbol": tc.symbol}
			if tc.maxResults > 0 {
				args["maxResults"] = float64(tc.maxResults)
			}
			out, text, isErr := callGraphTool(t, dir, "get_callers", args)
			if isErr {
				t.Fatalf("unexpected error:\n%s", text)
			}
			if got := edgeTargets(out.Edges); !reflect.DeepEqual(got, tc.wantEdges) {
				t.Errorf("edges = %v, want %v", got, tc.wantEdges)
			}
			if *out.Unattributed != tc.wantUnattributed {
				t.Errorf("unattributed = %d, want %d", *out.Unattributed, tc.wantUnattributed)
			}
			if got := candidateNames(out.Candidates); !reflect.DeepEqual(got, tc.wantCandidates) {
				t.Errorf("candidates = %v, want %v", got, tc.wantCandidates)
			}
			if out.Truncated != tc.wantTruncated || out.Total != tc.wantTotal {
				t.Errorf("truncated=%v total=%d, want %v %d", out.Truncated, out.Total, tc.wantTruncated, tc.wantTotal)
			}
			// Candidates are never edges.
			for _, c := range out.Candidates {
				for _, e := range out.Edges {
					if e.To == c.Symbol {
						t.Errorf("%s is both an edge and a candidate", c.Symbol)
					}
				}
			}
		})
	}
	// Not found is an error, never an empty answer.
	if _, text, isErr := callGraphTool(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": "DefinitelyDoesNotExist"}); !isErr {
		t.Errorf("not found must be an error:\n%s", text)
	}
	// Byte-stable output.
	_, first, _ := callGraphTool(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton"})
	for i := 0; i < 3; i++ {
		if _, again, _ := callGraphTool(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton"}); again != first {
			t.Fatalf("get_callers output is not stable:\n%s\n---\n%s", first, again)
		}
	}
}

func TestGetCallees_ResultSemantics(t *testing.T) {
	dir := phpSemanticsRepo(t)
	cases := []struct {
		symbol           string
		wantEdges        []string
		wantUnattributed int
	}{
		{symbol: "LegacyController.render", wantEdges: []string{}, wantUnattributed: 1}, // candidate call
		{symbol: "LoginController.showLoginForm", wantEdges: []string{`App\Services\LoginScreenPolicy.showsSsoButton [exact]`}, wantUnattributed: 0},
		{symbol: "Child.run", wantEdges: []string{}, wantUnattributed: 1},         // unresolved inherited call
		{symbol: "VendorUser.handle", wantEdges: []string{}, wantUnattributed: 0}, // outside the repository: known
		{symbol: "TypedController.show", wantEdges: []string{`App\Services\LoginScreenPolicy.showsSsoButton [exact]`}, wantUnattributed: 0},
		{symbol: "LoginScreenPolicy.unused", wantEdges: []string{}, wantUnattributed: 0},
	}
	for _, tc := range cases {
		out, text, isErr := callGraphTool(t, dir, "get_callees", map[string]interface{}{"path": ".", "symbol": tc.symbol})
		if isErr {
			t.Fatalf("%s: unexpected error:\n%s", tc.symbol, text)
		}
		if got := edgeTargets(out.Edges); !reflect.DeepEqual(got, tc.wantEdges) {
			t.Errorf("%s: edges = %v, want %v", tc.symbol, got, tc.wantEdges)
		}
		if *out.Unattributed != tc.wantUnattributed {
			t.Errorf("%s: unattributed = %d, want %d", tc.symbol, *out.Unattributed, tc.wantUnattributed)
		}
		if len(out.Candidates) != 0 {
			t.Errorf("%s: callees carry no candidate callers: %v", tc.symbol, out.Candidates)
		}
	}
}

func TestGetRelations_ResultSemantics(t *testing.T) {
	dir := phpSemanticsRepo(t)
	out, text, isErr := callGraphTool(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.unused"})
	if isErr || out.Relations == nil || len(out.Relations) != 0 || *out.Unattributed != 0 {
		t.Errorf("true zero: want relations [] and unattributed 0:\n%s", text)
	}
	out, text, _ = callGraphTool(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": "Base.common"})
	if len(out.Relations) != 0 || *out.Unattributed != 1 {
		t.Errorf("unresolved only: want relations [] and unattributed 1:\n%s", text)
	}
	out, text, _ = callGraphTool(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton", "maxResults": float64(1)})
	if len(out.Relations) != 1 || !out.Truncated || out.Total != 4 || *out.Unattributed != 1 {
		t.Errorf("truncated: want 1 of 4 relations, unattributed 1:\n%s", text)
	}
	if _, text, isErr := callGraphTool(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": "DefinitelyDoesNotExist"}); !isErr {
		t.Errorf("not found must be an error:\n%s", text)
	}
}

func TestAnalyzeChangeImpact_DefiniteAndPossible(t *testing.T) {
	dir := phpSemanticsRepo(t)
	res, text := callAt(t, dir, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton"})
	mustOK(t, res, "analyze_change_impact", text)
	definite, possible := impactDefiniteAndPossible(text)
	contains(t, "definite", definite, `App\Http\TypedController.show`, `App\Services\LoginScreenPolicy.isSsoOnly`,
		`App\Http\LoginController.showLoginForm`, "Unattributed references: 1")
	excludes(t, "definite", definite, "LegacyController", "app/Http/LegacyController.php")
	contains(t, "possible", possible, `App\Http\LegacyController.render`, "[candidate]")

	res, text = callAt(t, dir, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton", "format": "json"})
	mustOK(t, res, "analyze_change_impact(json)", text)
	var out struct {
		Entries []struct {
			Symbol   string `json:"symbol"`
			Category string `json:"category"`
		} `json:"entries"`
		AffectedFiles []string `json:"affected_files"`
		Unattributed  *int     `json:"unattributed"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.Unattributed == nil || *out.Unattributed != 1 {
		t.Errorf("json unattributed = %v, want 1", out.Unattributed)
	}
	for _, e := range out.Entries {
		if strings.Contains(e.Symbol, "LegacyController") && e.Category != "possible_dependent" {
			t.Errorf("candidate caller promoted to %s", e.Category)
		}
	}
	for _, f := range out.AffectedFiles {
		if strings.Contains(f, "LegacyController") {
			t.Errorf("possible dependent's file listed as affected: %v", out.AffectedFiles)
		}
	}

	res, text = callAt(t, dir, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.unused"})
	mustOK(t, res, "analyze_change_impact(zero)", text)
	contains(t, "zero", text, "Unattributed references: 0")
}

// Strong + unresolved in Go: a typed call resolves (Strong), a call promoted
// through an embedded struct does not — it is reported, not dropped.
func TestGetCallers_GoStrongPlusUnresolved(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"p/p.go": `package p

type Inner struct{}

func (Inner) Do() {}

type Wrapper struct{ Inner }

func UseInner() { var i Inner; i.Do() }

func UseWrapper() { var w Wrapper; w.Do() }

func Lonely() {}
`,
	})
	out, text, isErr := callGraphTool(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": "Inner.Do"})
	if isErr {
		t.Fatalf("unexpected error:\n%s", text)
	}
	if got, want := edgeTargets(out.Edges), []string{"UseInner [strong]"}; !reflect.DeepEqual(got, want) {
		t.Errorf("edges = %v, want %v", got, want)
	}
	if *out.Unattributed != 1 {
		t.Errorf("unattributed = %d, want 1:\n%s", *out.Unattributed, text)
	}
	out, text, _ = callGraphTool(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": "Lonely"})
	if len(out.Edges) != 0 || *out.Unattributed != 0 {
		t.Errorf("Lonely: want a true zero:\n%s", text)
	}
}

// Several candidate callers are listed in a deterministic order (qualified
// name), whatever order the index stores them in.
func TestGetCallers_CandidateOrdering(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"app/A.php": "<?php\nnamespace App;\n\nclass A\n{\n    public function save() { return 1; }\n}\n",
		"app/B.php": "<?php\nnamespace App;\n\nclass B\n{\n    public function save() { return 2; }\n}\n",
	}
	callers := []string{"Zeta", "Alpha", "Mu", "Delta", "Omega", "Beta", "Kappa"}
	for _, c := range callers {
		files["app/"+c+".php"] = "<?php\nnamespace App;\n\nclass " + c + "\n{\n    private $x;\n    public function go() { return $this->x->save(); }\n}\n"
	}
	writeTree(t, root, files)
	out, text, isErr := callGraphTool(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": `App\A.save`})
	if isErr {
		t.Fatalf("unexpected error:\n%s", text)
	}
	var got []string
	for _, c := range out.Candidates {
		got = append(got, c.Symbol)
	}
	want := []string{`App\Alpha.go`, `App\Beta.go`, `App\Delta.go`, `App\Kappa.go`, `App\Mu.go`, `App\Omega.go`, `App\Zeta.go`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
	if *out.Unattributed != len(callers) {
		t.Errorf("unattributed = %d, want %d", *out.Unattributed, len(callers))
	}
}

// An ambiguous target lists a bounded, deterministic set of candidates.
func TestTargetLookup_AmbiguityIsBounded(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	for i := 0; i < maxAmbiguousCandidates+5; i++ {
		name := string(rune('A'+i%26)) + string(rune('a'+i/26))
		files["app/"+name+".php"] = "<?php\nnamespace App;\n\nclass " + name + "\n{\n    public function run() { return 1; }\n}\n"
	}
	writeTree(t, root, files)
	res, text := callAt(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": "run"})
	if !res.IsError || !strings.Contains(text, "Ambiguous") {
		t.Fatalf("want ambiguity error:\n%s", text)
	}
	if n := strings.Count(text, ".run  (method)"); n != maxAmbiguousCandidates {
		t.Errorf("listed %d candidates, want %d:\n%s", n, maxAmbiguousCandidates, text)
	}
	contains(t, "ambiguity", text, "... and 5 more", `App\Aa.run`)
	_, again := callAt(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": "run"})
	if again != text {
		t.Errorf("ambiguity output is not stable")
	}
}

// get_context carries resolved callers as context and candidate callers only as
// the completeness count.
func TestGetContext_CallersAndCompleteness(t *testing.T) {
	dir := phpSemanticsRepo(t)
	res, text := callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton", "maxTokens": float64(4000)})
	mustOK(t, res, "get_context", text)
	contains(t, "get_context", text,
		"Symbol: App\\Http\\TypedController.show\nReason: caller\nConfidence: exact",
		"Symbol: App\\Services\\LoginScreenPolicy.isSsoOnly\nReason: caller\nConfidence: exact",
		"Symbol: App\\Http\\LoginController.showLoginForm\nReason: caller\nConfidence: exact",
		"unattributed: 1 callers, 0 callees")
	excludes(t, "get_context", text, "LegacyController", "Clinic")

	res, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.unused"})
	mustOK(t, res, "get_context(zero)", text)
	contains(t, "get_context(zero)", text, "unattributed: 0 callers, 0 callees")

	res, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton", "format": "json"})
	mustOK(t, res, "get_context(json)", text)
	contains(t, "get_context(json)", text, `"UnattributedCallers": 1`, `"reason": "caller"`)
}

// Phase 4 core acceptance, through the MCP surface: a Laravel 6 constructor-
// injected protected property makes the call a Strong graph edge, so the
// caller is a resolved caller, nothing is left unattributed, and the Context
// Engine includes it as an ordinary caller — with no context-side change.
func TestPHPCtorInjection_CallerReachesContext(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/Services/LoginScreenPolicy.php": `<?php
namespace App\Services;

class LoginScreenPolicy
{
    public function showsSsoButton()
    {
        return true;
    }
}
`,
		"app/Models/Clinic.php": "<?php\nnamespace App\\Models;\n\nclass Clinic\n{\n    public function showsSsoButton() { return false; }\n}\n",
		"app/Http/Controllers/LoginController.php": `<?php
namespace App\Http\Controllers;

use App\Services\LoginScreenPolicy;

class LoginController
{
    protected $loginScreenPolicy;

    public function __construct(LoginScreenPolicy $loginScreenPolicy)
    {
        $this->loginScreenPolicy = $loginScreenPolicy;
    }

    public function showLoginForm()
    {
        return $this->loginScreenPolicy->showsSsoButton();
    }
}
`,
	})
	out, text, isErr := callGraphTool(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton"})
	if isErr {
		t.Fatalf("unexpected error:\n%s", text)
	}
	if got, want := edgeTargets(out.Edges), []string{`App\Http\Controllers\LoginController.showLoginForm [strong]`}; !reflect.DeepEqual(got, want) {
		t.Errorf("edges = %v, want %v", got, want)
	}
	if *out.Unattributed != 0 || len(out.Candidates) != 0 {
		t.Errorf("want nothing unattributed, got %d %v", *out.Unattributed, out.Candidates)
	}
	if out, _, _ := callGraphTool(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": "Clinic.showsSsoButton"}); len(out.Edges) != 0 || *out.Unattributed != 0 {
		t.Errorf("the same-named Clinic method must be untouched: %+v", out)
	}

	res, text := callAt(t, root, "get_context", map[string]interface{}{"path": ".", "symbol": "LoginScreenPolicy.showsSsoButton", "maxTokens": float64(4000)})
	mustOK(t, res, "get_context", text)
	contains(t, "get_context", text,
		"Symbol: App\\Http\\Controllers\\LoginController.showLoginForm\nReason: caller\nConfidence: strong",
		"return $this->loginScreenPolicy->showsSsoButton();",
		"unattributed: 0 callers, 0 callees")
	excludes(t, "get_context", text, "protected $loginScreenPolicy", "Clinic")
}
