package xml

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/java"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

const fixtureRepo = "testdata/repo"

func buildIndex(t testing.TB, dir string, providers ...language.Provider) *index.RepositoryIndex {
	t.Helper()
	if len(providers) == 0 {
		providers = []language.Provider{NewProvider()}
	}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

// edgeLines renders every forward edge as "from -kind-> to confidence",
// leaving out the contains edges to elements, attributes and namespaces
// (the document's structure; TestGraph_Structure) unless all is set.
func edgeLines(idx *index.RepositoryIndex, all bool) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			if !all && e.Kind == index.EdgeContains && (to.Kind == symbol.KindElement || to.Kind == symbol.KindAttribute || to.Kind == symbol.KindNamespace) {
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

// candidateLines renders every candidate relation (no edge) as
// "from -kind-> to confidence".
func candidateLines(idx *index.RepositoryIndex) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, c := range idx.CandidateCalleeSample(s.ID).Relations {
			to, _ := idx.GetSymbol(c.Symbol)
			out = append(out, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, c.Kind, to.Qualified, c.Confidence))
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

// TestGraph_Fixture compares the fixture's graph — every edge but the
// document structure — with the expected one: a document contains its POM's
// project; a project depends on its dependencies, plugins and extensions
// and contains its managed ones and a profile's; a dependency on a project
// of the repository is Strong through the project's coordinates (also with
// ${project.groupId}); a parent is Strong by its relativePath and
// coordinates (default ../pom.xml, explicit, and through the coordinates
// when the POM at the path is another project); modules link their POM
// files Exact; an xlink:href fragment references the xml:id, an XInclude
// links its file.
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx, false), []string{
		"app/pom.xml -contains-> app/pom.xml::com.example:app exact",
		"app/pom.xml::com.example:app -contains-> app/pom.xml::profile:it::dependencies::org.testcontainers:testcontainers exact",
		"app/pom.xml::com.example:app -depends_on-> app/pom.xml::dependencies::${vendor.group}:vendor-lib exact",
		"app/pom.xml::com.example:app -depends_on-> app/pom.xml::dependencies::com.example:core exact",
		"app/pom.xml::com.example:app -extends-> pom.xml::com.example:parent strong",
		"app/pom.xml::dependencies::com.example:core -depends_on-> core/pom.xml::com.example:core strong",
		"core/fallback/pom.xml -contains-> core/fallback/pom.xml::com.example:fallback exact",
		"core/fallback/pom.xml::com.example:fallback -extends-> pom.xml::com.example:parent strong",
		"core/pom.xml -contains-> core/pom.xml::com.example:core exact",
		"core/pom.xml::com.example:core -depends_on-> core/pom.xml::dependencies::com.google.guava:guava exact",
		"core/pom.xml::com.example:core -depends_on-> core/pom.xml::dependencies::org.junit.jupiter:junit-jupiter exact",
		"core/pom.xml::com.example:core -extends-> pom.xml::com.example:parent strong",
		"docs/book.xml#/b:book/b:chapter/b:para/b:link/@xlink:href -references-> docs/book.xml#top exact",
		"docs/book.xml#/b:book/xi:include/@href -links-> docs/chapter.xml exact",
		"javaapp/pom.xml -contains-> javaapp/pom.xml::com.example:javaapp exact",
		"other/pom.xml -contains-> other/pom.xml::com.example.other:other exact",
		"pom.xml -contains-> pom.xml::com.example:parent exact",
		"pom.xml::com.example:parent -contains-> pom.xml::dependencyManagement::org.junit.jupiter:junit-jupiter exact",
		"pom.xml::com.example:parent -contains-> pom.xml::dependencyManagement::org.springframework.boot:spring-boot-dependencies exact",
		"pom.xml::com.example:parent -depends_on-> pom.xml::plugins::org.apache.maven.plugins:maven-compiler-plugin exact",
		"pom.xml::com.example:parent -links-> app/pom.xml exact",
		"pom.xml::com.example:parent -links-> core/pom.xml exact",
	})
}

