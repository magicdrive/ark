package java

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/resolver"
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
// one. Strong, not Exact: a name the package's other files or an on-demand
// import supply (another file of the package may declare it, or a scope
// outside the repository may), one only another source root declares
// (src/test → src/main), and what an anonymous Runnable's body reaches past
// the Runnable's unknown members. Absent on purpose:
//   - u.grant(Role.ADMIN), u.grant("MEMBER"): grant(Role) and grant(String)
//     both may apply (a parameter type's identity is not proven);
//   - grant(Role.valueOf(…)) in grant(String): the argument's type is a
//     method's result;
//   - Role.valueOf, Point's canonical constructor: the language's implicit
//     members, no declarations;
//   - friends.add, s.toUpperCase(), String, List: outside the repository;
//   - Point.origin().shift(x): a call on a method's result.
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx), []string{
		"com.acme.model.Entity -implements-> com.acme.model.Named exact",
		"com.acme.model.Entity.id -references-> com.acme.model.Entity.id exact",
		"com.acme.model.Entity.touch -references-> com.acme.model.Entity.id exact",
		"com.acme.model.Named.label -calls-> com.acme.model.Named.name exact",
		"com.acme.model.Point.origin -calls-> com.acme.model.Point exact",
		"com.acme.model.Point.origin -uses_type-> com.acme.model.Point exact",
		"com.acme.model.Point.shift -calls-> com.acme.model.Point exact",
		"com.acme.model.Point.shift -references-> com.acme.model.Point.x exact",
		"com.acme.model.Point.shift -references-> com.acme.model.Point.y exact",
		"com.acme.model.Point.shift -uses_type-> com.acme.model.Point exact",
		"com.acme.model.Role.<anonymous> -extends-> com.acme.model.Role exact",
		"com.acme.model.Role.ADMIN -calls-> com.acme.model.Role.<init> exact",
		"com.acme.model.Role.code -references-> com.acme.model.Role.code exact",
		"com.acme.model.User -extends-> com.acme.model.Entity exact",
		"com.acme.model.User.<init> -calls-> com.acme.model.User.<init> exact",
		"com.acme.model.User.Badge -extends-> com.acme.model.Entity.Key exact",
		"com.acme.model.User.Badge.show -calls-> com.acme.model.Entity.Key.text exact",
		"com.acme.model.User.Badge.show -calls-> com.acme.model.User.name exact",
		"com.acme.model.User.befriend -calls-> com.acme.model.Entity.touch exact",
		"com.acme.model.User.befriend -references-> com.acme.model.User.friends exact",
		"com.acme.model.User.befriend -uses_type-> com.acme.model.User exact",
		"com.acme.model.User.friends -uses_type-> com.acme.model.User exact",
		"com.acme.model.User.grant -uses_type-> com.acme.model.Role exact",
		"com.acme.model.User.grant -uses_type-> com.acme.model.Role exact",
		"com.acme.model.User.name -references-> com.acme.model.User.name exact",
		"com.acme.model.User.role -references-> com.acme.model.Role.MEMBER exact",
		"com.acme.model.User.role -uses_type-> com.acme.model.Role exact",
		"com.acme.service.Repository -uses_type-> com.acme.model.Entity exact",
		"com.acme.service.UserService.<anonymous>.run -calls-> com.acme.model.Entity.touch strong",
		"com.acme.service.UserService.<anonymous>.run -calls-> com.acme.service.UserService.log strong",
		"com.acme.service.UserService.<init> -uses_type-> com.acme.model.User strong",
		"com.acme.service.UserService.<init> -uses_type-> com.acme.service.Repository exact",
		"com.acme.service.UserService.corner -calls-> com.acme.service.UserService.corner.Grid exact",
		"com.acme.service.UserService.corner -calls-> com.acme.service.UserService.corner.Grid.at exact",
		"com.acme.service.UserService.corner -uses_type-> com.acme.model.Point strong",
		"com.acme.service.UserService.corner.Grid.at -calls-> com.acme.model.Point.origin strong",
		"com.acme.service.UserService.corner.Grid.at -uses_type-> com.acme.model.Point strong",
		"com.acme.service.UserService.factory -calls-> com.acme.model.User strong",
		"com.acme.service.UserService.factory -uses_type-> com.acme.model.User strong",
		"com.acme.service.UserService.later -uses_type-> com.acme.model.User strong",
		"com.acme.service.UserService.register -calls-> com.acme.model.User strong",
		"com.acme.service.UserService.register -calls-> com.acme.model.User.<init> strong",
		"com.acme.service.UserService.register -calls-> com.acme.service.Repository.save exact",
		"com.acme.service.UserService.register -calls-> com.acme.service.Strings.upper exact",
		"com.acme.service.UserService.register -calls-> com.acme.service.UserService.log exact",
		"com.acme.service.UserService.register -calls-> com.acme.service.UserService.log exact",
		"com.acme.service.UserService.register -references-> com.acme.model.Role.ADMIN strong",
		"com.acme.service.UserService.register -references-> com.acme.service.UserService.users exact",
		"com.acme.service.UserService.register -uses_type-> com.acme.model.Role strong",
		"com.acme.service.UserService.register -uses_type-> com.acme.model.User strong",
		"com.acme.service.UserService.touchAll -calls-> com.acme.model.Entity.touch strong",
		"com.acme.service.UserService.touchAll -uses_type-> com.acme.model.Entity strong",
		"com.acme.service.UserService.users -uses_type-> com.acme.model.User strong",
		"com.acme.service.UserService.users -uses_type-> com.acme.service.Repository exact",
		"com.acme.service.UserServiceTest.registers -calls-> com.acme.model.Named.label strong",
		"com.acme.service.UserServiceTest.registers -calls-> com.acme.service.UserService strong",
		"com.acme.service.UserServiceTest.registers -calls-> com.acme.service.UserService.<init> strong",
		"com.acme.service.UserServiceTest.registers -calls-> com.acme.service.UserService.register strong",
		"com.acme.service.UserServiceTest.registers -uses_type-> com.acme.model.User strong",
		"com.acme.service.UserServiceTest.registers -uses_type-> com.acme.service.UserService strong",
	})
}

