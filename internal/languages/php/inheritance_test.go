package php_test

import (
	"strings"
	"testing"
	"time"
)

// Member lookup through supertypes and traits: the nearest structural
// declaration wins; any gap in the structural evidence leaves the reference
// unresolved, and a collision is never decided.

func phpFile(ns, body string) string {
	return "<?php\nnamespace " + ns + ";\n\n" + strings.TrimSpace(body) + "\n"
}

type edgeWant struct {
	target, caller, want string // want "" = no edge
}

func checkEdges(t *testing.T, files map[string]string, wants ...edgeWant) {
	t.Helper()
	idx := phpTreeIndex(t, files)
	for _, w := range wants {
		if got := callerEdge(t, idx, w.target, w.caller); got != w.want {
			t.Errorf("%s → %s: got %q, want %q", w.caller, w.target, got, w.want)
		}
	}
}

func TestInheritance_ParentMember(t *testing.T) {
	files := map[string]string{
		"app/ParentService.php": phpFile(`App`, `class ParentService
{
    public function inheritedMethod() { return true; }
}`),
		"app/ChildService.php": phpFile(`App`, `class ChildService extends ParentService
{
    public function run() { return $this->inheritedMethod(); }
}`),
	}
	checkEdges(t, files, edgeWant{`App\ParentService.inheritedMethod`, `App\ChildService.run`, "exact"})
	idx := phpTreeIndex(t, files)
	target := idx.FindSymbolsByQualified(`App\ParentService.inheritedMethod`)[0]
	if in, _ := idx.Unattributed(target.ID); in != 0 {
		t.Errorf("resolved inherited call still unattributed (%d)", in)
	}
}

func TestInheritance_OverrideNearestAndMultiHop(t *testing.T) {
	checkEdges(t, map[string]string{
		"app/A.php": phpFile(`App`, `class A { public function foo() {} public function bar() {} }`),
		"app/B.php": phpFile(`App`, `class B extends A { public function foo() {} public function run() { $this->foo(); } }`),
		"app/C.php": phpFile(`App`, `class C extends B { public function run() { $this->foo(); } }`),
		"app/D.php": phpFile(`App`, `class D extends C { public function run() { $this->bar(); } }`),
	},
		// Local override wins.
		edgeWant{`App\B.foo`, `App\B.run`, "exact"},
		edgeWant{`App\A.foo`, `App\B.run`, ""},
		// Nearest declaration wins.
		edgeWant{`App\B.foo`, `App\C.run`, "exact"},
		edgeWant{`App\A.foo`, `App\C.run`, ""},
		// Multi-hop: D → C → B → A.
		edgeWant{`App\A.bar`, `App\D.run`, "exact"},
	)
}

func TestInheritance_RelativeAndExplicitScopes(t *testing.T) {
	files := func(final string) map[string]string {
		return map[string]string{
			"app/A.php": phpFile(`App`, `class A { public static function foo() {} public static function make() {} }`),
			"app/B.php": phpFile(`App`, final+`class B extends A
{
    public static function foo() {}
    public function viaParent() { return parent::foo(); }
    public function viaSelf() { return self::make(); }
    public function viaStatic() { return static::make(); }
    public function viaThis() { return $this->make(); }
}`),
			"app/U.php": phpFile(`App`, `class U { public function viaClass() { return B::make(); } }`),
		}
	}
	for _, tc := range []struct{ final, static string }{{"", "strong"}, {"final ", "exact"}} {
		checkEdges(t, files(tc.final),
			// parent:: starts at the parent even though B declares foo.
			edgeWant{`App\A.foo`, `App\B.viaParent`, "exact"},
			edgeWant{`App\B.foo`, `App\B.viaParent`, ""},
			edgeWant{`App\A.make`, `App\B.viaSelf`, "exact"},
			// Late static binding: a subclass may override make.
			edgeWant{`App\A.make`, `App\B.viaStatic`, tc.static},
			edgeWant{`App\A.make`, `App\B.viaThis`, "exact"},
			edgeWant{`App\A.make`, `App\U.viaClass`, "exact"},
		)
	}
}

