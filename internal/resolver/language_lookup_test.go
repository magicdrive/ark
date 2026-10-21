package resolver_test

import (
	"fmt"
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Synthetic, provider-independent evidence for the language lookup
// (language.ReferenceDraft.Lookup / ReceiverLookup, SymbolDraft.Parameters /
// ImplicitMembers, Extraction.SourceRoot). The language name "lk" is
// arbitrary; every file is IdentityOnly, as the provider that states lookups
// makes them.

type lkFile struct {
	fi resolver.FileIndex
	n  map[string]int // ordinals per kind+qualified
}

func newLK(path, pkg, root string) *lkFile {
	return &lkFile{fi: resolver.FileIndex{FileID: source.FileID(path), Language: "lk", IdentityOnly: true, Package: pkg, SourceRoot: root}, n: map[string]int{}}
}

// decl adds a declaration; parent "" is a top-level one.
func (f *lkFile) decl(kind symbol.SymbolKind, qualified, parent string, mods ...func(*symbol.Symbol)) symbol.Symbol {
	key := string(kind) + "\x00" + qualified
	f.n[key]++
	name := qualified
	if parent != "" {
		name = qualified[len(parent)+1:]
	}
	s := symbol.Symbol{
		ID:              symbol.NewDeclarationID("lk", string(f.fi.FileID), kind, qualified, f.n[key]),
		Name:            name,
		Qualified:       qualified,
		Kind:            kind,
		Language:        "lk",
		Location:        source.Location{File: f.fi.FileID, Range: source.Range{Start: source.Position{Line: uint32(len(f.fi.Symbols) + 1)}}},
		ParentQualified: parent,
	}
	for _, m := range mods {
		m(&s)
	}
	f.fi.Symbols = append(f.fi.Symbols, s)
	return s
}

// extends states a supertype relation of type t, found by lookup.
func (f *lkFile) extends(t, lookup string) {
	f.fi.References = append(f.fi.References, reference.Reference{
		ID: reference.ReferenceID(fmt.Sprintf("%s#rel%d", f.fi.FileID, len(f.fi.References))), Name: "S", Kind: reference.KindInheritance,
		Language: "lk", Location: source.Location{File: f.fi.FileID}, Container: t, Lookup: lookup,
	})
}

func params(p string) func(*symbol.Symbol)     { return func(s *symbol.Symbol) { s.Parameters = p } }
func visibility(v string) func(*symbol.Symbol) { return func(s *symbol.Symbol) { s.Visibility = v } }
func implicit(m string) func(*symbol.Symbol)   { return func(s *symbol.Symbol) { s.ImplicitMembers = m } }

func lkCall(file, name, lookup string, args int) reference.Reference {
	return reference.Reference{
		ID: reference.ReferenceID(file + "#" + name + lookup), Name: name, Kind: reference.KindCall, Language: "lk",
		Location: source.Location{File: source.FileID(file)}, Lookup: lookup, CountsArguments: args >= 0, ArgumentCount: max(args, 0),
	}
}

func lkResolve(t *testing.T, files []*lkFile, from string, ref reference.Reference) resolver.Resolution {
	t.Helper()
	var fis []resolver.FileIndex
	for _, f := range files {
		fis = append(fis, f.fi)
	}
	return resolveIn(fis, from, ref)
}

func lkWantTarget(t *testing.T, res resolver.Resolution, conf resolver.Confidence, target symbol.Symbol) {
	t.Helper()
	if res.Confidence != conf || len(res.Candidates) != 1 || res.Candidates[0].SymbolID != target.ID {
		t.Errorf("want %s %s, got %s %v (%v)", conf, target.Qualified, res.Confidence, res.Candidates, res.Evidence)
	}
}

func lkWantNone(t *testing.T, res resolver.Resolution, conf resolver.Confidence, candidates int, outside bool) {
	t.Helper()
	if res.Confidence != conf || len(res.Candidates) != candidates || res.OutsideRepository != outside {
		t.Errorf("want %s with %d candidates (outside %v), got %s %v outside %v (%v)", conf, candidates, outside, res.Confidence, res.Candidates, res.OutsideRepository, res.Evidence)
	}
}

// An identity: the declaration is the target; none is outside the
// repository; one after a step the repository leaves open is Strong.
func TestLookup_IdentitySteps(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	cls := a.decl(symbol.KindClass, "p.A", "")
	use := newLK("src/q/U.lk", "q", "src")
	files := []*lkFile{a, use}

	ref := lkCall("src/q/U.lk", "A", "=T:p.A", -1)
	ref.Kind = reference.KindTypeUse
	lkWantTarget(t, lkResolve(t, files, "src/q/U.lk", ref), resolver.ConfidenceExact, cls)

	ref.Lookup = "=T:q.A;=T:p.A" // the package's own A, absent here, may be outside
	lkWantTarget(t, lkResolve(t, files, "src/q/U.lk", ref), resolver.ConfidenceStrong, cls)

	ref.Lookup = "=T:q.Missing;=T:r.Missing"
	lkWantNone(t, lkResolve(t, files, "src/q/U.lk", ref), resolver.ConfidenceUnresolved, 0, true)

	ref.Lookup = "=M:p.A" // a class is no method
	lkWantNone(t, lkResolve(t, files, "src/q/U.lk", ref), resolver.ConfidenceUnresolved, 0, true)

	ref.Lookup = "?;=T:p.A"
	lkWantTarget(t, lkResolve(t, files, "src/q/U.lk", ref), resolver.ConfidenceStrong, cls)

	for _, bad := range []string{"=T", "=X:p.A", "p.A", "@M:A", "@M:.m", "=T:p.A|"} {
		ref.Lookup = bad
		lkWantNone(t, lkResolve(t, files, "src/q/U.lk", ref), resolver.ConfidenceUnresolved, 0, false)
	}
}

// Overloads: the argument count selects; two that accept it are Candidates.
func TestLookup_ArityOverloads(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "")
	one := a.decl(symbol.KindMethod, "p.A.m", "p.A", params("(int)"))
	two := a.decl(symbol.KindMethod, "p.A.m", "p.A", params("(int,int)"))
	vararg := a.decl(symbol.KindMethod, "p.A.v", "p.A", params("(int,?String...)"))
	a.decl(symbol.KindMethod, "p.A.v", "p.A", params("(int)"))
	files := []*lkFile{a}

	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "m", "@M:p.A.m", 1)), resolver.ConfidenceExact, one)
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "m", "@M:p.A.m", 2)), resolver.ConfidenceExact, two)
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "m", "@M:p.A.m", 3)), resolver.ConfidenceUnresolved, 0, false)
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "m", "@M:p.A.m", -1)), resolver.ConfidenceCandidate, 2, false)
	// A variadic one accepts one argument less and any number more.
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "v", "@M:p.A.v", 3)), resolver.ConfidenceExact, vararg)
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "v", "@M:p.A.v", 1)), resolver.ConfidenceCandidate, 2, false)
}