// TestGraph_Soundness: every edge is Exact or Strong (a Candidate is never
// an edge), and the two calls of register to log are its two overloads,
// told apart by their arguments' types.
func TestGraph_Soundness(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			if e.Confidence != resolver.ConfidenceExact && e.Confidence != resolver.ConfidenceStrong {
				to, _ := idx.GetSymbol(e.To)
				t.Errorf("%s -> %s: %s", s.Qualified, to.Qualified, e.Confidence)
			}
		}
	}
	logs := map[string]bool{}
	for _, s := range idx.FindSymbolsByQualified("com.acme.service.UserService.register") {
		for _, e := range idx.GetCallees(s.ID) {
			if to, _ := idx.GetSymbol(e.To); to.Qualified == "com.acme.service.UserService.log" {
				logs[to.Signature] = true
			}
		}
	}
	if !logs["(int level)"] || !logs["(String message)"] || len(logs) != 2 {
		t.Errorf("register's calls of log: %v", logs)
	}
}

// TestGraph_Deterministic: the graph is the same on every build, warm or
// cold, whatever order the files are found in.
func TestGraph_Deterministic(t *testing.T) {
	first := strings.Join(edgeLines(buildIndex(t, fixtureRepo)), "\n")
	for i := 0; i < 5; i++ {
		if got := strings.Join(edgeLines(buildIndex(t, fixtureRepo)), "\n"); got != first {
			t.Fatalf("build %d differs:\n%s\n---\n%s", i, got, first)
		}
	}
	// Copy the fixture under another root name and with files written in
	// reverse order: the graph must not change.
	dir := t.TempDir()
	files := listFiles(t, fixtureRepo)
	for i := len(files) - 1; i >= 0; i-- {
		b, err := os.ReadFile(fixtureRepo + "/" + files[i])
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir+"/"+files[i], b)
	}
	if got := strings.Join(edgeLines(buildIndex(t, dir)), "\n"); got != first {
		t.Fatalf("a copy differs:\n%s\n---\n%s", got, first)
	}
}

func equalLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		g, w := map[string]bool{}, map[string]bool{}
		for _, l := range got {
			g[l] = true
		}
		for _, l := range want {
			w[l] = true
		}
		for _, l := range got {
			if !w[l] {
				t.Errorf("%s: unexpected %s", what, l)
			}
		}
		for _, l := range want {
			if !g[l] {
				t.Errorf("%s: missing %s", what, l)
			}
		}
	}
}
