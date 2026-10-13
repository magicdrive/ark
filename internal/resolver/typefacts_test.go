package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Declared-type evaluation (resolver/typefacts.go) — receiver chains and
// promoted members — driven by synthetic, provider-independent evidence:
// language "x" is no real language; its files are package-scoped and state
// type facts the way a provider would.

// tfDecl is one declaration of a synthetic file: "struct S", "interface I",
// "type N", "func F", "method T.M", "var V"; fact, when set, is its type
// fact (Decl and position are filled in).
type tfDecl struct {
	decl string
	fact *language.TypeFact
}

func tfFile(path, pkg string, imports []language.ImportDraft, decls ...tfDecl) resolver.FileIndex {
	fi := resolver.FileIndex{FileID: source.FileID(path), Language: "x", Package: pkg, PackageScoped: true, Imports: imports}
	for i, d := range decls {
		var kindWord, name string
		for j := 0; j < len(d.decl); j++ {
			if d.decl[j] == ' ' {
				kindWord, name = d.decl[:j], d.decl[j+1:]
				break
			}
		}
		qualified, recv, parent := name, "", ""
		short := name
		kind := map[string]symbol.SymbolKind{
			"struct": symbol.KindStruct, "interface": symbol.KindInterface, "type": symbol.KindType,
			"func": symbol.KindFunction, "method": symbol.KindMethod, "imethod": symbol.KindMethod, "var": symbol.KindVariable,
		}[kindWord]
		if kindWord == "method" || kindWord == "imethod" {
			for j := 0; j < len(name); j++ {
				if name[j] == '.' {
					recv, short = name[:j], name[j+1:]
				}
			}
			if kindWord == "imethod" {
				parent = recv
			}
		}
		line := uint32(i + 1)
		fi.Symbols = append(fi.Symbols, symbol.Symbol{
			ID: symbol.SymbolID(path + "#" + name), Name: short, Qualified: qualified, Kind: kind, Language: "x",
			Receiver: recv, ParentQualified: parent, Exported: short[0] >= 'A' && short[0] <= 'Z',
			Location: source.Location{File: source.FileID(path), Range: source.Range{Start: source.Position{Line: line, Column: 1}}},
		})
		if d.fact != nil {
			f := *d.fact
			f.Decl, f.Line, f.Column = qualified, line, 1
			fi.TypeFacts = append(fi.TypeFacts, f)
		}
	}
	return fi
}

// tfRef resolves a call of name on a receiver in file from, with the given
// evidence.
func tfRef(t *testing.T, files []resolver.FileIndex, from, recv, name string, evidence func(*reference.Reference)) resolver.Resolution {
	t.Helper()
	r := resolver.NewInRoot(files, "repo")
	for _, fi := range files {
		if string(fi.FileID) == from {
			ref := reference.Reference{
				ID: reference.ReferenceID(from + "/" + recv + "." + name), Name: name, Kind: reference.KindCall, Language: "x",
				Location: source.Location{File: fi.FileID}, Container: "caller", ReceiverExpr: recv, IsCall: true,
			}
			evidence(&ref)
			return r.ResolveReference(ref, fi)
		}
	}
	t.Fatalf("no file %s", from)
	return resolver.Resolution{}
}

func typed(typ string) func(*reference.Reference) {
	return func(r *reference.Reference) { r.ReceiverType = typ }
}

func chained(chain string) func(*reference.Reference) {
	return func(r *reference.Reference) { r.ReceiverChain, r.ConfidenceCap = chain, "strong" }
}

func structOf(fields ...language.FieldFact) *language.TypeFact {
	return &language.TypeFact{Struct: true, Complete: true, Fields: fields}
}

func embed(t string) language.FieldFact       { return language.FieldFact{Type: t, Embedded: true} }
func field(name, t string) language.FieldFact { return language.FieldFact{Name: name, Type: t} }
func results(ts ...string) *language.TypeFact { return &language.TypeFact{Results: ts} }
func ifaceOf(embeds ...string) *language.TypeFact {
	f := &language.TypeFact{Interface: true, Complete: true}
	for _, e := range embeds {
		f.Fields = append(f.Fields, embed(e))
	}
	return f
}