// Overloads accumulate along supertypes: a subtype's method does not hide a
// supertype's of the name with other parameters; an override (equal
// Parameters) does.
func TestLookup_InheritedOverloadsAndOverrides(t *testing.T) {
	b := newLK("src/p/Base.lk", "p", "src")
	b.decl(symbol.KindClass, "p.Base", "")
	baseM := b.decl(symbol.KindMethod, "p.Base.m", "p.Base", params("(?String)"))
	baseN := b.decl(symbol.KindMethod, "p.Base.n", "p.Base", params("()"))
	baseO := b.decl(symbol.KindMethod, "p.Base.o", "p.Base", params("(?List)"))
	s := newLK("src/p/Sub.lk", "p", "src")
	s.decl(symbol.KindClass, "p.Sub", "")
	s.extends("p.Sub", "=T:p.Base")
	s.decl(symbol.KindMethod, "p.Sub.m", "p.Sub", params("(int)"))
	subN := s.decl(symbol.KindMethod, "p.Sub.n", "p.Sub", params("()"))
	subO := s.decl(symbol.KindMethod, "p.Sub.o", "p.Sub", params("(?List)"))
	files := []*lkFile{b, s}

	// m(int) and the inherited m(String): the argument's type decides.
	lkWantNone(t, lkResolve(t, files, "src/p/Sub.lk", lkCall("src/p/Sub.lk", "m", "@M:p.Sub.m", 1)), resolver.ConfidenceCandidate, 2, false)
	// No parameters: an override, proven.
	lkWantTarget(t, lkResolve(t, files, "src/p/Sub.lk", lkCall("src/p/Sub.lk", "n", "@M:p.Sub.n", 0)), resolver.ConfidenceExact, subN)
	// Parameters of types a file's imports decide: an override presumed.
	lkWantTarget(t, lkResolve(t, files, "src/p/Sub.lk", lkCall("src/p/Sub.lk", "o", "@M:p.Sub.o", 1)), resolver.ConfidenceStrong, subO)
	// From the base itself, its own.
	lkWantTarget(t, lkResolve(t, files, "src/p/Base.lk", lkCall("src/p/Base.lk", "m", "@M:p.Base.m", 1)), resolver.ConfidenceExact, baseM)
	lkWantTarget(t, lkResolve(t, files, "src/p/Base.lk", lkCall("src/p/Base.lk", "n", "@M:p.Base.n", 0)), resolver.ConfidenceExact, baseN)
	lkWantTarget(t, lkResolve(t, files, "src/p/Base.lk", lkCall("src/p/Base.lk", "o", "@M:p.Base.o", 1)), resolver.ConfidenceExact, baseO)
	// Without an argument count: both m.
	lkWantNone(t, lkResolve(t, files, "src/p/Sub.lk", lkCall("src/p/Sub.lk", "m", "@M:p.Sub.m", -1)), resolver.ConfidenceCandidate, 2, false)
}