func TestInheritance_PrivateParentMemberIsNotInherited(t *testing.T) {
	files := map[string]string{
		"app/A.php": phpFile(`App`, `class A { private function foo() {} protected function bar() {} }`),
		"app/B.php": phpFile(`App`, `class B extends A
{
    public function run() { $this->foo(); }
    public function run2() { $this->bar(); }
}`),
	}
	checkEdges(t, files,
		edgeWant{`App\A.foo`, `App\B.run`, ""},
		edgeWant{`App\A.bar`, `App\B.run2`, "exact"},
	)
	idx := phpTreeIndex(t, files)
	target := idx.FindSymbolsByQualified(`App\A.foo`)[0]
	if in, _ := idx.Unattributed(target.ID); in == 0 {
		t.Errorf("the unresolved call must stay unattributed")
	}
}

func TestInheritance_Traits(t *testing.T) {
	t.Run("single trait", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/T.php": phpFile(`App`, `trait T { public function helper() {} }`),
			"app/C.php": phpFile(`App`, `class C { use T; public function run() { $this->helper(); } }`),
		}, edgeWant{`App\T.helper`, `App\C.run`, "exact"})
	})
	t.Run("trait member wins over the parent's", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/A.php": phpFile(`App`, `class A { public function foo() {} }`),
			"app/T.php": phpFile(`App`, `trait T { public function foo() {} }`),
			"app/B.php": phpFile(`App`, `class B extends A { use T; public function run() { $this->foo(); } }`),
		},
			edgeWant{`App\T.foo`, `App\B.run`, "exact"},
			edgeWant{`App\A.foo`, `App\B.run`, ""},
		)
	})
	t.Run("own member wins over the trait's", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/T.php": phpFile(`App`, `trait T { public function foo() {} }`),
			"app/C.php": phpFile(`App`, `class C { use T; public function foo() {} public function run() { $this->foo(); } }`),
		},
			edgeWant{`App\C.foo`, `App\C.run`, "exact"},
			edgeWant{`App\T.foo`, `App\C.run`, ""},
		)
	})
	t.Run("several traits, no collision", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/A.php": phpFile(`App`, `trait A { public function foo() {} }`),
			"app/B.php": phpFile(`App`, `trait B { public function bar() {} }`),
			"app/C.php": phpFile(`App`, `class C { use A, B; public function run() { $this->foo(); $this->bar(); } }`),
		},
			edgeWant{`App\A.foo`, `App\C.run`, "exact"},
			edgeWant{`App\B.bar`, `App\C.run`, "exact"},
		)
	})
	t.Run("collision is never decided", func(t *testing.T) {
		files := map[string]string{
			"app/A.php": phpFile(`App`, `trait A { public function foo() {} }`),
			"app/B.php": phpFile(`App`, `trait B { public function foo() {} }`),
			"app/C.php": phpFile(`App`, `class C { use A, B; public function run() { $this->foo(); } }`),
		}
		checkEdges(t, files,
			edgeWant{`App\A.foo`, `App\C.run`, ""},
			edgeWant{`App\B.foo`, `App\C.run`, ""},
		)
		idx := phpTreeIndex(t, files)
		for _, q := range []string{`App\A.foo`, `App\B.foo`} {
			if in, _ := idx.Unattributed(idx.FindSymbolsByQualified(q)[0].ID); in != 1 {
				t.Errorf("%s: the colliding call must be a candidate (unattributed %d)", q, in)
			}
		}
	})
	t.Run("insteadof is not modelled: no choice is made", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/A.php": phpFile(`App`, `trait A { public function foo() {} }`),
			"app/B.php": phpFile(`App`, `trait B { public function foo() {} }`),
			"app/C.php": phpFile(`App`, `class C { use A, B { A::foo insteadof B; } public function run() { $this->foo(); } }`),
		},
			edgeWant{`App\A.foo`, `App\C.run`, ""},
			edgeWant{`App\B.foo`, `App\C.run`, ""},
		)
	})
	t.Run("alias is not modelled: no fabricated target", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/A.php": phpFile(`App`, `trait A { public function foo() {} }`),
			"app/P.php": phpFile(`App`, `class P { public function bar() {} }`),
			"app/C.php": phpFile(`App`, `class C extends P { use A { foo as bar; } public function run() { $this->bar(); } }`),
		},
			edgeWant{`App\A.foo`, `App\C.run`, ""},
			edgeWant{`App\P.bar`, `App\C.run`, ""},
		)
	})
	t.Run("trait using a trait", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/A.php": phpFile(`App`, `trait A { public function foo() {} }`),
			"app/B.php": phpFile(`App`, `trait B { use A; }`),
			"app/C.php": phpFile(`App`, `class C { use B; public function run() { $this->foo(); } }`),
		}, edgeWant{`App\A.foo`, `App\C.run`, "exact"})
	})
	t.Run("trait method calling the using class is not resolved", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/T.php": phpFile(`App`, `trait T { public function run() { return $this->helper(); } }`),
			"app/C.php": phpFile(`App`, `class C { use T; public function helper() {} }`),
			"app/D.php": phpFile(`App`, `class D { public function helper() {} }`),
		},
			edgeWant{`App\C.helper`, `App\T.run`, ""},
			edgeWant{`App\D.helper`, `App\T.run`, ""},
		)
	})
	t.Run("unknown trait beside a known declaring trait", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/T.php": phpFile(`App`, `trait T { public function helper() {} }`),
			"app/C.php": phpFile(`App`, `class C { use T, \Vendor\Helpers; public function run() { $this->helper(); $this->other(); } }`),
			"app/X.php": phpFile(`App`, `class X { public function other() {} }`),
		},
			// The unseen trait may declare helper too: a candidate, no edge.
			edgeWant{`App\T.helper`, `App\C.run`, ""},
			edgeWant{`App\X.other`, `App\C.run`, ""},
		)
	})
	t.Run("private trait member is the class's own", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/T.php": phpFile(`App`, `trait T { private function helper() {} }`),
			"app/C.php": phpFile(`App`, `class C { use T; public function run() { $this->helper(); } }`),
			"app/D.php": phpFile(`App`, `class D extends C { public function run2() { $this->helper(); } }`),
		},
			edgeWant{`App\T.helper`, `App\C.run`, "exact"},
			edgeWant{`App\T.helper`, `App\D.run2`, ""},
		)
	})
}