// The shallowest depth that declares the name decides; deeper ones are
// hidden by it.
func TestTypeFacts_PromotionShallowestDepthWins(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"struct Outer", structOf(embed("Mid"))},
		tfDecl{"struct Mid", structOf(embed("Inner"))},
		tfDecl{"struct Inner", structOf()},
		tfDecl{"method Inner.Deep", nil},
		tfDecl{"method Inner.Both", nil},
		tfDecl{"method Mid.Both", nil},
	)}
	if got := target(tfRef(t, files, "p/a.go", "o", "Deep", typed("Outer"))); got != "p/a.go#Inner.Deep strong" {
		t.Errorf("depth 2: got %s", got)
	}
	if got := target(tfRef(t, files, "p/a.go", "o", "Both", typed("Outer"))); got != "p/a.go#Mid.Both strong" {
		t.Errorf("depth 1 hides depth 2: got %s", got)
	}
}

// Two members of one name at the decisive depth: ambiguous, never a target.
func TestTypeFacts_PromotionAmbiguousAtOneDepth(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"struct AB", structOf(embed("A"), embed("B"))},
		tfDecl{"struct A", structOf()},
		tfDecl{"struct B", structOf()},
		tfDecl{"method A.M", nil},
		tfDecl{"method B.M", nil},
	)}
	for _, ev := range []func(*reference.Reference){typed("AB"), chained("type:AB")} {
		res := tfRef(t, files, "p/a.go", "x", "M", ev)
		if res.HasUniqueTarget() || res.Confidence != resolver.ConfidenceCandidate || len(res.Candidates) != 2 {
			t.Errorf("want two candidates, got %s %+v", target(res), res.Candidates)
		}
	}
}

// A field of the name at a shallower depth hides a promoted method: no
// method is meant (a function-typed field called).
func TestTypeFacts_FieldHidesPromotedMethod(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"struct S", structOf(embed("Inner"), field("M", ""))},
		tfDecl{"struct Inner", structOf()},
		tfDecl{"method Inner.M", nil},
	)}
	for _, ev := range []func(*reference.Reference){typed("S"), chained("type:S")} {
		if res := tfRef(t, files, "p/a.go", "s", "M", ev); len(res.Candidates) != 0 {
			t.Errorf("a field hides the promoted method: got %s", target(res))
		}
	}
}

// An embedded type of unknown members (outside the repository) above the
// decisive depth stops the search; at that depth, it leaves the found
// method a Candidate.
func TestTypeFacts_UnknownParticipant(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", []language.ImportDraft{{Path: "sync"}},
		tfDecl{"struct Mixed", structOf(embed("sync.Mutex"), embed("Inner"))},
		tfDecl{"struct Deep", structOf(embed("sync.Mutex"), embed("Mid"))},
		tfDecl{"struct Mid", structOf(embed("Inner"))},
		tfDecl{"struct Inner", structOf()},
		tfDecl{"method Inner.Do", nil},
	)}
	if res := tfRef(t, files, "p/a.go", "m", "Do", typed("Mixed")); res.HasUniqueTarget() || len(res.Candidates) != 1 {
		t.Errorf("same depth as an unknown participant: want one Candidate, got %s", target(res))
	}
	if res := tfRef(t, files, "p/a.go", "d", "Do", typed("Deep")); len(res.Candidates) != 0 {
		t.Errorf("an unknown participant above: want no target, got %s", target(res))
	}
}

// A struct whose fields could not all be read has unknown members.
func TestTypeFacts_IncompleteStructIsUnknown(t *testing.T) {
	incomplete := structOf(embed("Inner"))
	incomplete.Complete = false
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"struct S", incomplete},
		tfDecl{"struct Inner", structOf()},
		tfDecl{"method Inner.M", nil},
	)}
	if res := tfRef(t, files, "p/a.go", "s", "M", typed("S")); len(res.Candidates) != 0 {
		t.Errorf("want no target, got %s", target(res))
	}
}

