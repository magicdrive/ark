package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Package scoping (resolver/packagescope.go), driven by synthetic,
// provider-independent evidence: language "x" is no real language.

type psFile struct {
	path, pkg string
	decls     []string // exported if capitalized; "T.m" is a member of T
	imports   []language.ImportDraft
}

func psIndex(files []psFile, scoped bool) []resolver.FileIndex {
	var out []resolver.FileIndex
	for _, f := range files {
		fi := resolver.FileIndex{FileID: source.FileID(f.path), Language: "x", Imports: f.imports, Package: f.pkg, PackageScoped: scoped}
		for _, d := range f.decls {
			name, recv := d, ""
			for i := 0; i < len(d); i++ {
				if d[i] == '.' {
					recv, name = d[:i], d[i+1:]
				}
			}
			kind := symbol.KindFunction
			if name != "" && name[0] == 'T' && recv == "" {
				kind = symbol.KindStruct
			}
			if recv != "" {
				kind = symbol.KindMethod
			}
			fi.Symbols = append(fi.Symbols, symbol.Symbol{
				ID: symbol.SymbolID(f.path + "#" + d), Name: name, Qualified: d, Kind: kind, Language: "x",
				Receiver: recv, Exported: name[0] >= 'A' && name[0] <= 'Z',
				Location: source.Location{File: source.FileID(f.path)},
			})
		}
		out = append(out, fi)
	}
	return out
}

func psResolve(t *testing.T, files []resolver.FileIndex, rootName, from, name, recv string) resolver.Resolution {
	t.Helper()
	r := resolver.NewInRoot(files, rootName)
	for _, fi := range files {
		if string(fi.FileID) == from {
			return r.ResolveReference(reference.Reference{
				ID: reference.ReferenceID(from + "/" + recv + "." + name), Name: name, Kind: reference.KindCall, Language: "x",
				Location: source.Location{File: fi.FileID}, Container: "caller", ReceiverExpr: recv, IsCall: true,
			}, fi)
		}
	}
	t.Fatalf("no file %s", from)
	return resolver.Resolution{}
}

func target(res resolver.Resolution) string {
	if !res.HasUniqueTarget() {
		return res.Confidence.String()
	}
	return string(res.Candidates[0].SymbolID) + " " + res.Confidence.String()
}

var psRepo = []psFile{
	{path: "app/main.go", pkg: "app", decls: []string{"local", "Shared"}, imports: []language.ImportDraft{
		{Path: "example.com/m/lib"}, {Path: "example.com/m/internal/event"}, {Path: "example.com/m/yamlx"},
		{Path: "example.com/m/gen", Alias: "g"}, {Path: "example.com/m/dot", Alias: "."}, {Path: "fmt"},
	}},
	{path: "app/more.go", pkg: "app", decls: []string{"other"}},
	{path: "app/app_test.go", pkg: "app", decls: []string{"testHelper"}},
	{path: "app/ext_test.go", pkg: "app_test", decls: []string{"extOnly"}},
	{path: "lib/lib.go", pkg: "lib", decls: []string{"Run", "hidden", "T", "T.Method"}},
	{path: "lib/lib_test.go", pkg: "lib", decls: []string{"TestOnly"}},
	{path: "far/far.go", pkg: "far", decls: []string{"uniqueFar", "Unique"}},
	{path: "event/event.go", pkg: "event", decls: []string{"Log"}},
	{path: "internal/event/event.go", pkg: "event", decls: []string{"Log"}},
	{path: "yamlx/y.go", pkg: "yaml", decls: []string{"Parse"}},
	{path: "gen/gen.go", pkg: "gen", decls: []string{"Make"}},
	{path: "gen/tool.go", pkg: "main", decls: []string{"Make"}},
	{path: "dot/dot.go", pkg: "dot", decls: []string{"Dotted", "unexported"}},
}

