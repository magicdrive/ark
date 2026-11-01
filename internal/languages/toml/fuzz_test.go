package toml

import (
	"context"
	"reflect"
	"testing"

	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractTOML: on any input, as a Cargo manifest (also with Windows
// file IDs), a pyproject.toml and a data file, extraction never panics or
// fails, every range lies within the source and is ordered, a symbol's
// location is its bytes', every name and identity is a one-line name, no
// two declarations share an identity, every reference's container — and
// every contains' target — is a declaration of the file, and a second
// extraction is the same.
func FuzzExtractTOML(f *testing.F) {
	for _, s := range []string{
		"[package]\nname = \"a\"\n[dependencies]\nb = { path = \"../b\" }\nc.workspace = true\n[target.'cfg(unix)'.dependencies]\nd = \"1\"\n[features]\nx = [\"dep:d\", \"b/y\", \"x\"]\n",
		"[workspace]\nmembers = [\"a\", \"b/*\", \"../c\"]\n[workspace.dependencies]\ne = \"1\"\n",
		"[project]\nname = \"p\"\ndependencies = [\"a>=1\", \"A; x\", \">=\"]\n[build-system]\nrequires = [\"s\"]\n",
		"[[bin]]\nname = \"a\"\n[[bin]]\n[[bin]]\nname = \"a\"\n[bin.x]\ny = 1\n",
		"a.b.c = 1\na.b.d = {e = [1, {f = 2}], g.h = 3}\n\"~/\" = \"\"\"\nx\"\"\"\n",
		"a = 1\na = 2\n[t]\n[t]\n[x\n",
		"\"\\u202e\" = 1\r\nk = 1979-05-27T00:00:00+12:60\r\n",
		"x = {a = 1,\n b = 2,}\ne = \"\\e\"\n",
	} {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<16 {
			return
		}
		for _, file := range []string{"crates/a/Cargo.toml", `crates\a\Cargo.toml`, "py/pyproject.toml", "data.toml"} {
			ex, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				t.Fatal(err)
			}
			ls := newLines(src)
			qualified := map[string]bool{}
			for _, s := range ex.Symbols {
				if qualified[s.Qualified] {
					t.Fatalf("%s: two declarations %s", file, s.Qualified)
				}
				qualified[s.Qualified] = true
				if int(s.StartByte) > int(s.EndByte) || int(s.EndByte) > len(src) {
					t.Fatalf("%s: symbol %s at [%d,%d) of %d bytes", file, s.Qualified, s.StartByte, s.EndByte, len(src))
				}
				if s.Location.Range.Start != ls.pos(int(s.StartByte)) || s.Location.Range.End != ls.pos(int(s.EndByte)) {
					t.Fatalf("%s: symbol %s location %v is not its bytes'", file, s.Qualified, s.Location.Range)
				}
				if !declname.IsName(s.Name) || !declname.IsName(s.Qualified) {
					t.Fatalf("%s: symbol %q / %q is no name", file, s.Name, s.Qualified)
				}
			}
			for _, r := range ex.References {
				rg := r.Location.Range
				if rg.Start.Line > rg.End.Line || rg.Start.Line == rg.End.Line && rg.Start.Column > rg.End.Column {
					t.Fatalf("%s: reference %s range %v", file, r.Name, rg)
				}
				if !qualified[r.Container] {
					t.Fatalf("%s: reference %s in %q, no declaration of the file", file, r.Name, r.Container)
				}
				// A structural contains is named by its target's identity
				// (a workspace's member by its path).
				if r.Kind == "contains" && r.Name == r.NameQualified && !qualified[r.NameQualified] {
					t.Fatalf("%s: contains of %s, no declaration of the file", file, r.NameQualified)
				}
				if !declname.IsName(r.Name) {
					t.Fatalf("%s: reference name %q", file, r.Name)
				}
			}
			for _, d := range ex.Diagnostics {
				if d.Code == "" {
					t.Fatalf("%s: diagnostic without a code: %s", file, d.Message)
				}
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ex, again) {
				t.Fatalf("%s: two extractions differ", file)
			}
		}
	})
}
