package javascript_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/repomap"
)

// The script symbol of a JavaScript file (the target of an HTML <script
// src>) is reached by path evidence only: it takes no name, no container
// and no parent from the program's declarations, and no place in an
// overview's listing.

func indexOf(t *testing.T, files map[string]string) (*index.RepositoryIndex, string) {
	t.Helper()
	root := t.TempDir()
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	return idx, root
}

func edges(idx *index.RepositoryIndex) []string {
	var out []string
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				out = append(out, fmt.Sprintf("%s(%s) -%s-> %s", s.Qualified, s.Kind, e.Kind, to.Qualified))
			}
		}
	}
	sort.Strings(out)
	return out
}

// A member whose qualified name is the file's path (a method js of app, in
// app.js) keeps its edges and its parent.
func TestScriptSymbol_MemberOfTheFilesName(t *testing.T) {
	idx, _ := indexOf(t, map[string]string{
		"app.js": "const app = {\n  js() { helper(); },\n  n: 1,\n};\napp.js();\nfunction helper() {}\n",
		"Foo.js": "class Foo {\n  js() { this.k(); }\n  k() {}\n}\n",
	})
	got := strings.Join(edges(idx), "\n")
	for _, want := range []string{"app.js(method) -calls-> helper", "Foo.js(method) -calls-> Foo.k"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, s := range idx.FindSymbolsByQualified("Foo.k") {
		if p, ok := idx.GetSymbol(s.Parent); !ok || p.Kind != "class" {
			t.Errorf("parent of Foo.k: %+v", p)
		}
	}
}

// An import specifier naming a file ("b.js", a package chart.js) is no name
// of the script: nothing counts toward it.
func TestScriptSymbol_NoName(t *testing.T) {
	idx, _ := indexOf(t, map[string]string{
		"lib/b.js": "export function b() {}\n",
		"lib/c.js": "import 'b.js'\nimport x from 'b.js'\nconst y = require('b.js')\nconst z = require('./b.js')\n",
	})
	for _, s := range idx.FindSymbolsByQualified("lib/b.js") {
		if in, _ := idx.Unattributed(s.ID); in != 0 || len(idx.GetCallers(s.ID)) != 0 {
			t.Errorf("the script b.js has %d unattributed callers, %d callers", in, len(idx.GetCallers(s.ID)))
		}
	}
}

// An overview lists a script's declarations, not the script.
func TestScriptSymbol_NotListed(t *testing.T) {
	files := map[string]string{
		"p1.html": `<script src="js/a.js"></script>`,
		"p2.html": `<script src="js/a.js"></script>`,
	}
	for i := range 6 {
		files[fmt.Sprintf("js/f%d.js", i)] = fmt.Sprintf("export function fn%d() {}\nexport class C%d {}\n", i, i)
	}
	files["js/a.js"] = "export function a() {}\n"
	idx, root := indexOf(t, files)
	m := repomap.Build(idx, root, repomap.Options{})
	for _, p := range m.Packages {
		for _, s := range p.Symbols {
			if s.Kind == "script" {
				t.Errorf("%s lists the script %s", p.Path, s.Name)
			}
		}
	}
}
