package toml

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
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

// edgeLines renders every forward edge as "from -kind-> to confidence",
// except the contains edges of the TOML layer when structural is false.
func edgeLines(idx *index.RepositoryIndex, structural bool) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			if !structural && e.Kind == index.EdgeContains && to.Location.File == s.Location.File {
				continue
			}
			out = append(out, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
		}
	}
	sort.Strings(out)
	return out
}

// unresolvedLines renders every outgoing reference that resolves to no
// candidate as "from: name kind reason".
func unresolvedLines(idx *index.RepositoryIndex) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, r := range idx.UnresolvedOutgoing(s.ID).References {
			out = append(out, fmt.Sprintf("%s: %s %s %s", s.Qualified, r.Name, r.Kind, r.Reason))
		}
	}
	sort.Strings(out)
	return out
}

func equalLines(t testing.TB, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestGraph_Fixture compares the fixture's whole graph with the expected
// one. Structure: contains from a file to its top tables and keys, from a
// table to its entries, from an array of tables to its elements (named by
// their name key, else "#<n>"), from a requirement array to its
// requirements. Meaning: a workspace contains its literal members'
// packages; a package depends_on its dependencies (normal, dev, build,
// target); a path dependency depends_on the package of the manifest in its
// directory; workspace = true depends_on the workspace's entry where the
// root is known (the manifest itself or package.workspace); a feature
// references the features and dependencies it enables. Absent on purpose
// (TestGraph_NonEdgesAreAccountedFor): registries, git, PyPI, a path or a
// member leaving the repository, a glob member, a workspace root one file
// cannot see, a feature naming nothing, and dep:libc — two dependencies
// named libc (a Candidate).
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx, true), []string{
		"Cargo.toml -contains-> Cargo.toml#/workspace exact",
		"Cargo.toml#/workspace -contains-> Cargo.toml#/workspace/dependencies exact",
		"Cargo.toml#/workspace -contains-> Cargo.toml#/workspace/exclude exact",
		"Cargo.toml#/workspace -contains-> Cargo.toml#/workspace/members exact",
		"Cargo.toml#/workspace -contains-> Cargo.toml#/workspace/resolver exact",
		"Cargo.toml#/workspace -contains-> crates/app/Cargo.toml#/package exact",
		"Cargo.toml#/workspace -contains-> crates/core/Cargo.toml#/package exact",
		"Cargo.toml#/workspace -contains-> crates/inherit/Cargo.toml#/package exact",
		"Cargo.toml#/workspace -contains-> crates/tool/Cargo.toml#/package exact",
		"Cargo.toml#/workspace/dependencies -contains-> Cargo.toml#/workspace/dependencies/core-lib exact",
		"Cargo.toml#/workspace/dependencies -contains-> Cargo.toml#/workspace/dependencies/serde exact",
		"Cargo.toml#/workspace/dependencies/core-lib -contains-> Cargo.toml#/workspace/dependencies/core-lib/path exact",
		"Cargo.toml#/workspace/dependencies/core-lib -depends_on-> crates/core/Cargo.toml#/package exact",
		"Cargo.toml#/workspace/dependencies/serde -contains-> Cargo.toml#/workspace/dependencies/serde/features exact",
		"Cargo.toml#/workspace/dependencies/serde -contains-> Cargo.toml#/workspace/dependencies/serde/version exact",
		"config/app.toml -contains-> config/app.toml#/database exact",
		"config/app.toml -contains-> config/app.toml#/owner exact",
		"config/app.toml -contains-> config/app.toml#/products exact",
		"config/app.toml -contains-> config/app.toml#/servers exact",
		"config/app.toml -contains-> config/app.toml#/text exact",
		"config/app.toml -contains-> config/app.toml#/title exact",
		"config/app.toml#/database -contains-> config/app.toml#/database/enabled exact",
		"config/app.toml#/database -contains-> config/app.toml#/database/ports exact",
		"config/app.toml#/database -contains-> config/app.toml#/database/temp_targets exact",
		"config/app.toml#/database/temp_targets -contains-> config/app.toml#/database/temp_targets/case exact",
		"config/app.toml#/database/temp_targets -contains-> config/app.toml#/database/temp_targets/cpu exact",
		"config/app.toml#/owner -contains-> config/app.toml#/owner/dob exact",
		"config/app.toml#/owner -contains-> config/app.toml#/owner/name exact",
		"config/app.toml#/products -contains-> config/app.toml#/products/#1 exact",
		"config/app.toml#/products -contains-> config/app.toml#/products/Hammer exact",
		"config/app.toml#/products -contains-> config/app.toml#/products/Nail exact",
		"config/app.toml#/products/Hammer -contains-> config/app.toml#/products/Hammer/name exact",
		"config/app.toml#/products/Hammer -contains-> config/app.toml#/products/Hammer/sku exact",
		"config/app.toml#/products/Nail -contains-> config/app.toml#/products/Nail/color exact",
		"config/app.toml#/products/Nail -contains-> config/app.toml#/products/Nail/name exact",
		"config/app.toml#/servers -contains-> config/app.toml#/servers/alpha exact",
		"config/app.toml#/servers -contains-> config/app.toml#/servers/beta~1gamma exact",
		"config/app.toml#/servers/alpha -contains-> config/app.toml#/servers/alpha/ip exact",
		"config/app.toml#/servers/beta~1gamma -contains-> config/app.toml#/servers/beta~1gamma/ip exact",
		"config/app.toml#/text -contains-> config/app.toml#/text/poem exact",
		"config/app.toml#/text -contains-> config/app.toml#/text/site exact",
		"config/app.toml#/text/site -contains-> config/app.toml#/text/site/url exact",
		"config/broken.toml -contains-> config/broken.toml#/a exact",
		"crates/app/Cargo.toml -contains-> crates/app/Cargo.toml#/bin exact",
		"crates/app/Cargo.toml -contains-> crates/app/Cargo.toml#/build-dependencies exact",
		"crates/app/Cargo.toml -contains-> crates/app/Cargo.toml#/dependencies exact",
		"crates/app/Cargo.toml -contains-> crates/app/Cargo.toml#/dev-dependencies exact",
		"crates/app/Cargo.toml -contains-> crates/app/Cargo.toml#/features exact",
		"crates/app/Cargo.toml -contains-> crates/app/Cargo.toml#/package exact",
		"crates/app/Cargo.toml -contains-> crates/app/Cargo.toml#/target exact",
		"crates/app/Cargo.toml#/bin -contains-> crates/app/Cargo.toml#/bin/#1 exact",
		"crates/app/Cargo.toml#/bin -contains-> crates/app/Cargo.toml#/bin/app-cli exact",
		"crates/app/Cargo.toml#/bin/#1 -contains-> crates/app/Cargo.toml#/bin/#1/path exact",
		"crates/app/Cargo.toml#/bin/app-cli -contains-> crates/app/Cargo.toml#/bin/app-cli/name exact",
		"crates/app/Cargo.toml#/bin/app-cli -contains-> crates/app/Cargo.toml#/bin/app-cli/path exact",
		"crates/app/Cargo.toml#/build-dependencies -contains-> crates/app/Cargo.toml#/build-dependencies/cc exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/core exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/json exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/libc exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/log exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/outside exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/private exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/regex exact",
		"crates/app/Cargo.toml#/dependencies -contains-> crates/app/Cargo.toml#/dependencies/serde exact",
		"crates/app/Cargo.toml#/dependencies/core -contains-> crates/app/Cargo.toml#/dependencies/core/package exact",
		"crates/app/Cargo.toml#/dependencies/core -contains-> crates/app/Cargo.toml#/dependencies/core/path exact",
		"crates/app/Cargo.toml#/dependencies/core -depends_on-> crates/core/Cargo.toml#/package exact",
		"crates/app/Cargo.toml#/dependencies/json -contains-> crates/app/Cargo.toml#/dependencies/json/package exact",
		"crates/app/Cargo.toml#/dependencies/json -contains-> crates/app/Cargo.toml#/dependencies/json/version exact",
		"crates/app/Cargo.toml#/dependencies/libc -contains-> crates/app/Cargo.toml#/dependencies/libc/optional exact",
		"crates/app/Cargo.toml#/dependencies/libc -contains-> crates/app/Cargo.toml#/dependencies/libc/version exact",
		"crates/app/Cargo.toml#/dependencies/log -contains-> crates/app/Cargo.toml#/dependencies/log/registry exact",
		"crates/app/Cargo.toml#/dependencies/log -contains-> crates/app/Cargo.toml#/dependencies/log/version exact",
		"crates/app/Cargo.toml#/dependencies/outside -contains-> crates/app/Cargo.toml#/dependencies/outside/path exact",
		"crates/app/Cargo.toml#/dependencies/private -contains-> crates/app/Cargo.toml#/dependencies/private/branch exact",
		"crates/app/Cargo.toml#/dependencies/private -contains-> crates/app/Cargo.toml#/dependencies/private/git exact",
		"crates/app/Cargo.toml#/dependencies/serde -contains-> crates/app/Cargo.toml#/dependencies/serde/workspace exact",
		"crates/app/Cargo.toml#/dev-dependencies -contains-> crates/app/Cargo.toml#/dev-dependencies/tempfile exact",
		"crates/app/Cargo.toml#/features -contains-> crates/app/Cargo.toml#/features/default exact",
		"crates/app/Cargo.toml#/features -contains-> crates/app/Cargo.toml#/features/fast exact",
		"crates/app/Cargo.toml#/features -contains-> crates/app/Cargo.toml#/features/std exact",
		"crates/app/Cargo.toml#/features/default -references-> crates/app/Cargo.toml#/features/std exact",
		"crates/app/Cargo.toml#/features/std -references-> crates/app/Cargo.toml#/dependencies/regex exact",
		"crates/app/Cargo.toml#/features/std -references-> crates/app/Cargo.toml#/dependencies/serde exact",
		"crates/app/Cargo.toml#/package -contains-> crates/app/Cargo.toml#/package/edition exact",
		"crates/app/Cargo.toml#/package -contains-> crates/app/Cargo.toml#/package/name exact",
		"crates/app/Cargo.toml#/package -contains-> crates/app/Cargo.toml#/package/version exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/build-dependencies/cc exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/core exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/json exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/libc exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/log exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/outside exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/private exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/regex exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dependencies/serde exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/dev-dependencies/tempfile exact",
		"crates/app/Cargo.toml#/package -depends_on-> crates/app/Cargo.toml#/target/cfg(unix)/dependencies/libc exact",
		"crates/app/Cargo.toml#/target -contains-> crates/app/Cargo.toml#/target/cfg(unix) exact",
		"crates/app/Cargo.toml#/target/cfg(unix) -contains-> crates/app/Cargo.toml#/target/cfg(unix)/dependencies exact",
		"crates/app/Cargo.toml#/target/cfg(unix)/dependencies -contains-> crates/app/Cargo.toml#/target/cfg(unix)/dependencies/libc exact",
		"crates/core/Cargo.toml -contains-> crates/core/Cargo.toml#/package exact",
		"crates/core/Cargo.toml#/package -contains-> crates/core/Cargo.toml#/package/name exact",
		"crates/core/Cargo.toml#/package -contains-> crates/core/Cargo.toml#/package/version exact",
		"crates/inherit/Cargo.toml -contains-> crates/inherit/Cargo.toml#/dependencies exact",
		"crates/inherit/Cargo.toml -contains-> crates/inherit/Cargo.toml#/package exact",
		"crates/inherit/Cargo.toml#/dependencies -contains-> crates/inherit/Cargo.toml#/dependencies/core-lib exact",
		"crates/inherit/Cargo.toml#/dependencies -contains-> crates/inherit/Cargo.toml#/dependencies/serde exact",
		"crates/inherit/Cargo.toml#/dependencies/core-lib -contains-> crates/inherit/Cargo.toml#/dependencies/core-lib/workspace exact",
		"crates/inherit/Cargo.toml#/dependencies/core-lib -depends_on-> Cargo.toml#/workspace/dependencies/core-lib exact",
		"crates/inherit/Cargo.toml#/dependencies/serde -contains-> crates/inherit/Cargo.toml#/dependencies/serde/workspace exact",
		"crates/inherit/Cargo.toml#/dependencies/serde -depends_on-> Cargo.toml#/workspace/dependencies/serde exact",
		"crates/inherit/Cargo.toml#/package -contains-> crates/inherit/Cargo.toml#/package/name exact",
		"crates/inherit/Cargo.toml#/package -contains-> crates/inherit/Cargo.toml#/package/workspace exact",
		"crates/inherit/Cargo.toml#/package -depends_on-> crates/inherit/Cargo.toml#/dependencies/core-lib exact",
		"crates/inherit/Cargo.toml#/package -depends_on-> crates/inherit/Cargo.toml#/dependencies/serde exact",
		"crates/tool/Cargo.toml -contains-> crates/tool/Cargo.toml#/dependencies exact",
		"crates/tool/Cargo.toml -contains-> crates/tool/Cargo.toml#/package exact",
		"crates/tool/Cargo.toml#/dependencies -contains-> crates/tool/Cargo.toml#/dependencies/serde exact",
		"crates/tool/Cargo.toml#/dependencies/serde -contains-> crates/tool/Cargo.toml#/dependencies/serde/workspace exact",
		"crates/tool/Cargo.toml#/package -contains-> crates/tool/Cargo.toml#/package/name exact",
		"crates/tool/Cargo.toml#/package -contains-> crates/tool/Cargo.toml#/package/version exact",
		"crates/tool/Cargo.toml#/package -depends_on-> crates/tool/Cargo.toml#/dependencies/serde exact",
		"py/pyproject.toml -contains-> py/pyproject.toml#/build-system exact",
		"py/pyproject.toml -contains-> py/pyproject.toml#/dependency-groups exact",
		"py/pyproject.toml -contains-> py/pyproject.toml#/project exact",
		"py/pyproject.toml -contains-> py/pyproject.toml#/tool exact",
		"py/pyproject.toml#/build-system -contains-> py/pyproject.toml#/build-system/build-backend exact",
		"py/pyproject.toml#/build-system -contains-> py/pyproject.toml#/build-system/requires exact",
		"py/pyproject.toml#/build-system/requires -contains-> py/pyproject.toml#/build-system/requires/setuptools exact",
		"py/pyproject.toml#/build-system/requires -contains-> py/pyproject.toml#/build-system/requires/wheel exact",
		"py/pyproject.toml#/dependency-groups -contains-> py/pyproject.toml#/dependency-groups/test exact",
		"py/pyproject.toml#/dependency-groups/test -contains-> py/pyproject.toml#/dependency-groups/test/coverage exact",
		"py/pyproject.toml#/project -contains-> py/pyproject.toml#/project/dependencies exact",
		"py/pyproject.toml#/project -contains-> py/pyproject.toml#/project/name exact",
		"py/pyproject.toml#/project -contains-> py/pyproject.toml#/project/optional-dependencies exact",
		"py/pyproject.toml#/project -contains-> py/pyproject.toml#/project/requires-python exact",
		"py/pyproject.toml#/project -contains-> py/pyproject.toml#/project/version exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/build-system/requires/setuptools exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/build-system/requires/wheel exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/dependency-groups/test/coverage exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/project/dependencies/regex exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/project/dependencies/regex#2 exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/project/dependencies/requests exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/project/optional-dependencies/dev/pytest exact",
		"py/pyproject.toml#/project -depends_on-> py/pyproject.toml#/project/optional-dependencies/dev/ruff exact",
		"py/pyproject.toml#/project/dependencies -contains-> py/pyproject.toml#/project/dependencies/regex exact",
		"py/pyproject.toml#/project/dependencies -contains-> py/pyproject.toml#/project/dependencies/regex#2 exact",
		"py/pyproject.toml#/project/dependencies -contains-> py/pyproject.toml#/project/dependencies/requests exact",
		"py/pyproject.toml#/project/optional-dependencies -contains-> py/pyproject.toml#/project/optional-dependencies/dev exact",
		"py/pyproject.toml#/project/optional-dependencies/dev -contains-> py/pyproject.toml#/project/optional-dependencies/dev/pytest exact",
		"py/pyproject.toml#/project/optional-dependencies/dev -contains-> py/pyproject.toml#/project/optional-dependencies/dev/ruff exact",
		"py/pyproject.toml#/tool -contains-> py/pyproject.toml#/tool/ruff exact",
		"py/pyproject.toml#/tool/ruff -contains-> py/pyproject.toml#/tool/ruff/line-length exact",
		"py/pyproject.toml#/tool/ruff -contains-> py/pyproject.toml#/tool/ruff/lint exact",
		"py/pyproject.toml#/tool/ruff/lint -contains-> py/pyproject.toml#/tool/ruff/lint/select exact",
	})
}