func TestPackageScope_Names(t *testing.T) {
	files := psIndex(psRepo, true)
	for _, c := range []struct {
		name, from, ref, recv, want string
	}{
		{"same file", "app/main.go", "local", "", "app/main.go#local exact"},
		{"same package, other file", "app/main.go", "other", "", "app/more.go#other strong"},
		{"another directory, unique in the repository", "app/main.go", "uniqueFar", "", "unresolved"},
		{"another directory, exported and unique", "app/main.go", "Unique", "", "unresolved"},
		{"external test package of the same directory", "app/main.go", "extOnly", "", "unresolved"},
		{"a non-test file never sees test files", "app/main.go", "testHelper", "", "unresolved"},
		{"a test file sees its package", "app/app_test.go", "other", "", "app/more.go#other strong"},
		{"dot import, exported", "app/main.go", "Dotted", "", "dot/dot.go#Dotted exact"},
		{"dot import, unexported", "app/main.go", "unexported", "", "unresolved"},
		{"import", "app/main.go", "Run", "lib", "lib/lib.go#Run exact"},
		{"import, unexported", "app/main.go", "hidden", "lib", "unresolved"},
		{"import, test file", "app/main.go", "TestOnly", "lib", "unresolved"},
		{"import, member", "app/main.go", "Method", "lib", "unresolved"},
		{"longest directory wins", "app/main.go", "Log", "event", "internal/event/event.go#Log exact"},
		{"implicit name is the package's", "app/main.go", "Parse", "yaml", "yamlx/y.go#Parse exact"},
		{"explicit alias over a directory of two packages", "app/main.go", "Make", "g", "candidate"},
		{"package outside the repository", "app/main.go", "Println", "fmt", "unresolved"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := psResolve(t, files, "m", c.from, c.ref, c.recv)
			if got := target(res); got != c.want {
				t.Errorf("got %q, want %q (evidence %v)", got, c.want, res.Evidence)
			}
		})
	}
	if res := psResolve(t, files, "m", "app/main.go", "Println", "fmt"); !res.OutsideRepository {
		t.Error("an import of a non-repository package is not OutsideRepository")
	}
}

// An import path maps to a repository directory only under a prefix the
// repository establishes: two or more import paths, or the only candidate
// prefix ending in the root directory's name.
func TestPackageScope_ImportPrefixEvidence(t *testing.T) {
	single := []psFile{
		{path: "cmd/main.go", pkg: "main", imports: []language.ImportDraft{{Path: "github.com/u/repo/lib"}}},
		{path: "lib/lib.go", pkg: "lib", decls: []string{"Run"}},
	}
	files := psIndex(single, true)
	if got := target(psResolve(t, files, "repo", "cmd/main.go", "Run", "lib")); got != "lib/lib.go#Run exact" {
		t.Errorf("single import under the root's name: %s", got)
	}
	// Root named otherwise: no prefix is established, so the import names no
	// directory — but it ends in the repository's lib/, so it is not proven
	// external either (a renamed checkout): Unresolved, without the
	// OutsideRepository claim.
	if res := psResolve(t, files, "elsewhere", "cmd/main.go", "Run", "lib"); res.HasUniqueTarget() || res.OutsideRepository {
		t.Errorf("single import, root named otherwise: %s (outside %v)", target(res), res.OutsideRepository)
	}
	// A coincidence: an external package whose last element names a
	// repository directory, next to one internal import.
	coincidence := []psFile{
		{path: "cmd/main.go", pkg: "main", imports: []language.ImportDraft{{Path: "github.com/u/repo/lib"}, {Path: "github.com/ext/errors"}}},
		{path: "lib/lib.go", pkg: "lib", decls: []string{"Run"}},
		{path: "errors/errors.go", pkg: "errors", decls: []string{"Wrap"}},
	}
	files = psIndex(coincidence, true)
	if res := psResolve(t, files, "repo", "cmd/main.go", "Wrap", "errors"); res.HasUniqueTarget() {
		t.Errorf("an external import was mapped into the repository: %s", target(res))
	}
}

// A module's root package is imported by its module path, the prefix
// itself: with the root package named after the path's last element, that
// import is one of the two paths establishing the prefix, whatever the
// checkout directory is called. An import path nothing in the repository
// could be stays OutsideRepository; one that could (it ends in a repository
// directory, or names the root package) but no prefix establishes is
// Unresolved without that claim.
func TestPackageScope_RootPackageImport(t *testing.T) {
	module := []psFile{
		{path: "cobra.go", pkg: "cobra", decls: []string{"Command"}},
		{path: "doc/doc.go", pkg: "doc", decls: []string{"Gen"}, imports: []language.ImportDraft{{Path: "github.com/spf13/cobra"}}},
		{path: "cmd/main.go", pkg: "main", imports: []language.ImportDraft{{Path: "github.com/spf13/cobra"}, {Path: "github.com/spf13/cobra/doc"}, {Path: "fmt"}}},
	}
	files := psIndex(module, true)
	for _, root := range []string{"cobra", "cobra@v1.9.1", "fork"} {
		if got := target(psResolve(t, files, root, "cmd/main.go", "Command", "cobra")); got != "cobra.go#Command exact" {
			t.Errorf("root %q: root package import: %s", root, got)
		}
		if got := target(psResolve(t, files, root, "cmd/main.go", "Gen", "doc")); got != "doc/doc.go#Gen exact" {
			t.Errorf("root %q: subpackage import: %s", root, got)
		}
		if res := psResolve(t, files, root, "cmd/main.go", "Println", "fmt"); !res.OutsideRepository {
			t.Errorf("root %q: fmt is not OutsideRepository", root)
		}
	}
	// Only the root import: no second path establishes the prefix.
	alone := []psFile{
		{path: "cobra.go", pkg: "cobra", decls: []string{"Command"}},
		{path: "doc/doc.go", pkg: "doc", imports: []language.ImportDraft{{Path: "github.com/spf13/cobra"}}},
	}
	files = psIndex(alone, true)
	res := psResolve(t, files, "cobra@v1.9.1", "doc/doc.go", "Command", "cobra")
	if res.HasUniqueTarget() || res.OutsideRepository {
		t.Errorf("unestablished root import: %s (outside %v)", target(res), res.OutsideRepository)
	}
}

