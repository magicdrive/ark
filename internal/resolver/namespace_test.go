package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Name spaces (FileIndex.NameSpace), driven by synthetic languages: "a" and
// its dialect "ad" share one name space; "b" has its own. A name-based stage
// never reaches a declaration of another name space — not as Exact, Strong
// or Candidate — and counts only its own name space's declarations.

type nsDecl struct {
	name, recv string
	kind       symbol.SymbolKind
}

func nsFile(path, lang, space string, decls ...nsDecl) resolver.FileIndex {
	fi := resolver.FileIndex{FileID: source.FileID(path), Language: lang, NameSpace: space}
	for _, d := range decls {
		q := d.name
		if d.recv != "" {
			q = d.recv + "." + d.name
		}
		fi.Symbols = append(fi.Symbols, symbol.Symbol{
			ID: symbol.SymbolID(path + "#" + q), Name: d.name, Qualified: q, Kind: d.kind, Language: lang,
			Receiver: d.recv, Location: source.Location{File: source.FileID(path)},
		})
	}
	return fi
}

func fn(name string) nsDecl           { return nsDecl{name: name, kind: symbol.KindFunction} }
func class(name string) nsDecl        { return nsDecl{name: name, kind: symbol.KindClass} }
func method(recv, name string) nsDecl { return nsDecl{name: name, recv: recv, kind: symbol.KindMethod} }

var nsRepo = []resolver.FileIndex{
	nsFile("app/main.a", "a", "a"),
	nsFile("app/same_dir.b", "b", "", fn("bSameDir")),                                 // same directory, other name space
	nsFile("far/only.b", "b", "", fn("bOnly"), class("Repo"), method("Repo", "find")), // unique in the repository
	nsFile("far/shared.b", "b", "", fn("shared")),
	nsFile("near/shared.a", "a", "a", fn("shared")),
	nsFile("ui/view.ad", "ad", "a", fn("dialectFn")), // dialect of a
	nsFile("lib/x.b", "b", "", fn("Imported")),
	nsFile("store/repo.a", "a", "a", class("Store"), method("Store", "save")),
	nsFile("store/repo.b", "b", "", method("Store", "save")), // attached by receiver name, same directory
}

func nsResolve(t *testing.T, files []resolver.FileIndex, from string, ref reference.Reference) resolver.Resolution {
	t.Helper()
	r := resolver.New(files)
	for _, fi := range files {
		if string(fi.FileID) == from {
			ref.ID = reference.ReferenceID(from + "/" + ref.ReceiverExpr + "." + ref.Name)
			ref.Location = source.Location{File: fi.FileID}
			ref.Language = fi.Language
			if ref.Kind == "" {
				ref.Kind = reference.KindCall
				ref.IsCall = true
			}
			return r.ResolveReference(ref, fi)
		}
	}
	t.Fatalf("no file %s", from)
	return resolver.Resolution{}
}

func TestNameSpace_NameStagesStayInTheirNameSpace(t *testing.T) {
	files := append([]resolver.FileIndex(nil), nsRepo...)
	// A legacy import naming the other name space's directory.
	files[0].Imports = []language.ImportDraft{{Path: "lib"}}
	for _, c := range []struct {
		name string
		ref  reference.Reference
		want string
	}{
		{"unique in the repository, other name space", reference.Reference{Name: "bOnly"}, "unresolved"},
		{"same directory, other name space", reference.Reference{Name: "bSameDir"}, "unresolved"},
		{"a namesake in another name space is not counted", reference.Reference{Name: "shared"}, "near/shared.a#shared strong"},
		{"a dialect shares the name space", reference.Reference{Name: "dialectFn"}, "ui/view.ad#dialectFn strong"},
		{"legacy import of the other name space's directory", reference.Reference{Name: "Imported", ReceiverExpr: "lib"}, "unresolved"},
		{"type receiver of the other name space", reference.Reference{Name: "find", ReceiverExpr: "Repo"}, "unresolved"},
		{"receiver-name match in the other name space", reference.Reference{Name: "find", ReceiverExpr: "repo"}, "unresolved"},
		{"suffix of a qualified name in the other name space", reference.Reference{Name: "Repo.find"}, "unresolved"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := nsResolve(t, files, "app/main.a", c.ref)
			if got := target(res); got != c.want {
				t.Errorf("got %q, want %q (candidates %v, evidence %v)", got, c.want, res.Candidates, res.Evidence)
			}
			for _, cand := range res.Candidates {
				if f := string(cand.File); f != "" && f[len(f)-2:] == ".b" {
					t.Errorf("candidate of another name space: %s", cand.SymbolID)
				}
			}
		})
	}
}

// A type's members attached by receiver name come from the type's own name
// space only, whichever stage found the type.
func TestNameSpace_MembersOfATypeStayInItsNameSpace(t *testing.T) {
	res := nsResolve(t, nsRepo, "app/main.a", reference.Reference{Name: "save", ReceiverExpr: "s", ReceiverType: "Store"})
	for _, c := range res.Candidates {
		if c.File == "store/repo.b" {
			t.Errorf("member of another name space: %s (%v)", c.SymbolID, res.Evidence)
		}
	}
}

// Without NameSpace a file's name space is its language.
func TestNameSpace_DefaultsToLanguage(t *testing.T) {
	files := []resolver.FileIndex{
		nsFile("p/a.x", "x", ""),
		nsFile("q/b.y", "y", "", fn("onlyY")),
		nsFile("r/c.x", "x", "", fn("onlyX")),
	}
	if got := target(nsResolve(t, files, "p/a.x", reference.Reference{Name: "onlyY"})); got != "unresolved" {
		t.Errorf("other language: %s", got)
	}
	if got := target(nsResolve(t, files, "p/a.x", reference.Reference{Name: "onlyX"})); got != "r/c.x#onlyX strong" {
		t.Errorf("own language: %s", got)
	}
}

// A package-scoped file's fallback for an untyped receiver (the
// repository-wide stages, capped at Candidate) sees only its name space.
func TestNameSpace_PackageScopedFallback(t *testing.T) {
	files := []resolver.FileIndex{
		{FileID: "app/main.go", Language: "x", Package: "app", PackageScoped: true},
		nsFile("web/greeter.b", "b", "", class("Greeter"), method("Greeter", "greet")),
	}
	res := nsResolve(t, files, "app/main.go", reference.Reference{Name: "greet", ReceiverExpr: "g"})
	if len(res.Candidates) != 0 {
		t.Errorf("untyped receiver reached another name space: %v", res.Candidates)
	}
}