// A field or a member type hides those of the supertypes; inherited from two
// supertypes, it is ambiguous.
func TestLookup_FieldsHide(t *testing.T) {
	b := newLK("src/p/B.lk", "p", "src")
	b.decl(symbol.KindClass, "p.B", "")
	bf := b.decl(symbol.KindField, "p.B.f", "p.B")
	i := newLK("src/p/I.lk", "p", "src")
	i.decl(symbol.KindInterface, "p.I", "")
	i.decl(symbol.KindField, "p.I.f", "p.I")
	s := newLK("src/p/S.lk", "p", "src")
	s.decl(symbol.KindClass, "p.S", "")
	s.extends("p.S", "=T:p.B")
	sf := s.decl(symbol.KindField, "p.S.f", "p.S")
	s.decl(symbol.KindClass, "p.T", "")
	s.extends("p.T", "=T:p.B")
	s.decl(symbol.KindClass, "p.U", "")
	s.extends("p.U", "=T:p.B")
	s.extends("p.U", "=T:p.I")
	files := []*lkFile{b, i, s}
	field := func(lookup string) reference.Reference {
		r := lkCall("src/p/S.lk", "f", lookup, -1)
		r.Kind = reference.KindValueReference
		return r
	}
	lkWantTarget(t, lkResolve(t, files, "src/p/S.lk", field("@V:p.S.f")), resolver.ConfidenceExact, sf)
	lkWantTarget(t, lkResolve(t, files, "src/p/S.lk", field("@V:p.T.f")), resolver.ConfidenceExact, bf)
	lkWantNone(t, lkResolve(t, files, "src/p/S.lk", field("@V:p.U.f")), resolver.ConfidenceCandidate, 2, false)
	// A method of the name is no field.
	lkWantNone(t, lkResolve(t, files, "src/p/S.lk", field("@M:p.T.f")), resolver.ConfidenceUnresolved, 0, false)
}

// A supertype outside the repository leaves its contribution unknown: what
// the type declares is Strong, and nothing found is outside.
func TestLookup_SupertypeOutside(t *testing.T) {
	s := newLK("src/p/S.lk", "p", "src")
	s.decl(symbol.KindClass, "p.S", "")
	s.extends("p.S", "=T:p.External;=T:lib.External")
	own := s.decl(symbol.KindMethod, "p.S.m", "p.S", params("()"))
	files := []*lkFile{s}
	lkWantTarget(t, lkResolve(t, files, "src/p/S.lk", lkCall("src/p/S.lk", "m", "@M:p.S.m", 0)), resolver.ConfidenceStrong, own)
	lkWantNone(t, lkResolve(t, files, "src/p/S.lk", lkCall("src/p/S.lk", "x", "@M:p.S.x", 0)), resolver.ConfidenceUnresolved, 0, true)
}

