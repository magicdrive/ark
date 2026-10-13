package golang

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func extractFor(t *testing.T, src string) language.Extraction {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID("p/a.go"), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func factOf(ex language.Extraction, decl string) *language.TypeFact {
	for i := range ex.TypeFacts {
		if ex.TypeFacts[i].Decl == decl {
			return &ex.TypeFacts[i]
		}
	}
	return nil
}

// Each fact belongs to the symbol of its declaration: same qualified name,
// same start.
func TestGoTypeFacts_MatchTheirSymbols(t *testing.T) {
	ex := extractFor(t, `package p

type S struct{ a int }

func F() int { return 0 }

func (S) M() S { return S{} }

var V, W = 1, 2
`)
	for _, f := range ex.TypeFacts {
		found := false
		for _, s := range ex.Symbols {
			if s.Qualified == f.Decl && s.Location.Range.Start.Line == f.Line && s.Location.Range.Start.Column == f.Column {
				found = true
			}
		}
		if !found {
			t.Errorf("fact %+v matches no symbol", f)
		}
	}
	// S, F, S.M — V and W are initialized by literals, which state no type.
	if len(ex.TypeFacts) != 3 {
		t.Errorf("facts: %+v", ex.TypeFacts)
	}
}

func TestGoTypeFacts_Types(t *testing.T) {
	ex := extractFor(t, `package p

import "net/http"

type S struct {
	Inner
	*http.Client
	Box[int]
	a, b []*T
	m    map[string]http.Header
	f    func() T
	arr  [4]T
	p    *T
	ch   chan T
}

type Box[X any] struct {
	v  X
	vs []X
	n  int
}

type I interface {
	Reader
	http.Handler
	~int | string
	M() int
}

type Nodes []*Node

type A = http.Client

type L = []T
`)
	want := map[string]language.TypeFact{
		"S": {Struct: true, Complete: true, Fields: []language.FieldFact{
			{Type: "Inner", Embedded: true}, {Type: "*http.Client", Embedded: true}, {Type: "Box", Embedded: true},
			{Name: "a", Type: "[]*T"}, {Name: "b", Type: "[]*T"}, {Name: "m", Type: "map[]http.Header"},
			{Name: "f"}, {Name: "arr", Type: "[]T"}, {Name: "p", Type: "*T"}, {Name: "ch"},
		}},
		"Box":   {Struct: true, Complete: true, Fields: []language.FieldFact{{Name: "v"}, {Name: "vs"}, {Name: "n"}}},
		"I":     {Interface: true, Complete: true, Fields: []language.FieldFact{{Type: "Reader", Embedded: true}, {Type: "http.Handler", Embedded: true}}},
		"Nodes": {Underlying: "[]*Node"},
		"A":     {Alias: "http.Client"},
		"L":     {Underlying: "[]T"},
	}
	for decl, w := range want {
		f := factOf(ex, decl)
		if f == nil {
			t.Errorf("%s: no fact", decl)
			continue
		}
		got := *f
		got.Decl, got.Line, got.Column = "", 0, 0
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s:\n got %+v\nwant %+v", decl, got, w)
		}
	}
	// A predeclared type (int, string — a map's key) is no stated type: "".
	// The interface's method is a
	// symbol of its own, a member contained in it.
	var m *language.SymbolDraft
	for i, s := range ex.Symbols {
		if s.Qualified == "I.M" {
			m = &ex.Symbols[i]
		}
	}
	if m == nil || m.Kind != symbol.KindMethod || m.Receiver != "I" || m.Parent != "I" || !m.Exported {
		t.Errorf("interface method symbol: %+v", m)
	}
}

func TestGoTypeFacts_ParseErrorMakesAStructIncomplete(t *testing.T) {
	ex := extractFor(t, "package p\n\ntype S struct {\n\ta int\n\tb ]]\n}\n")
	if f := factOf(ex, "S"); f != nil && f.Complete {
		t.Errorf("a struct with a parse error inside is complete: %+v", f)
	}
}

