package rust

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

const fixtureRepo = "testdata/repo"

func buildIndex(t testing.TB, dir string) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), dir, []language.Provider{NewProvider()})
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

// edgeLines renders every forward edge as "from -kind-> to confidence".
func edgeLines(idx *index.RepositoryIndex) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			out = append(out, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
		}
	}
	sort.Strings(out)
	return out
}

// TestGraph_Fixture compares the fixture's whole graph with the expected
// one. Every edge is Exact. Absent on purpose:
//   - crate::which -> crate::platform: two #[cfg] alternatives (Candidate);
//   - anything through the package name (`app::open`, `use app::...`):
//     another crate, whose name is in Cargo.toml;
//   - method calls on values (`self.backend.put(..)`);
//   - Memory::default(): a derived impl, which no source declares.
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx), []string{
		"<crate::model::User as crate::model::Named> -implements-> crate::model::Named exact",
		"<crate::model::User as crate::model::Named> -uses_type-> crate::model::User exact",
		"<crate::model::User as {crate::model}fmt::Display> -uses_type-> crate::model::User exact",
		"<crate::storage::Repository as crate::storage::Store> -implements-> crate::storage::Store exact",
		"<crate::storage::Repository as crate::storage::Store> -uses_type-> crate::storage::Repository exact",
		"<crate::storage::Repository as crate::storage::Store>::save -calls-> crate::storage::Repository::save exact",
		"<crate::storage::Repository as crate::storage::Store>::save -uses_type-> crate::model::User exact",
		"crate::first_user -calls-> crate::model::User::new exact",
		"crate::first_user -uses_type-> crate::model::User exact",
		"crate::main -calls-> crate::run exact",
		"crate::model::Named::greet -calls-> crate::model::Named::name exact",
		"crate::model::User::new -calls-> crate::model::User exact",
		"crate::model::User::new -uses_type-> crate::model::User exact",
		"crate::model::User::role -calls-> crate::model::Role::Member exact",
		"crate::model::User::role -references-> crate::model::Role::Admin exact",
		"crate::model::User::role -uses_type-> crate::model::Role exact",
		"crate::model::repository -calls-> crate::storage::Repository::new exact", // through the re-export crate::Repo
		"crate::model::repository -uses_type-> crate::storage::Repository exact",
		"crate::open -calls-> crate::log_line! exact",
		"crate::open -calls-> crate::storage::Repository::new exact",
		"crate::open -uses_type-> crate::storage::Repository exact",
		"crate::run -calls-> crate::helper exact",
		"crate::storage::Repository -uses_type-> crate::storage::memory::Memory exact",
		"crate::storage::Repository::new -calls-> crate::storage::Repository exact",
		"crate::storage::Repository::new -uses_type-> crate::storage::Repository exact",
		"crate::storage::Repository::save -uses_type-> crate::model::User exact",
		"crate::storage::Store::save -uses_type-> crate::model::User exact",
		"crate::storage::memory::Memory -uses_type-> crate::model::User exact",
		"crate::storage::memory::Memory::put -uses_type-> crate::model::User exact",
		"crates/util/crate::helper -calls-> crates/util/crate::text::upper exact",
		"impl crate::model::User -uses_type-> crate::model::User exact",
		"impl crate::storage::Repository -uses_type-> crate::storage::Repository exact",
		"impl crate::storage::memory::Memory -uses_type-> crate::storage::memory::Memory exact",
		"src/bin/tool.rs/crate::main -calls-> src/bin/tool.rs/crate::helper exact",
		"tests/integration.rs/crate::opens -calls-> tests/integration.rs/crate::setup exact",
	})
}

// TestGraph_Soundness: every edge is Exact, and stays in its crate (the
// crate key prefix of both identities is the same).
func TestGraph_Soundness(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			if e.Confidence != resolver.ConfidenceExact {
				t.Errorf("%s -> %s: %s", s.Qualified, to.Qualified, e.Confidence)
			}
			if crateOf(s.Qualified) != crateOf(to.Qualified) {
				t.Errorf("%s -> %s crosses crates", s.Qualified, to.Qualified)
			}
		}
	}
}

// crateOf is an identity's crate key: its text before the first "::" or "}",
// past the "impl " / "<" / "{" an impl's or an unnamed type's identity
// starts with.
func crateOf(q string) string {
	q = strings.TrimPrefix(q, "impl ")
	q = strings.TrimLeft(q, "<{")
	if i := strings.IndexAny(q, ":}"); i >= 0 {
		q = q[:i]
	}
	return q
}

func TestCrateOf(t *testing.T) {
	for q, want := range map[string]string{
		"crate::a::f":                                   "crate",
		"crates/u/crate::a":                             "crates/u/crate",
		"impl crates/u/crate::T":                        "crates/u/crate",
		"<crate::T as crate::R>::m":                     "crate",
		"<{crate::body}&'static str as crate::body::B>": "crate",
		"{scripts/x.rs}::f":                             "scripts/x.rs",
		"<{crate}&'a str as crate::R>":                  "crate",
		"<{{tests/ui/x.rs}}S as {{tests/ui/x.rs}}T>::m": "tests/ui/x.rs",
	} {
		if got := crateOf(q); got != want {
			t.Errorf("crateOf(%q) = %q, want %q", q, got, want)
		}
	}
}

// TestGraph_CfgAlternativesAreCandidates: the call of a #[cfg]-duplicated
// function resolves to both declarations as Candidates, never one of them.
func TestGraph_CfgAlternativesAreCandidates(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	which := idx.FindSymbolsByQualified("crate::which")
	if len(which) != 1 {
		t.Fatalf("crate::which: %d", len(which))
	}
	if got := idx.GetCallees(which[0].ID); len(got) != 0 {
		t.Errorf("crate::which has edges %v", got)
	}
	if got := idx.CandidateCalleeSample(which[0].ID); got.Total != 2 {
		t.Errorf("crate::which -> platform is not two candidates: %+v", got)
	}
}

// TestAliasLookupAgreesWithResolution: the index's alias lookup (MCP target
// lookup of a re-exported name) names what resolver R0 resolves a reference
// with that identity to.
func TestAliasLookupAgreesWithResolution(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	targetOf := map[reference.ReferenceID]symbol.SymbolID{}
	for _, s := range idx.FindSymbols("") {
		for _, r := range idx.ReferencesByTarget(s.ID) {
			targetOf[r.ID] = s.ID
		}
	}
	checked := 0
	for _, f := range idx.Files() {
		for _, r := range idx.ReferencesByFile(f) {
			tgt, ok := targetOf[r.ID]
			if !ok || r.NameQualified == "" || len(idx.FindSymbolsByQualified(r.NameQualified)) > 0 {
				continue
			}
			checked++
			got := idx.SymbolsThroughAliases(r.NameQualified)
			if len(got) != 1 || got[0].ID != tgt {
				t.Errorf("%s: alias lookup %v, resolution %s", r.NameQualified, got, tgt)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no reference resolved through an alias")
	}
}