// Steps: the first scope that has the name decides, whatever the argument
// count; a closed scope without it is passed without trace, an open one
// bounds what follows at Strong.
func TestLookup_FirstScopeWithTheName(t *testing.T) {
	o := newLK("src/p/O.lk", "p", "src")
	o.decl(symbol.KindClass, "p.O", "")
	outerM := o.decl(symbol.KindMethod, "p.O.m", "p.O", params("()"))
	o.decl(symbol.KindClass, "p.O.In", "p.O")
	inM := o.decl(symbol.KindMethod, "p.O.In.m", "p.O.In", params("(int)"))
	o.decl(symbol.KindClass, "p.O.Ext", "p.O")
	o.extends("p.O.Ext", "=T:lib.X")
	files := []*lkFile{o}
	// In has an m: it is searched, and m(int) does not take no argument —
	// the outer m() is not reached.
	lkWantNone(t, lkResolve(t, files, "src/p/O.lk", lkCall("src/p/O.lk", "m", "@M:p.O.In.m;@M:p.O.m", 0)), resolver.ConfidenceUnresolved, 0, false)
	lkWantTarget(t, lkResolve(t, files, "src/p/O.lk", lkCall("src/p/O.lk", "m", "@M:p.O.In.m;@M:p.O.m", 1)), resolver.ConfidenceExact, inM)
	// A closed scope without m: passed.
	o.decl(symbol.KindClass, "p.O.Plain", "p.O")
	lkWantTarget(t, lkResolve(t, files, "src/p/O.lk", lkCall("src/p/O.lk", "m", "@M:p.O.Plain.m;@M:p.O.m", 0)), resolver.ConfidenceExact, outerM)
	// An open one (a supertype outside): Strong.
	lkWantTarget(t, lkResolve(t, files, "src/p/O.lk", lkCall("src/p/O.lk", "m", "@M:p.O.Ext.m;@M:p.O.m", 0)), resolver.ConfidenceStrong, outerM)
}

// A private member is not inherited, nor one without Visibility from another
// package; the search goes on past them.
func TestLookup_AccessLimitsInheritance(t *testing.T) {
	b := newLK("src/a/B.lk", "a", "src")
	b.decl(symbol.KindClass, "a.B", "")
	b.decl(symbol.KindMethod, "a.B.hidden", "a.B", params("()"), visibility("private"))
	b.decl(symbol.KindMethod, "a.B.pkg", "a.B", params("()"))
	pub := b.decl(symbol.KindMethod, "a.B.pub", "a.B", params("()"), visibility("public"))
	o := newLK("src/c/O.lk", "c", "src")
	o.decl(symbol.KindClass, "c.O", "")
	hidden := o.decl(symbol.KindMethod, "c.O.hidden", "c.O", params("()"))
	pkg := o.decl(symbol.KindMethod, "c.O.pkg", "c.O", params("()"))
	o.decl(symbol.KindClass, "c.O.In", "c.O")
	o.extends("c.O.In", "=T:a.B")
	files := []*lkFile{b, o}
	call := func(n string) reference.Reference { return lkCall("src/c/O.lk", n, "@M:c.O.In."+n+";@M:c.O."+n, 0) }
	lkWantTarget(t, lkResolve(t, files, "src/c/O.lk", call("hidden")), resolver.ConfidenceExact, hidden)
	lkWantTarget(t, lkResolve(t, files, "src/c/O.lk", call("pkg")), resolver.ConfidenceExact, pkg)
	lkWantTarget(t, lkResolve(t, files, "src/c/O.lk", call("pub")), resolver.ConfidenceExact, pub)
	// Within a.B's own package, a member without Visibility is inherited.
	same := newLK("src/a/S.lk", "a", "src")
	same.decl(symbol.KindClass, "a.S", "")
	same.extends("a.S", "=T:a.B")
	files = append(files, same)
	lkWantNone(t, lkResolve(t, files, "src/a/S.lk", lkCall("src/a/S.lk", "pkg", "@M:a.S.pkg", 0)), resolver.ConfidenceExact, 1, false)
}