// TestGraph_NonEdgesAreAccountedFor: every reference that is no edge is
// reported for what it is — outside the repository (an external artifact,
// plugin, BOM or parent), unresolved (a module, stylesheet or ID the
// repository does not have), a dynamic name (a property the file does not
// define) — or a Candidate (an href to a plain id attribute, an ID declared
// twice); none is attributed to a declaration by its name.
func TestGraph_NonEdgesAreAccountedFor(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "unresolved", unresolvedLines(idx), []string{
		"app/pom.xml::dependencies::${vendor.group}:vendor-lib: ${vendor.group}:vendor-lib depends_on dynamic_name",
		"app/pom.xml::profile:it::dependencies::org.testcontainers:testcontainers: org.testcontainers:testcontainers depends_on outside_repository",
		"core/pom.xml::dependencies::com.google.guava:guava: com.google.guava:guava depends_on outside_repository",
		"core/pom.xml::dependencies::org.junit.jupiter:junit-jupiter: org.junit.jupiter:junit-jupiter depends_on outside_repository",
		"docs/book.xml#/b:book/b:chapter/b:para[2]/b:link/@xlink:href: #nowhere id_reference unresolved",
		"docs/book.xml: book.xsl resource_link unresolved",
		"other/pom.xml::com.example.other:other: org.springframework.boot:spring-boot-starter-parent inheritance outside_repository",
		"pom.xml::com.example:parent: missing resource_link unresolved",
		"pom.xml::dependencyManagement::org.springframework.boot:spring-boot-dependencies: org.springframework.boot:spring-boot-dependencies depends_on outside_repository",
		"pom.xml::plugins::org.apache.maven.plugins:maven-compiler-plugin: org.apache.maven.plugins:maven-compiler-plugin depends_on outside_repository",
	})
	equalLines(t, "candidates", candidateLines(idx), []string{
		"docs/book.xml#/b:book/b:chapter/b:para/b:ref/@href -id_reference-> docs/book.xml#intro candidate",
		"docs/ids.xml#/doc/a/@xlink:href -id_reference-> docs/ids.xml#dup candidate",
		"docs/ids.xml#/doc/a/@xlink:href -id_reference-> docs/ids.xml#dup candidate",
	})
	// A Candidate is unattributed by definition (no edge claims it); every
	// other reference is attributed.
	candidate := map[string]bool{
		"docs/book.xml#/b:book/b:chapter/b:para/b:ref/@href": true, "docs/book.xml#intro": true,
		"docs/ids.xml#/doc/a/@xlink:href": true, "docs/ids.xml#dup": true,
	}
	for _, s := range idx.FindSymbols("") {
		if in, out := idx.Unattributed(s.ID); (in != 0 || out != 0) != candidate[s.Qualified] {
			t.Errorf("%s: unattributed %d in, %d out", s.Qualified, in, out)
		}
	}
}

// TestGraph_Structure: the document contains its root element, each
// element its child elements and attributes, by namespace-aware paths.
func TestGraph_Structure(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	var got []string
	for _, l := range edgeLines(idx, true) {
		if strings.HasPrefix(l, "docs/chapter.xml") {
			got = append(got, l)
		}
	}
	equalLines(t, "structure", got, []string{
		"docs/chapter.xml -contains-> docs/chapter.xml#/chapter exact",
		"docs/chapter.xml#/chapter -contains-> docs/chapter.xml#/chapter/@xmlns exact",
		"docs/chapter.xml#/chapter -contains-> docs/chapter.xml#/chapter/para exact",
	})
	// Every element and attribute of the fixture is contained by exactly
	// one declaration.
	for _, s := range idx.FindSymbols("") {
		if s.Kind != symbol.KindElement && s.Kind != symbol.KindAttribute && s.Kind != symbol.KindNamespace {
			continue
		}
		n := 0
		for _, e := range idx.GetCallers(s.ID) {
			if e.RefKind == reference.KindContains {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: contained by %d declarations", s.Qualified, n)
		}
	}
}

// TestGraph_JavaUnaffected: indexing the Maven project's XML beside its
// Java code changes no Java symbol, edge or unresolved reference, and no
// Java name reaches an XML declaration (IdentityOnly): Java methods named
// artifactId, project, dependency, parent and core stay Java's alone.
func TestGraph_JavaUnaffected(t *testing.T) {
	javaOnly := buildIndex(t, fixtureRepo, java.NewProvider())
	both := buildIndex(t, fixtureRepo, java.NewProvider(), NewProvider())
	javaLines := func(idx *index.RepositoryIndex) (syms, edges, unresolved []string) {
		for _, s := range idx.FindSymbols("") {
			if s.Language != "java" {
				continue
			}
			syms = append(syms, fmt.Sprintf("%s %s %s", s.ID, s.Kind, s.Qualified))
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				if to.Language != "java" {
					t.Errorf("java %s -%s-> %s %s", s.Qualified, e.Kind, to.Language, to.Qualified)
				}
				edges = append(edges, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
			}
			for _, c := range idx.CandidateCalleeSample(s.ID).Relations {
				if to, _ := idx.GetSymbol(c.Symbol); to.Language != "java" {
					t.Errorf("java %s candidate -> %s %s", s.Qualified, to.Language, to.Qualified)
				}
			}
			for _, r := range idx.UnresolvedOutgoing(s.ID).References {
				unresolved = append(unresolved, fmt.Sprintf("%s: %s %s %s", s.Qualified, r.Name, r.Kind, r.Reason))
			}
		}
		sort.Strings(syms)
		sort.Strings(edges)
		sort.Strings(unresolved)
		return
	}
	s1, e1, u1 := javaLines(javaOnly)
	s2, e2, u2 := javaLines(both)
	if len(s1) == 0 || len(e1) == 0 {
		t.Fatalf("the fixture's Java code states nothing: %v %v", s1, e1)
	}
	equalLines(t, "java symbols", s2, s1)
	equalLines(t, "java edges", e2, e1)
	equalLines(t, "java unresolved", u2, u1)
	for _, s := range both.FindSymbols("") {
		if s.Language != "xml" {
			continue
		}
		for _, e := range both.GetCallers(s.ID) {
			if from, _ := both.GetSymbol(e.To); from.Language != "xml" {
				t.Errorf("%s %s reaches xml %s", from.Language, from.Qualified, s.Qualified)
			}
		}
	}
}
