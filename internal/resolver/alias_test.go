package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Synthetic, provider-independent evidence for aliases (language.AliasDraft):
// an identity that names another one.

func aliasFixture(aliases ...language.AliasDraft) []resolver.FileIndex {
	return []resolver.FileIndex{
		{FileID: "src/a.cfg", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
			cfgSym("src/a.cfg", "f", "crate::a::f", symbol.KindFunction),
			cfgSym("src/a.cfg", "g", "crate::a::g", symbol.KindFunction),
			cfgSym("src/a.cfg", "T", "crate::a::T", symbol.KindStruct),
			cfgSym("src/a.cfg", "new", "crate::a::T::new", symbol.KindMethod),
		}},
		{FileID: "src/lib.cfg", Language: "cfg", IdentityOnly: true, Aliases: aliases},
		{FileID: "src/b.cfg", Language: "cfg", IdentityOnly: true},
	}
}

func resolveQualified(fis []resolver.FileIndex, q string) resolver.Resolution {
	ref := cfgRef("src/b.cfg", q)
	ref.NameQualified = q
	ref.IdentityInRepository = true
	return resolveIn(fis, "src/b.cfg", ref)
}

func TestAlias_ResolvesToTheTargetsDeclaration(t *testing.T) {
	fis := aliasFixture(
		language.AliasDraft{Qualified: "crate::f", Target: "crate::a::f"},
		language.AliasDraft{Qualified: "crate::F2", Target: "crate::f"},       // a chain
		language.AliasDraft{Qualified: "crate::m", Target: "crate::a"},        // a module
		language.AliasDraft{Qualified: "crate::Ty", Target: "crate::a::T"},    // a type, then its member
		language.AliasDraft{Qualified: "crate::nothing", Target: "crate::zz"}, // names nothing
	)
	want := map[string]string{"crate::f": "crate::a::f", "crate::F2": "crate::a::f", "crate::m::g": "crate::a::g", "crate::Ty::new": "crate::a::T::new"}
	for q, w := range want {
		res := resolveQualified(fis, q)
		if res.Confidence != resolver.ConfidenceExact {
			t.Errorf("%s: %s %v, want Exact", q, res.Confidence, res.Evidence)
			continue
		}
		var got string
		for _, f := range fis {
			for _, s := range f.Symbols {
				if s.ID == res.Candidates[0].SymbolID {
					got = s.Qualified
				}
			}
		}
		if got != w {
			t.Errorf("%s resolved to %s, want %s", q, got, w)
		}
	}
	if res := resolveQualified(fis, "crate::nothing"); res.Confidence != resolver.ConfidenceUnresolved || res.OutsideRepository {
		t.Errorf("an alias of nothing: %s outside=%v, want Unresolved in the repository", res.Confidence, res.OutsideRepository)
	}
}

// Two aliases of one identity (#[cfg] alternatives) leave both targets: a
// Candidate, never one of them.
func TestAlias_TwoTargetsAreCandidates(t *testing.T) {
	fis := aliasFixture(
		language.AliasDraft{Qualified: "crate::h", Target: "crate::a::f"},
		language.AliasDraft{Qualified: "crate::h", Target: "crate::a::g"},
	)
	if res := resolveQualified(fis, "crate::h"); res.Confidence != resolver.ConfidenceCandidate || len(res.Candidates) != 2 {
		t.Errorf("got %s with %d candidates, want 2 Candidates", res.Confidence, len(res.Candidates))
	}
}

// A cycle of aliases names nothing (and terminates).
func TestAlias_CycleNamesNothing(t *testing.T) {
	fis := aliasFixture(
		language.AliasDraft{Qualified: "crate::p", Target: "crate::q"},
		language.AliasDraft{Qualified: "crate::q", Target: "crate::p"},
		language.AliasDraft{Qualified: "crate::r", Target: "crate::r::x"},
	)
	for _, q := range []string{"crate::p", "crate::r::x", "crate::r"} {
		if res := resolveQualified(fis, q); res.Confidence != resolver.ConfidenceUnresolved {
			t.Errorf("%s: %s, want Unresolved", q, res.Confidence)
		}
	}
}

