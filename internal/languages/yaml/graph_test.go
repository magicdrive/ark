package yaml

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
// one: contains (workflow -> triggers and jobs, job and composite action ->
// steps), depends_on (needs, in string and list form, through an alias too),
// calls (a local reusable workflow, Exact; a local action, Strong) and
// references (an alias to its anchor, from the job around it). Absent on
// purpose — they are no repository declarations: actions/checkout and the
// other remote actions, docker://, the remote reusable workflow, the job
// "missing", the expression in uses and the invalid uses
// (TestGraph_NonEdgesAreAccountedFor).
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx), []string{
		".github/actions/setup -contains-> .github/actions/setup::./.github/actions/lint exact",
		".github/actions/setup -contains-> .github/actions/setup::actions/setup-go exact",
		".github/actions/setup -contains-> .github/actions/setup::run exact",
		".github/actions/setup::./.github/actions/lint -calls-> .github/actions/lint strong",
		".github/workflows/anchors.yaml -contains-> .github/workflows/anchors.yaml::first exact",
		".github/workflows/anchors.yaml -contains-> .github/workflows/anchors.yaml::on:push exact",
		".github/workflows/anchors.yaml -contains-> .github/workflows/anchors.yaml::second exact",
		".github/workflows/anchors.yaml -contains-> .github/workflows/anchors.yaml::third exact",
		".github/workflows/anchors.yaml::first -contains-> .github/workflows/anchors.yaml::first::./.github/actions/setup exact",
		".github/workflows/anchors.yaml::first -contains-> .github/workflows/anchors.yaml::first::actions/checkout exact",
		".github/workflows/anchors.yaml::first::./.github/actions/setup -calls-> .github/actions/setup strong",
		".github/workflows/anchors.yaml::second -contains-> .github/workflows/anchors.yaml::second::./.github/actions/setup exact",
		".github/workflows/anchors.yaml::second -contains-> .github/workflows/anchors.yaml::second::actions/checkout exact",
		".github/workflows/anchors.yaml::second -depends_on-> .github/workflows/anchors.yaml::first exact",
		".github/workflows/anchors.yaml::second -depends_on-> .github/workflows/anchors.yaml::third exact",
		".github/workflows/anchors.yaml::second -references-> .github/workflows/anchors.yaml#&common exact",
		".github/workflows/anchors.yaml::second -references-> .github/workflows/anchors.yaml#&runner exact",
		".github/workflows/anchors.yaml::second::./.github/actions/setup -calls-> .github/actions/setup strong",
		".github/workflows/anchors.yaml::third -contains-> .github/workflows/anchors.yaml::third::run exact",
		".github/workflows/anchors.yaml::third -depends_on-> .github/workflows/anchors.yaml::second exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::build exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::lint exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::on:pull_request exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::on:push exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::on:workflow_dispatch exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::release exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::remote exact",
		".github/workflows/ci.yml -contains-> .github/workflows/ci.yml::test exact",
		".github/workflows/ci.yml::build -contains-> .github/workflows/ci.yml::build::${{ matrix.action }} exact",
		".github/workflows/ci.yml::build -contains-> .github/workflows/ci.yml::build::actions/checkout exact",
		".github/workflows/ci.yml::build -contains-> .github/workflows/ci.yml::build::not-a-reference exact",
		".github/workflows/ci.yml::build -contains-> .github/workflows/ci.yml::build::pack exact",
		".github/workflows/ci.yml::build -depends_on-> .github/workflows/ci.yml::lint exact",
		".github/workflows/ci.yml::build -depends_on-> .github/workflows/ci.yml::test exact",
		".github/workflows/ci.yml::lint -contains-> .github/workflows/ci.yml::lint::./.github/actions/lint exact",
		".github/workflows/ci.yml::lint -contains-> .github/workflows/ci.yml::lint::Checkout exact",
		".github/workflows/ci.yml::lint -contains-> .github/workflows/ci.yml::lint::run exact",
		".github/workflows/ci.yml::lint::./.github/actions/lint -calls-> .github/actions/lint strong",
		".github/workflows/ci.yml::release -calls-> .github/workflows/reusable.yml exact",
		".github/workflows/ci.yml::release -depends_on-> .github/workflows/ci.yml::build exact",
		".github/workflows/ci.yml::test -contains-> .github/workflows/ci.yml::test::Checkout exact",
		".github/workflows/ci.yml::test -contains-> .github/workflows/ci.yml::test::run exact",
		".github/workflows/ci.yml::test -contains-> .github/workflows/ci.yml::test::run#2 exact",
		".github/workflows/ci.yml::test -contains-> .github/workflows/ci.yml::test::setup exact",
		".github/workflows/ci.yml::test -depends_on-> .github/workflows/ci.yml::lint exact",
		".github/workflows/ci.yml::test::setup -calls-> .github/actions/setup strong",
		".github/workflows/reusable.yml -contains-> .github/workflows/reusable.yml::on:workflow_call exact",
		".github/workflows/reusable.yml -contains-> .github/workflows/reusable.yml::publish exact",
		".github/workflows/reusable.yml::publish -contains-> .github/workflows/reusable.yml::publish::./.github/actions/setup exact",
		".github/workflows/reusable.yml::publish::./.github/actions/setup -calls-> .github/actions/setup strong",
		"config/app.yaml#/development -references-> config/app.yaml#&defaults exact",
	})
}

// TestGraph_NonEdgesAreAccountedFor: every reference that is no edge is
// reported for what it is — outside the repository (a remote action or
// workflow, a docker image), unresolved (a job no workflow declares, an
// expression: a dynamic name) — and none is attributed to a repository
// declaration by its name. A uses GitHub does not accept is a diagnostic,
// no reference.
func TestGraph_NonEdgesAreAccountedFor(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "unresolved", unresolvedLines(idx), []string{
		".github/actions/setup::actions/setup-go: actions/setup-go call outside_repository",
		".github/workflows/anchors.yaml::first::actions/checkout: actions/checkout call outside_repository",
		".github/workflows/anchors.yaml::second::actions/checkout: actions/checkout call outside_repository",
		".github/workflows/ci.yml::build::${{ matrix.action }}: ${{ matrix.action }} call dynamic_name",
		".github/workflows/ci.yml::build::actions/checkout: actions/checkout call outside_repository",
		".github/workflows/ci.yml::build::pack: docker://alpine:3.20 call outside_repository",
		".github/workflows/ci.yml::lint::Checkout: actions/checkout call outside_repository",
		".github/workflows/ci.yml::release: missing depends_on unresolved",
		".github/workflows/ci.yml::remote: octo-org/shared/.github/workflows/deploy.yml call outside_repository",
		".github/workflows/ci.yml::test::Checkout: actions/checkout call outside_repository",
	})
	for _, s := range idx.FindSymbols("") {
		if in, out := idx.Unattributed(s.ID); in != 0 || out != 0 {
			t.Errorf("%s: unattributed %d in, %d out", s.Qualified, in, out)
		}
	}
}
