package treediag

import (
	"strings"
	"testing"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func parse(t *testing.T, src string) *ts.Node {
	t.Helper()
	tree, err := ts.NewParser(grammars.GoLanguage()).Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)
	return tree.RootNode()
}

func TestParseErrors_ValidSourceHasNone(t *testing.T) {
	if got := ParseErrors(parse(t, "package a\n\nfunc A() { B() }\nfunc B() {}\n"), grammars.GoLanguage(), "a.go"); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestParseErrors_BoundedAndClassified(t *testing.T) {
	src := "package a\n" + strings.Repeat("func ( {\n}\n", 40)
	got := ParseErrors(parse(t, src), grammars.GoLanguage(), "a.go")
	if len(got) == 0 || len(got) > MaxParseErrors {
		t.Fatalf("%d diagnostics, want 1..%d", len(got), MaxParseErrors)
	}
	for _, d := range got {
		if d.Code != CodeParseError || d.Severity != "error" || d.Location.File != "a.go" || d.Location.Range.Start.Line == 0 {
			t.Errorf("%+v", d)
		}
		if !strings.Contains(d.Message, "may be valid") {
			t.Errorf("message claims the source is invalid: %q", d.Message)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].Location.Range.Start.Line < got[i-1].Location.Range.Start.Line {
			t.Errorf("not in source order: %+v", got)
		}
	}
}
