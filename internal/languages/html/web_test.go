package html_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/source"
)

// Across languages (Phase 7): a page's stylesheets and scripts are linked by
// path — the one relation between two languages — and a shared name is no
// relation at all.

var webSite = map[string]string{
	"site/index.html": strings.Join([]string{
		`<link rel="stylesheet" href="css/main.css"><link rel="alternate stylesheet" href="css/alt.css">`, // 1
		`<script src="js/app.js"></script><script type="module" src="./js/mod.mjs"></script>`,             // 2
		`<link rel="modulepreload" href="js/mod.mjs"><link rel="preload" href="css/main.css" as="style">`, // 3
		`<script type="text/template" src="js/app.js"></script><script src="js/app.ts"></script>`,         // 4
		`<script src="/js/app.js"></script><script src="https://cdn/x.js"></script><link rel=stylesheet href="missing.css">`,
		`<p id="run">x</p><p id="spin">y</p>`, // 6: names other languages declare too
	}, "\n"),
	"site/css/main.css": "@keyframes run {} @keyframes spin {}\n.a { animation-name: run; }\n",
	"site/css/alt.css":  "",
	"site/js/app.js":    "function run() {}\nfunction spin() { run() }\n",
	"site/js/mod.mjs":   "export function run() {}\n",
	"site/js/app.ts":    "export function run(): void {}\n",
	"base.html":         `<base href="/x/"><link rel=stylesheet href="site/css/main.css">`,
}

func writeSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range webSite {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// relations lists every edge "from -kind-> to confidence", and every
// reference of the HTML files with its resolution.
func relations(idx *index.RepositoryIndex) []string {
	var out []string
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				out = append(out, fmt.Sprintf("%s -%s-> %s (%s) %s", s.Qualified, e.Kind, to.Qualified, to.Kind, e.Confidence))
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestWebResourceLinks(t *testing.T) {
	root := writeSite(t)
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	got := relations(idx)
	want := []string{
		"site/css/main.css -references-> site/css/main.css @keyframes run (keyframes) strong",
		"site/index.html -links-> site/css/alt.css (stylesheet) strong",
		"site/index.html -links-> site/css/main.css (stylesheet) strong",
		"site/index.html -links-> site/js/app.js (script) strong",
		"site/index.html -links-> site/js/mod.mjs (script) strong",
		"spin -calls-> run (function) exact",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("relations\n got %q\nwant %q", got, want)
	}
	// The page's references: only the links above and the missing
	// stylesheet (unresolved, not outside); no name of another language.
	var links []string
	for _, r := range idx.ReferencesByFile(source.FileID("site/index.html")) {
		links = append(links, fmt.Sprintf("%d:%s", r.Location.Range.Start.Line, r.Name))
	}
	sort.Strings(links)
	wantLinks := []string{"1:alt.css", "1:main.css", "2:app.js", "2:mod.mjs", "3:mod.mjs", "5:missing.css"}
	if !reflect.DeepEqual(links, wantLinks) {
		t.Errorf("references of index.html %q, want %q", links, wantLinks)
	}
	doc := idx.FindSymbolsByQualified("site/index.html")
	if len(doc) != 1 {
		t.Fatalf("site/index.html: %d symbols", len(doc))
	}
	if s := idx.UnresolvedOutgoing(doc[0].ID); s.Unresolved != 1 || s.OutsideRepository != 0 {
		t.Errorf("unresolved outgoing of index.html: %+v, want the missing stylesheet only", s)
	}
	// No relation by a shared name: run and spin are an HTML id, a CSS
	// @keyframes and JavaScript and TypeScript functions.
	for _, r := range got {
		if strings.Contains(r, "#run") || strings.Contains(r, "#spin") {
			t.Errorf("a relation by a name: %s", r)
		}
	}
	// A page with <base> states no link.
	if refs := idx.ReferencesByFile("base.html"); len(refs) != 0 {
		t.Errorf("base.html: %d references", len(refs))
	}
}

func TestWebCacheColdWarm(t *testing.T) {
	root := writeSite(t)
	providers := languages.Registry().Providers()
	plain, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	want := relations(plain)
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"cold", "warm"} {
		idx, err := index.NewWithCache(context.Background(), root, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := relations(idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q, want %q", run, got, want)
		}
	}
}

// Reviewer B (Phase 7): a link only where the browser fetches the file.
func TestWebResourceLinks_Fetched(t *testing.T) {
	cases := map[string]bool{
		`<script src="a.js"></script>`:                                       true,
		`<script type="" src="a.js"></script>`:                               true,
		`<script type="text/JavaScript" src="a.js"></script>`:                true,
		`<script type=" text/javascript " src="a.js"></script>`:              true, // ASCII whitespace stripped
		`<script type="MODULE" src="a.js"></script>`:                         true,
		`<script language="JavaScript1.2" src="a.js"></script>`:              true,
		`<script language="" src="a.js"></script>`:                           true,
		`<script type="" language="vbscript" src="a.js"></script>`:           true,
		`<script nomodule src="a.js"></script>`:                              true, // read by older browsers
		`<script language="vbscript" src="a.js"></script>`:                   false,
		"<script type=\" text/javascript\" src=\"a.js\"></script>":           false, // NBSP is no ASCII whitespace
		"<script type=\"\vtext/javascript\" src=\"a.js\"></script>":          false,
		`<script type=" module " src="a.js"></script>`:                       false, // module: nothing stripped
		"<script type=\"text/plain\x00\" src=\"a.js\"></script>":             false, // an unverified type
		`<script type="text/javascript; charset=utf-8" src="a.js"></script>`: false,
		`<script type="importmap" src="a.js"></script>`:                      false,
		`<link rel=stylesheet href=a.css>`:                                   true,
		`<link rel=" StyleSheet	" href=a.css>`:                               true,
		`<link rel=stylesheet type="TEXT/CSS" href=a.css>`:                   true,
		`<link rel=stylesheet type="text/plain" href=a.css>`:                 false,
		`<link rel=stylesheet type="" href=a.css>`:                           false,
		`<link rel=stylesheet disabled href=a.css>`:                          false,
		`<link rel=preload rel=stylesheet href=a.css>`:                       false, // the first rel wins
		`<link rel=modulepreload href=a.js>`:                                 true,
		`<link rel=modulepreload as=worker href=a.js>`:                       true,
		`<link rel=modulepreload as=image href=a.js>`:                        false,
		`<link rel=stylesheet href="a.css?v=1#x">`:                           true,
		`<link rel=stylesheet href="">`:                                      false,
		`<link rel=stylesheet href="#">`:                                     false,
		`<link rel=stylesheet href="..\a.css">`:                              false,
		`<template><link rel=stylesheet href=a.css></template>`:              true, // the URL is the page's
		`<svg><script href="a.js"></script></svg>`:                           false,
		`<textarea><script src="a.js"></script></textarea>`:                  false,
	}
	for src, want := range cases {
		root := t.TempDir()
		for name, body := range map[string]string{"p.html": src, "a.js": "function f() {}\n", "a.css": ".a{}\n"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		idx, err := index.New(context.Background(), root, languages.Registry().Providers())
		if err != nil {
			t.Fatal(err)
		}
		got := false
		for _, r := range relations(idx) {
			got = got || strings.HasPrefix(r, "p.html -links->")
		}
		if got != want {
			t.Errorf("%q: link %v, want %v", src, got, want)
		}
	}
}