// Members the language gives a type without a declaration: reached, they end
// the search and are no target; overridden, the declaration is.
func TestLookup_ImplicitMembers(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "", implicit("toString(),equals(?Object),wait(long,int),+values()"))
	own := a.decl(symbol.KindMethod, "p.A.toString", "p.A", params("()"))
	eq := a.decl(symbol.KindMethod, "p.A.equals", "p.A", params("(?A)"))
	a.decl(symbol.KindClass, "p.A.In", "p.A")
	inner := a.decl(symbol.KindMethod, "p.A.wait", "p.A", params("()"))
	files := []*lkFile{a}
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "toString", "@M:p.A.toString", 0)), resolver.ConfidenceExact, own)
	// equals(A) beside the implicit equals(Object): the argument decides.
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "equals", "@M:p.A.equals", 1)), resolver.ConfidenceCandidate, 1, false)
	_ = eq
	// wait(long,int) is implicit: no target, outside.
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "wait", "@M:p.A.wait", 2)), resolver.ConfidenceUnresolved, 0, true)
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "wait", "@M:p.A.wait", 0)), resolver.ConfidenceExact, inner)
	// values() is the type's own: no target, not outside; the outer scope is
	// not reached.
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "values", "@M:p.A.values;@M:p.Other.values", 0)), resolver.ConfidenceUnresolved, 0, false)
}

// A declaration of the reference's source root hides one of another root;
// one only another root holds is Strong, several are Candidates.
func TestLookup_SourceRoots(t *testing.T) {
	main := newLK("guava/src/p/A.lk", "p", "guava/src")
	inMain := main.decl(symbol.KindClass, "p.A", "")
	android := newLK("android/src/p/A.lk", "p", "android/src")
	inAndroid := android.decl(symbol.KindClass, "p.A", "")
	only := newLK("guava/src/p/B.lk", "p", "guava/src")
	b := only.decl(symbol.KindClass, "p.B", "")
	files := []*lkFile{main, android, only,
		newLK("guava/src/q/U.lk", "q", "guava/src"), newLK("android/src/q/U.lk", "q", "android/src"), newLK("tests/q/T.lk", "q", "tests")}
	use := func(from, lookup string) reference.Reference {
		r := lkCall(from, "A", lookup, -1)
		r.Kind = reference.KindTypeUse
		return r
	}
	lkWantTarget(t, lkResolve(t, files, "guava/src/q/U.lk", use("guava/src/q/U.lk", "=T:p.A")), resolver.ConfidenceExact, inMain)
	lkWantTarget(t, lkResolve(t, files, "android/src/q/U.lk", use("android/src/q/U.lk", "=T:p.A")), resolver.ConfidenceExact, inAndroid)
	lkWantNone(t, lkResolve(t, files, "tests/q/T.lk", use("tests/q/T.lk", "=T:p.A")), resolver.ConfidenceCandidate, 2, false)
	lkWantTarget(t, lkResolve(t, files, "tests/q/T.lk", use("tests/q/T.lk", "=T:p.B")), resolver.ConfidenceStrong, b)
	lkWantTarget(t, lkResolve(t, files, "android/src/q/U.lk", use("android/src/q/U.lk", "=T:p.B")), resolver.ConfidenceStrong, b)
}

// A receiver lookup finds the receiver's type, then its member; a value
// found instead types nothing.
func TestLookup_Receivers(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "")
	m := a.decl(symbol.KindMethod, "p.A.m", "p.A", params("()"))
	a.decl(symbol.KindField, "p.A.f", "p.A")
	files := []*lkFile{a}
	ref := lkCall("src/p/A.lk", "m", "", 0)
	ref.ReceiverExpr, ref.ReceiverLookup = "A", "=T:p.A"
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", ref), resolver.ConfidenceExact, m)
	ref.ReceiverLookup = "@V:p.A.f;=T:p.A"
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", ref), resolver.ConfidenceUnresolved, 0, false)
	ref.ReceiverLookup = "=T:lib.X"
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", ref), resolver.ConfidenceUnresolved, 0, true)
	ref.ReceiverLookup = "=T:q.X;=T:p.A"
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", ref), resolver.ConfidenceStrong, m)
}

