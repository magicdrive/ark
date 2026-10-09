package mcp

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Observed references with no target must not disappear from the graph tools
// ("Unknown is not empty"). Every call/construction/type reference inside a
// symbol is exactly one of: an edge, unattributed, unresolved, outside
// repository — and get_callees / get_relations / get_context /
// analyze_change_impact say how many of each there are, so `unattributed: 0`
// never reads as "nothing else is called".

// calleesResult is the get_callees result with the unresolved breakdown.
type calleesResult struct {
	Edges                     []edgeEntry          `json:"edges"`
	Unattributed              *int                 `json:"unattributed"`
	Unresolved                *int                 `json:"unresolved"`
	OutsideRepository         *int                 `json:"outsideRepository"`
	UnresolvedReferences      []unresolvedRefEntry `json:"unresolvedReferences"`
	UnresolvedReferencesTotal int                  `json:"unresolvedReferencesTotal"`
	Candidates                []candidateEntry     `json:"candidates"`
}

func callees(t *testing.T, dir, sym string) (calleesResult, string) {
	t.Helper()
	res, text := callAt(t, dir, "get_callees", map[string]interface{}{"path": ".", "symbol": sym})
	if res.IsError {
		t.Fatalf("get_callees(%s): %s", sym, text)
	}
	var out calleesResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("get_callees(%s): %v\n%s", sym, err, text)
	}
	if out.Unattributed == nil || out.Unresolved == nil || out.OutsideRepository == nil {
		t.Fatalf("get_callees(%s): unattributed, unresolved and outsideRepository must always be present:\n%s", sym, text)
	}
	return out, text
}

func unresolvedNames(rs []unresolvedRefEntry) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Name+" ["+r.Reason+"]")
	}
	return out
}

func dynamicRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		// The field report: an untyped container property (Laravel 6 style,
		// injected elsewhere) — makeWith has no target the index can name.
		"app/Factories/KarteServiceFactory.php": `<?php
namespace App\Factories;

use App\Contracts\KarteInterface;

class KarteServiceFactory
{
    protected $container;

    public function make($type): KarteInterface
    {
        return $this->container->makeWith($this->resolveClass($type), []);
    }

    private function resolveClass($type) { return KarteInterface::class; }
}
`,
		// The same call through a typed vendor container: proven external.
		"app/Factories/TypedFactory.php": `<?php
namespace App\Factories;

use Illuminate\Contracts\Container\Container;
use App\Contracts\KarteInterface;

class TypedFactory
{
    private $container;
    public function __construct(Container $container) { $this->container = $container; }
    public function make() { return $this->container->makeWith(KarteInterface::class, []); }
}
`,
		"app/Contracts/KarteInterface.php": "<?php\nnamespace App\\Contracts;\n\ninterface KarteInterface\n{\n    public function render();\n}\n",
		"app/Models/A.php":                 "<?php\nnamespace App\\Models;\n\nclass A\n{\n    public function save() { return 1; }\n}\n",
		"app/Models/B.php":                 "<?php\nnamespace App\\Models;\n\nclass B\n{\n    public function save() { return 2; }\n}\n",
		"app/Services/Mixed.php": `<?php
namespace App\Services;

use App\Models\A;

class Mixed
{
    public function none() { return 1; }
    public function onlyExternal($c) { return $c->makeWith('x'); }
    public function partial(A $a, $x, $m)
    {
        $a->save();
        $x->save();
        $this->$m();
        return new $m();
    }
    public function classString()
    {
        $cls = A::class;
        $obj = new $cls();
        return app(A::class);
    }
}
`,
	})
	return root
}