// A chain through declared results, fields and elements leads to the one
// declaration; at most Strong.
func TestTypeFacts_ChainThroughDeclarations(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"struct T", structOf(field("items", "[]*Item"), field("m", "map[string]Item"))},
		tfDecl{"struct Item", structOf()},
		tfDecl{"method Item.Run", nil},
		tfDecl{"method T.Next", results("*T", "error")},
		tfDecl{"func NewT", results("*T")},
		tfDecl{"type Items", &language.TypeFact{Underlying: "[]Item"}},
		tfDecl{"func AllItems", results("Items")},
		tfDecl{"var Default", &language.TypeFact{Chain: "decl:NewT|call:0"}},
	)}
	for chain, want := range map[string]string{
		"decl:NewT|call:0|member:items|elem":                "p/a.go#Item.Run strong",
		"decl:NewT|call:0|member:Next|call:0|member:m|elem": "p/a.go#Item.Run strong",
		"decl:AllItems|call:0|elem":                         "p/a.go#Item.Run strong",
		"decl:Default|member:items|elem":                    "p/a.go#Item.Run strong",
		"type:[]Item|elem":                                  "p/a.go#Item.Run strong",
	} {
		if got := target(tfRef(t, files, "p/a.go", "x", "Run", chained(chain))); got != want {
			t.Errorf("%s: got %s, want %s", chain, got, want)
		}
	}
	// The second result of Next is not stated as a type of the repository;
	// an out-of-range result names nothing.
	for _, chain := range []string{"decl:NewT|call:0|member:Next|call:1", "decl:NewT|call:3", "decl:NewT|call:0|member:missing|call:0"} {
		if res := tfRef(t, files, "p/a.go", "x", "Run", chained(chain)); res.Confidence > resolver.ConfidenceCandidate {
			t.Errorf("%s: want no Strong, got %s", chain, target(res))
		}
	}
}

// A step naming no single declaration proves nothing: the reference resolves
// as an untyped receiver (at most Candidate), never through the chain.
func TestTypeFacts_ChainStepWithoutSingleDeclaration(t *testing.T) {
	files := []resolver.FileIndex{
		tfFile("p/a.go", "p", []language.ImportDraft{{Path: "net/http"}},
			tfDecl{"struct T", structOf()},
			tfDecl{"method T.Run", nil},
			tfDecl{"func Twice", results("T")},
			tfDecl{"func Generic", results("")},
		),
		tfFile("p/b.go", "p", nil, tfDecl{"func Twice", results("T")}),
	}
	for _, chain := range []string{"decl:Twice|call:0", "decl:Generic|call:0", "decl:http.Get|call:0", "decl:Missing|call:0"} {
		res := tfRef(t, files, "p/a.go", "x", "Run", chained(chain))
		if res.Confidence > resolver.ConfidenceCandidate || res.OutsideRepository {
			t.Errorf("%s: want at most Candidate and no outside claim, got %s (outside %v)", chain, target(res), res.OutsideRepository)
		}
	}
}

// A chain through an import of a repository package uses the declaring
// file's own scope for the facts it reaches.
func TestTypeFacts_ChainAcrossPackages(t *testing.T) {
	files := []resolver.FileIndex{
		tfFile("app/main.go", "app", []language.ImportDraft{{Path: "example.com/repo/lib"}, {Path: "example.com/repo/util"}}),
		tfFile("lib/lib.go", "lib", []language.ImportDraft{{Path: "example.com/repo/util"}},
			tfDecl{"func Open", results("*util.Conn")},
		),
		tfFile("util/util.go", "util", nil,
			tfDecl{"struct Conn", structOf()},
			tfDecl{"method Conn.Close", nil},
		),
	}
	if got := target(tfRef(t, files, "app/main.go", "c", "Close", chained("decl:lib.Open|call:0"))); got != "util/util.go#Conn.Close strong" {
		t.Errorf("got %s", got)
	}
}