// Paths without a host (no dot in their first element) are the standard
// library's as much as a module's: two of them sharing a prefix by
// coincidence (encoding/json, encoding/xml beside repository directories
// json/ and xml/), or one naming the root package's name (log, with a root
// package log), map nothing into the repository.
func TestPackageScope_StandardLibraryPathsMapNothing(t *testing.T) {
	repo := []psFile{
		{path: "log.go", pkg: "log", decls: []string{"Println"}},
		{path: "syslog/syslog.go", pkg: "syslog", decls: []string{"New"}},
		{path: "json/json.go", pkg: "json", decls: []string{"Marshal"}},
		{path: "xml/xml.go", pkg: "xml", decls: []string{"Marshal"}},
		{path: "cmd/app/main.go", pkg: "main", imports: []language.ImportDraft{
			{Path: "log"}, {Path: "log/syslog"}, {Path: "encoding/json"}, {Path: "encoding/xml"}}},
	}
	files := psIndex(repo, true)
	for _, c := range []struct{ name, recv string }{{"Println", "log"}, {"New", "syslog"}, {"Marshal", "json"}, {"Marshal", "xml"}} {
		if res := psResolve(t, files, "mylog", "cmd/app/main.go", c.name, c.recv); res.HasUniqueTarget() {
			t.Errorf("%s.%s resolved into the repository: %s", c.recv, c.name, target(res))
		}
	}
	// A module named without a host, checked out under its own name.
	local := []psFile{
		{path: "util/util.go", pkg: "util", decls: []string{"Do"}},
		{path: "main.go", pkg: "main", imports: []language.ImportDraft{{Path: "myapp/util"}}},
	}
	files = psIndex(local, true)
	if got := target(psResolve(t, files, "myapp", "main.go", "Do", "util")); got != "util/util.go#Do exact" {
		t.Errorf("hostless module under its own name: %s", got)
	}
}

// The same repository without PackageScoped keeps the name-based stages:
// package scoping changes nothing for other files.
func TestPackageScope_OnlyForPackageScopedFiles(t *testing.T) {
	files := psIndex(psRepo, false)
	if got := target(psResolve(t, files, "m", "app/main.go", "uniqueFar", "")); got != "far/far.go#uniqueFar strong" {
		t.Errorf("unscoped file: %s", got)
	}
}

// A package-scoped file reaches only package-scoped files: a declaration of a
// file without package scoping (another language) is never its target, in its
// own directory, in an imported directory, or as the repository's only
// declaration of the name. The other file's own resolution is unchanged.
func TestPackageScope_OtherFilesNeverInScope(t *testing.T) {
	files := psIndex(psRepo, true)
	for _, other := range []psFile{
		{path: "app/view.ts", decls: []string{"Widget"}},    // same directory
		{path: "lib/client.js", decls: []string{"Greeter"}}, // imported directory, exported
		{path: "php/Caller.php", decls: []string{"Caller"}}, // only declaration of the name
		{path: "dot/extra.js", decls: []string{"DotLike"}},  // dot-imported directory
	} {
		fi := psIndex([]psFile{other}, false)[0]
		fi.Language = "y"
		files = append(files, fi)
	}
	for _, c := range []struct{ ref, recv string }{
		{"Widget", ""}, {"Greeter", "lib"}, {"Caller", ""}, {"DotLike", ""},
	} {
		if got := target(psResolve(t, files, "m", "app/main.go", c.ref, c.recv)); got != "unresolved" {
			t.Errorf("%s.%s from a package-scoped file: %s", c.recv, c.ref, got)
		}
	}
	// Package scoping does not change how the other file resolves.
	if got := target(psResolve(t, files, "m", "php/Caller.php", "Caller", "")); got != "php/Caller.php#Caller exact" {
		t.Errorf("unscoped file: %s", got)
	}
}