func TestUnresolved_FieldCase_MakeWithIsReported(t *testing.T) {
	dir := dynamicRepo(t)

	out, text := callees(t, dir, "KarteServiceFactory.make")
	if got := edgeTargets(out.Edges); !reflect.DeepEqual(got, []string{
		`App\Contracts\KarteInterface [exact]`, // return type
		`App\Factories\KarteServiceFactory.resolveClass [exact]`,
	}) {
		t.Errorf("edges = %v\n%s", got, text)
	}
	if *out.Unattributed != 0 || *out.Unresolved != 1 || *out.OutsideRepository != 0 {
		t.Errorf("unattributed/unresolved/outside = %d/%d/%d, want 0/1/0\n%s", *out.Unattributed, *out.Unresolved, *out.OutsideRepository, text)
	}
	if got := unresolvedNames(out.UnresolvedReferences); !reflect.DeepEqual(got, []string{"makeWith [unresolved]"}) || out.UnresolvedReferencesTotal != 1 {
		t.Errorf("unresolvedReferences = %v (total %d)\n%s", got, out.UnresolvedReferencesTotal, text)
	}
	if r := out.UnresolvedReferences[0]; r.Receiver != "$this->container" || r.Line != 12 || r.Kind != "call" {
		t.Errorf("makeWith entry = %+v", r)
	}

	// Through a vendor-typed container: known external, never unattributed.
	typed, text := callees(t, dir, "TypedFactory.make")
	if *typed.Unattributed != 0 || *typed.Unresolved != 0 || *typed.OutsideRepository != 1 {
		t.Errorf("typed: unattributed/unresolved/outside = %d/%d/%d, want 0/0/1\n%s", *typed.Unattributed, *typed.Unresolved, *typed.OutsideRepository, text)
	}
	if got := unresolvedNames(typed.UnresolvedReferences); !reflect.DeepEqual(got, []string{"makeWith [outside_repository]"}) {
		t.Errorf("typed: unresolvedReferences = %v", got)
	}
	// The class-string argument names the interface (a type use), it is not a
	// call to it, its constructor or the class the container builds.
	if got := edgeTargets(typed.Edges); !reflect.DeepEqual(got, []string{`App\Contracts\KarteInterface [exact]`}) {
		t.Errorf("typed: edges = %v", got)
	}
	for _, e := range typed.Edges {
		if e.Kind != "uses_type" {
			t.Errorf("class-string formed a %s edge: %+v", e.Kind, e)
		}
	}

	// get_relations, get_context and analyze_change_impact carry the same facts.
	var rel struct {
		Unattributed         *int                 `json:"unattributed"`
		Unresolved           *int                 `json:"unresolved"`
		OutsideRepository    *int                 `json:"outsideRepository"`
		UnresolvedReferences []unresolvedRefEntry `json:"unresolvedReferences"`
	}
	_, text = callAt(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": "KarteServiceFactory.make"})
	if err := json.Unmarshal([]byte(text), &rel); err != nil {
		t.Fatal(err)
	}
	if rel.Unresolved == nil || *rel.Unresolved != 1 || rel.OutsideRepository == nil || *rel.OutsideRepository != 0 ||
		!reflect.DeepEqual(unresolvedNames(rel.UnresolvedReferences), []string{"makeWith [unresolved]"}) {
		t.Errorf("get_relations:\n%s", text)
	}
	_, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "KarteServiceFactory.make"})
	contains(t, "get_context", text, "unattributed: 0 callers, 0 callees; unresolved callees: 1, outside repository: 0")
	_, text = callAt(t, dir, "get_context", map[string]interface{}{"path": ".", "symbol": "KarteServiceFactory.make", "format": "json"})
	contains(t, "get_context json", text, `"UnresolvedCallees": 1`, `"OutsideCallees": 0`)
	_, text = callAt(t, dir, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "KarteServiceFactory.make"})
	contains(t, "analyze_change_impact", text,
		"Unresolved outgoing references: 1 unresolved, 0 outside repository",
		`"makeWith" (call) at app/Factories/KarteServiceFactory.php:12  [unresolved, receiver $this->container]`)
	_, text = callAt(t, dir, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": "TypedFactory.make", "format": "json"})
	contains(t, "analyze_change_impact json", text, `"unresolved_count": 0`, `"outside_repository_count": 1`, `"reason": "outside_repository"`)
}

// Empty is not incomplete: a symbol with no outgoing references reports all
// zeros; one whose only call has no target has no edges but says so.
func TestUnresolved_EmptyVersusIncomplete(t *testing.T) {
	dir := dynamicRepo(t)
	none, text := callees(t, dir, `App\Services\Mixed.none`)
	if len(none.Edges) != 0 || *none.Unattributed+*none.Unresolved+*none.OutsideRepository != 0 || len(none.UnresolvedReferences) != 0 {
		t.Errorf("no references: want an empty, complete answer:\n%s", text)
	}
	ext, text := callees(t, dir, `App\Services\Mixed.onlyExternal`)
	if len(ext.Edges) != 0 || *ext.Unresolved != 1 {
		t.Errorf("only an unknown call: want no edges and unresolved 1:\n%s", text)
	}
}

// A method that is only partly resolvable reports each part in its own bucket:
// the typed call is an edge, the ambiguous one unattributed with candidates
// (never an edge), the dynamic ones unresolved.
func TestUnresolved_PartialMethod(t *testing.T) {
	dir := dynamicRepo(t)
	out, text := callees(t, dir, `App\Services\Mixed.partial`)
	var calls []string
	for _, e := range out.Edges {
		if e.Kind == "calls" {
			calls = append(calls, e.To+" ["+e.Confidence+"]")
		}
	}
	if !reflect.DeepEqual(calls, []string{`App\Models\A.save [exact]`}) {
		t.Errorf("call edges = %v\n%s", calls, text)
	}
	if *out.Unattributed != 1 || *out.Unresolved != 2 || *out.OutsideRepository != 0 {
		t.Errorf("unattributed/unresolved/outside = %d/%d/%d, want 1/2/0\n%s", *out.Unattributed, *out.Unresolved, *out.OutsideRepository, text)
	}
	if got := candidateNames(out.Candidates); !reflect.DeepEqual(got, []string{`App\Models\B.save [candidate]`}) {
		t.Errorf("candidates = %v (A.save is already an edge)", got)
	}
	if got := unresolvedNames(out.UnresolvedReferences); !reflect.DeepEqual(got, []string{"$m [dynamic_name]", "$m [dynamic_name]"}) {
		t.Errorf("unresolvedReferences = %v", got)
	}
	for _, e := range out.Edges {
		if e.Confidence == "candidate" || e.Confidence == "unresolved" {
			t.Errorf("non-unique resolution became an edge: %+v", e)
		}
	}
}