// Cyclic supertypes end the search (open), never loop.
func TestLookup_CyclicSupertypes(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "")
	a.extends("p.A", "=T:p.B")
	a.decl(symbol.KindClass, "p.B", "")
	a.extends("p.B", "=T:p.A")
	files := []*lkFile{a}
	res := lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "m", "@M:p.A.m", 0))
	if len(res.Candidates) != 0 || res.Confidence != resolver.ConfidenceUnresolved {
		t.Errorf("cycle: %s %v", res.Confidence, res.Candidates)
	}
}

// Supertypes are identified by their own lookup, through the members of an
// enclosing type's supertypes.
func TestLookup_SupertypeThroughInheritedMemberType(t *testing.T) {
	b := newLK("src/p/Base.lk", "p", "src")
	b.decl(symbol.KindClass, "p.Base", "")
	b.decl(symbol.KindClass, "p.Base.Node", "p.Base")
	nodeM := b.decl(symbol.KindMethod, "p.Base.Node.m", "p.Base.Node", params("()"))
	o := newLK("src/p/O.lk", "p", "src")
	o.decl(symbol.KindClass, "p.O", "")
	o.extends("p.O", "=T:p.Base")
	o.decl(symbol.KindClass, "p.O.Leaf", "p.O")
	o.extends("p.O.Leaf", "@T:p.O.Node;=T:p.Node")
	files := []*lkFile{b, o}
	lkWantTarget(t, lkResolve(t, files, "src/p/O.lk", lkCall("src/p/O.lk", "m", "@M:p.O.Leaf.m", 0)), resolver.ConfidenceExact, nodeM)
}

// The same input resolves the same way, whatever the order of the files.
func TestLookup_Deterministic(t *testing.T) {
	b := newLK("src/p/Base.lk", "p", "src")
	b.decl(symbol.KindClass, "p.Base", "")
	b.decl(symbol.KindMethod, "p.Base.m", "p.Base", params("(?String)"))
	s := newLK("src/p/Sub.lk", "p", "src")
	s.decl(symbol.KindClass, "p.Sub", "")
	s.extends("p.Sub", "=T:p.Base")
	s.decl(symbol.KindMethod, "p.Sub.m", "p.Sub", params("(int)"))
	ref := lkCall("src/p/Sub.lk", "m", "@M:p.Sub.m", 1)
	first := fmt.Sprint(lkResolve(t, []*lkFile{b, s}, "src/p/Sub.lk", ref))
	for i := 0; i < 20; i++ {
		if got := fmt.Sprint(lkResolve(t, []*lkFile{s, b}, "src/p/Sub.lk", ref)); got != first {
			t.Fatalf("order changed the resolution:\n%s\n%s", first, got)
		}
	}
}

// A member path: the head, found, is a type whose members are followed; a
// value or a missing member on the way ends the search without a target,
// and the steps after it are not reached.
func TestLookup_MemberPaths(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "")
	in := a.decl(symbol.KindClass, "p.A.In", "p.A")
	a.decl(symbol.KindField, "p.A.F", "p.A")
	deep := a.decl(symbol.KindClass, "p.A.In.Deep", "p.A.In")
	other := newLK("src/q/In.lk", "q", "src")
	qIn := other.decl(symbol.KindClass, "q.In", "")
	files := []*lkFile{a, other}
	use := func(lookup string) reference.Reference {
		r := lkCall("src/p/A.lk", "X", lookup, -1)
		r.Kind = reference.KindTypeUse
		return r
	}
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", use("=T:p.A/In")), resolver.ConfidenceExact, in)
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", use("=T:p.A/In/Deep")), resolver.ConfidenceExact, deep)
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", use("=T:p.A~In/Deep")), resolver.ConfidenceExact, deep)
	// The head is a type: a member it lacks ends the search.
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", use("=T:p.A/Nope;=T:q.In")), resolver.ConfidenceUnresolved, 0, false)
	// A field on the way is a value.
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", use("=T:p.A~F/In;=T:q.In")), resolver.ConfidenceUnresolved, 0, false)
	// A head not found passes the step.
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", use("=T:p.B/In;=T:q.In")), resolver.ConfidenceStrong, qIn)
	// "!": a local variable — found, no target, nothing after it.
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", use("!;=T:q.In")), resolver.ConfidenceUnresolved, 0, false)
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", use("@T:p.A.Nope;!;=T:q.In")), resolver.ConfidenceUnresolved, 0, false)
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", use("@T:p.A.In;!")), resolver.ConfidenceExact, in)
	for _, bad := range []string{"=T:p.A/", "=T:p.A//In", "!/In"} {
		lkWantNone(t, lkResolve(t, files, "src/p/A.lk", use(bad)), resolver.ConfidenceUnresolved, 0, false)
	}
}