func TestInheritance_Interfaces(t *testing.T) {
	checkEdges(t, map[string]string{
		"app/A.php":    phpFile(`App`, `interface A { public function foo(); }`),
		"app/B.php":    phpFile(`App`, `interface B extends A {}`),
		"app/Impl.php": phpFile(`App`, `class Impl implements B { public function foo() {} }`),
		"app/I.php":    phpFile(`App`, `interface I { public function bar(); }`),
		"app/C.php":    phpFile(`App`, `class C implements I { public function bar() {} }`),
		"app/Abs.php":  phpFile(`App`, `abstract class Abs implements I { abstract public function baz(); }`),
		"app/Sub.php":  phpFile(`App`, `class Sub extends Abs { public function bar() {} public function baz() {} }`),
		"app/U.php": phpFile(`App`, `class U
{
    private B $b;
    public function viaInterfaceParent() { return $this->b->foo(); }
    public function viaImplementer(C $c) { return $c->bar(); }
    public function viaAbstract(Abs $a) { return $a->baz(); }
    public function viaAbstractInterface(Abs $a) { return $a->bar(); }
}`),
	},
		// Interface inheritance: B → A.foo; never the implementation.
		edgeWant{`App\A.foo`, `App\U.viaInterfaceParent`, "exact"},
		edgeWant{`App\Impl.foo`, `App\U.viaInterfaceParent`, ""},
		// A class's own member, not the interface declaration.
		edgeWant{`App\C.bar`, `App\U.viaImplementer`, "exact"},
		edgeWant{`App\I.bar`, `App\U.viaImplementer`, ""},
		// Typed as the abstract class: its declaration, not a subclass's.
		edgeWant{`App\Abs.baz`, `App\U.viaAbstract`, "exact"},
		edgeWant{`App\Sub.baz`, `App\U.viaAbstract`, ""},
		// An abstract class's interface member.
		edgeWant{`App\I.bar`, `App\U.viaAbstractInterface`, "exact"},
		edgeWant{`App\Sub.bar`, `App\U.viaAbstractInterface`, ""},
	)
}

