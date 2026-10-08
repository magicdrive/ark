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
