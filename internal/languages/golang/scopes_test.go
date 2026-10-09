package golang

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// refsOf extracts src and returns "Lline recv.name cap=… type=…" per call /
// construction reference, sorted.
func refsOf(t *testing.T, src string) []string {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), "x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range ex.References {
		if r.Kind != "call" && r.Kind != "construction" {
			continue
		}
		out = append(out, fmt.Sprintf("L%d %s %s.%s cap=%s type=%s", r.Location.Range.Start.Line, r.Kind, r.ReceiverExpr, r.Name, r.ConfidenceCap, r.ReceiverType))
	}
	sort.Strings(out)
	return out
}

// A local declaration shadows a name only where it is in scope: after its
// declaration, to the end of its block (or of the statement whose header
// declares it); parameters throughout the body. A shadowed call — or a
// receiver hiding an import or a type — is capped at Candidate.
func TestScopes_ShadowingFollowsGoScopes(t *testing.T) {
	src := `package x

import "example.com/m/packages"

func cancel() {}
func run()    {}

type T struct{}

func (T) M() {}

func f(run func()) {
	cancel()
	packages.Load()
	run()
	{
		cancel := func() {}
		cancel()
	}
	cancel()
	var packages []int
	_ = packages
	packages.Load()
	if cancel := 1; cancel > 0 {
		cancel()
	}
	for _, T := range []int{} {
		T.M()
	}
	T{}.M()
	var o T
	_ = T{}
	_ = o
	switch v := any(1).(type) {
	default:
		v.M()
	}
label:
	cancel()
	goto label
}
`
	got := refsOf(t, src)
	want := []string{
		"L13 call .cancel cap= type=",          // not yet shadowed
		"L14 call packages.Load cap= type=",    // the import: the local comes later
		"L15 call .run cap=candidate type=",    // the parameter
		"L18 call .cancel cap=candidate type=", // the block's local
		"L20 call .cancel cap= type=",          // the block ended
		"L23 call packages.Load cap=candidate type=",
		"L25 call .cancel cap=candidate type=", // if-header local
		"L28 call T.M cap=candidate type=",     // range variable T hides the type
		"L30 call T{}.M cap= type=",            // a composite value's method
		"L30 construction .T cap= type=",
		"L32 construction .T cap= type=", // var o T declares o, not T
		"L34 call .any cap= type=",       // the conversion any(1)
		"L36 call v.M cap=candidate type=",
		"L39 call .cancel cap= type=", // a label shadows nothing
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("references:\n got %q\nwant %q", got, want)
	}
}

// A written type is a variable's type: var x I = &T{} proves nothing about
// x's methods, and only var x = &T{} / var x T / x := &T{} prove T.
func TestReceiverTypes_DeclaredTypeWins(t *testing.T) {
	src := `package x

import "fmt"

type T struct{}

func (*T) String() string { return "" }

func f() {
	var a fmt.Stringer = &T{}
	var b = &T{}
	var c T
	d := &T{}
	_ = a.String()
	_ = b.String()
	_ = c.String()
	_ = d.String()
}
`
	types := map[string]string{}
	ex, _ := NewProvider().Extract(context.Background(), "x.go", []byte(src))
	for _, r := range ex.References {
		if r.Name == "String" {
			types[r.ReceiverExpr] = r.ReceiverType
		}
	}
	if want := map[string]string{"a": "", "b": "T", "c": "T", "d": "T"}; !reflect.DeepEqual(types, want) {
		t.Errorf("receiver types %v, want %v", types, want)
	}
}

func TestExtract_PackageScopeEvidence(t *testing.T) {
	ex, _ := NewProvider().Extract(context.Background(), "x_test.go", []byte(`package x_test

import (
	. "example.com/m/d"
	_ "example.com/m/e"
	al "example.com/m/b"
	"example.com/m/lib"
)

var _ = lib.T{}
`))
	if ex.Package != "x_test" || !ex.PackageScoped {
		t.Errorf("package %q scoped %v", ex.Package, ex.PackageScoped)
	}
	aliases := map[string]string{}
	for _, i := range ex.Imports {
		aliases[i.Path] = i.Alias
	}
	if want := map[string]string{"example.com/m/d": ".", "example.com/m/e": "_", "example.com/m/b": "al", "example.com/m/lib": ""}; !reflect.DeepEqual(aliases, want) {
		t.Errorf("import aliases %v, want %v", aliases, want)
	}
	// pkg.T{} is the construction of T on receiver pkg, like pkg.F().
	var got []string
	for _, r := range ex.References {
		if r.Kind == "construction" {
			got = append(got, r.ReceiverExpr+"|"+r.Name)
		}
	}
	if !reflect.DeepEqual(got, []string{"lib|T"}) {
		t.Errorf("constructions %v", got)
	}
}
