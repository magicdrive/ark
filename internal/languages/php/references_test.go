package php

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

type phpRef struct {
	kind      string
	container string
	receiver  string
	call      bool
}

// refIndex extracts references and returns a lookup by Name (last wins when a
// name repeats; tests use distinct names or check counts explicitly).
func refIndex(t *testing.T, src string) ([]language.ReferenceDraft, map[string]phpRef) {
	t.Helper()
	ext, err := NewProvider().Extract(context.Background(), source.FileID("t.php"), []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	m := map[string]phpRef{}
	for _, r := range ext.References {
		m[r.Name] = phpRef{kind: r.Kind, container: r.Container, receiver: r.ReceiverExpr, call: r.IsCall}
	}
	return ext.References, m
}

func TestPHPRef_Calls(t *testing.T) {
	_, m := refIndex(t, `<?php
namespace App;
class S {
    public function m() {
        helper();
        $this->save();
        $other->flush();
        User::create();
        self::make();
    }
}`)
	cases := map[string]phpRef{
		"helper": {kind: "call", container: "App\\S.m", receiver: "", call: true},
		"save":   {kind: "call", container: "App\\S.m", receiver: "S", call: true},      // $this → class bare
		"flush":  {kind: "call", container: "App\\S.m", receiver: "$other", call: true}, // other var → verbatim (untyped, R4)
		"create": {kind: "call", container: "App\\S.m", receiver: "User", call: true},   // static receiver
		"make":   {kind: "call", container: "App\\S.m", receiver: "S", call: true},      // self:: → enclosing class
	}
	for name, want := range cases {
		if got, ok := m[name]; !ok {
			t.Errorf("call %q not extracted", name)
		} else if got != want {
			t.Errorf("call %q = %+v, want %+v", name, got, want)
		}
	}
}

func TestPHPRef_Construction(t *testing.T) {
	refs, m := refIndex(t, `<?php
function f() {
    new User();
    new \App\Model\Account();
}`)
	if got, ok := m["User"]; !ok || got.kind != "construction" || got.call {
		t.Errorf("new User() = %+v", got)
	}
	if got, ok := m["Account"]; !ok || got.kind != "construction" {
		t.Errorf("new \\App\\Model\\Account() should be construction with bare name Account: %+v (%d refs)", got, len(refs))
	}
}

func TestPHPRef_ConstantAccessIsRead(t *testing.T) {
	_, m := refIndex(t, `<?php
function f() {
    $x = User::STATUS_ACTIVE;
    $y = Status::Active;
}`)
	if got, ok := m["STATUS_ACTIVE"]; !ok || got.kind != "read" || got.call || got.receiver != "User" {
		t.Errorf("User::STATUS_ACTIVE = %+v, want read/receiver=User/non-call", got)
	}
	if got, ok := m["Active"]; !ok || got.kind != "read" || got.receiver != "Status" {
		t.Errorf("Status::Active = %+v, want read/receiver=Status", got)
	}
}

func TestPHPRef_Types(t *testing.T) {
	refs, m := refIndex(t, `<?php
class C {
    private Repository $repo;
    public function f(User $u, ?Logger $l, Admin|Guest $g, A&B $i): Result {}
    public function g(): void {}
    public function h(): self {}
    public function k(int $n, string $s): bool {}
}`)
	// Class types in positions → type_use.
	for _, name := range []string{"Repository", "User", "Logger", "Admin", "Guest", "A", "B", "Result"} {
		if got, ok := m[name]; !ok || got.kind != "type_use" {
			t.Errorf("type %q should be type_use: %+v", name, got)
		}
	}
	// Builtins and self must NOT be emitted as type references.
	for _, name := range []string{"int", "string", "bool", "void", "self"} {
		if _, ok := m[name]; ok {
			t.Errorf("builtin/self %q must not be a type reference", name)
		}
	}
	// Container of a parameter type is the method.
	for _, r := range refs {
		if r.Name == "User" && r.Container != "C.f" {
			t.Errorf("User type container = %q, want C.f", r.Container)
		}
		if r.Name == "Repository" && r.Container != "C" {
			t.Errorf("Repository property type container = %q, want C", r.Container)
		}
	}
}

func TestPHPRef_Relations(t *testing.T) {
	refs, _ := refIndex(t, `<?php
namespace App;
class Child extends Base implements A, B {
    use T1, T2;
}
interface IChild extends IA, IB {}`)
	type rk struct{ name, kind, container string }
	got := map[rk]bool{}
	for _, r := range refs {
		got[rk{r.Name, r.Kind, r.Container}] = true
	}
	want := []rk{
		{"Base", "inheritance", "App\\Child"},
		{"A", "implementation", "App\\Child"},
		{"B", "implementation", "App\\Child"},
		{"T1", "uses_trait", "App\\Child"},
		{"T2", "uses_trait", "App\\Child"},
		{"IA", "inheritance", "App\\IChild"},
		{"IB", "inheritance", "App\\IChild"},
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing relation %+v", w)
		}
	}
}

func TestPHPRef_TraitAdaptationKeepsBaseRelation(t *testing.T) {
	refs, _ := refIndex(t, `<?php
class S {
    use A, B {
        A::foo insteadof B;
        A::bar as baz;
    }
}`)
	traits := map[string]bool{}
	for _, r := range refs {
		if r.Kind == "uses_trait" {
			traits[r.Name] = true
		}
	}
	if !traits["A"] || !traits["B"] {
		t.Errorf("trait use with adaptations must keep A and B relations: %v", traits)
	}
	// insteadof/as adaptation targets must not create bogus references.
	if len(traits) != 2 {
		t.Errorf("expected exactly 2 trait-use relations, got %v", traits)
	}
}

