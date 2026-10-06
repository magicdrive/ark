package resolver_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// R0 — authoritative qualified identity. These tests use hand-built FileIndexes
// so they pin the resolver contract independently of any provider.

func qiLoc(file string, line uint32) source.Location {
	return source.Location{File: source.FileID(file), Range: source.Range{
		Start: source.Position{Line: line, Column: 1}, End: source.Position{Line: line, Column: 2}}}
}

func qiBare(q string) string {
	if i := strings.LastIndexAny(q, `\.`); i >= 0 {
		return q[i+1:]
	}
	return q
}

// qiType declares a type-like symbol (qualified is its full identity).
func qiType(lang, file, qualified string, kind symbol.SymbolKind) symbol.Symbol {
	l := qiLoc(file, 1)
	return symbol.Symbol{
		ID: symbol.NewSymbolID(lang, file, kind, qualified), Name: qiBare(qualified),
		Qualified: qualified, Kind: kind, Language: lang, Location: l, Exported: true,
	}
}

// qiMember declares a member of the type whose Qualified is parent.
func qiMember(lang, file, parent, name string, kind symbol.SymbolKind) symbol.Symbol {
	q := parent + "." + name
	return symbol.Symbol{
		ID: symbol.NewSymbolID(lang, file, kind, q), Name: name, Qualified: q, Kind: kind,
		Language: lang, Location: qiLoc(file, 2), Parent: symbol.NewSymbolID(lang, file, symbol.KindUnknown, parent),
		ParentQualified: parent, Receiver: qiBare(parent), Exported: true,
	}
}

type qiRefSpec struct {
	lang, file, name, container, receiver string
	kind                                  reference.ReferenceKind
	nameQ, recvQ                          string
	recvType                              string
}

func qiRef(s qiRefSpec) reference.Reference {
	l := qiLoc(s.file, 10)
	return reference.Reference{
		ID:   reference.NewReferenceID(s.lang, l.File, s.kind, s.name, l),
		Name: s.name, Kind: s.kind, Language: s.lang, Location: l, Container: s.container,
		ReceiverExpr: s.receiver, ReceiverType: s.recvType,
		NameQualified: s.nameQ, ReceiverTypeQualified: s.recvQ,
		IsCall: s.kind == reference.KindCall,
	}
}

func qiFile(lang, path string, syms ...symbol.Symbol) resolver.FileIndex {
	return resolver.FileIndex{FileID: source.FileID(path), Language: lang, Symbols: syms}
}

func qiQualifieds(res resolver.Resolution) []string {
	var out []string
	for _, c := range res.Candidates {
		out = append(out, c.Qualified)
	}
	return out
}

func qiHasEvidence(res resolver.Resolution, k resolver.EvidenceKind) bool {
	for _, e := range res.Evidence {
		if e.Kind == k {
			return true
		}
	}
	return false
}

func TestQualifiedIdentityEvidenceKindName(t *testing.T) {
	if resolver.EvidenceQualifiedIdentity != "qualified_identity" {
		t.Fatalf("evidence kind = %q; the name is part of the CQ contract", resolver.EvidenceQualifiedIdentity)
	}
}