// TestGraph_NonEdgesAreAccountedFor: every reference that is no edge is
// reported for what it is — outside the repository (a crate of a registry
// or git, a PyPI distribution, a path or member leaving the repository),
// unresolved (a glob member, a workspace root no file names, a feature
// naming nothing) or a Candidate — and none is attributed to a declaration
// by its name.
func TestGraph_NonEdgesAreAccountedFor(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "unresolved", unresolvedLines(idx), []string{
		"Cargo.toml#/workspace/dependencies/serde: serde depends_on outside_repository",
		"Cargo.toml#/workspace: ../elsewhere contains outside_repository",
		"Cargo.toml#/workspace: plugins/* contains unresolved",
		"crates/app/Cargo.toml#/build-dependencies/cc: cc depends_on outside_repository",
		"crates/app/Cargo.toml#/dependencies/json: serde_json depends_on outside_repository",
		"crates/app/Cargo.toml#/dependencies/libc: libc depends_on outside_repository",
		"crates/app/Cargo.toml#/dependencies/log: log depends_on outside_repository",
		"crates/app/Cargo.toml#/dependencies/outside: outside depends_on outside_repository",
		"crates/app/Cargo.toml#/dependencies/private: private depends_on outside_repository",
		"crates/app/Cargo.toml#/dependencies/regex: regex depends_on outside_repository",
		"crates/app/Cargo.toml#/dependencies/serde: serde depends_on unresolved",
		"crates/app/Cargo.toml#/dev-dependencies/tempfile: tempfile depends_on outside_repository",
		"crates/app/Cargo.toml#/features/fast: missing value_reference unresolved",
		"crates/app/Cargo.toml#/target/cfg(unix)/dependencies/libc: libc depends_on outside_repository",
		"crates/tool/Cargo.toml#/dependencies/serde: serde depends_on unresolved",
		"py/pyproject.toml#/build-system/requires/setuptools: setuptools depends_on outside_repository",
		"py/pyproject.toml#/build-system/requires/wheel: wheel depends_on outside_repository",
		"py/pyproject.toml#/dependency-groups/test/coverage: coverage depends_on outside_repository",
		"py/pyproject.toml#/project/dependencies/regex#2: regex depends_on outside_repository",
		"py/pyproject.toml#/project/dependencies/regex: Regex depends_on outside_repository",
		"py/pyproject.toml#/project/dependencies/requests: requests depends_on outside_repository",
		"py/pyproject.toml#/project/optional-dependencies/dev/pytest: pytest depends_on outside_repository",
		"py/pyproject.toml#/project/optional-dependencies/dev/ruff: ruff depends_on outside_repository",
	})
	var cands, unattributed []string
	for _, s := range idx.FindSymbols("") {
		for _, r := range idx.CandidateCalleeSample(s.ID).Relations {
			to, _ := idx.GetSymbol(r.Symbol)
			cands = append(cands, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, r.Kind, to.Qualified, r.Confidence))
		}
		if in, out := idx.Unattributed(s.ID); in != 0 || out != 0 {
			unattributed = append(unattributed, fmt.Sprintf("%s: %d in, %d out", s.Qualified, in, out))
		}
	}
	// The Candidate is counted on both sides: neither reads complete.
	equalLines(t, "unattributed", unattributed, []string{
		"crates/app/Cargo.toml#/dependencies/libc: 1 in, 0 out",
		"crates/app/Cargo.toml#/features/std: 0 in, 1 out",
	})
	equalLines(t, "candidates", cands, []string{
		"crates/app/Cargo.toml#/features/std -value_reference-> crates/app/Cargo.toml#/dependencies/libc candidate",
	})
}

// TestGraph_Parents: the index places every declaration in the one its
// provider names (each table's bytes enclose what it holds).
func TestGraph_Parents(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	for _, s := range idx.FindSymbols("") {
		if s.ParentQualified != "" && s.Parent == "" {
			t.Errorf("%s: parent %s not placed", s.Qualified, s.ParentQualified)
		}
	}
}
