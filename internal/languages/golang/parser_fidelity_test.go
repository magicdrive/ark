package golang

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// goParserFuncs lists the functions and methods go/parser finds in src, as
// Ark names them (Receiver.Name for a method), sorted.
func goParserFuncs(t *testing.T, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			typ := fd.Recv.List[0].Type
			if s, ok := typ.(*ast.StarExpr); ok {
				typ = s.X
			}
			switch x := typ.(type) {
			case *ast.IndexExpr:
				typ = x.X
			case *ast.IndexListExpr:
				typ = x.X
			}
			if id, ok := typ.(*ast.Ident); ok {
				name = id.Name + "." + name
			}
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func arkFuncs(t *testing.T, file string, src []byte) ([]string, int) {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), src)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range ex.Symbols {
		if s.Kind == symbol.KindFunction || s.Kind == symbol.KindMethod {
			out = append(out, s.Qualified)
		}
	}
	slices.Sort(out)
	return out, len(ex.Diagnostics)
}

// The file where the production parser route hid four of eight functions.
func TestFidelity_CompletenessTestFileHasAllEightFunctions(t *testing.T) {
	path := filepath.Join("..", "..", "index", "completeness_test.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	got, diags := arkFuncs(t, "completeness_test.go", src)
	for _, want := range []string{
		"TestCompleteness_CandidateSourcesBoundedAndOrdered",
		"TestCompleteness_IdentityInRepositoryIsNeverSameNameAttributed",
		"TestCompleteness_OutgoingPartition",
		"TestCompleteness_Rules",
		"TestCompleteness_UnresolvedSampleBounded",
		"cands", "complRef", "complSym",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s (got %v)", want, got)
		}
	}
	if diags != 0 {
		t.Errorf("%d diagnostics on a valid file", diags)
	}
	if want := goParserFuncs(t, src); !slices.Equal(got, want) {
		t.Errorf("Ark %v\ngo/parser %v", got, want)
	}
}

// Differential: on every Go file of this repository that go/parser accepts,
// Ark extracts exactly the functions and methods go/parser finds, and
// reports no diagnostic.
func TestFidelity_RepositoryFunctionsMatchGoParser(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	n := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		want := goParserFuncs(t, src)
		if want == nil {
			return nil // not valid Go
		}
		n++
		got, diags := arkFuncs(t, p, src)
		if !slices.Equal(got, want) || diags != 0 {
			t.Errorf("%s: Ark %v (%d diagnostics)\n  go/parser %v", p, got, diags, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 100 {
		t.Fatalf("only %d files checked", n)
	}
}

// Methods of a generic type: the receiver is the type's name, never a type
// of the method's own parameters, and never dropped.
func TestExtract_GenericReceivers(t *testing.T) {
	src := []byte(`package p

type sampler[T any] struct{}
type pair[K comparable, V any] struct{}

func (s *sampler[T]) add(symKey, relKey string, item T) {}
func (s *sampler[T]) seal()                              {}
func (s sampler[T]) value() T                            { var t T; return t }
func (p *pair[K, V]) get(k K) V                          { var v V; return v }
func (pair[K, V]) none()                                 {}
func plain(a string) {}
`)
	got, diags := arkFuncs(t, "g.go", src)
	want := goParserFuncs(t, src)
	if !slices.Equal(got, want) || diags != 0 {
		t.Errorf("Ark %v (%d diagnostics)\ngo/parser %v", got, diags, want)
	}
}

// TestFidelity_ExternalCorpus runs the differential check on the directories
// ARK_GO_FIDELITY_ROOTS lists (OS path-list separated); it is skipped
// otherwise. It reports every disagreement with go/parser.
func TestFidelity_ExternalCorpus(t *testing.T) {
	list := os.Getenv("ARK_GO_FIDELITY_ROOTS")
	if list == "" {
		t.Skip("ARK_GO_FIDELITY_ROOTS not set")
	}
	for _, root := range filepath.SplitList(list) {
		files, mismatched, withDiag := 0, 0, 0
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
				if d != nil && d.IsDir() && p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			src, _ := os.ReadFile(p)
			want := goParserFuncs(t, src)
			if want == nil {
				return nil
			}
			files++
			got, diags := arkFuncs(t, p, src)
			if diags > 0 {
				withDiag++
			}
			if !slices.Equal(got, want) {
				mismatched++
				t.Logf("MISMATCH %s: missing %v extra %v", p, minus(want, got), minus(got, want))
			}
			return nil
		})
		t.Logf("%s: %d valid Go files, %d with function-set mismatches, %d with diagnostics", root, files, mismatched, withDiag)
	}
}

func minus(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

// The minimized production-route misparse (internal/tsparse): the
// declarations after it and the references inside it are extracted, with no
// diagnostic.
func TestExtract_RecoversDeclarationsAfterNestedRange(t *testing.T) {
	src := []byte(`package q

func h() {
	for range l {
		z.e(func() {
			for range a {
				k.k(y.f(s.x), s.x != "")
			}
			for range s {
				if m != a.c[n] {
					after()
				}
			}
		})
	}
}

func after() {}

func (r *recv) last() { after() }
`)
	ex, err := NewProvider().Extract(context.Background(), source.FileID("q.go"), src)
	if err != nil {
		t.Fatal(err)
	}
	var syms, calls []string
	for _, s := range ex.Symbols {
		syms = append(syms, s.Qualified)
	}
	for _, r := range ex.References {
		if r.Name == "after" {
			calls = append(calls, r.Container)
		}
	}
	slices.Sort(syms)
	slices.Sort(calls)
	if !slices.Equal(syms, []string{"after", "h", "recv.last"}) {
		t.Errorf("symbols %v", syms)
	}
	if !slices.Equal(calls, []string{"h", "recv.last"}) {
		t.Errorf("calls to after from %v", calls)
	}
	if len(ex.Diagnostics) != 0 {
		t.Errorf("diagnostics on valid source: %+v", ex.Diagnostics)
	}
}