// One / none / several declarations of the identity.
func TestQualifiedIdentity_Cardinality(t *testing.T) {
	const id = `App\Services\Foo`
	user := qiFile("php", "app/Http/C.php")
	ref := func() reference.Reference {
		return qiRef(qiRefSpec{lang: "php", file: "app/Http/C.php", name: "Foo", container: `App\Http\C.a`,
			kind: reference.KindConstruction, nameQ: id})
	}

	t.Run("one declaration is Exact", func(t *testing.T) {
		r := resolver.New([]resolver.FileIndex{user, qiFile("php", "app/Services/Foo.php", qiType("php", "app/Services/Foo.php", id, symbol.KindClass))})
		res := r.ResolveReference(ref(), user)
		if res.Confidence != resolver.ConfidenceExact || !res.HasUniqueTarget() || !qiHasEvidence(res, resolver.EvidenceQualifiedIdentity) {
			t.Errorf("want unique Exact qualified_identity, got %v %v %v", res.Confidence, qiQualifieds(res), res.Evidence)
		}
	})
	t.Run("no declaration is Unresolved", func(t *testing.T) {
		r := resolver.New([]resolver.FileIndex{user})
		res := r.ResolveReference(ref(), user)
		if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 || res.HasUniqueTarget() {
			t.Errorf("want Unresolved, got %v %v", res.Confidence, qiQualifieds(res))
		}
		if !qiHasEvidence(res, resolver.EvidenceQualifiedIdentity) {
			t.Errorf("an authoritative miss must say why: %v", res.Evidence)
		}
	})
	t.Run("several declarations are Candidate", func(t *testing.T) {
		r := resolver.New([]resolver.FileIndex{user,
			qiFile("php", "app/B/Foo.php", qiType("php", "app/B/Foo.php", id, symbol.KindClass)),
			qiFile("php", "app/A/Foo.php", qiType("php", "app/A/Foo.php", id, symbol.KindClass)),
		})
		res := r.ResolveReference(ref(), user)
		if res.Confidence != resolver.ConfidenceCandidate || res.HasUniqueTarget() || len(res.Candidates) != 2 {
			t.Fatalf("want Candidate with 2 candidates, got %v %v", res.Confidence, qiQualifieds(res))
		}
		if res.Candidates[0].File != "app/A/Foo.php" || res.Candidates[1].File != "app/B/Foo.php" {
			t.Errorf("candidates not in deterministic (file) order: %+v", res.Candidates)
		}
	})
}

