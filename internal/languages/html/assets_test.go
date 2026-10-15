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
	"github.com/magicdrive/ark/internal/languages/html"
	"github.com/magicdrive/ark/internal/source"
)

// Asset links (hardening Phase B): HTML and CSS name images, fonts and media
// by path; each is a node of the index (internal/asset), reached by path
// evidence only.

var assetSite = map[string]string{
	"website/index.html": strings.Join([]string{
		`<link rel="stylesheet" href="style.css"><script src="app.js"></script>`,                       // 1
		`<img src="assets/logo.svg" alt=""><img srcset="assets/hero.webp 1x, assets/hero@2x.webp 2x">`, // 2
		`<picture><source srcset="assets/hero.webp"><img src="assets/missing.png"></picture>`,          // 3
		`<video poster="assets/hero.webp" src="media/clip.mp4"><source src="media/clip.webm"><track src="media/subs.vtt"></video>`,
		`<audio src="media/a.mp3"></audio><input type="image" src="assets/logo.svg">`,                                                             // 5
		`<link rel="icon" href="assets/fav.ico"><link rel="preload" as="font" href="assets/font.woff2">`,                                          // 6
		`<svg><image href="assets/logo.svg"/><use href="assets/sprite.svg#icon"/><use href="#local"/></svg>`,                                      // 7
		`<img src="https://cdn.example/x.png"><img src="//cdn.example/y.png"><img src="/assets/logo.svg">`,                                        // 8
		`<img src="assets/logo.svg?v=3#frag"><img src="assets/%C3%A9t%C3%A9.png"><img src="assets/LOGO.SVG">`,                                     // 9
		`<img src="../outside.png"><img src="../../escape.png"><img src="assets/"><img src="data:image/png;base64,AAAA"><img src="assets/x.txt">`, // 10
		`<img src="assets/link.png"><source src="assets/hero.webp"><a href="assets/logo.svg">l</a>`,                                               // 11
		`<link rel="preload" as="script" href="assets/hero.webp"><link rel="prefetch" href="assets/logo.svg">`,                                    // 12
		`<style>.x { background: url(assets/hero.webp) }</style>`,                                                                                 // 13
	}, "\n"),
	"website/style.css": strings.Join([]string{
		`.a { background: url(assets/hero.webp); mask: url("assets/logo.svg") }`,                                // 1
		`@font-face { font-family: F; src: url(assets/font.woff2) format("woff2"), local("F") }`,                // 2
		`.b { background-image: image-set("assets/hero.webp" 1x, url(assets/hero@2x.webp) 2x) }`,                // 3
		`.c { --bg: url(assets/logo.svg); background: url(https://x/y.png), url(/root.png), url(missing.png) }`, // 4
		`.d { background: url(assets/hero.webp) }`,                                                              // 5
	}, "\n"),
	"website/app.js":               "export function run() {}\n",
	"website/assets/logo.svg":      "<svg/>",
	"website/assets/hero.webp":     "RIFF",
	"website/assets/hero@2x.webp":  "RIFF",
	"website/assets/font.woff2":    "wOF2",
	"website/assets/fav.ico":       "",
	"website/assets/sprite.svg":    "<svg/>",
	"website/assets/été.png":       "",
	"website/assets/x.txt":         "",
	"website/media/clip.mp4":       "",
	"website/media/clip.webm":      "",
	"website/media/subs.vtt":       "WEBVTT",
	"website/media/a.mp3":          "",
	"outside.png":                  "",
	"website/node_modules/m/i.png": "",
	"website/.hidden/h.png":        "",
}

func writeAssetSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range assetSite {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A symbolic link is no asset: where it leads is not known without
	// following it.
	if err := os.Symlink(filepath.Join(root, "outside.png"), filepath.Join(root, "website/assets/link.png")); err != nil {
		t.Fatal(err)
	}
	return root
}

