package typescript_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/source"
)

// CSS Modules (Phase 8): a class a JavaScript or TypeScript file uses through
// an import of a .module.css file is that file's class, by path evidence.

var cssModulesRepo = map[string]string{
	"src/a.module.css": ".btn {} .title {} .x-y {} .used-by-name {}\n",
	"src/b.module.css": ".btn {}\n",
	"src/plain.css":    ".btn {}\n",
	"src/app.js": strings.Join([]string{
		`import s from './a.module.css'`,
		`import * as t from './b.module.css'`,
		`import { title, btn as b2, "x-y" as xy } from './a.module.css'`,
		`import p from './plain.css'`,
		`import alias from '@/a.module.css'`,
		`import far from '../../a.module.css'`,
		`import esc from './a\x2emodule.css'`,
		`export function view(k) {`,
		`  use(s.btn, s?.title, s['x-y'], s.missing, t.btn, p.btn, alias.btn, far.btn, esc.btn)`,
		`  use(s.btn(), s[k], s[` + "`btn`" + `], s['bt\n'], (s).btn)`,
		`}`,
		`export function shadowed() { { const s = {}; use(s.title) } }`,
		`export function named(title) { return [b2, { xy }, xy(), title] }`,
		`export function named2() { return [title, b2.length] }`,
		`function use() {}`,
		`function usedByName() { return usedByName }`,
	}, "\n"),
	"src/c.tsx": "import s from './a.module.css'\nimport type T from './b.module.css'\nexport const C = () => <div className={s.title} />\nimport { btn } from './b.module.css'\nexport const E = () => <btn className={btn}></btn>\nexport function u() { return T.btn }\n",
	"lib/d.ts":  "import s from '../src/b.module.css'\nexport const d = s.btn\n",
	// Reviewer A (Phase 8): no use of a class.
	"neg/t.ts": strings.Join([]string{
		`import { title, type btn, default as whole } from '../src/a.module.css'`,
		`import * as ns from '../src/a.module.css'`,
		`import s from '../src/a.module.css'`,
		`import w from '../src/b.module.css' with { type: 'css' }`,
		`export type F = (title: string) => void`,
		`export type G = { [title: string]: number }`,
		`export const o = { m(s: any, title: any) { return [s.btn, title] } }`,
		`export function n() { delete s.btn; s.title = 1; s.btn += 1; s.btn++; new title(); return [ns.default, w.btn, btn, whole] }`,
		`export function p() { (s.btn) = 'q'; (s.btn)++; delete (s.btn); s.btn! = 'r'; (s.btn)(); s.btn!(); (title)(); title!() }`,
	}, "\n"),
	// Hardening Phase E: export default s.x, destructuring.
	"pos/e.ts": "import s from '../src/a.module.css'\nimport * as ns from '../src/b.module.css'\nexport default s.title\nexport function g() { const { btn, 'x-y': xy, ...rest } = s; const { default: d } = ns; return [btn, xy, rest, d] }\nexport function h(s: any) { const { title } = s; return title }\n",
	"pos/k.ts": "import s from '../src/a.module.css'\nexport function k(on: boolean) { if (s.btn) { return (s.title) } while ((s.btn)!) {} return { [s.btn]: on } }\n",
	// Reviewer C (C8-9): a file named like a class of another file.
	"q/a.module.css":        ".c\\.css {}\n",
	"q/a.module.css .c.css": "",
	"q/u.ts":                "import s from './a.module.css'\nexport const u = s['c.css']\n",
	"neg/j.tsx":             "import { title } from '../src/a.module.css'\nexport const J = () => <svg><a:title /><b xlink:title=\"1\" /></svg>\n",
}

