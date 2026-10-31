package maven

import (
	"testing"

	"github.com/magicdrive/ark/internal/languages/xml/xmldoc"
)

// TestExpand: properties the file defines are replaced, once each
// (a value is never expanded again); others keep their ${…}.
func TestExpand(t *testing.T) {
	x := &extractor{props: map[string]string{"a": "1", "b": "${a}"}}
	for in, want := range map[string]string{
		"x${a}y":       "x1y",
		"${b}":         "${a}",
		"${c}-${a}":    "${c}-1",
		"${unclosed":   "${unclosed",
		"plain":        "plain",
		"${a}${a}${a}": "111",
	} {
		if got := x.expand(in); got != want {
			t.Errorf("expand(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRelativePath: only plain relative paths join.
func TestRelativePath(t *testing.T) {
	for _, c := range []struct {
		dir, rel, want string
		ok             bool
	}{
		{"a/b", "../c/pom.xml", "a/c/pom.xml", true},
		{"", "../pom.xml", "../pom.xml", true},
		{"a", "./x/./y.xml", "a/x/y.xml", true},
		{"a", "/etc/passwd", "", false},
		{"a", "C:/x.xml", "", false},
		{"a", "http://h/x.xml", "", false},
		{"a", `x\y.xml`, "", false},
		{"a", "x.xml#f", "", false},
		{"a", "${dir}/pom.xml", "", false},
		{"a", "", "", false},
	} {
		got, ok := RelativePath(c.dir, c.rel)
		if got != c.want || ok != c.ok {
			t.Errorf("RelativePath(%q, %q) = %q, %v", c.dir, c.rel, got, ok)
		}
	}
}

// TestCoordinates: a project's groupId and version are its parent's when
// it does not write them; a project without coordinates is "::project".
func TestCoordinates(t *testing.T) {
	r, ok := Extract("p/pom.xml", xmldoc.Parse([]byte(`<project><parent><groupId>g</groupId><artifactId>pa</artifactId><version>7</version></parent><artifactId>a</artifactId></project>`)))
	if !ok || r.Project != "p/pom.xml::g:a" || r.Symbols[0].Signature != "project g:a:7" {
		t.Errorf("%v %+v", ok, r)
	}
	r, _ = Extract("pom.xml", xmldoc.Parse([]byte(`<project><artifactId>a b</artifactId></project>`)))
	if r.Project != "pom.xml::project" || len(r.Aliases) != 0 {
		t.Errorf("%+v", r)
	}
}

// TestModuleSpan: a module's reference is at its name, without the
// blanks around it.
func TestModuleSpan(t *testing.T) {
	src := "<project><artifactId>a</artifactId><modules><module>\n  core \n</module></modules></project>"
	r, _ := Extract("pom.xml", xmldoc.Parse([]byte(src)))
	for _, ref := range r.Refs {
		if ref.Name == "core" && src[ref.Span.Start:ref.Span.End] != "core" {
			t.Errorf("module at %q", src[ref.Span.Start:ref.Span.End])
		}
	}
}