// assetRelations lists the links to assets, "from line -> asset confidence",
// and the asset references with their resolution.
func assetRelations(t *testing.T, idx *index.RepositoryIndex) (edges []string, refs []string) {
	t.Helper()
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				if to.Kind == "asset" {
					edges = append(edges, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
				}
			}
		}
		for _, r := range idx.ReferencesByFile(f) {
			if r.TargetKinds == "asset" {
				refs = append(refs, fmt.Sprintf("%s:%d %s", f, r.Location.Range.Start.Line, r.NameQualified))
			}
		}
	}
	sort.Strings(edges)
	sort.Strings(refs)
	return edges, refs
}

func TestAssetLinks(t *testing.T) {
	root := writeAssetSite(t)
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	var assets []string
	for _, a := range idx.Assets() {
		assets = append(assets, a.Qualified+" "+a.Signature)
	}
	wantAssets := []string{
		"outside.png image",
		"website/assets/fav.ico image", "website/assets/font.woff2 font", "website/assets/hero.webp image",
		"website/assets/hero@2x.webp image", "website/assets/logo.svg image", "website/assets/sprite.svg image",
		"website/assets/été.png image",
		"website/media/a.mp3 media", "website/media/clip.mp4 media", "website/media/clip.webm media", "website/media/subs.vtt media",
	}
	if !reflect.DeepEqual(assets, wantAssets) {
		t.Errorf("assets\n got %q\nwant %q", assets, wantAssets)
	}
	for _, f := range idx.Files() {
		if strings.HasSuffix(string(f), ".png") || strings.HasSuffix(string(f), ".svg") {
			t.Errorf("an asset is a source file: %s", f)
		}
	}

	edges, refs := assetRelations(t, idx)
	wantEdges := []string{
		"website/index.html -links-> outside.png strong", // ../outside.png: the repository's root
		"website/index.html -links-> website/assets/fav.ico strong",
		"website/index.html -links-> website/assets/font.woff2 strong",
		"website/index.html -links-> website/assets/hero.webp strong",
		"website/index.html -links-> website/assets/hero@2x.webp strong",
		"website/index.html -links-> website/assets/logo.svg strong",
		"website/index.html -links-> website/assets/sprite.svg strong",
		"website/index.html -links-> website/assets/été.png strong",
		"website/index.html -links-> website/media/a.mp3 strong",
		"website/index.html -links-> website/media/clip.mp4 strong",
		"website/index.html -links-> website/media/clip.webm strong",
		"website/index.html -links-> website/media/subs.vtt strong",
		"website/style.css -links-> website/assets/font.woff2 strong",
		"website/style.css -links-> website/assets/hero.webp strong",
		"website/style.css -links-> website/assets/hero@2x.webp strong",
		"website/style.css -links-> website/assets/logo.svg strong",
	}
	if !reflect.DeepEqual(edges, wantEdges) {
		t.Errorf("edges\n got %q\nwant %q", edges, wantEdges)
	}
	wantRefs := []string{
		"website/index.html:10 outside.png",
		"website/index.html:11 website/assets/link.png",  // a symbolic link: no node, unresolved
		"website/index.html:13 website/assets/hero.webp", // inline style
		"website/index.html:2 website/assets/hero.webp",
		"website/index.html:2 website/assets/hero@2x.webp",
		"website/index.html:2 website/assets/logo.svg",
		"website/index.html:3 website/assets/hero.webp",
		"website/index.html:3 website/assets/missing.png",
		"website/index.html:4 website/assets/hero.webp",
		"website/index.html:4 website/media/clip.mp4",
		"website/index.html:4 website/media/clip.webm",
		"website/index.html:4 website/media/subs.vtt",
		"website/index.html:5 website/assets/logo.svg",
		"website/index.html:5 website/media/a.mp3",
		"website/index.html:6 website/assets/fav.ico",
		"website/index.html:6 website/assets/font.woff2",
		"website/index.html:7 website/assets/logo.svg",
		"website/index.html:7 website/assets/sprite.svg",
		"website/index.html:9 website/assets/LOGO.SVG",
		"website/index.html:9 website/assets/logo.svg",
		"website/index.html:9 website/assets/été.png",
		"website/style.css:1 website/assets/hero.webp",
		"website/style.css:1 website/assets/logo.svg",
		"website/style.css:2 website/assets/font.woff2",
		"website/style.css:3 website/assets/hero.webp",
		"website/style.css:3 website/assets/hero@2x.webp",
		"website/style.css:4 website/missing.png",
		"website/style.css:5 website/assets/hero.webp",
	}
	if !reflect.DeepEqual(refs, wantRefs) {
		t.Errorf("references\n got %q\nwant %q", refs, wantRefs)
	}
	// Missing files and another case are unresolved, never outside; the
	// symbolic link is no asset (unresolved too).
	for _, f := range []string{"website/index.html", "website/style.css"} {
		ds := idx.FindSymbolsByQualified(f)
		if len(ds) != 1 {
			t.Fatalf("%s: %d", f, len(ds))
		}
		u := idx.UnresolvedOutgoing(ds[0].ID)
		if u.OutsideRepository != 0 {
			t.Errorf("%s: %+v outside", f, u)
		}
	}
	for _, q := range []string{"website/assets/link.png", "website/node_modules/m/i.png", "website/.hidden/h.png", "website/assets/x.txt"} {
		if len(idx.FindSymbolsByQualified(q)) != 0 {
			t.Errorf("%s is an asset", q)
		}
	}
	// No name reaches an asset, and an asset reaches nothing.
	for _, a := range idx.Assets() {
		if len(idx.GetCallees(a.ID)) != 0 {
			t.Errorf("%s has callees", a.Qualified)
		}
		for _, e := range idx.GetCallers(a.ID) {
			if e.RefKind != "resource_link" {
				t.Errorf("%s: a %s caller", a.Qualified, e.RefKind)
			}
		}
	}
	if got := idx.FindSymbols("logo"); len(got) != 0 {
		t.Errorf("an asset has a name in the name index: %+v", got)
	}
}

