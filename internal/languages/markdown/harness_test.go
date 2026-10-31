package markdown

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/source"
)

func extract(t testing.TB, file, src string) language.Extraction {
	t.Helper()
	x, err := NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return x
}

func pos(l source.Location) string {
	r := l.Range
	return fmt.Sprintf("%d:%d-%d:%d", r.Start.Line, r.Start.Column, r.End.Line, r.End.Column)
}

// symbolLines renders each declaration as "kind qualified name parent sig @range".
func symbolLines(x language.Extraction) []string {
	var out []string
	for _, s := range x.Symbols {
		out = append(out, fmt.Sprintf("%s %s %q parent=%s sig=%s @%s", s.Kind, s.Qualified, s.Name, s.Parent, s.Signature, pos(s.Location)))
	}
	return out
}

// refLines renders each reference as "kind name -> qualified [flags] in container @range".
func refLines(x language.Extraction) []string {
	var out []string
	for _, r := range x.References {
		var flags []string
		if r.FileIdentity {
			flags = append(flags, "file")
		}
		if r.IdentityInRepository {
			flags = append(flags, "repo")
		}
		if r.ConfidenceCap != "" {
			flags = append(flags, "cap="+r.ConfidenceCap)
		}
		if r.TargetKinds != "" {
			flags = append(flags, "kinds="+r.TargetKinds)
		}
		out = append(out, fmt.Sprintf("%s %q -> %s [%s] in %s @%s", r.Kind, r.Name, r.NameQualified, strings.Join(flags, " "), r.Container, pos(r.Location)))
	}
	return out
}

func equalLines(t testing.TB, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func isName(s string) bool { return declname.IsName(s) }