// A leading part of a path both declared and an alias can only be #[cfg]
// alternatives (a module and an import of one name do not compile
// together): an identity both readings give names both — a Candidate —
// and one only the declaration gives stays Exact.
func TestAlias_PrefixAliasAndDeclarationAreCandidates(t *testing.T) {
	fis := append(aliasFixture(
		language.AliasDraft{Qualified: "crate::a", Target: "crate::zz"},
	), resolver.FileIndex{FileID: "src/zz.cfg", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
		cfgSym("src/zz.cfg", "f", "crate::zz::f", symbol.KindFunction),
	}})
	if res := resolveQualified(fis, "crate::a::f"); res.Confidence != resolver.ConfidenceCandidate || len(res.Candidates) != 2 {
		t.Errorf("crate::a::f: %s with %d candidates, want 2 Candidates", res.Confidence, len(res.Candidates))
	}
	if res := resolveQualified(fis, "crate::a::g"); res.Confidence != resolver.ConfidenceExact {
		t.Errorf("crate::a::g: %s, want Exact (only the declaration names it)", res.Confidence)
	}
}

// Expansion visits each identity once: many aliases sharing targets cost
// what their identities do, not a product of chains.
func TestAlias_SharedTargetsExpandOnce(t *testing.T) {
	calls := 0
	aliases := map[string][]string{}
	for i := 0; i < 8; i++ {
		var next []string
		for j := 0; j < 8; j++ {
			next = append(next, "crate::l"+string(rune('0'+i+1))+"::"+string(rune('a'+j)))
		}
		for j := 0; j < 8; j++ {
			aliases["crate::l"+string(rune('0'+i))+"::"+string(rune('a'+j))] = next
		}
	}
	resolver.ExpandAliases("crate::l0::a",
		func(string) []symbol.Symbol { calls++; return nil },
		func(q string, _ bool) []string { return aliases[q] })
	if calls > 8*9+1 {
		t.Errorf("%d expansions; each identity at most once (%d)", calls, 8*9+1)
	}
}

// A declaration a short chain reaches is found although a longer chain
// reaches the same identity first in another order (found by review).
func TestAlias_ShortestChainWins(t *testing.T) {
	for _, first := range [][]string{{"L1", "S"}, {"S", "L1"}} { // whatever order the targets come in
		aliases := map[string][]string{"q0": first, "S": {"X"}, "X": {"Y"}}
		for i := 1; i < 7; i++ {
			aliases["L"+string(rune('0'+i))] = []string{"L" + string(rune('0'+i+1))}
		}
		aliases["L7"] = []string{"X"}
		decls := map[string][]symbol.Symbol{"Y": {cfgSym("src/y.cfg", "Y", "Y", symbol.KindFunction)}}
		got, _ := resolver.ExpandAliases("q0", func(q string) []symbol.Symbol { return decls[q] }, func(q string, _ bool) []string { return aliases[q] })
		if len(got) != 1 {
			t.Errorf("targets %v: q0 names %d declarations, want Y (q0 → S → X → Y)", first, len(got))
		}
	}
}

// Two leading parts of a path both aliases (#[cfg] alternatives at two
// levels) give two readings: both are followed.
func TestAlias_EveryAliasedPrefixIsARead(t *testing.T) {
	aliases := map[string][]string{"crate::a": {"crate::p"}, "crate::a::b": {"crate::q"}}
	decls := map[string][]symbol.Symbol{
		"crate::p::b::c": {cfgSym("src/p.cfg", "c", "crate::p::b::c", symbol.KindFunction)},
		"crate::q::c":    {cfgSym("src/q.cfg", "c", "crate::q::c", symbol.KindFunction)},
	}
	got, _ := resolver.ExpandAliases("crate::a::b::c", func(q string) []symbol.Symbol { return decls[q] }, func(q string, _ bool) []string { return aliases[q] })
	if len(got) != 2 {
		t.Errorf("crate::a::b::c names %d declarations, want both readings", len(got))
	}
}

