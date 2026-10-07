package php_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// Constructor property evidence, end to end (provider → resolver → graph):
// typed constructor parameter → untyped property → `$this->p->m()`.

const policyClass = `<?php
namespace App\Services;

class LoginScreenPolicy
{
    public function showsSsoButton() { return true; }
}
`

// otherPolicy declares a same-named method, so a call without type evidence is
// a Candidate (never an edge).
const otherPolicy = `<?php
namespace App\Models;

class Clinic
{
    public function showsSsoButton() { return false; }
}
`

func phpTreeIndex(t *testing.T, files map[string]string) *index.RepositoryIndex {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), root, phpProviders())
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

// callerEdge returns the confidence of the graph edge from caller to target,
// or "" when there is none.
func callerEdge(t *testing.T, idx *index.RepositoryIndex, target, caller string) string {
	t.Helper()
	ts := idx.FindSymbolsByQualified(target)
	if len(ts) != 1 {
		t.Fatalf("target %q: %d symbols", target, len(ts))
	}
	for _, e := range idx.GetCallers(ts[0].ID) {
		if s, ok := idx.GetSymbol(e.To); ok && s.Qualified == caller {
			return e.Confidence.String()
		}
	}
	return ""
}

// controller renders a controller class around a constructor and body.
func controller(head, members string) string {
	return "<?php\nnamespace App\\Http;\n\nuse App\\Services\\LoginScreenPolicy;\n\n" + head + "\n{\n" + members + "\n}\n"
}

const injected = `    public function __construct(LoginScreenPolicy $policy)
    {
        $this->policy = $policy;
    }

    public function show()
    {
        return $this->policy->showsSsoButton();
    }`

func TestCtorEvidence_ConfidencePolicy(t *testing.T) {
	cases := []struct {
		name, head, decl string
		trait            bool
		want             string
	}{
		{"protected / non-final (Laravel 6 core case)", "class C", "    protected $policy;", false, "strong"},
		{"private / non-final", "class C", "    private $policy;", false, "exact"},
		{"private / final", "final class C", "    private $policy;", false, "exact"},
		{"protected / final", "final class C", "    protected $policy;", false, "exact"},
		{"public / non-final", "class C", "    public $policy;", false, "strong"},
		{"public / final (writable from any code)", "final class C", "    public $policy;", false, "strong"},
		{"var (public)", "class C", "    var $policy;", false, "strong"},
		{"undeclared (dynamic or inherited)", "class C", "", false, "strong"},
		{"private / trait used", "class C", "    private $policy;", true, "strong"},
		{"private / final / trait used", "final class C", "    private $policy;", true, "strong"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			members := tc.decl + "\n" + injected
			files := map[string]string{"app/Services/LoginScreenPolicy.php": policyClass, "app/Models/Clinic.php": otherPolicy}
			if tc.trait {
				members = "    use Helpers;\n" + members
				files["app/Http/Helpers.php"] = "<?php\nnamespace App\\Http;\n\ntrait Helpers\n{\n    public function noop() {}\n}\n"
			}
			files["app/Http/C.php"] = controller(tc.head, members)
			idx := phpTreeIndex(t, files)
			if got := callerEdge(t, idx, `App\Services\LoginScreenPolicy.showsSsoButton`, `App\Http\C.show`); got != tc.want {
				t.Errorf("edge confidence = %q, want %q", got, tc.want)
			}
			if got := callerEdge(t, idx, `App\Models\Clinic.showsSsoButton`, `App\Http\C.show`); got != "" {
				t.Errorf("same-name method of another class got an edge (%s)", got)
			}
		})
	}
}

