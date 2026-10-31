package markdown

import (
	"fmt"
	"sort"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// The scenarios a reader of a repository's documents asks of Ark, answered
// from the index the MCP tools read (get_symbols, find_symbol, get_symbol,
// find_references, get_callers / get_callees, the unresolved samples of
// get_context and analyze_change_impact).

func one(t *testing.T, idx *index.RepositoryIndex, q string) symbol.Symbol {
	t.Helper()
	s := idx.FindSymbolsByQualified(q)
	if len(s) != 1 {
		t.Fatalf("%s: %d declarations", q, len(s))
	}
	return s[0]
}

func callers(idx *index.RepositoryIndex, s symbol.Symbol) []string {
	var out []string
	for _, e := range idx.GetCallers(s.ID) {
		// A reverse edge leads from s to the declaration that refers to it.
		from, _ := idx.GetSymbol(e.To)
		out = append(out, fmt.Sprintf("%s -> %s %s", from.Qualified, s.Qualified, e.Confidence))
	}
	sort.Strings(out)
	return out
}

// Scenario 1: the headings of a README, in order, with their level and line.
func TestScenario_ReadmeHeadings(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	var got []string
	for _, s := range idx.SymbolsByFile("README.md") {
		if s.Kind == symbol.KindSection {
			got = append(got, fmt.Sprintf("%s %s L%d", s.Signature, s.Name, s.Location.Range.Start.Line))
		}
	}
	equalLines(t, "headings", got, []string{"h1 Example Project L1", "h2 Usage L14", "h2 Usage L20"})
}

// Scenario 2: one section, found by its name or its anchor, with its
// extent and its subsections.
func TestScenario_FindSection(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	var named []string
	for _, s := range idx.FindSymbols("Architecture") {
		if s.Kind == symbol.KindSection && s.Name == "Architecture" {
			named = append(named, s.Qualified)
		}
	}
	equalLines(t, "sections named Architecture", named, []string{"docs/design.md#architecture"})
	s := one(t, idx, "docs/design.md#architecture")
	r := s.Location.Range
	if got := fmt.Sprintf("%d-%d", r.Start.Line, r.End.Line); got != "7-11" {
		t.Errorf("Architecture spans lines %s, want 7-11 (its heading to its subsection's last line)", got)
	}
	var sub []string
	for _, e := range idx.GetCallees(s.ID) {
		to, _ := idx.GetSymbol(e.To)
		sub = append(sub, string(e.Kind)+" "+to.Qualified)
	}
	equalLines(t, "subsections", sub, []string{"contains docs/design.md#index"})
}

// Scenario 3: the links of one document, with what each names.
func TestScenario_LinksOfDocument(t *testing.T) {
	idx := buildIndex(t, "testdata/repo")
	var got []string
	for _, r := range idx.ReferencesByFile("docs/adr/0001-markdown.md") {
		if r.Kind == reference.KindContains {
			continue
		}
		got = append(got, fmt.Sprintf("L%d %s %s %s", r.Location.Range.Start.Line, r.Kind, r.Name, r.NameQualified))
	}
	sort.Strings(got)
	equalLines(t, "links", got, []string{
		"L10 resource_link runbook.md docs/ops/runbook.md",
		"L5 id_reference architecture docs/design.md#architecture",
		"L5 id_reference index docs/design.md#index",
		"L6 id_reference removed docs/design.md#removed",
	})
}

// Scenario 4: which documents link to a document (and to its sections).
func TestScenario_DocumentsLinkingToDocument(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "links to docs/design.md", callers(idx, one(t, idx, "docs/design.md")), []string{
		"README.md#example-project -> docs/design.md exact",
	})
	equalLines(t, "links to docs/ops/runbook.md", callers(idx, one(t, idx, "docs/ops/runbook.md")), []string{
		"docs/adr/0001-markdown.md#decision -> docs/ops/runbook.md exact",
	})
}

// Scenario 5: the references to an anchor — of the same document, and of
// another.
func TestScenario_AnchorReferences(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "README.md#usage", callers(idx, one(t, idx, "README.md#usage")), []string{
		"README.md#example-project -> README.md#usage exact",
		"docs/design.md#design -> README.md#usage exact",
	})
	equalLines(t, "runbook HTML anchor", callers(idx, one(t, idx, "docs/ops/runbook.md#manual-rollback")), []string{
		"docs/ops/runbook.md#rollback -> docs/ops/runbook.md#manual-rollback exact",
	})
}

// Scenario 6: the broken links — those whose file or anchor no declaration
// carries (a web URL is outside the repository, not broken).
func TestScenario_BrokenLinks(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	var got []string
	for _, s := range idx.FindSymbols("") {
		for _, r := range idx.UnresolvedOutgoing(s.ID).References {
			if r.Reason == "unresolved" {
				got = append(got, fmt.Sprintf("%s L%d %s", r.Location.File, r.Location.Range.Start.Line, r.Name))
			}
		}
	}
	sort.Strings(got)
	equalLines(t, "broken", got, []string{
		"README.md L10 docs",
		"README.md L10 main.go",
		"README.md L4 nowhere",
		"README.md L6 missing.png",
		"README.md L9 ../outside.md",
		"README.md L9 gone.md",
		"docs/adr/0001-markdown.md L6 removed",
	})
}

// Scenario 7: the languages of the code fences.
func TestScenario_CodeFenceLanguages(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	var got []string
	for _, s := range idx.FindSymbols("") {
		if s.Kind == symbol.KindCodeBlock {
			got = append(got, fmt.Sprintf("%s %s in %s", s.Name, s.Location.File, s.ParentQualified))
		}
	}
	sort.Strings(got)
	equalLines(t, "code fences", got, []string{
		"bash README.md in README.md#usage",
		"sh docs/ops/runbook.md in docs/ops/runbook.md#deploy",
	})
}

// Scenario 8: from an ADR to the design documents it relies on.
func TestScenario_ADRToDesign(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	var got []string
	for _, s := range idx.SymbolsByFile("docs/adr/0001-markdown.md") {
		for _, e := range idx.GetCallees(s.ID) {
			if e.Kind == "contains" {
				continue
			}
			to, _ := idx.GetSymbol(e.To)
			got = append(got, fmt.Sprintf("%s -%s-> %s (%s)", s.Name, e.Kind, to.Qualified, to.Kind))
		}
	}
	sort.Strings(got)
	equalLines(t, "ADR relations", got, []string{
		"Context -references-> docs/design.md#architecture (section)",
		"Context -references-> docs/design.md#index (section)",
		"Decision -links-> docs/ops/runbook.md (document)",
	})
}