// A miss must not fall back to any repository-local similarity rule. Each
// decoy below is matched by exactly one legacy rule when the reference carries
// no identity (the control), and must NOT be matched when it does.
func TestQualifiedIdentity_NoFallbackToHeuristics(t *testing.T) {
	const external = `Vendor\Lib\Request`

	cases := []struct {
		name  string
		decoy symbol.Symbol
		file  string // file of the decoy
		from  string // file of the referencing code
		imps  bool   // referencing file imports the alias `Request`
		kind  reference.ReferenceKind
	}{
		{name: "same_file", decoy: qiType("php", "app/Http/C.php", `App\Models\Request`, symbol.KindClass), file: "app/Http/C.php", from: "app/Http/C.php", kind: reference.KindConstruction},
		{name: "same_package", decoy: qiType("php", "app/Http/Request.php", `App\Http\Request`, symbol.KindClass), file: "app/Http/Request.php", from: "app/Http/C.php", kind: reference.KindConstruction},
		{name: "unique_repo_match", decoy: qiType("php", "app/Models/Request.php", `App\Models\Request`, symbol.KindClass), file: "app/Models/Request.php", from: "app/Http/C.php", kind: reference.KindConstruction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := []resolver.FileIndex{qiFile("php", tc.from)}
			if tc.file == tc.from {
				files[0].Symbols = []symbol.Symbol{tc.decoy}
			} else {
				files = append(files, qiFile("php", tc.file, tc.decoy))
			}
			r := resolver.New(files)

			control := qiRef(qiRefSpec{lang: "php", file: tc.from, name: "Request", container: `App\Http\C.a`, kind: tc.kind})
			if got := r.ResolveReference(control, files[0]); got.Confidence < resolver.ConfidenceStrong {
				t.Fatalf("control: the legacy %s rule should match the decoy, got %v", tc.name, got.Confidence)
			}

			authoritative := control
			authoritative.NameQualified = external
			res := r.ResolveReference(authoritative, files[0])
			if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
				t.Errorf("external identity must stay Unresolved, got %v %v", res.Confidence, qiQualifieds(res))
			}
		})
	}

	t.Run("receiver_suffix", func(t *testing.T) {
		// `use Vendor\Package\Factory as Factory; Factory::make()` — a method
		// `make` on a type whose name merely ends in "factory".
		pdf := qiType("php", "app/Pdf/PdfFactory.php", `App\Pdf\PdfFactory`, symbol.KindClass)
		user := qiFile("php", "app/Http/C.php")
		// The alias import is what lets the legacy rules treat `Factory` as a
		// module receiver and reach the suffix match.
		user.Imports = []language.ImportDraft{{Path: `Vendor\Package\Factory`, Alias: "Factory"}}
		files := []resolver.FileIndex{
			user,
			qiFile("php", "app/Pdf/PdfFactory.php", pdf, qiMember("php", "app/Pdf/PdfFactory.php", `App\Pdf\PdfFactory`, "make", symbol.KindMethod)),
		}
		r := resolver.New(files)
		control := qiRef(qiRefSpec{lang: "php", file: "app/Http/C.php", name: "make", container: `App\Http\C.a`, kind: reference.KindCall, receiver: "Factory"})
		if got := r.ResolveReference(control, files[0]); got.Confidence < resolver.ConfidenceStrong {
			t.Fatalf("control: the legacy suffix rule should match, got %v", got.Confidence)
		}
		authoritative := control
		authoritative.ReceiverTypeQualified = `Vendor\Package\Factory`
		res := r.ResolveReference(authoritative, files[0])
		if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
			t.Errorf("external receiver type must stay Unresolved, got %v %v", res.Confidence, qiQualifieds(res))
		}
	})

	t.Run("explicit_import", func(t *testing.T) {
		// The legacy importMatch resolves an ImportDraft alias against file
		// paths. An identity-bearing reference never reaches it.
		req := qiType("php", "vendor_copy/Lib/Request.php", `Other\Request`, symbol.KindClass)
		from := qiFile("php", "app/Http/C.php")
		from.Imports = []language.ImportDraft{{Path: "vendor_copy/Lib", Alias: "Request"}}
		files := []resolver.FileIndex{from, qiFile("php", "vendor_copy/Lib/Request.php", req)}
		r := resolver.New(files)

		control := qiRef(qiRefSpec{lang: "php", file: "app/Http/C.php", name: "Request", container: `App\Http\C.a`, kind: reference.KindConstruction})
		if got := r.ResolveReference(control, from); got.Confidence != resolver.ConfidenceExact || !qiHasEvidence(got, resolver.EvidenceExplicitImport) {
			t.Fatalf("control: the legacy import rule should match, got %v %v", got.Confidence, got.Evidence)
		}
		authoritative := control
		authoritative.NameQualified = external
		if res := r.ResolveReference(authoritative, from); res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
			t.Errorf("got %v %v", res.Confidence, qiQualifieds(res))
		}
	})
}

