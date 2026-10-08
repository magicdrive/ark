package php

import (
	"sort"
	"strings"
	"testing"
)

// Calls whose name is computed at run time are observed (Dynamic), never
// given a name they do not have; class names written as `X::class`, `new
// self` / `new parent` and class-string variables are resolved only by the
// lexical rules that fix them.

// dynIdents is idents plus the Dynamic marker and the confidence cap.
func dynIdents(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, r := range extractRefs(t, src) {
		s := r.Kind + " " + r.Name
		if r.ReceiverExpr != "" {
			s += " recv=" + r.ReceiverExpr
		}
		if r.NameQualified != "" {
			s += " name=" + r.NameQualified
		}
		if r.ReceiverTypeQualified != "" {
			s += " rtype=" + r.ReceiverTypeQualified
		}
		if r.ConfidenceCap != "" {
			s += " cap=" + r.ConfidenceCap
		}
		if r.Dynamic {
			s += " dynamic"
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func wantDyn(t *testing.T, src string, want ...string) {
	t.Helper()
	got := dynIdents(t, src)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("references:\n got  %q\n want %q", got, want)
	}
}

func inMethod(body string) string {
	return "<?php\nnamespace App;\nclass C {\n    public function m($o, $m, $x) {\n" + body + "\n    }\n}\n"
}

func TestDynamicCalls_Observed(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"variable function", `$fn();`, []string{"call $fn dynamic"}},
		{"subscript callee", `$arr[0]();`, []string{"call $arr[0] dynamic"}},
		{"closure call", `(fn() => 1)();`, []string{"call (fn() => 1) dynamic"}},
		{"variable method", `$o->$m();`, []string{"call $m recv=$o dynamic"}},
		{"braced method", `$o->{$m . 'x'}();`, []string{"call $m . 'x' recv=$o dynamic"}},
		{"$this variable method", `$this->$m();`, []string{"call $m recv=C dynamic"}},
		{"nullsafe dynamic method", `$o?->$m();`, []string{"call $m recv=$o dynamic"}},
		{"static variable method", `Foo::$m();`, []string{"call $m recv=Foo dynamic"}},
		{"static braced method", `Foo::{'run'}();`, []string{"call 'run' recv=Foo dynamic"}},
		{"new variable", `new $x();`, []string{"construction $x dynamic"}},
		{"new expression", `new ($o->cls);`, []string{"construction ($o->cls) dynamic"}},
		{"long name is shortened", `$fn` + strings.Repeat("x", 80) + `();`, []string{"call $fn" + strings.Repeat("x", 61) + "… dynamic"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { wantDyn(t, inMethod(tc.body), tc.want...) })
	}
}

// Syntax with a fixed name keeps it: these are ordinary references.
func TestDynamicCalls_FixedNamesAreNotDynamic(t *testing.T) {
	wantDyn(t, inMethod(`$o?->run(); $x::make(); helper(); \App\helper(); new class {};`),
		"call run recv=$o",
		"call make recv=$x", // untyped receiver: the resolver caps it at Candidate
		"call helper",
		"call helper",
	)
}

func TestClassString_IsATypeUse(t *testing.T) {
	wantDyn(t, "<?php\nnamespace App;\nuse Vendor\\Container;\nclass C extends P {\n    public function m() {\n"+
		"        $this->c->make(Repo::class);\n        Container::class;\n        self::class; static::class; parent::class;\n        $o::class;\n    }\n}\n",
		"inheritance P name=App\\P",
		"call make recv=$this->c",
		"type_use Repo name=App\\Repo",
		"type_use Container name=Vendor\\Container",
		"type_use C name=App\\C",
	)
}

func TestRelativeConstruction(t *testing.T) {
	wantDyn(t, "<?php\nnamespace App;\nclass C extends P {\n    public function m() { new self(); new static(); new parent(); }\n}\n",
		"inheritance P name=App\\P",
		"construction C name=App\\C",
		"construction static", // a subclass inheriting m constructs itself
		"construction P name=App\\P",
	)
	wantDyn(t, "<?php\nnamespace App;\nfinal class F {\n    public function m() { new static(); }\n}\n",
		"construction F name=App\\F")
	// In a trait, self is the using class: not lexically known.
	wantDyn(t, "<?php\nnamespace App;\ntrait T {\n    public function m() { new self(); }\n}\n",
		"construction self")
	// A class without a parent: parent names no class.
	wantDyn(t, "<?php\nnamespace App;\nclass C {\n    public function m() { new parent(); }\n}\n",
		"construction parent")
}

func TestClassStringVariable(t *testing.T) {
	ok := func(body string, want ...string) {
		t.Helper()
		wantDyn(t, "<?php\nnamespace App;\nuse App\\Models\\Repo;\nclass C {\n    public function m($arg) {\n"+body+"\n    }\n}\n", want...)
	}
	ok(`$c = Repo::class; $r = new $c(); $c::make(); $c::K;`,
		"type_use Repo name=App\\Models\\Repo",
		"construction Repo name=App\\Models\\Repo",
		"call make recv=Repo rtype=App\\Models\\Repo",
		"read K recv=Repo rtype=App\\Models\\Repo",
	)
	// Use before the assignment: no evidence.
	ok(`new $c(); $c = Repo::class;`,
		"construction $c dynamic",
		"type_use Repo name=App\\Models\\Repo",
	)
	// Reassigned, passed on (possibly by reference), built dynamically, or a
	// parameter: no evidence.
	ok(`$c = Repo::class; $c = $arg; new $c();`, "type_use Repo name=App\\Models\\Repo", "construction $c dynamic")
	ok(`$c = Repo::class; f($c); new $c();`, "type_use Repo name=App\\Models\\Repo", "call f", "construction $c dynamic")
	ok(`$c = 'App\\' . $arg; new $c(); $c::make();`, "construction $c dynamic", "call make recv=$c")
	ok(`new $arg(); $arg::make();`, "construction $arg dynamic", "call make recv=$arg")
	ok(`$c = Repo::class; $$arg = 1; new $c();`, "type_use Repo name=App\\Models\\Repo", "construction $c dynamic")
	// Several possible classes are not one: no class is chosen.
	ok(`$c = $arg ? Repo::class : Other::class; new $c();`,
		"type_use Repo name=App\\Models\\Repo", "type_use Other name=App\\Other", "construction $c dynamic")
	ok(`$c = Repo::class; if ($arg) { $c = Other::class; } new $c();`,
		"type_use Repo name=App\\Models\\Repo", "type_use Other name=App\\Other", "construction $c dynamic")
	// A class-string is not an instance: `$c->m()` gets no receiver type.
	ok(`$c = Repo::class; $c->run();`, "type_use Repo name=App\\Models\\Repo", "call run recv=$c")
	// Closures have their own scope.
	ok(`$c = Repo::class; $f = function () use ($c) { return new $c(); };`, "type_use Repo name=App\\Models\\Repo", "construction $c dynamic")
}

// Reading a variable as a class cannot rebind it, so it keeps existing
// instance evidence.
func TestClassPositionDoesNotPoisonInstanceEvidence(t *testing.T) {
	wantDyn(t, "<?php\nnamespace App;\nclass C {\n    public function m() {\n        $r = new Repo(); $r::K; $r->save();\n    }\n}\n",
		"construction Repo name=App\\Repo",
		"read K recv=$r",
		"call save recv=$r rtype=App\\Repo",
	)
}

// The grammar's error recovery can turn valid code into a scoped call whose
// "scope" is an array and an ERROR node (here: the second `[...] =
// static::x()`). Such a shape proves no call: nothing is emitted for it,
// rather than a call with a fabricated receiver.
func TestDynamicCalls_ErrorRecoveryEmitsNothing(t *testing.T) {
	src := "<?php\nnamespace App;\nclass P\n{\n    protected static function a($t)\n    {\n        [$t, $d] = static::x($t);\n    }\n" +
		"    protected static function b($t)\n    {\n        [$t, $d] = static::x($t);\n    }\n}\n"
	for _, r := range extractRefs(t, src) {
		if r.Dynamic || (r.Name == "x" && r.ReceiverTypeQualified == "") {
			t.Errorf("reference from a recovered parse: %+v", r)
		}
	}
}
