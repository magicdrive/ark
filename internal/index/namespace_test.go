package index_test

import (
	"context"
	"path"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/asset"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// A repository of every language, where names are shared only by accident:
// no edge, candidate or unidentified source joins two name spaces by a name,
// whatever the rule; the languages' own resolutions — TSX importing
// TypeScript included — stay. The one relation that joins two name spaces
// is explicit path evidence (language.ReferenceDraft.FileIdentity): a links
// edge to the declaration of a file itself (symbol.DeclaresFile), an HTML
// document's stylesheet or script, and a reference to a declaration a path
// names in its file (symbol.FileScoped), a CSS Modules class a TypeScript
// file uses; candidates and unidentified sources never cross.
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
	"web/util.ts":      "export function util(): number { return 2 }\n",
	"web/s.module.css": ".tsOnly {} .GoOnly {}\n",
	"web/styled.ts":    "import s from './s.module.css'\nexport function styled() { return [s.tsOnly, GoOnly] }\n",
	"web/view.tsx":     "import { main } from './app'\nexport function View() { main(); return <div /> }\n",
	"w/index.html": `<p id=GoOnly></p><a href="#GoOnly">g</a><label for=tsOnly>t</label><p aria-describedby="jsLocal run">x</p>
<a href="../g/g.go">go</a><a href="../web/app.ts">ts</a><a href="page.html#run">run</a>
<link rel=stylesheet href=site.css><script src="../j/a.js"></script><script src="../web/app.ts"></script>
`,
	"w/page.html": "<h1 id=run>R</h1><a href=\"index.html\">i</a><img src=\"logo.png\"><img src=\"GoOnly.png\">\n",
	"w/logo.png":  "",
	"w/run.png":   "",
	"w/site.css":  "@import \"base.css\";\n@keyframes GoOnly {}\n.run { animation-name: GoOnly, tsOnly, run; font-family: GoType; color: var(--jsLocal); }\n",
	"w/base.css":  ":root { --jsLocal: red; }\n",
}

func TestNameSpaces_NoRelationJoinsTwoNameSpaces(t *testing.T) {
	root := writeFiles(t, mixedRepo)
	reg := languages.Registry()
	idx, err := index.New(context.Background(), root, reg.Providers())
	if err != nil {
		t.Fatal(err)
	}
	spaceOfFile := func(f source.FileID) string {
		if asset.Category(path.Ext(string(f))) != "" {
			return asset.Language
		}
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
	for _, s := range idx.Assets() {
		byID[s.ID] = s
	}
	space := func(id symbol.SymbolID) string { return spaceOfFile(byID[id].Location.File) }
	cross := func(what string, s symbol.Symbol, other string) {
		if other != spaceOfFile(s.Location.File) {
			t.Errorf("%s joins %s (%s) and the %s name space", what, s.Qualified, s.Location.File, other)
		}
	}
	// pathEvidence: a links relation to the declaration of a file itself, or
	// a reference to a declaration a path names in its file.
	pathEvidence := func(kind index.EdgeKind, refKind reference.ReferenceKind, to symbol.SymbolID) bool {
		target := byID[to]
		return (kind == index.EdgeLinks || refKind == reference.KindResourceLink) &&
			symbol.DeclaresFile(target.Kind) && target.Qualified == string(target.Location.File) ||
			refKind == reference.KindValueReference && symbol.FileScoped(target.Kind) &&
				strings.HasPrefix(target.Qualified, string(target.Location.File)+" ")
	}
	crossings := 0
	for _, s := range byID {
		for _, e := range idx.GetCallees(s.ID) {
			if pathEvidence(e.Kind, e.RefKind, e.To) && space(e.To) != spaceOfFile(s.Location.File) {
				crossings++
				continue
			}
			cross("edge", s, space(e.To))
		}
		for _, e := range idx.GetCallers(s.ID) {
			if pathEvidence(e.Kind, e.RefKind, s.ID) {
				continue
			}
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
	edge("p/a.py", "run", "p/b.py", "helper", resolver.ConfidenceExact) // a relative import binding
	edge("j/a.js", "run", "j/a.js", "jsLocal", resolver.ConfidenceExact)
	edge("g/g.go", "Use", "g/g.go", "helperGo", resolver.ConfidenceExact)
	edge("w/index.html", "w/index.html", "w/page.html", "w/page.html#run", resolver.ConfidenceStrong)      // a link to another document
	edge("w/index.html", "w/index.html", "w/index.html", "w/index.html#GoOnly", resolver.ConfidenceStrong) // a document with a script
	edge("w/site.css", "w/site.css", "w/site.css", "w/site.css @keyframes GoOnly", resolver.ConfidenceStrong)
	edge("w/site.css", "w/site.css", "w/base.css", "w/base.css", resolver.ConfidenceStrong) // @import
	// Path evidence joins name spaces: an HTML document's stylesheet and
	// script, and no other relation (a .ts is no script a browser loads).
	edge("w/index.html", "w/index.html", "w/site.css", "w/site.css", resolver.ConfidenceStrong)
	edge("w/index.html", "w/index.html", "j/a.js", "j/a.js", resolver.ConfidenceStrong)
	edge("web/styled.ts", "styled", "web/s.module.css", "web/s.module.css .tsOnly", resolver.ConfidenceStrong)
	if crossings != 4 {
		t.Errorf("%d relations join two name spaces, want the 4 of path evidence", crossings)
	}
	// An asset is reached by its path only: logo.png by the <img>, never
	// run.png (a name the document's id and functions share).
	page := symbolIn(t, idx, "w/page.html", "w/page.html")
	var assetEdges []string
	for _, e := range idx.GetCallees(page.ID) {
		if to := byID[e.To]; to.Kind == symbol.KindAsset {
			assetEdges = append(assetEdges, to.Qualified+" "+e.Confidence.String())
		}
	}
	if len(assetEdges) != 1 || assetEdges[0] != "w/logo.png strong" {
		t.Errorf("asset edges of w/page.html: %q", assetEdges)
	}

	// The names only another language declares stay unresolved.
	goOnly := symbolIn(t, idx, "g/g.go", "GoOnly")
	if n := len(idx.GetCallers(goOnly.ID)) + idx.CandidateCallerSample(goOnly.ID).Total; n != 0 {
		t.Errorf("GoOnly has %d callers from other languages", n)
	}
}
