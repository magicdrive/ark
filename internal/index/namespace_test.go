package index_test

import (
	"context"
	"path"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// A repository of every language, where names are shared only by accident:
// no edge, candidate or unidentified source joins two name spaces, whatever
// the rule; the languages' own resolutions — TSX importing TypeScript
// included — stay.
var mixedRepo = map[string]string{
	"go.mod": "module example.com/mix\n\ngo 1.22\n",
	"g/g.go": `package g

func GoOnly() {}

type GoType struct{}

func helperGo() {}

func Use(x interface{ jsMethod() }) {
	helperGo()
	x.jsMethod()
	x.phpMethod()
}
`,
	"p/a.py": "from .b import helper\n\ndef run():\n    GoOnly()\n    GoType()\n    helper()\n    tsOnly()\n",
	"p/b.py": "def helper():\n    return 1\n",
	"j/a.js": `class Widget { jsMethod() { return GoOnly() } }
function run() { GoOnly(); new GoType(); tsOnly(); jsLocal() }
function jsLocal() { return new Widget().jsMethod() }
module.exports = { run }
`,
	"h/a.php": `<?php
namespace App;

class Svc {
    public function phpMethod(): void { GoOnly(); }
}

function run(): void { GoOnly(); new GoType(); jsLocal(); }
`,
	"t/main.tf": `resource "aws_s3_bucket" "GoOnly" { bucket = "x" }
output "go_only" { value = aws_s3_bucket.GoOnly.id }
`,
	"web/app.ts": `import { util } from './util'
export function main(): void { util(); jsLocal(); GoOnly() }
export function tsOnly(): number { return 1 }
`,
	"web/util.ts":  "export function util(): number { return 2 }\n",
	"web/view.tsx": "import { main } from './app'\nexport function View() { main(); return <div /> }\n",
}

func TestNameSpaces_NoRelationJoinsTwoNameSpaces(t *testing.T) {
	root := writeFiles(t, mixedRepo)
	reg := languages.Registry()
	idx, err := index.New(context.Background(), root, reg.Providers())
	if err != nil {
		t.Fatal(err)
	}
	spaceOfFile := func(f source.FileID) string {
		d, ok := reg.DetectByExtension(path.Ext(string(f)))
		if !ok {
			t.Fatalf("no language for %s", f)
		}
		return string(language.NameSpace(d.Provider))
	}
	byID := map[symbol.SymbolID]symbol.Symbol{}
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			byID[s.ID] = s
		}
	}
	space := func(id symbol.SymbolID) string { return spaceOfFile(byID[id].Location.File) }
	cross := func(what string, s symbol.Symbol, other string) {
		if other != spaceOfFile(s.Location.File) {
			t.Errorf("%s joins %s (%s) and the %s name space", what, s.Qualified, s.Location.File, other)
		}
	}
	for _, s := range byID {
		for _, e := range idx.GetCallees(s.ID) {
			cross("edge", s, space(e.To))
		}
		for _, e := range idx.GetCallers(s.ID) {
			cross("edge", s, space(e.To))
		}
		for _, sample := range []index.CandidateSample{idx.CandidateCalleeSample(s.ID), idx.CandidateCallerSample(s.ID)} {
			for _, c := range sample.Relations {
				cross("candidate", s, space(c.Symbol))
			}
		}
		u := idx.UnidentifiedSources(s.ID)
		for _, c := range append(u.Resolved, u.Candidates...) {
			cross("unidentified source", s, spaceOfFile(c.File))
		}
	}

	// Each language keeps its own resolutions.
	edge := func(fromFile source.FileID, from string, toFile source.FileID, to string, conf resolver.Confidence) {
		t.Helper()
		src := symbolIn(t, idx, fromFile, from)
		dst := symbolIn(t, idx, toFile, to)
		for _, e := range idx.GetCallees(src.ID) {
			if e.To == dst.ID {
				if e.Confidence != conf {
					t.Errorf("%s → %s: %s, want %s", from, to, e.Confidence, conf)
				}
				return
			}
		}
		t.Errorf("edge %s → %s lost", from, to)
	}
	edge("web/view.tsx", "View", "web/app.ts", "main", resolver.ConfidenceExact) // TSX imports TypeScript
	edge("web/app.ts", "main", "web/util.ts", "util", resolver.ConfidenceExact)
	edge("p/a.py", "run", "p/b.py", "helper", resolver.ConfidenceStrong) // same directory
	edge("j/a.js", "run", "j/a.js", "jsLocal", resolver.ConfidenceExact)
	edge("g/g.go", "Use", "g/g.go", "helperGo", resolver.ConfidenceExact)

	// The names only another language declares stay unresolved.
	goOnly := symbolIn(t, idx, "g/g.go", "GoOnly")
	if n := len(idx.GetCallers(goOnly.ID)) + idx.CandidateCallerSample(goOnly.ID).Total; n != 0 {
		t.Errorf("GoOnly has %d callers from other languages", n)
	}
}