func TestGoTypeFacts_ResultsAndVars(t *testing.T) {
	ex := extractFor(t, `package p

import "os"

func A() *T                    { return nil }
func B() (t *T, err error)     { return }
func C() (a, b T)              { return }
func G[X any]() (X, *T)        { var x X; return x, nil }
func (b Box[X]) Get() (X, []T) { var x X; return x, nil }
func D()                       {}

var V1 T
var V2 = &T{}
var V3, V4 = NewPair()
var V5 = os.Stdout
var V6 = A().next
`)
	for decl, want := range map[string][]string{
		"A": {"*T"}, "B": {"*T", ""}, "C": {"T", "T"}, "G": {"", "*T"}, "Box.Get": {"", "[]T"}, "D": nil,
	} {
		f := factOf(ex, decl)
		if f == nil || !reflect.DeepEqual(f.Results, want) {
			t.Errorf("%s results: %+v, want %v", decl, f, want)
		}
	}
	for decl, want := range map[string][2]string{
		"V1": {"T", ""}, "V2": {"", "type:T"}, "V3": {"", "decl:NewPair|call:0"}, "V4": {"", "decl:NewPair|call:1"},
		"V5": {"", "decl:os.Stdout"}, "V6": {"", "decl:A|call:0|member:next"},
	} {
		f := factOf(ex, decl)
		if f == nil || [2]string{f.Type, f.Chain} != want {
			t.Errorf("%s: %+v, want type %q chain %q", decl, f, want[0], want[1])
		}
	}
}

// chainsOf returns "name@line chain" of the file's references with a chain.
func chainsOf(ex language.Extraction) []string {
	var out []string
	for _, r := range ex.References {
		if r.ReceiverChain != "" {
			out = append(out, r.Name+"@"+itoaGo(int(r.Location.Range.Start.Line))+" "+r.ReceiverChain)
		}
	}
	return out
}

func itoaGo(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}

func TestGoReceiverChains(t *testing.T) {
	ex := extractFor(t, `package p

import (
	"os"
	"strings"
)

func f(items []*Item, m map[string]T, b strings.Builder) {
	x := NewT()
	x.Run()
	a, err := os.Open("x")
	_ = err
	a.Close()
	for _, it := range items {
		it.Run()
	}
	for k := range m {
		k.Run()
	}
	v := any(x).(*T)
	v.Run()
	n := new(T)
	n.Run()
	c := &T{}
	c.Run()
	x.Next().Run()
	NewT().Run()
	m["k"].Run()
	if y := NewT(); y != nil {
		y.Run()
	}
	strings := NewT()
	strings.Run()
	Default.Run()
	func() {
		x.Run()
	}()
}
`)
	want := []string{
		"Run@10 decl:NewT|call:0",
		"Close@13 decl:os.Open|call:0",
		"Run@15 type:[]*Item|elem",
		"Run@21 type:*T",
		"Run@23 type:*T",
		"Run@26 decl:NewT|call:0|member:Next|call:0",
		"Next@26 decl:NewT|call:0",
		"Run@27 decl:NewT|call:0",
		"Run@28 type:map[]T|elem",
		"Run@30 decl:NewT|call:0",
		"Run@33 decl:NewT|call:0",
		"Run@34 decl:Default",
		"Run@36 decl:NewT|call:0",
	}
	got := chainsOf(ex)
	// A range key has no chain: k.Run (line 18) is absent; c := &T{} is a
	// proven ReceiverType (receiver_types.go), no chain (line 25).
	if !reflect.DeepEqual(got, want) {
		t.Errorf("chains:\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
	// A chained receiver is at most Strong.
	for _, r := range ex.References {
		if r.ReceiverChain != "" && r.ConfidenceCap != "strong" {
			t.Errorf("%s: chain without the strong cap: %q", r.Name, r.ConfidenceCap)
		}
	}
}

// No chain for what the code does not state: a type parameter, a local
// type, a variable declared in a type switch, a builtin's result, an
// out-of-scope local (then the package-level name is meant).
func TestGoReceiverChains_NotStated(t *testing.T) {
	ex := extractFor(t, `package p

func g[X Runner](x X, xs []X) {
	x.Run()
	xs[0].Run()
	type local struct{ T }
	var l local
	l.Run()
	switch v := any(x).(type) {
	case *T:
		v.Run()
	}
	_ = append(xs, x)[0]
	{
		z := NewT()
		_ = z
	}
	z.Run()
}
`)
	got := chainsOf(ex)
	if want := []string{"Run@18 decl:z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("chains: %v, want %v", got, want)
	}
}

// A selector x.f read with type evidence is Speculative (a field reads
// nothing); without evidence, or written, it is not recorded that way.
func TestGoSpeculativeSelectors(t *testing.T) {
	ex := extractFor(t, `package p

func h(t T, u interface{ M() }) {
	f := t.Method
	_ = t.field
	_ = NewT().Method
	_ = u.M
	_ = unknown().x
	t.field = 1
	_ = (*T).Method
}
`)
	var spec []string
	for _, r := range ex.References {
		if r.Speculative {
			spec = append(spec, r.Name+"@"+itoaGo(int(r.Location.Range.Start.Line)))
		}
	}
	want := []string{"Method@4", "field@5", "Method@6", "x@8", "Method@10"}
	if !reflect.DeepEqual(spec, want) {
		t.Errorf("speculative: %v, want %v", spec, want)
	}
}