// An alias stated for the members below a declaration (AliasDraft.Members:
// a constant whose value another identity names) reads only those members:
// the identity itself names its own declaration alone, never the target's,
// and a member path below it reaches the target's member.
func TestAlias_MembersReadOnlyBelowTheDeclaration(t *testing.T) {
	fis := aliasFixture(language.AliasDraft{Qualified: "crate::v", Target: "crate::a::T", Members: true})
	fis[1].Symbols = []symbol.Symbol{cfgSym("src/lib.cfg", "v", "crate::v", symbol.KindConstant)}
	for q, want := range map[string]string{"crate::v": "crate::v", "crate::v::new": "crate::a::T::new"} {
		res := resolveQualified(fis, q)
		if res.Confidence != resolver.ConfidenceExact || len(res.Candidates) != 1 {
			t.Errorf("%s: %s with %d candidates, want Exact", q, res.Confidence, len(res.Candidates))
			continue
		}
		for _, f := range fis {
			for _, s := range f.Symbols {
				if s.ID == res.Candidates[0].SymbolID && s.Qualified != want {
					t.Errorf("%s resolved to %s, want %s", q, s.Qualified, want)
				}
			}
		}
	}
	// Without the flag the identity itself is read through the alias too:
	// its declaration and the target are both candidates.
	fis = aliasFixture(language.AliasDraft{Qualified: "crate::v", Target: "crate::a::T"})
	fis[1].Symbols = []symbol.Symbol{cfgSym("src/lib.cfg", "v", "crate::v", symbol.KindConstant)}
	if res := resolveQualified(fis, "crate::v"); res.Confidence != resolver.ConfidenceCandidate || len(res.Candidates) != 2 {
		t.Errorf("crate::v through a plain alias: %s with %d candidates, want 2 Candidates", res.Confidence, len(res.Candidates))
	}
	// Two member aliases of one declaration (alternatives a condition picks
	// between) leave a member path ambiguous.
	fis = append(aliasFixture(
		language.AliasDraft{Qualified: "crate::v", Target: "crate::a::T", Members: true},
		language.AliasDraft{Qualified: "crate::v", Target: "crate::zz::T", Members: true},
	), resolver.FileIndex{FileID: "src/zz.cfg", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
		cfgSym("src/zz.cfg", "new", "crate::zz::T::new", symbol.KindMethod),
	}})
	if res := resolveQualified(fis, "crate::v::new"); res.Confidence != resolver.ConfidenceCandidate || len(res.Candidates) != 2 {
		t.Errorf("crate::v::new through two member aliases: %s with %d candidates, want 2 Candidates", res.Confidence, len(res.Candidates))
	}
}

// An alias stating a cap (a module a build script maps) bounds every
// resolution that follows it — directly, below it, or through another
// alias that leads to it — and never raises one.
func TestAlias_CapBoundsWhatFollowsIt(t *testing.T) {
	fis := aliasFixture(
		language.AliasDraft{Qualified: "{module}a", Target: "crate::a", ConfidenceCap: "strong"},
		language.AliasDraft{Qualified: "crate::m", Target: "{module}a", Members: true},
		language.AliasDraft{Qualified: "crate::two", Target: "crate::a::f", ConfidenceCap: "strong"},
		language.AliasDraft{Qualified: "crate::two", Target: "crate::a::g"},
	)
	for _, q := range []string{"{module}a::f", "crate::m::f", "{module}a::T::new"} {
		if res := resolveQualified(fis, q); res.Confidence != resolver.ConfidenceStrong || len(res.Candidates) != 1 {
			t.Errorf("%s: %s with %d candidates, want Strong", q, res.Confidence, len(res.Candidates))
		}
	}
	if res := resolveQualified(fis, "crate::a::f"); res.Confidence != resolver.ConfidenceExact {
		t.Errorf("crate::a::f follows no alias: %s, want Exact", res.Confidence)
	}
	if res := resolveQualified(fis, "crate::two"); res.Confidence != resolver.ConfidenceCandidate {
		t.Errorf("two targets: %s, want Candidate (a cap never raises)", res.Confidence)
	}
}

// A tentative identity no declaration carries is Unresolved, and claims
// nothing: not outside the repository.
func TestTentativeIdentity_MissIsUnresolvedNotOutside(t *testing.T) {
	fis := aliasFixture()
	ref := cfgRef("src/b.cfg", "zz")
	ref.NameQualified = "crate::zz"
	ref.TentativeIdentity = true
	if res := resolveIn(fis, "src/b.cfg", ref); res.Confidence != resolver.ConfidenceUnresolved || res.OutsideRepository {
		t.Errorf("got %s outside=%v, want Unresolved, not outside", res.Confidence, res.OutsideRepository)
	}
	ref.NameQualified = "crate::a::f"
	if res := resolveIn(fis, "src/b.cfg", ref); res.Confidence != resolver.ConfidenceExact {
		t.Errorf("a declared tentative identity: %s, want Exact", res.Confidence)
	}
	ref.NameQualified, ref.TentativeIdentity = "crate::zz", false
	if res := resolveIn(fis, "src/b.cfg", ref); !res.OutsideRepository {
		t.Error("without the qualifier a missing identity is outside the repository (unchanged rule)")
	}
}
