package golang_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
)

// A call of an element of a local slice (`validators[i](t, d)`) — which the
// parser reads as a generic instantiation — must not become an edge to a
// package-level function that happens to share the slice's name, while a real
// generic call of the same shape must.
func TestGraph_SubscriptedCallOfALocalIsNoEdge(t *testing.T) {
	root := t.TempDir()
	src := `package p

type check func(int, int)

func validators(a, b int) {}

func Make[T any](a, b int) {}

func run(validators []check, d int) {
	for i := 0; i < len(validators); i++ {
		validators[i](i, d)
	}
}

func build(d int) {
	Make[check](d, d)
}
`
	if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), root, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	callees := func(name string) map[string]string {
		out := map[string]string{}
		for _, s := range idx.FindSymbolsByQualified(name) {
			if s.Qualified != name {
				continue
			}
			for _, e := range idx.GetCallees(s.ID) {
				if to, ok := idx.GetSymbol(e.To); ok {
					out[to.Qualified] = fmt.Sprint(e.Confidence)
				}
			}
		}
		return out
	}
	if got := callees("run"); len(got) != 0 {
		t.Errorf("run calls an element of its parameter, not a function: %v", got)
	}
	if got := callees("build"); got["Make"] == "" {
		t.Errorf("build calls Make[check]: %v", got)
	}
}

// Element calls of package-level function slices and maps — the variable in
// the same file or another file of the package, a function of the same name
// in another package — are no edges; generic calls and constructions are.
func TestGraph_ElementCallsOfPackageValuesAreNoEdges(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/m\n",
		"a/a.go": `package a

var handlers []func(int)
var table map[string]func(int)

type Registry struct{ handlers []func(int) }

func Identity[T any](value T) T { return value }

type Box[T any] struct{ Value T }

func useA(i int)             { handlers[i](1) }
func useB(name string)       { table[name](1) }
func useC()                  { Identity[int](1) }
func useD(v int)             { _ = Box[int]{Value: v} }
func useE(i int)             { others[i](1) }
func useF(r Registry, i int) { r.handlers[i](1) }
func useG()                  { Identity[Key](k) }
func useH()                  { others[Mode](1) }
func useI()                  { Generic[Key](k) }
`,
		"a/b.go": "package a\n\nvar others []func(int)\n\ntype Key int\n\nvar k Key\n\nconst Mode = 0\n\nfunc Generic[T any](v T) {}\n",
		"b/b.go": "package b\n\nfunc handlers(x int) {}\nfunc table(x int) {}\nfunc others(x int) {}\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), root, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				if to, ok := idx.GetSymbol(e.To); ok {
					got[s.Qualified+" -> "+to.Qualified+"@"+string(to.Location.File)] = fmt.Sprint(e.Confidence)
				}
			}
			for _, c := range idx.CandidateCallers(s.ID) {
				if from, ok := idx.GetSymbol(c); ok {
					got[from.Qualified+" ~> "+s.Qualified+"@"+string(s.Location.File)] = "candidate"
				}
			}
		}
	}
	want := map[string]string{
		"useC -> Identity@a/a.go": "exact",
		"useD -> Box@a/a.go":      "exact",
		"useG -> Identity@a/a.go": "exact",
		// Both names declared in another file: decided by the target.
		"useI -> Generic@a/b.go": "strong",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("missing %s (%s)", k, v)
		}
	}
	for k, v := range got {
		if want[k] == "" {
			t.Errorf("unexpected relation %s (%s)", k, v)
		}
	}
}
