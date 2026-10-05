package php

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// receiverTypes returns ReceiverExpr → ReceiverType for every call named name.
func receiverTypes(t *testing.T, src, name string) map[string]string {
	t.Helper()
	ext, err := NewProvider().Extract(context.Background(), source.FileID("t.php"), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range ext.References {
		if r.Name == name {
			out[r.ReceiverExpr] = r.ReceiverType
		}
	}
	return out
}

func TestPHPReceiverType_Proven(t *testing.T) {
	src := `<?php
class Svc {
    private Repo $repo;
    public function __construct(private Logger $log) {}
    public function a(Repo $p, ?Cache $c) {
        $r = new Repo();
        $r->find();
        $p->find();
        $c->find();
        $this->repo->find();
        $this->log->find();
    }
}`
	got := receiverTypes(t, src, "find")
	want := map[string]string{
		"$r":          "Repo",
		"$p":          "Repo",
		"$c":          "Cache",
		"$this->repo": "Repo",
		"$this->log":  "Logger",
	}
	for expr, typ := range want {
		if got[expr] != typ {
			t.Errorf("%s: ReceiverType = %q, want %q", expr, got[expr], typ)
		}
	}
}

// Every case here could rebind the variable or is otherwise unproven: no
// ReceiverType may be emitted.
func TestPHPReceiverType_Unproven(t *testing.T) {
	cases := map[string]string{
		"reassigned":       `<?php function f() { $r = new Repo(); $r = new Other(); $r->find(); }`,
		"used before":      `<?php function f() { $r->find(); $r = new Repo(); }`,
		"param written":    `<?php function f(Repo $r) { $r = make(); $r->find(); }`,
		"untyped param":    `<?php function f($r) { $r->find(); }`,
		"union param":      `<?php function f(Repo|Other $r) { $r->find(); }`,
		"by-ref param":     `<?php function f(Repo &$r) { $r->find(); }`,
		"passed as arg":    `<?php function f() { $r = new Repo(); g($r); $r->find(); }`,
		"foreach":          `<?php function f($xs) { $r = new Repo(); foreach ($xs as $r) {} $r->find(); }`,
		"closure by-ref":   `<?php function f() { $r = new Repo(); $g = function () use (&$r) {}; $r->find(); }`,
		"global":           `<?php function f() { global $r; $r->find(); }`,
		"variable var":     `<?php function f() { $r = new Repo(); $$n = 1; $r->find(); }`,
		"extract":          `<?php function f($a) { $r = new Repo(); extract($a); $r->find(); }`,
		"non-new assign":   `<?php function f() { $r = make(); $r->find(); }`,
		"inside closure":   `<?php function f() { $r = new Repo(); $g = fn() => $r->find(); }`,
		"untyped property": `<?php class C { public $repo; function m() { $this->repo->find(); } }`,
		"static property":  `<?php class C { public static Repo $repo; function m() { $this->repo->find(); } }`,
		"deep chain":       `<?php class C { private Repo $repo; function m() { $this->repo->inner->find(); } }`,
	}
	for name, src := range cases {
		for expr, typ := range receiverTypes(t, src, "find") {
			if typ != "" {
				t.Errorf("%s: %s got ReceiverType %q, want none", name, expr, typ)
			}
		}
	}
}