func TestInheritance_EvidenceGaps(t *testing.T) {
	t.Run("parent outside the repository", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/Controller.php": phpFile(`App`, `class Controller extends \Vendor\Controller { public function run() { $this->authorize(); } }`),
			"app/Other.php":      phpFile(`App`, `class Other { public function authorize() {} }`),
		}, edgeWant{`App\Other.authorize`, `App\Controller.run`, ""})
	})
	t.Run("same-named repository class is not the vendor parent", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/Controller.php":   phpFile(`App\Http`, `use Illuminate\Routing\Controller as Base; class Controller extends Base { public function run() { $this->middleware(); } }`),
			"app/Routing/Base.php": phpFile(`App\Routing`, `class Controller { public function middleware() {} }`),
		}, edgeWant{`App\Routing\Controller.middleware`, `App\Http\Controller.run`, ""})
	})
	t.Run("ambiguous parent identity", func(t *testing.T) {
		checkEdges(t, map[string]string{
			"app/a/Base.php": phpFile(`App`, `class Base { public function foo() {} }`),
			"app/b/Base.php": phpFile(`App`, `class Base { public function foo() {} }`),
			"app/C.php":      phpFile(`App`, `class C extends Base { public function run() { $this->foo(); } }`),
		})
		idx := phpTreeIndex(t, map[string]string{
			"app/a/Base.php": phpFile(`App`, `class Base { public function foo() {} }`),
			"app/b/Base.php": phpFile(`App`, `class Base { public function foo() {} }`),
			"app/C.php":      phpFile(`App`, `class C extends Base { public function run() { $this->foo(); } }`),
		})
		for _, s := range idx.FindSymbolsByQualified(`App\Base.foo`) {
			for _, e := range idx.GetCallers(s.ID) {
				t.Errorf("ambiguous parent produced an edge: %+v", e)
			}
		}
	})
	t.Run("cycles terminate", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			checkEdges(t, map[string]string{
				"app/A.php": phpFile(`App`, `class A extends B { public function run() { $this->foo(); } }`),
				"app/B.php": phpFile(`App`, `class B extends A {}`),
				"app/T.php": phpFile(`App`, `trait T { use U; }`),
				"app/U.php": phpFile(`App`, `trait U { use T; }`),
				"app/C.php": phpFile(`App`, `class C { use T; public function run() { $this->foo(); } }`),
				"app/F.php": phpFile(`App`, `class F { public function foo() {} }`),
			},
				edgeWant{`App\F.foo`, `App\A.run`, ""},
				edgeWant{`App\F.foo`, `App\C.run`, ""},
			)
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("structural lookup did not terminate on cyclic relations")
		}
	})
}

// Phase 4's constructor property cap survives the inheritance lookup.
func TestInheritance_ConfidenceCapPreserved(t *testing.T) {
	checkEdges(t, map[string]string{
		"app/ParentService.php": phpFile(`App`, `class ParentService { public function foo() {} }`),
		"app/ChildService.php":  phpFile(`App`, `class ChildService extends ParentService {}`),
		"app/Controller.php": phpFile(`App`, `class Controller
{
    protected $service;
    public function __construct(ChildService $service) { $this->service = $service; }
    public function run() { $this->service->foo(); }
}`),
		"app/Exact.php": phpFile(`App`, `class Exact
{
    private $service;
    public function __construct(ChildService $service) { $this->service = $service; }
    public function run() { $this->service->foo(); }
}`),
	},
		edgeWant{`App\ParentService.foo`, `App\Controller.run`, "strong"},
		edgeWant{`App\ParentService.foo`, `App\Exact.run`, "exact"},
	)
}

// A call targets the inherited method, never a same-named inherited property;
// a read targets the property.
func TestInheritance_CallSelectsMethodNotProperty(t *testing.T) {
	idx := phpTreeIndex(t, map[string]string{
		"app/A.php": phpFile(`App`, `abstract class A { protected $path; public function path() { return $this->path; } }`),
		"app/B.php": phpFile(`App`, `class B extends A { public function run() { return $this->path(); } }`),
	})
	for _, s := range idx.FindSymbolsByQualified(`App\A.path`) {
		var got []string
		for _, e := range idx.GetCallers(s.ID) {
			if c, ok := idx.GetSymbol(e.To); ok && c.Qualified == `App\B.run` {
				got = append(got, e.Confidence.String())
			}
		}
		want := []string(nil)
		if s.Kind == "method" {
			want = []string{"exact"}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s %s: B.run edges %v, want %v", s.Kind, s.Qualified, got, want)
		}
	}
}

