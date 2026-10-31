package markdown

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/html"
	"github.com/magicdrive/ark/internal/languages/yaml"
)

const fixtureRepo = "testdata/repo"

func buildIndex(t testing.TB, dir string) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), dir, []language.Provider{NewProvider(), html.NewProvider(), yaml.NewProvider()})
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

// TestGraph_Fixture compares the fixture's whole graph with the expected
// one: contains (document -> top sections -> subsections, setext headings
// too), links (path evidence to a Markdown document, an HTML document, a
// YAML document and an asset), references (an anchor of the same document
// — a heading's, a repeated heading's "-1", an HTML id — and of another
// document, through a reference link too). An ADR reaches the design
// sections it relies on. Absent on purpose: the links
// TestGraph_NonEdgesAreAccountedFor lists.
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx), []string{
		"README.md -contains-> README.md#example-project exact",
		"README.md#example-project -contains-> README.md#usage exact",
		"README.md#example-project -contains-> README.md#usage-1 exact",
		"README.md#example-project -links-> config/app.yaml exact",
		"README.md#example-project -links-> docs/design.md exact",
		"README.md#example-project -links-> img/logo.png exact",
		"README.md#example-project -links-> site/index.html exact",
		"README.md#example-project -references-> README.md#usage exact",
		"README.md#example-project -references-> README.md#usage-1 exact",
		"README.md#example-project -references-> docs/design.md#architecture exact",
		"README.md#example-project -references-> docs/ops/runbook.md#rollback exact",
		"docs/adr/0001-markdown.md -contains-> docs/adr/0001-markdown.md#adr-0001-markdown-support exact",
		"docs/adr/0001-markdown.md#adr-0001-markdown-support -contains-> docs/adr/0001-markdown.md#context exact",
		"docs/adr/0001-markdown.md#adr-0001-markdown-support -contains-> docs/adr/0001-markdown.md#decision exact",
		"docs/adr/0001-markdown.md#context -references-> docs/design.md#architecture exact",
		"docs/adr/0001-markdown.md#context -references-> docs/design.md#index exact",
		"docs/adr/0001-markdown.md#decision -links-> docs/ops/runbook.md exact",
		"docs/design.md -contains-> docs/design.md#design exact",
		"docs/design.md#architecture -contains-> docs/design.md#index exact",
		"docs/design.md#design -contains-> docs/design.md#architecture exact",
		"docs/design.md#design -contains-> docs/design.md#limits exact",
		"docs/design.md#design -links-> docs/adr/0001-markdown.md exact",
		"docs/design.md#design -references-> README.md#usage exact",
		"docs/ops/runbook.md -contains-> docs/ops/runbook.md#runbook exact",
		"docs/ops/runbook.md#rollback -references-> docs/ops/runbook.md#deploy exact",
		"docs/ops/runbook.md#rollback -references-> docs/ops/runbook.md#manual-rollback exact",
		"docs/ops/runbook.md#runbook -contains-> docs/ops/runbook.md#deploy exact",
		"docs/ops/runbook.md#runbook -contains-> docs/ops/runbook.md#rollback exact",
	})
}

// TestGraph_NonEdgesAreAccountedFor: every link that is no edge is
// reported for what it is — a missing file, a missing anchor (of this
// document or another), a file no provider declares (Go source without its
// provider, a directory), a path that leaves the repository: unresolved;
// a web URL: outside the repository, named without its query and fragment
// — and none reaches a declaration by its name. An undefined reference
// ("[x][nope]") is no link.
func TestGraph_NonEdgesAreAccountedFor(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "unresolved", unresolvedLines(idx), []string{
		"README.md#example-project: ../outside.md resource_link unresolved",
		"README.md#example-project: docs resource_link unresolved",
		"README.md#example-project: gone.md resource_link unresolved",
		"README.md#example-project: https://example.com resource_link outside_repository",
		"README.md#example-project: main.go resource_link unresolved",
		"README.md#example-project: missing.png resource_link unresolved",
		"README.md#example-project: nowhere id_reference unresolved",
		"docs/adr/0001-markdown.md#context: removed id_reference unresolved",
	})
	for _, s := range idx.FindSymbols("") {
		if in, out := idx.Unattributed(s.ID); in != 0 || out != 0 {
			t.Errorf("%s: unattributed %d in, %d out", s.Qualified, in, out)
		}
	}
}

// TestGraph_RootAnchorsAndSecrets: a fragment of a file named from the
// repository's root ("/docs/b.md#x") reaches its section at Strong at
// most, as a path from the root does; a fragment that names a heading
// whose text holds a secret — declared as no section — is unresolved.
func TestGraph_RootAnchorsAndSecrets(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/a.md", "# A\n\n[root](/docs/b.md#sec-1) [secret](b.md#password--hunter2secretvalue)\n")
	write("docs/c.md", "# C\n\n[rel](b.md#sec-1)\n")
	write("docs/b.md", "# B\n\n## Sec 1\n\n## password = hunter2secretvalue\n")
	idx := buildIndex(t, dir)
	var edges []string
	for _, l := range edgeLines(idx) {
		if !strings.Contains(l, "-contains->") {
			edges = append(edges, l)
		}
	}
	sep := string(filepath.Separator)
	equalLines(t, "edges", edges, []string{
		"docs" + sep + "a.md#a -references-> docs" + sep + "b.md#sec-1 strong",
		"docs" + sep + "c.md#c -references-> docs" + sep + "b.md#sec-1 exact",
	})
	equalLines(t, "unresolved", unresolvedLines(idx), []string{
		"docs" + sep + "a.md#a: password--hunter2secretvalue id_reference unresolved",
	})
}
