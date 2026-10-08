package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Reference fidelity: Ark's call and construction references compared with
// go/ast, and — for the syntactically ambiguous subscripted callee f[x](...)
// — with go/types, which knows whether f[x] instantiates a generic or indexes
// a value. Imports are type-checked as empty packages, so only what one
// package itself declares is decided; the rest stays undecided and is not
// asserted.

type refAt struct {
	line, col uint32
}

// emptyImporter gives every import an empty, complete package.
type emptyImporter struct{}

func (emptyImporter) Import(path string) (*types.Package, error) {
	p := types.NewPackage(path, filepath.Base(path))
	p.MarkComplete()
	return p, nil
}

// fidelityReport accumulates disagreements, by kind.
type fidelityReport struct {
	checked  int
	problems map[string][]string
	// held counts generic calls capped, or not restricted to function /
	// method / type targets.
	held, generic int
}

func (r *fidelityReport) add(kind, where string) {
	if r.problems == nil {
		r.problems = map[string][]string{}
	}
	r.problems[kind] = append(r.problems[kind], where)
}

// checkPackage compares one directory's files of one package.
func checkPackage(t *testing.T, fset *token.FileSet, files []*ast.File, srcs map[*ast.File][]byte, names map[*ast.File]string, rep *fidelityReport) {
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Instances: map[*ast.Ident]types.Instance{}}
	conf := types.Config{Importer: emptyImporter{}, Error: func(error) {}}
	_, _ = conf.Check("p", fset, files, info)
	for _, f := range files {
		src, name := srcs[f], names[f]
		ex, err := NewProvider().Extract(context.Background(), source.FileID(name), src)
		if err != nil {
			t.Fatal(err)
		}
		if len(ex.Diagnostics) > 0 {
			rep.add("file with diagnostics", name)
			continue
		}
		rep.checked++
		calls := map[refAt][]language.ReferenceDraft{}
		constructions := map[refAt][]string{}
		for _, r := range ex.References {
			at := refAt{r.Location.Range.Start.Line, r.Location.Range.Start.Column}
			switch r.Kind {
			case "call":
				calls[at] = append(calls[at], r)
			case "construction":
				constructions[at] = append(constructions[at], r.Name)
			}
		}
		text := func(n ast.Node) string {
			return string(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset])
		}
		posOf := func(n ast.Node) refAt {
			p := fset.Position(n.Pos())
			return refAt{uint32(p.Line), uint32(p.Column)}
		}
		where := func(n ast.Node, s string) string {
			return fmt.Sprintf("%s:%d %s", name, fset.Position(n.Pos()).Line, s)
		}
		findCall := func(at refAt, recv, nm string) *language.ReferenceDraft {
			for i, r := range calls[at] {
				if r.Name == nm && r.ReceiverExpr == recv {
					return &calls[at][i]
				}
			}
			return nil
		}
		wantCalls := map[refAt]map[string]bool{}
		wantCons := map[refAt]map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				typ := x.Type
				switch g := typ.(type) {
				case *ast.IndexExpr:
					typ = g.X
				case *ast.IndexListExpr:
					typ = g.X
				}
				switch typ.(type) {
				case *ast.Ident, *ast.SelectorExpr:
					at := posOf(typ)
					if wantCons[at] == nil {
						wantCons[at] = map[string]bool{}
					}
					wantCons[at][text(typ)] = true
					found := false
					for _, c := range constructions[at] {
						found = found || c == text(typ)
					}
					if !found {
						rep.add("missing construction", where(x, text(typ)))
					}
				}
			case *ast.CallExpr:
				fun, subscripted := x.Fun, false
				switch g := fun.(type) {
				case *ast.IndexExpr:
					fun, subscripted = g.X, true
				case *ast.IndexListExpr:
					fun, subscripted = g.X, true
				}
				var recv, nm string
				var base *ast.Ident
				switch g := fun.(type) {
				case *ast.Ident:
					nm, base = g.Name, g
				case *ast.SelectorExpr:
					recv, nm, base = text(g.X), g.Sel.Name, g.Sel
				default:
					return true
				}
				at := posOf(fun)
				if wantCalls[at] == nil {
					wantCalls[at] = map[string]bool{}
				}
				wantCalls[at][recv+"."+nm] = true
				got := findCall(at, recv, nm)
				if !subscripted {
					if got == nil {
						rep.add("missing call", where(x, text(x.Fun)))
					}
					return true
				}
				// An element of a value: f[x] is a value (not a type, as in a
				// conversion T[X](v)) and f is not a generic function.
				_, generic := info.Instances[base]
				index := false
				if tv, ok := info.Types[x.Fun]; ok && tv.IsValue() && !generic {
					if bt, ok := info.Types[fun]; ok && bt.Type != nil {
						sig, isSig := bt.Type.Underlying().(*types.Signature)
						index = !isSig || sig.TypeParams() == nil
					}
				}
				if generic {
					rep.generic++
					if got != nil && (got.ConfidenceCap != "" || got.TargetKinds == "") {
						rep.held++
					}
				}
				switch {
				case generic && got == nil:
					rep.add("missing generic call", where(x, text(x.Fun)))
				case index && got != nil && got.ConfidenceCap == "" && got.TargetKinds == "":
					// f[i](...) calls an element of a value, not f: the
					// reference must not be able to resolve to the value.
					rep.add("FALSE call (element of a value)", where(x, text(x.Fun)))
				}
			}
			return true
		})
		for at, rs := range calls {
			for _, r := range rs {
				if !wantCalls[at][r.ReceiverExpr+"."+r.Name] {
					rep.add("FALSE call (no such call)", fmt.Sprintf("%s:%d:%d %s.%s", name, at.line, at.col, r.ReceiverExpr, r.Name))
				}
			}
		}
		for at, cs := range constructions {
			for _, c := range cs {
				if !wantCons[at][c] {
					rep.add("FALSE construction", fmt.Sprintf("%s:%d:%d %s", name, at.line, at.col, c))
				}
			}
		}
	}
}

