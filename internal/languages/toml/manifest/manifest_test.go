package manifest

import (
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/languages/toml/tomldoc"
)

func TestPEP508Names(t *testing.T) {
	for in, want := range map[string]string{
		"requests":                             "requests",
		"requests>=2.31":                       "requests",
		"requests [security,tests] >= 2.8.1":   "requests",
		"name@ https://example.com/x.zip":      "name",
		"Foo.Bar_baz-qux ; python_version<'3'": "Foo.Bar_baz-qux",
		"  pip":                                "pip",
		"a":                                    "a",
		"zope.interface (>=4.0)":               "zope.interface",
		">=1.0":                                "",
		"-x":                                   "",
		"x-":                                   "x",
		"":                                     "",
	} {
		got := ""
		if m := pep508Name.FindStringSubmatch(in); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Foo.Bar_baz--qux": "foo-bar-baz-qux", "Regex": "regex", "a__b": "a-b"} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWithoutUserinfo(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:pass@example.com/org/r.git?token=abc#frag": "https://example.com/org/r.git",
		"https://example.com":         "https://example.com",
		"ssh://git@github.com/o/r":    "ssh://github.com/o/r",
		"git@github.com:o/r.git":      "github.com:o/r.git",
		"user:tok@github.com:o/r.git": "github.com:o/r.git",
		"https://a@b@example.com/x":   "https://example.com/x",
	} {
		if got := withoutUserinfo(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestRepoPath(t *testing.T) {
	r := &reader{dir: "crates/app"}
	for in, want := range map[string]string{
		"../core": "crates/core", ".": "crates/app", "x/../y": "crates/app/y", "../../x": "x",
		"../../../x": "", "/abs": "", `..\core`: "", "C:/x": "", "": "",
	} {
		got, ok := r.repoPath(in)
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	root := &reader{dir: "."}
	if got, _ := root.repoPath("crates/a"); got != "crates/a" {
		t.Errorf("root: %q", got)
	}
	if root.manifestIdentity(".", "Cargo.toml", "/package") != "Cargo.toml#/package" {
		t.Error("root manifest identity")
	}
}

// TestRead_NotAManifest: only Cargo.toml and pyproject.toml are read.
func TestRead_NotAManifest(t *testing.T) {
	d := tomldoc.Parse([]byte("[package]\nname = \"x\"\n"))
	id := func(*tomldoc.Node) string { return "x" }
	for _, f := range []string{"cargo.toml", "a/Cargo.toml.toml", "pyproject.TOML", "x.toml"} {
		if _, ok := Read(f, d, id); ok {
			t.Errorf("%s read as a manifest", f)
		}
	}
	for _, f := range []string{"Cargo.toml", `a\Cargo.toml`, "a/b/pyproject.toml"} {
		if _, ok := Read(f, d, id); !ok {
			t.Errorf("%s not read as a manifest", f)
		}
	}
}

// TestRead_NoNameNoPackage: a [package] without a one-line name is no
// package (its dependencies are still declared, with no owner).
func TestRead_NoNameNoPackage(t *testing.T) {
	for _, src := range []string{"[package]\nversion = \"1\"\n", "[package]\nname = \"a\\nb\"\n", "package = 1\n", "[package]\nname = 1\n"} {
		d := tomldoc.Parse([]byte(src + "[dependencies]\nx = \"1\"\n"))
		ids := map[*tomldoc.Node]string{}
		var walk func(n *tomldoc.Node, at string)
		walk = func(n *tomldoc.Node, at string) {
			for _, c := range n.Children {
				ids[c] = "Cargo.toml#" + at + "/" + c.Key
				walk(c, at+"/"+c.Key)
			}
		}
		walk(d.Root, "")
		r, _ := Read("Cargo.toml", d, func(n *tomldoc.Node) string { return ids[n] })
		for n, o := range r.Overrides {
			if o.Kind == "package" {
				t.Errorf("%q: package %q at %s", src, o.Name, ids[n])
			}
		}
		for _, x := range r.Refs {
			if strings.HasSuffix(x.Container, "#/package") {
				t.Errorf("%q: a reference of the package: %+v", src, x)
			}
		}
	}
}