func TestCtorEvidence_Poison(t *testing.T) {
	cases := []struct{ name, members string }{
		{"assigned outside the constructor", injected + "\n    public function replace($p) { $this->policy = $p; }"},
		{"compound assignment", injected + "\n    public function f() { $this->policy .= 'x'; }"},
		{"null-coalescing assignment", injected + "\n    public function f() { $this->policy ??= null; }"},
		{"reference assignment (target)", injected + "\n    public function f() { $this->policy =& $x; }"},
		{"reference assignment (source)", injected + "\n    public function f() { $y = &$this->policy; }"},
		{"unset", injected + "\n    public function f() { unset($this->policy); }"},
		{"list() destructuring", injected + "\n    public function f($a) { list($this->policy) = $a; }"},
		{"[] destructuring", injected + "\n    public function f($a) { [$this->policy, $b] = $a; }"},
		{"keyed destructuring", injected + "\n    public function f($a) { ['k' => $this->policy] = $a; }"},
		{"foreach target", injected + "\n    public function f($xs) { foreach ($xs as $this->policy) {} }"},
		{"increment", injected + "\n    public function f() { $this->policy++; }"},
		{"passed as an argument (maybe by reference)", injected + "\n    public function f() { reset_it($this->policy); }"},
		{"by-reference capture", injected + "\n    public function f() { return function () use (&$this->policy) {}; }"},
		{"dynamic property write", injected + "\n    public function f($n) { $this->$n = null; }"},
		{"write through another instance", injected + "\n    public function f(self $o) { $o->policy = null; }"},
		{"conditional assignment only", `    public function __construct(LoginScreenPolicy $policy, $ok)
    {
        if ($ok) { $this->policy = $policy; }
    }

    public function show() { return $this->policy->showsSsoButton(); }`},
		{"parameter rebound before assignment", `    public function __construct(LoginScreenPolicy $policy)
    {
        $policy = make_other();
        $this->policy = $policy;
    }

    public function show() { return $this->policy->showsSsoButton(); }`},
		{"by-reference parameter", `    public function __construct(LoginScreenPolicy &$policy)
    {
        $this->policy = $policy;
    }

    public function show() { return $this->policy->showsSsoButton(); }`},
		{"conflicting constructor types", `    public function __construct(LoginScreenPolicy $policy, \App\Models\Clinic $clinic)
    {
        $this->policy = $policy;
        $this->policy = $clinic;
    }

    public function show() { return $this->policy->showsSsoButton(); }`},
		{"dynamic scope in constructor", `    public function __construct(LoginScreenPolicy $policy, $vars)
    {
        extract($vars);
        $this->policy = $policy;
    }

    public function show() { return $this->policy->showsSsoButton(); }`},
		{"union type", `    public function __construct(LoginScreenPolicy|\App\Models\Clinic $policy)
    {
        $this->policy = $policy;
    }

    public function show() { return $this->policy->showsSsoButton(); }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx := phpTreeIndex(t, map[string]string{
				"app/Services/LoginScreenPolicy.php": policyClass,
				"app/Models/Clinic.php":              otherPolicy,
				"app/Http/C.php":                     controller("final class C", "    private $policy;\n"+tc.members),
			})
			if got := callerEdge(t, idx, `App\Services\LoginScreenPolicy.showsSsoButton`, `App\Http\C.show`); got != "" {
				t.Errorf("poisoned property produced a %s edge", got)
			}
			target := idx.FindSymbolsByQualified(`App\Services\LoginScreenPolicy.showsSsoButton`)[0]
			if in, _ := idx.Unattributed(target.ID); in == 0 {
				t.Errorf("the unresolved call must stay unattributed")
			}
		})
	}
	// Still exact: the property is read, passed on as a receiver chain, or
	// used in an ordinary array literal.
	idx := phpTreeIndex(t, map[string]string{
		"app/Services/LoginScreenPolicy.php": policyClass,
		"app/Models/Clinic.php":              otherPolicy,
		"app/Http/C.php": controller("final class C", "    private $policy;\n"+injected+`
    public function view() { return ['policy' => $this->policy, 'x' => $this->policy->showsSsoButton()]; }
    public function same(self $o) { return $o->policy; }`),
	})
	if got := callerEdge(t, idx, `App\Services\LoginScreenPolicy.showsSsoButton`, `App\Http\C.show`); got != "exact" {
		t.Errorf("reads must not poison: got %q", got)
	}
}

func TestCtorEvidence_Construction(t *testing.T) {
	idx := phpTreeIndex(t, map[string]string{
		"app/Services/LoginScreenPolicy.php": policyClass,
		"app/Models/Clinic.php":              otherPolicy,
		"app/Http/C.php": controller("class C", `    private $policy;

    public function __construct()
    {
        $this->policy = new LoginScreenPolicy();
    }

    public function show() { return $this->policy->showsSsoButton(); }`),
	})
	if got := callerEdge(t, idx, `App\Services\LoginScreenPolicy.showsSsoButton`, `App\Http\C.show`); got != "exact" {
		t.Errorf("`new T` evidence: got %q, want exact", got)
	}
}

func TestCtorEvidence_QualifiedIdentity(t *testing.T) {
	requestIn := func(ns string) string {
		return "<?php\nnamespace " + ns + ";\n\nclass Request\n{\n    public function foo() { return 1; }\n}\n"
	}
	using := func(use string) string {
		return "<?php\nnamespace App\\Http;\n\n" + use + "\n\nclass C\n{\n    private $request;\n\n" +
			"    public function __construct(Request $request) { $this->request = $request; }\n\n" +
			"    public function run() { return $this->request->foo(); }\n}\n"
	}
	base := map[string]string{
		"app/Services/Request.php": requestIn(`App\Services`),
		"app/Models/Request.php":   requestIn(`App\Models`),
	}
	cases := []struct {
		name, use, wantTarget string
	}{
		{"use selects one of several same-named classes", `use App\Services\Request;`, `App\Services\Request.foo`},
		{"the other one", `use App\Models\Request;`, `App\Models\Request.foo`},
		{"vendor type: never a same-named repository class", `use Illuminate\Http\Request;`, ""},
		{"alias", `use App\Services\Request as Req;`, ""}, // `Request` is not the alias: resolves to App\Http\Request (absent)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"app/Http/C.php": using(tc.use)}
			for k, v := range base {
				files[k] = v
			}
			idx := phpTreeIndex(t, files)
			for _, target := range []string{`App\Services\Request.foo`, `App\Models\Request.foo`} {
				got := callerEdge(t, idx, target, `App\Http\C.run`)
				if target == tc.wantTarget && got != "exact" {
					t.Errorf("%s: want exact edge, got %q", target, got)
				}
				if target != tc.wantTarget && got != "" {
					t.Errorf("%s: fabricated %s edge", target, got)
				}
			}
		})
	}
	// A real alias.
	idx := phpTreeIndex(t, map[string]string{
		"app/Services/LoginScreenPolicy.php": policyClass,
		"app/Models/Clinic.php":              otherPolicy,
		"app/Http/C.php": strings.Replace(controller("class C", "    private $policy;\n"+strings.Replace(injected, "LoginScreenPolicy $policy", "Policy $policy", 1)),
			`use App\Services\LoginScreenPolicy;`, `use App\Services\LoginScreenPolicy as Policy;`, 1),
	})
	if got := callerEdge(t, idx, `App\Services\LoginScreenPolicy.showsSsoButton`, `App\Http\C.show`); got != "exact" {
		t.Errorf("alias: got %q, want exact", got)
	}
}

func TestCtorEvidence_InterfaceStaysInterface(t *testing.T) {
	idx := phpTreeIndex(t, map[string]string{
		"app/Policy.php": "<?php\nnamespace App;\n\ninterface Policy\n{\n    public function allowed();\n}\n",
		"app/A.php":      "<?php\nnamespace App;\n\nclass A implements Policy\n{\n    public function allowed() { return true; }\n}\n",
		"app/B.php":      "<?php\nnamespace App;\n\nclass B implements Policy\n{\n    public function allowed() { return false; }\n}\n",
		"app/Controller.php": "<?php\nnamespace App;\n\nclass Controller\n{\n    private $policy;\n\n" +
			"    public function __construct(Policy $policy) { $this->policy = $policy; }\n\n" +
			"    public function run() { return $this->policy->allowed(); }\n}\n",
	})
	if got := callerEdge(t, idx, `App\Policy.allowed`, `App\Controller.run`); got != "exact" {
		t.Errorf("interface member: got %q, want exact", got)
	}
	for _, impl := range []string{`App\A.allowed`, `App\B.allowed`} {
		if got := callerEdge(t, idx, impl, `App\Controller.run`); got != "" {
			t.Errorf("implementation %s fabricated (%s)", impl, got)
		}
	}
}

// A declared property type is enforced by PHP on every write: it wins over a
// constructor assignment.
func TestCtorEvidence_TypedPropertyWins(t *testing.T) {
	idx := phpTreeIndex(t, map[string]string{
		"app/Services/LoginScreenPolicy.php": policyClass,
		"app/Models/Clinic.php":              otherPolicy,
		"app/Http/C.php": controller("class C", `    private \App\Models\Clinic $policy;

    public function __construct(LoginScreenPolicy $policy) { $this->policy = $policy; }

    public function show() { return $this->policy->showsSsoButton(); }`),
	})
	if got := callerEdge(t, idx, `App\Models\Clinic.showsSsoButton`, `App\Http\C.show`); got != "exact" {
		t.Errorf("declared type: got %q, want exact", got)
	}
	if got := callerEdge(t, idx, `App\Services\LoginScreenPolicy.showsSsoButton`, `App\Http\C.show`); got != "" {
		t.Errorf("constructor type overrode the declared type (%s)", got)
	}
}

func TestRelativeScopes(t *testing.T) {
	src := func(head string) string {
		return "<?php\nnamespace App;\n\n" + head + " extends Base\n{\n" +
			"    public static function helper() { return 1; }\n" +
			"    public function viaSelf() { return self::helper(); }\n" +
			"    public function viaStatic() { return static::helper(); }\n" +
			"    public function viaParent() { return parent::inherited(); }\n" +
			"    public function selfInherited() { return self::inherited(); }\n}\n"
	}
	base := "<?php\nnamespace App;\n\nclass Base\n{\n    public static function inherited() { return 2; }\n}\n"
	for _, tc := range []struct{ head, wantStatic string }{{"class C", "strong"}, {"final class C", "exact"}} {
		idx := phpTreeIndex(t, map[string]string{"app/C.php": src(tc.head), "app/Base.php": base})
		if got := callerEdge(t, idx, `App\C.helper`, `App\C.viaSelf`); got != "exact" {
			t.Errorf("%s: self:: got %q, want exact", tc.head, got)
		}
		if got := callerEdge(t, idx, `App\C.helper`, `App\C.viaStatic`); got != tc.wantStatic {
			t.Errorf("%s: static:: got %q, want %s", tc.head, got, tc.wantStatic)
		}
		// Inherited members are an inheritance lookup (Phase 5): no edge yet.
		for _, caller := range []string{`App\C.viaParent`, `App\C.selfInherited`} {
			if got := callerEdge(t, idx, `App\Base.inherited`, caller); got != "" {
				t.Errorf("%s: %s reached an inherited member (%s)", tc.head, caller, got)
			}
		}
	}
}