// A class-string held by a variable proven to keep it makes `new $cls()` a
// construction of that class; a class-string handed to a function (a
// container, a factory) is only a type use — never a construction.
func TestUnresolved_ClassStrings(t *testing.T) {
	dir := dynamicRepo(t)
	var rel struct {
		Relations []relationEntry `json:"relations"`
	}
	_, text := callAt(t, dir, "get_relations", map[string]interface{}{"path": ".", "symbol": `App\Services\Mixed.classString`})
	if err := json.Unmarshal([]byte(text), &rel); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rel.Relations {
		got = append(got, r.Direction+" "+r.Kind+" "+r.Qualified+" ["+r.Confidence+"]")
	}
	sort.Strings(got)
	want := []string{
		`calls construction App\Models\A [exact]`, // new $cls()
		`calls type_use App\Models\A [exact]`,     // A::class (twice: one relation)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("relations = %v, want %v\n%s", got, want, text)
	}
	out, text := callees(t, dir, `App\Services\Mixed.classString`)
	// `app(...)` itself is a call the index cannot name.
	if got := unresolvedNames(out.UnresolvedReferences); !reflect.DeepEqual(got, []string{"app [unresolved]"}) {
		t.Errorf("unresolvedReferences = %v", got)
	}
	if strings.Contains(text, "__construct") {
		t.Errorf("a class-string must not produce a constructor edge:\n%s", text)
	}
}

// get_callers is unchanged: outgoing unresolved references cannot be
// attributed to any symbol, so they appear in no caller answer.
func TestUnresolved_CallersShape(t *testing.T) {
	dir := dynamicRepo(t)
	_, text := callAt(t, dir, "get_callers", map[string]interface{}{"path": ".", "symbol": `App\Models\B.save`})
	if strings.Contains(text, "unresolved") || strings.Contains(text, "outsideRepository") {
		t.Errorf("get_callers must not carry outgoing breakdowns:\n%s", text)
	}
	contains(t, "get_callers", text, `"unattributed": 1`)
}

// The breakdown is a generic contract: every provider's unresolved calls are
// reported the same way, nothing becomes an edge, and calls the index can
// resolve still do. Python and JavaScript members are not extracted as
// symbols (internal/conformance Q3), which is exactly why their member calls
// must be counted rather than dropped.
func TestUnresolved_CrossLanguage(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod": "module example.com/m\n",
		"g/a.go": "package g\n\nimport \"fmt\"\n\ntype Svc interface{ Do() }\n\nfunc Run(s Svc, f func()) {\n\tfmt.Println(\"x\")\n\ts.Do()\n\tf()\n\thelper()\n}\n\nfunc helper() {}\n",
		"t/a.ts": "import { thing } from 'lib';\nexport function tsRun(o: any) {\n  thing();\n  o.foo();\n  tsLocal();\n}\nfunction tsLocal() {}\n",
		"j/a.js": "function jsRun(o) {\n  o.go();\n  jsLocal();\n}\nfunction jsLocal() {}\n",
		"p/a.py": "def py_run(o):\n    o.go()\n    py_local()\n\ndef py_local():\n    pass\n",
	})
	cases := []struct {
		sym, edge  string
		unresolved []string
		outside    int
	}{
		// Go: an interface method call through a parameter and a func value
		// have no target in the index; a call qualified by an import of a
		// package outside the repository (fmt) is proven outside, as the
		// TypeScript named import below.
		{"Run", "helper", []string{"Println [outside_repository]", "Do [unresolved]", "f [unresolved]"}, 1},
		// TypeScript: a named import of an external package is proven outside.
		{"tsRun", "tsLocal", []string{"thing [outside_repository]", "foo [unresolved]"}, 1},
		{"jsRun", "jsLocal", []string{"go [unresolved]"}, 0},
		{"py_run", "py_local", []string{"go [unresolved]"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.sym, func(t *testing.T) {
			out, text := callees(t, root, tc.sym)
			if got := edgeTargets(out.Edges); len(got) != 1 || !strings.HasPrefix(got[0], tc.edge+" [") {
				t.Errorf("edges = %v, want only %s\n%s", got, tc.edge, text)
			}
			if got := unresolvedNames(out.UnresolvedReferences); !reflect.DeepEqual(got, tc.unresolved) {
				t.Errorf("unresolvedReferences = %v, want %v\n%s", got, tc.unresolved, text)
			}
			if *out.OutsideRepository != tc.outside || *out.Unresolved != len(tc.unresolved)-tc.outside || *out.Unattributed != 0 {
				t.Errorf("unattributed/unresolved/outside = %d/%d/%d\n%s", *out.Unattributed, *out.Unresolved, *out.OutsideRepository, text)
			}
			if out.UnresolvedReferencesTotal != len(tc.unresolved) {
				t.Errorf("unresolvedReferencesTotal = %d, want %d", out.UnresolvedReferencesTotal, len(tc.unresolved))
			}
		})
	}
}