func TestCSSModulesBinding(t *testing.T) {
	root := t.TempDir()
	for name, src := range cssModulesRepo {
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
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				if to, _ := idx.GetSymbol(e.To); to.Kind == "css_class" {
					got = append(got, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
				}
			}
		}
	}
	sort.Strings(got)
	want := []string{
		"C -references-> src/a.module.css .title strong",
		"E -references-> src/b.module.css .btn strong",
		"d -references-> src/b.module.css .btn strong",
		"g -references-> src/a.module.css .btn strong",
		"g -references-> src/a.module.css .x-y strong",
		"k -references-> src/a.module.css .btn strong",
		"k -references-> src/a.module.css .title strong",
		"named -references-> src/a.module.css .btn strong",
		"named -references-> src/a.module.css .x-y strong",
		"named2 -references-> src/a.module.css .btn strong",
		"named2 -references-> src/a.module.css .title strong",
		"u -references-> q/a.module.css .c.css strong",
		"view -references-> src/a.module.css .btn strong",
		"view -references-> src/a.module.css .title strong",
		"view -references-> src/a.module.css .x-y strong",
		"view -references-> src/b.module.css .btn strong",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CSS Modules relations\n got %q\nwant %q", got, want)
	}
	// The references, where they are written.
	var refs []string
	for _, f := range []string{"src/app.js", "src/c.tsx", "lib/d.ts", "neg/t.ts", "neg/j.tsx", "pos/e.ts"} {
		for _, r := range idx.ReferencesByFile(source.FileID(f)) {
			if r.TargetKinds == "css_class" {
				p := r.Location.Range
				refs = append(refs, fmt.Sprintf("%s %d:%d-%d:%d %s %s", f, p.Start.Line, p.Start.Column, p.End.Line, p.End.Column, r.Name, r.NameQualified))
			}
		}
	}
	sort.Strings(refs)
	// Not: the callee s.btn(), s[k], a template string, an escaped string,
	// s shadowed in shadowed, the parameter title of named, the callee xy(),
	// the tag <btn>, the specifiers themselves.
	wantRefs := []string{
		"lib/d.ts 2:20-2:23 btn src/b.module.css .btn",
		"pos/e.ts 3:18-3:23 title src/a.module.css .title", // export default s.title
		"pos/e.ts 4:31-4:34 btn src/a.module.css .btn",     // const { btn, 'x-y': xy } = s
		"pos/e.ts 4:37-4:40 x-y src/a.module.css .x-y",
		"src/app.js 10:47-10:50 btn src/a.module.css .btn", // (s).btn
		"src/app.js 13:40-13:42 btn src/a.module.css .btn", // b2
		"src/app.js 13:46-13:48 x-y src/a.module.css .x-y", // { xy }
		"src/app.js 14:36-14:41 title src/a.module.css .title",
		"src/app.js 14:43-14:45 btn src/a.module.css .btn", // b2.length
		"src/app.js 9:17-9:22 title src/a.module.css .title",
		"src/app.js 9:27-9:30 x-y src/a.module.css .x-y",
		"src/app.js 9:36-9:43 missing src/a.module.css .missing",
		"src/app.js 9:47-9:50 btn src/b.module.css .btn",
		"src/app.js 9:9-9:12 btn src/a.module.css .btn",
		"src/c.tsx 3:42-3:47 title src/a.module.css .title",
		"src/c.tsx 5:40-5:43 btn src/b.module.css .btn",
	}
	if !reflect.DeepEqual(refs, wantRefs) {
		t.Errorf("CSS Modules references\n got %q\nwant %q", refs, wantRefs)
	}
	// A condition, a parenthesized value, a computed key are uses.
	pos := 0
	for _, r := range idx.ReferencesByFile("pos/k.ts") {
		if r.TargetKinds == "css_class" {
			pos++
		}
	}
	if pos != 4 {
		t.Errorf("pos/k.ts: %d class references, want 4", pos)
	}
	// s.missing: no such class — unresolved, never outside the repository.
	view := idx.FindSymbolsByQualified("view")
	if len(view) != 1 {
		t.Fatalf("view: %d symbols", len(view))
	}
	for _, r := range idx.UnresolvedOutgoing(view[0].ID).References {
		if r.Kind == "value_reference" && r.Reason == "outside_repository" && r.Name == "missing" {
			t.Errorf("view: s.missing is outside the repository")
		}
	}
	if u := idx.UnresolvedOutgoing(view[0].ID); u.Unresolved < 1 {
		t.Errorf("view: %+v, want s.missing unresolved", u)
	}
	// No class reaches a JavaScript name, and no name reaches a class.
	for _, s := range idx.FindSymbolsByQualified("src/a.module.css .used-by-name") {
		if c := idx.GetCallers(s.ID); len(c) != 0 {
			t.Errorf("callers of .used-by-name: %+v", c)
		}
	}
}