func TestPHPRef_Containers(t *testing.T) {
	refs, _ := refIndex(t, `<?php
namespace App;
function top() { helper(); }
class C {
    public function __construct() { boot(); }
    public function m() { work(); }
}
trait T {
    public function tm() { log(); }
}
enum E {
    public function em() { emit(); }
}`)
	wantContainer := map[string]string{
		"helper": "App\\top",
		"boot":   "App\\C.__construct",
		"work":   "App\\C.m",
		"log":    "App\\T.tm",
		"emit":   "App\\E.em",
	}
	seen := map[string]string{}
	for _, r := range refs {
		if r.Kind == "call" {
			seen[r.Name] = r.Container
		}
	}
	for name, want := range wantContainer {
		if seen[name] != want {
			t.Errorf("call %q container = %q, want %q", name, seen[name], want)
		}
	}
}

// Dynamic PHP constructs must not fabricate references.
func TestPHPRef_DynamicNegative(t *testing.T) {
	refs, _ := refIndex(t, `<?php
class C {
    public function m() {
        $cls = 'User';
        new $cls();
        $meth = 'save';
        $this->$meth();
        User::{$meth}();
        $fn = 'helper';
        $fn();
    }
}`)
	// None of the dynamic targets should appear as references.
	for _, r := range refs {
		switch r.Name {
		case "cls", "meth", "fn", "User", "save", "helper", "this":
			// 'User' here only appears in dynamic positions; ensure no call/
			// construction fabricated. (A type reference would be fine, but
			// there are no type positions in this source.)
			if r.Kind == "call" || r.Kind == "construction" {
				t.Errorf("dynamic construct fabricated a %s reference to %q", r.Kind, r.Name)
			}
		}
	}
}

func TestPHPRef_ContainerSafety(t *testing.T) {
	refs, _ := refIndex(t, `<?php
class Outer {
    public function m() {
        $f = function() { inClosure(); };
        $a = new class {
            public function x() { inAnon(); }
        };
        $arrow = fn() => inArrow();
    }
}`)
	for _, r := range refs {
		// inAnon() is inside an anonymous class method and must NOT attach to
		// Outer.m (anonymous classes have no symbol identity, so we skip them).
		if r.Name == "inAnon" {
			t.Errorf("anonymous-class body reference must not be extracted: %+v", r)
		}
		// Closures / arrow functions are part of the method; their calls attach
		// to Outer.m (acceptable), but must never attach to a wrong container.
		if (r.Name == "inClosure" || r.Name == "inArrow") && r.Container != "Outer.m" {
			t.Errorf("%q container = %q, want Outer.m", r.Name, r.Container)
		}
	}
}

func TestPHPRef_Deterministic(t *testing.T) {
	src := `<?php
namespace App;
class C extends Base implements I {
    use T;
    public function m(User $u): Result {
        helper();
        $this->save();
        User::create();
        new Thing();
    }
}`
	a, _ := NewProvider().Extract(context.Background(), source.FileID("t.php"), []byte(src))
	b, _ := NewProvider().Extract(context.Background(), source.FileID("t.php"), []byte(src))
	if len(a.References) != len(b.References) {
		t.Fatalf("non-deterministic ref count: %d vs %d", len(a.References), len(b.References))
	}
	for i := range a.References {
		if a.References[i] != b.References[i] {
			t.Errorf("non-deterministic ref at %d: %+v vs %+v", i, a.References[i], b.References[i])
		}
	}
}

func TestPHPRef_Safety(t *testing.T) {
	inputs := map[string]string{
		"malformed_call":        `<?php function f() { helper( ; }`,
		"malformed_type":        `<?php function f(Foo| $x) {}`,
		"malformed_inheritance": `<?php class C extends {}`,
		"malformed_trait":       `<?php class C { use ; }`,
		"invalid_utf8":          "<?php \xff\xfe class C {}",
		"broken":                `<?php class C { public function m() { $x->`,
	}
	p := NewProvider()
	for name, src := range inputs {
		src := src
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", name, r)
				}
			}()
			if _, err := p.Extract(context.Background(), source.FileID("x.php"), []byte(src)); err != nil {
				t.Errorf("unexpected error on %q: %v", name, err)
			}
		})
	}
}

// STOP-3 contract: an instance-variable receiver is recorded verbatim (so the
// resolver can tell a member call from a receiverless name) and carries NO
// declared type unless it is proven. The resolver caps such untyped receivers
// at Candidate, so the verbatim text can never produce a false Exact/Strong.
func TestPHPRef_InstanceReceiverVerbatimUntyped(t *testing.T) {
	refs, m := refIndex(t, `<?php
class C {
    public function m() {
        $user->save();
        $repo->find();
    }
}`)
	if got := m["save"]; got.receiver != "$user" {
		t.Errorf("$user->save() receiver = %q, want verbatim \"$user\"", got.receiver)
	}
	if got := m["find"]; got.receiver != "$repo" {
		t.Errorf("$repo->find() receiver = %q, want verbatim \"$repo\"", got.receiver)
	}
	for _, r := range refs {
		if r.ReceiverType != "" {
			t.Errorf("%s: unproven receiver got ReceiverType %q", r.Name, r.ReceiverType)
		}
	}
}
