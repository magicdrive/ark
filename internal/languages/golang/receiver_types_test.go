package golang

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// callReceiverTypes returns ReceiverExpr → ReceiverType for calls named name.
func callReceiverTypes(t *testing.T, src, name string) map[string]string {
	t.Helper()
	ext, err := NewProvider().Extract(context.Background(), source.FileID("x.go"), []byte(src))
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

func TestGoReceiverType_Proven(t *testing.T) {
	src := `package p
type Svc struct {
	repo  *Repo
	cache Cache
}
func (s *Svc) Run(p *Repo, q Repo) {
	a := &Repo{}
	b := Repo{}
	var c Repo
	var d = &Repo{}
	s.Find()
	s.repo.Find()
	s.cache.Find()
	p.Find()
	q.Find()
	a.Find()
	b.Find()
	c.Find()
	d.Find()
}`
	got := callReceiverTypes(t, src, "Find")
	want := map[string]string{
		"s": "Svc", "s.repo": "Repo", "s.cache": "Cache",
		"p": "Repo", "q": "Repo", "a": "Repo", "b": "Repo", "c": "Repo", "d": "Repo",
	}
	for expr, typ := range want {
		if got[expr] != typ {
			t.Errorf("%s: ReceiverType = %q, want %q", expr, got[expr], typ)
		}
	}
}

// Unproven receivers must carry no ReceiverType.
func TestGoReceiverType_Unproven(t *testing.T) {
	cases := map[string]string{
		"qualified type":   `package p; func f(t *testing.T) { t.Find() }`,
		"generic type":     `package p; func f(r Repo[int]) { r.Find() }`,
		"inferred":         `package p; func f() { r := NewRepo(); r.Find() }`,
		"shadowed":         `package p; func f(r *Repo) { if x { r := &Other{}; _ = r }; r.Find() }`,
		"closure param":    `package p; func f(r *Repo) { g := func(r *Other) {}; _ = g; r.Find() }`,
		"range var":        `package p; func f(xs []Repo) { for _, r := range xs { r.Find() } }`,
		"multi assign":     `package p; func f() { r, e := &Repo{}, 1; _ = e; r.Find() }`,
		"use before decl":  `package p; func f() { r.Find(); r := &Repo{}; _ = r }`,
		"nested block":     `package p; func f() { { r := &Repo{}; r.Find() } }`,
		"deep field chain": `package p; type S struct{ a *A }; func (s *S) m() { s.a.b.Find() }`,
		"untyped field":    `package p; type S struct{ a interface{} }; func (s *S) m() { s.a.Find() }`,
		"call receiver":    `package p; func f() { get().Find() }`,
	}
	for name, src := range cases {
		for expr, typ := range callReceiverTypes(t, src, "Find") {
			if typ != "" {
				t.Errorf("%s: %s got ReceiverType %q, want none", name, expr, typ)
			}
		}
	}
}