// A type whose declaration states a cap (members it cannot list): what is
// found in it is bounded, and what is found past it is a Candidate (it may
// hold the name), never outside the repository when nothing is.
func TestLookup_CappedType(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "", func(s *symbol.Symbol) { s.ConfidenceCap = "candidate" })
	a.decl(symbol.KindMethod, "p.A.m", "p.A", params("(int)"))
	o := newLK("src/p/O.lk", "p", "src")
	o.decl(symbol.KindClass, "p.O", "")
	om := o.decl(symbol.KindMethod, "p.O.x", "p.O", params("()"))
	files := []*lkFile{a, o}
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "m", "@M:p.A.m", 1)), resolver.ConfidenceCandidate, 1, false)
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "x", "@M:p.A.x;@M:p.O.x", 0)), resolver.ConfidenceCandidate, 1, false)
	_ = om
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "y", "@M:p.A.y", 0)), resolver.ConfidenceUnresolved, 0, false)
}

// A method stated to override a supertype's member, whose supertype is
// outside the repository: the outside member of the name, which may be the
// overload a call denotes, leaves it a Candidate.
func TestLookup_OverrideOfAnOutsideMember(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "")
	a.extends("p.A", "=T:lib.Scanner")
	a.decl(symbol.KindMethod, "p.A.scan", "p.A", params("(?Tree,?Void)"), func(s *symbol.Symbol) { s.Overrides = true })
	plain := a.decl(symbol.KindMethod, "p.A.own", "p.A", params("()"))
	files := []*lkFile{a}
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "scan", "@M:p.A.scan", 2)), resolver.ConfidenceCandidate, 1, false)
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "own", "@M:p.A.own", 0)), resolver.ConfidenceStrong, plain)
}

// A field a class declares hides its supertypes' of the name for its
// subtypes even where it is not inherited (private): the search does not go
// past it (JLS 8.3).
func TestLookup_PrivateFieldHides(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.Top", "")
	a.decl(symbol.KindField, "p.Top.F", "p.Top")
	a.decl(symbol.KindClass, "p.Mid", "")
	a.extends("p.Mid", "=T:p.Top")
	mf := a.decl(symbol.KindField, "p.Mid.F", "p.Mid", visibility("private"))
	a.decl(symbol.KindClass, "p.Mid.Low", "p.Mid")
	a.extends("p.Mid.Low", "=T:p.Mid")
	files := []*lkFile{a}
	field := lkCall("src/p/A.lk", "F", "@V:p.Mid.Low.F;@V:p.Mid.F", -1)
	field.Kind = reference.KindValueReference
	lkWantTarget(t, lkResolve(t, files, "src/p/A.lk", field), resolver.ConfidenceExact, mf)
}

// A method an interface declares, while the class's superclass is outside
// the repository: the superclass may implement it — a Candidate, not the
// interface's declaration.
func TestLookup_InterfaceMethodBesideAnOutsideSuperclass(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindInterface, "p.I", "")
	a.decl(symbol.KindMethod, "p.I.get", "p.I", params("()"))
	a.decl(symbol.KindClass, "p.C", "")
	a.extends("p.C", "=T:java.lang.ref.WeakReference")
	a.extends("p.C", "=T:p.I")
	files := []*lkFile{a}
	lkWantNone(t, lkResolve(t, files, "src/p/A.lk", lkCall("src/p/A.lk", "get", "@M:p.C.get", 0)), resolver.ConfidenceCandidate, 1, false)
}