// A class referring to itself ($this, self) is the declaration it is written
// in, even when its qualified name is declared again elsewhere.
func TestInheritance_SelfReferenceDespiteDuplicateIdentity(t *testing.T) {
	idx := phpTreeIndex(t, map[string]string{
		"a/Service.php": phpFile(`App`, `class Service { public function a() { return $this->b(); } public function b() {} }`),
		"b/Service.php": phpFile(`App`, `class Service { public function b() {} }`),
	})
	var target, caller string
	for _, s := range idx.FindSymbolsByQualified(`App\Service.b`) {
		if string(s.Location.File) == "a/Service.php" {
			for _, e := range idx.GetCallers(s.ID) {
				if c, ok := idx.GetSymbol(e.To); ok {
					caller, target = c.Qualified, e.Confidence.String()
				}
			}
		} else if len(idx.GetCallers(s.ID)) != 0 {
			t.Errorf("the other declaration got a caller")
		}
	}
	if caller != `App\Service.a` || target != "exact" {
		t.Errorf("got caller %q (%s), want App\\Service.a exact", caller, target)
	}
}

// Unknown structural participant must not disappear behind a known candidate:
// a declaration observed in an identified trait is only a Candidate while a
// trait taking part in the same dispatch cannot be identified or searched. No
// resolved edge is created, the reference stays unattributed, and the observed
// declaration is kept as its candidate.
func TestInheritance_UnknownTraitParticipant(t *testing.T) {
	cases := []struct {
		name       string
		files      map[string]string
		target     string
		candidates []string // candidate callers of target; nil = none
	}{
		{
			name: "identified trait declares foo beside an unknown trait",
			files: map[string]string{
				"app/T1.php": phpFile(`App`, `trait T1 { public function foo() {} }`),
				"app/C.php":  phpFile(`App`, `class C { use T1, \Vendor\Ext; public function run() { $this->foo(); } }`),
			},
			target: `App\T1.foo`, candidates: []string{`App\C.run`},
		},
		{
			name: "no identified trait declares foo beside an unknown trait",
			files: map[string]string{
				"app/T1.php": phpFile(`App`, `trait T1 { public function bar() {} }`),
				"app/X.php":  phpFile(`App`, `class X { public function foo() {} }`),
				"app/C.php":  phpFile(`App`, `class C { use T1, \Vendor\Ext; public function run() { $this->foo(); } }`),
			},
			target: `App\X.foo`, // never fabricated: unresolved, same-name only
		},
		{
			name: "two identified traits both declare foo",
			files: map[string]string{
				"app/T1.php": phpFile(`App`, `trait T1 { public function foo() {} }`),
				"app/T2.php": phpFile(`App`, `trait T2 { public function foo() {} }`),
				"app/C.php":  phpFile(`App`, `class C { use T1, T2; public function run() { $this->foo(); } }`),
			},
			target: `App\T1.foo`, candidates: []string{`App\C.run`},
		},
		{
			name: "identified trait's foo is abstract; the unknown trait may implement it",
			files: map[string]string{
				"app/T1.php": phpFile(`App`, `trait T1 { abstract public function foo(); public function run() {} }`),
				"app/C.php":  phpFile(`App`, `class C { use T1, \Vendor\Ext; public function go() { $this->foo(); } }`),
			},
			target: `App\T1.foo`, candidates: []string{`App\C.go`},
		},
		{
			name: "an identified trait that uses an unknown trait",
			files: map[string]string{
				"app/T1.php": phpFile(`App`, `trait T1 { public function foo() {} }`),
				"app/T2.php": phpFile(`App`, `trait T2 { use \Vendor\Ext; }`),
				"app/C.php":  phpFile(`App`, `class C { use T1, T2; public function run() { $this->foo(); } }`),
			},
			target: `App\T1.foo`, candidates: []string{`App\C.run`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx := phpTreeIndex(t, tc.files)
			syms := idx.FindSymbolsByQualified(tc.target)
			if len(syms) != 1 {
				t.Fatalf("target %s: %d symbols", tc.target, len(syms))
			}
			if edges := idx.GetCallers(syms[0].ID); len(edges) != 0 {
				t.Errorf("resolved edge created: %+v", edges)
			}
			if in, _ := idx.Unattributed(syms[0].ID); in != 1 {
				t.Errorf("unattributed = %d, want 1 (the unknown must not disappear)", in)
			}
			var got []string
			for _, id := range idx.CandidateCallers(syms[0].ID) {
				if s, ok := idx.GetSymbol(id); ok {
					got = append(got, s.Qualified)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.candidates, ",") {
				t.Errorf("candidate callers = %v, want %v", got, tc.candidates)
			}
		})
	}
}