// checkTree compares every valid Go file under root, package by package.
func checkTree(t *testing.T, root string) *fidelityReport {
	t.Helper()
	dirs := map[string][]string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			dirs[filepath.Dir(p)] = append(dirs[filepath.Dir(p)], p)
		}
		return nil
	})
	rep := &fidelityReport{}
	keys := make([]string, 0, len(dirs))
	for k := range dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, dir := range keys {
		fset := token.NewFileSet()
		byPkg := map[string][]*ast.File{}
		srcs := map[*ast.File][]byte{}
		names := map[*ast.File]string{}
		for _, p := range dirs[dir] {
			src, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			f, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
			if err != nil {
				continue // not valid Go: nothing to compare with
			}
			byPkg[f.Name.Name] = append(byPkg[f.Name.Name], f)
			srcs[f], names[f] = src, p
		}
		pkgs := make([]string, 0, len(byPkg))
		for k := range byPkg {
			pkgs = append(pkgs, k)
		}
		sort.Strings(pkgs)
		for _, pkg := range pkgs {
			checkPackage(t, fset, byPkg[pkg], srcs, names, rep)
		}
	}
	return rep
}

func reportProblems(t *testing.T, root string, rep *fidelityReport) {
	t.Helper()
	kinds := make([]string, 0, len(rep.problems))
	for k := range rep.problems {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	t.Logf("%s: %d files compared; %d of %d generic calls of the package's own declarations capped or unrestricted", root, rep.checked, rep.held, rep.generic)
	for _, k := range kinds {
		ex := rep.problems[k]
		if len(ex) > 5 {
			ex = ex[:5]
		}
		t.Errorf("%s: %d %s, e.g. %v", root, len(rep.problems[k]), k, ex)
	}
}

// Every call and construction reference of the repository's own Go code is
// one go/ast sees, and every one go/ast sees is there.
func TestFidelity_RepositoryReferencesMatchGoAST(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	rep := checkTree(t, root)
	if rep.checked < 100 {
		t.Fatalf("only %d files compared: wrong root?", rep.checked)
	}
	reportProblems(t, root, rep)
}

// TestFidelity_ExternalReferences runs the same check on the directories
// ARK_GO_FIDELITY_ROOTS lists; it is skipped otherwise.
func TestFidelity_ExternalReferences(t *testing.T) {
	list := os.Getenv("ARK_GO_FIDELITY_ROOTS")
	if list == "" {
		t.Skip("ARK_GO_FIDELITY_ROOTS not set")
	}
	for _, root := range filepath.SplitList(list) {
		reportProblems(t, root, checkTree(t, root))
	}
}

func TestExtract_SubscriptedCallsFollowGoRules(t *testing.T) {
	src := []byte(`package p

import "lib"

var handlers []func(int)

func Make[T any](x int) T { var t T; return t }

type Set[T comparable] struct{}

type S struct{ hooks []func(int) }

func use(s S, fns []func(int), i int) {
	fns[i](1)
	fns[0](1)
	s.hooks[i](1)
	Make[int](1)
	lib.Make[int, string](1)
	_ = Set[int]{}
	_ = &lib.Box[int]{}
	handlers[i](1)
}

func more[T any]() {
	handlers[Mode](1)
	lib.Make[int](1)
	lib.Make[T](1)
	lib.Make[A, B](1)
	lib.Make[S](1)
	lib.Make[Q](1)
	lib.Handlers[Name](1)
	lib.Make[[]int](1)
}

const Name = "n"
`)
	ex, err := NewProvider().Extract(context.Background(), "p.go", src)
	if err != nil || len(ex.Diagnostics) > 0 {
		t.Fatal(err, ex.Diagnostics)
	}
	got := map[string]string{}
	for _, r := range ex.References {
		if r.Container != "use" && r.Container != "more" {
			continue
		}
		kinds := ""
		if r.TargetKinds != "" {
			kinds = "function/method/type only"
		}
		got[fmt.Sprintf("%s %s.%s L%d", r.Kind, r.ReceiverExpr, r.Name, r.Location.Range.Start.Line)] = r.ConfidenceCap + kinds
	}
	// fns[i](1), fns[0](1), s.hooks[i](1), handlers[i](1): an element of a
	// value (the index i is a parameter; handlers a var of the file) names no
	// declaration — no reference.
	want := map[string]string{
		// fns[i](1), fns[0](1): elements of a parameter — no reference.
		"call .Make L17":            "function/method/type only",
		"call lib.Make L18":         "function/method/type only",
		"construction .Set L19":     "",
		"construction .lib.Box L20": "",
		// handlers[Mode](1): handlers is a var of the file — no reference.
		"call lib.Make L26": "function/method/type only", // int: a predeclared type
		"call lib.Make L27": "function/method/type only", // T: a type parameter in scope
		"call lib.Make L28": "function/method/type only", // two subscripts: type arguments
		"call lib.Make L29": "function/method/type only", // S: a type of the file
		"call lib.Make L30": "function/method/type only", // Q: a type or a constant — decided by the target
		// lib.Handlers[Name](1): Name is a const of the file — an element.
		"call lib.Make L32": "function/method/type only", // []int: type syntax
	}
	for k, c := range want {
		if gc, ok := got[k]; !ok || gc != c {
			t.Errorf("want %q (cap %q), got %v", k, c, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected references: %v", got)
	}
}

// The parser derives this generic call as a call of an index expression (in
// this exact text; the shape depends on the context). Whether Node is a type
// or a constant indexing a package-level map of functions, the reference can
// denote only a generic function, method or type.
func TestExtract_GenericCallDerivedAsIndexExpression(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"package p\n\ntype Node struct{}\n\nfunc f() {\n\tfor v := range lib.Select[Node]() {\n\t\t_ = v\n\t}\n}\n", `call lib.Select function,method,type,struct,interface`},
		{"package p\n\nfunc f() {\n\tfor v := range lib.Select[Node]() {\n\t\t_ = v\n\t}\n}\n", `call lib.Select function,method,type,struct,interface`},
	} {
		ex, err := NewProvider().Extract(context.Background(), "p.go", []byte(c.src))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range ex.References {
			got = append(got, fmt.Sprintf("%s %s.%s %s", r.Kind, r.ReceiverExpr, r.Name, r.TargetKinds))
		}
		if !slices.Equal(got, []string{c.want}) {
			t.Errorf("got %v, want %v", got, c.want)
		}
	}
}