// ReceiverTypeQualified identifies the type; the member is looked up on it.
func TestQualifiedIdentity_ReceiverTypeThenMember(t *testing.T) {
	const foo = `App\Services\Foo`
	declFoo := func(members ...string) resolver.FileIndex {
		syms := []symbol.Symbol{qiType("php", "app/Services/Foo.php", foo, symbol.KindClass)}
		for _, m := range members {
			syms = append(syms, qiMember("php", "app/Services/Foo.php", foo, m, symbol.KindMethod))
		}
		return qiFile("php", "app/Services/Foo.php", syms...)
	}
	call := func(member string) reference.Reference {
		return qiRef(qiRefSpec{lang: "php", file: "app/Http/C.php", name: member, container: `App\Http\C.a`,
			kind: reference.KindCall, receiver: "Bar", recvQ: foo})
	}
	user := qiFile("php", "app/Http/C.php")

	t.Run("member on the type is Exact", func(t *testing.T) {
		r := resolver.New([]resolver.FileIndex{user, declFoo("make")})
		res := r.ResolveReference(call("make"), user)
		if res.Confidence != resolver.ConfidenceExact || !res.HasUniqueTarget() || !qiHasEvidence(res, resolver.EvidenceQualifiedIdentity) {
			t.Errorf("got %v %v %v", res.Confidence, qiQualifieds(res), res.Evidence)
		}
		if got := qiQualifieds(res); !reflect.DeepEqual(got, []string{foo + ".make"}) {
			t.Errorf("target = %v", got)
		}
	})
	t.Run("member missing on the type is Unresolved, not a same-name member elsewhere", func(t *testing.T) {
		other := qiFile("php", "app/Other/Baz.php",
			qiType("php", "app/Other/Baz.php", `App\Other\Baz`, symbol.KindClass),
			qiMember("php", "app/Other/Baz.php", `App\Other\Baz`, "make", symbol.KindMethod))
		r := resolver.New([]resolver.FileIndex{user, declFoo(), other})
		res := r.ResolveReference(call("make"), user)
		if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
			t.Errorf("got %v %v", res.Confidence, qiQualifieds(res))
		}
	})
	t.Run("type declared twice is Candidate even when only one has the member", func(t *testing.T) {
		dup := qiFile("php", "app/Dup/Foo.php", qiType("php", "app/Dup/Foo.php", foo, symbol.KindClass))
		r := resolver.New([]resolver.FileIndex{user, declFoo("make"), dup})
		res := r.ResolveReference(call("make"), user)
		if res.HasUniqueTarget() || res.Confidence > resolver.ConfidenceCandidate {
			t.Errorf("duplicate type identity must not yield a unique edge: %v %v", res.Confidence, qiQualifieds(res))
		}
	})
	t.Run("external receiver type is Unresolved", func(t *testing.T) {
		r := resolver.New([]resolver.FileIndex{user, declFoo("make")})
		ref := call("make")
		ref.ReceiverTypeQualified = `Vendor\Foo`
		if res := r.ResolveReference(ref, user); res.Confidence != resolver.ConfidenceUnresolved {
			t.Errorf("got %v", res.Confidence)
		}
	})
}

// Identities are comparable within one language only, and only type
// declarations answer them.
func TestQualifiedIdentity_LanguageAndKindScope(t *testing.T) {
	const id = `App\Services\Foo`
	user := qiFile("php", "app/Http/C.php")
	ref := qiRef(qiRefSpec{lang: "php", file: "app/Http/C.php", name: "Foo", container: `App\Http\C.a`, kind: reference.KindConstruction, nameQ: id})

	t.Run("another language's symbol with the same qualified string is not a match", func(t *testing.T) {
		r := resolver.New([]resolver.FileIndex{user, qiFile("go", "svc/foo.go", qiType("go", "svc/foo.go", id, symbol.KindStruct))})
		if res := r.ResolveReference(ref, user); res.Confidence != resolver.ConfidenceUnresolved {
			t.Errorf("cross-language match: %v %v", res.Confidence, qiQualifieds(res))
		}
	})
	for _, kind := range []symbol.SymbolKind{symbol.KindNamespace, symbol.KindFunction, symbol.KindConstant, symbol.KindMethod} {
		t.Run(fmt.Sprintf("a %s is not a type declaration", kind), func(t *testing.T) {
			s := qiType("php", "app/Services/Foo.php", id, kind)
			r := resolver.New([]resolver.FileIndex{user, qiFile("php", "app/Services/Foo.php", s)})
			if res := r.ResolveReference(ref, user); res.Confidence != resolver.ConfidenceUnresolved {
				t.Errorf("kind %s matched: %v", kind, res.Confidence)
			}
		})
	}
	for _, kind := range []symbol.SymbolKind{symbol.KindClass, symbol.KindInterface, symbol.KindTrait, symbol.KindEnum, symbol.KindStruct} {
		t.Run(fmt.Sprintf("a %s is a type declaration", kind), func(t *testing.T) {
			s := qiType("php", "app/Services/Foo.php", id, kind)
			r := resolver.New([]resolver.FileIndex{user, qiFile("php", "app/Services/Foo.php", s)})
			if res := r.ResolveReference(ref, user); res.Confidence != resolver.ConfidenceExact {
				t.Errorf("kind %s: %v", kind, res.Confidence)
			}
		})
	}
}