// The identity of an asset is its path, whatever it holds; the index's
// fingerprint changes with the set of assets, not with their content; cold
// and restored indexes agree.
func TestAssetIdentityFingerprintCache(t *testing.T) {
	root := writeAssetSite(t)
	providers := languages.Registry().Providers()
	build := func() *index.RepositoryIndex {
		idx, err := index.New(context.Background(), root, providers)
		if err != nil {
			t.Fatal(err)
		}
		return idx
	}
	first := build()
	logo := first.FindSymbolsByQualified("website/assets/logo.svg")
	if len(logo) != 1 {
		t.Fatal("no logo.svg node")
	}
	fp := func() string {
		s, err := index.SourceFingerprint(context.Background(), root, providers)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if fp() != first.Fingerprint() {
		t.Fatal("the fingerprint of the sources and the index differ")
	}
	// Content: identity and fingerprint unchanged.
	if err := os.WriteFile(filepath.Join(root, "website/assets/logo.svg"), []byte("<svg><g/></svg>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fp() != first.Fingerprint() {
		t.Error("an asset's content changed the fingerprint")
	}
	second := build()
	if l := second.FindSymbolsByQualified("website/assets/logo.svg"); len(l) != 1 || l[0].ID != logo[0].ID {
		t.Error("an asset's content changed its identity")
	}
	// Added, removed, moved: the fingerprint and the links follow.
	if err := os.WriteFile(filepath.Join(root, "website/assets/missing.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if fp() == first.Fingerprint() {
		t.Error("an added asset left the fingerprint")
	}
	third := build()
	edges, _ := assetRelations(t, third)
	if !strings.Contains(strings.Join(edges, "\n"), "website/index.html -links-> website/assets/missing.png strong") {
		t.Errorf("the added asset is not linked: %q", edges)
	}
	if err := os.Rename(filepath.Join(root, "website/assets/missing.png"), filepath.Join(root, "website/moved.png")); err != nil {
		t.Fatal(err)
	}
	moved := fp()
	fourth := build()
	edges, _ = assetRelations(t, fourth)
	// Moved where nothing links to it: no edge, and no node (an asset no
	// reference names costs its path only), but the fingerprint follows.
	if strings.Contains(strings.Join(edges, "\n"), "missing.png") || len(fourth.FindSymbolsByQualified("website/moved.png")) != 0 ||
		moved == third.Fingerprint() || moved != fourth.Fingerprint() {
		t.Errorf("the moved asset: %q", edges)
	}
	// Cold and warm agree with the plain build.
	snapshot := func(idx *index.RepositoryIndex) []string {
		e, r := assetRelations(t, idx)
		var a []string
		for _, s := range idx.Assets() {
			a = append(a, string(s.ID)+" "+s.Qualified)
		}
		return append(append(e, r...), a...)
	}
	want := snapshot(fourth)
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"cold", "warm"} {
		idx, err := index.NewWithCache(context.Background(), root, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := snapshot(idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s differs\n got %q\nwant %q", run, got, want)
		}
		if idx.Fingerprint() != fourth.Fingerprint() {
			t.Errorf("%s: fingerprint differs", run)
		}
	}
	_ = source.FileID("")
}

// A document with <base> or template syntax states no asset link; each
// reference covers its URL's text.
func TestAssetLinkSpans(t *testing.T) {
	src := "<img srcset=\"a.png 1x, b.png 2x\">\n<img src='c.png'>\n<style>.x{background:url( d.png )}</style>"
	x, err := html.NewProvider().Extract(context.Background(), "p.html", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range x.References {
		if r.TargetKinds != "asset" {
			continue
		}
		p := r.Location.Range
		got = append(got, fmt.Sprintf("%s %q", r.NameQualified, at(src, [2]uint32{p.Start.Line, p.Start.Column}, [2]uint32{p.End.Line, p.End.Column})))
	}
	want := []string{`a.png "a.png"`, `b.png "b.png"`, `c.png "c.png"`, `d.png "url( d.png )"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spans\n got %q\nwant %q", got, want)
	}
	for _, src := range []string{
		`<base href="/x/"><img src="a.png"><style>.x{background:url(b.png)}</style>`,
		`<img src="{{ .Logo }}.png"><img src="<?= $logo ?>.png">`,
	} {
		x, err := html.NewProvider().Extract(context.Background(), "p.html", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range x.References {
			if r.TargetKinds == "asset" {
				t.Errorf("%s: %+v", src, r)
			}
		}
	}
}

// Reviewer A (hardening Phase B): srcset candidates whose own spans are not
// known stay apart (HB-1); a link both an icon and a stylesheet keeps its
// stylesheet (HB-3); an asset no reference names has no node (HB-2).
func TestAssetReviewHB(t *testing.T) {
	root := t.TempDir()
	for name, src := range map[string]string{
		"a.html":   `<img srcset="a.png?x=1&amp;y=2 1x, b/a.png 2x"><link rel="icon stylesheet" href="s.css">` + "\n",
		"s.css":    "",
		"a.png":    "",
		"b/a.png":  "",
		"lone.png": "",
	} {
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
	var got []string
	for _, e := range idx.GetCallees(idx.FindSymbolsByQualified("a.html")[0].ID) {
		to, _ := idx.GetSymbol(e.To)
		got = append(got, to.Qualified)
	}
	sort.Strings(got)
	if want := []string{"a.png", "b/a.png", "s.css"}; !reflect.DeepEqual(got, want) {
		t.Errorf("links %q, want %q", got, want)
	}
	for _, d := range idx.Diagnostics() {
		if strings.Contains(d.Code, "collision") {
			t.Errorf("diagnostic %+v", d)
		}
	}
	if len(idx.FindSymbolsByQualified("lone.png")) != 0 || len(idx.Assets()) != 2 {
		t.Errorf("assets %+v: an unlinked asset has a node", idx.Assets())
	}
}