// An interface's members include the methods of the interfaces it embeds;
// an alias has the members of the type it names.
func TestTypeFacts_InterfaceEmbeddingAndAliases(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"interface Reader", ifaceOf()},
		tfDecl{"imethod Reader.Read", nil},
		tfDecl{"interface ReadCloser", ifaceOf("Reader")},
		tfDecl{"imethod ReadCloser.Close", nil},
		tfDecl{"struct T", structOf()},
		tfDecl{"method T.Run", nil},
		tfDecl{"type A", &language.TypeFact{Alias: "T"}},
	)}
	if got := target(tfRef(t, files, "p/a.go", "rc", "Read", typed("ReadCloser"))); got != "p/a.go#Reader.Read strong" {
		t.Errorf("embedded interface: got %s", got)
	}
	if got := target(tfRef(t, files, "p/a.go", "a", "Run", typed("A"))); got != "p/a.go#T.Run strong" {
		t.Errorf("alias: got %s", got)
	}
	if got := target(tfRef(t, files, "p/a.go", "rc", "Read", chained("type:ReadCloser"))); got != "p/a.go#Reader.Read strong" {
		t.Errorf("embedded interface through a chain: got %s", got)
	}
}

// A method expression selects a method of the type, never a field.
func TestTypeFacts_MethodExpressionSelectsMethodsOnly(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"struct T", structOf(field("f", "int"))},
		tfDecl{"method T.Run", nil},
	)}
	if got := target(tfRef(t, files, "p/a.go", "T", "Run", chained("decl:T"))); got != "p/a.go#T.Run strong" {
		t.Errorf("got %s", got)
	}
	if res := tfRef(t, files, "p/a.go", "T", "f", chained("decl:T")); len(res.Candidates) != 0 {
		t.Errorf("a field is no method expression: got %s", target(res))
	}
}

// A Speculative reference (a selector that may name a field) is kept only
// as what its evidence resolves to: without that, Unresolved without
// candidates — never a name match, never one of a type its package
// declares twice.
func TestTypeFacts_SpeculativeNeverFallsBack(t *testing.T) {
	files := []resolver.FileIndex{
		tfFile("p/a.go", "p", nil,
			tfDecl{"struct T", structOf(field("x", "int"))},
			tfDecl{"method Other.x", nil},
		),
		tfFile("q/one.go", "main", nil, tfDecl{"struct S", structOf(field("x", "int"))}),
		tfFile("q/two.go", "main", nil, tfDecl{"struct S", structOf()}, tfDecl{"method S.x", nil}),
	}
	spec := func(ev func(*reference.Reference)) func(*reference.Reference) {
		return func(r *reference.Reference) { ev(r); r.Speculative = true; r.Kind = reference.KindValueReference }
	}
	for _, tc := range []struct {
		from string
		ev   func(*reference.Reference)
	}{
		{"p/a.go", spec(chained("decl:Missing"))},
		{"p/a.go", spec(chained("type:T"))},
		{"q/one.go", spec(typed("S"))},
		{"q/one.go", spec(chained("type:S"))},
	} {
		if res := tfRef(t, files, tc.from, "v", "x", tc.ev); len(res.Candidates) != 0 {
			t.Errorf("%s: want no candidate, got %s %+v", tc.from, target(res), res.Candidates)
		}
	}
}

// Embedding cycles (through pointers) end; a missing member is no target.
func TestTypeFacts_CyclesEnd(t *testing.T) {
	files := []resolver.FileIndex{tfFile("p/a.go", "p", nil,
		tfDecl{"struct A", structOf(embed("*B"))},
		tfDecl{"struct B", structOf(embed("*A"))},
		tfDecl{"var Loop", &language.TypeFact{Chain: "decl:Loop"}},
	)}
	if res := tfRef(t, files, "p/a.go", "a", "Missing", typed("A")); len(res.Candidates) != 0 {
		t.Errorf("got %s", target(res))
	}
	if res := tfRef(t, files, "p/a.go", "x", "M", chained("decl:Loop")); res.Confidence > resolver.ConfidenceCandidate {
		t.Errorf("a self-referential variable proves nothing: got %s", target(res))
	}
}