// Without identity evidence the reference takes the existing rules, unchanged.
func TestQualifiedIdentity_AbsentEvidenceKeepsExistingPath(t *testing.T) {
	decl := qiFile("go", "pkg/a.go", qiType("go", "pkg/a.go", "Save", symbol.KindFunction))
	ref := qiRef(qiRefSpec{lang: "go", file: "pkg/a.go", name: "Save", kind: reference.KindCall})
	r := resolver.New([]resolver.FileIndex{decl})
	res := r.ResolveReference(ref, decl)
	if res.Confidence != resolver.ConfidenceExact || !qiHasEvidence(res, resolver.EvidenceSameFile) || qiHasEvidence(res, resolver.EvidenceQualifiedIdentity) {
		t.Errorf("got %v %v", res.Confidence, res.Evidence)
	}
}

// Candidate order and results do not depend on the order files were supplied.
func TestQualifiedIdentity_Deterministic(t *testing.T) {
	const id = `App\Services\Foo`
	user := qiFile("php", "app/Http/C.php")
	decls := []resolver.FileIndex{
		qiFile("php", "app/C/Foo.php", qiType("php", "app/C/Foo.php", id, symbol.KindClass)),
		qiFile("php", "app/A/Foo.php", qiType("php", "app/A/Foo.php", id, symbol.KindClass)),
		qiFile("php", "app/B/Foo.php", qiType("php", "app/B/Foo.php", id, symbol.KindClass)),
	}
	ref := qiRef(qiRefSpec{lang: "php", file: "app/Http/C.php", name: "Foo", container: `App\Http\C.a`, kind: reference.KindConstruction, nameQ: id})
	var want []string
	for i := 0; i < 20; i++ {
		files := append([]resolver.FileIndex{user}, decls...)
		files[1+i%3], files[1+(i+1)%3] = files[1+(i+1)%3], files[1+i%3]
		got := qiQualifieds(resolver.New(files).ResolveReference(ref, user))
		var fs []string
		for _, c := range resolver.New(files).ResolveReference(ref, user).Candidates {
			fs = append(fs, string(c.File))
		}
		got = append(got, fs...)
		if want == nil {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d differs:\n%v\n%v", i, got, want)
		}
	}
}

// BenchmarkQualifiedIdentityLookup measures the amortized cost of an identity
// lookup over a large repository: the index is built once, each lookup is a map
// access (it does not scan symbols).
func BenchmarkQualifiedIdentityLookup(b *testing.B) {
	const n = 20000
	files := make([]resolver.FileIndex, 0, n+1)
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("app/m%d/C%d.php", i%50, i)
		files = append(files, qiFile("php", p, qiType("php", p, fmt.Sprintf(`App\M%d\C%d`, i%50, i), symbol.KindClass)))
	}
	user := qiFile("php", "app/Http/C.php")
	files = append(files, user)
	r := resolver.New(files)
	refs := make([]reference.Reference, 1024)
	for i := range refs {
		k := (i * 7919) % n
		refs[i] = qiRef(qiRefSpec{lang: "php", file: "app/Http/C.php", name: fmt.Sprintf("C%d", k), container: `App\Http\C.a`,
			kind: reference.KindConstruction, nameQ: fmt.Sprintf(`App\M%d\C%d`, k%50, k)})
	}
	r.ResolveReference(refs[0], user) // build the index outside the timed loop
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r.ResolveReference(refs[i%len(refs)], user).Confidence != resolver.ConfidenceExact {
			b.Fatal("lookup missed")
		}
	}
}
