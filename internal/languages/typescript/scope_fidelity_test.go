package typescript_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
)

// The scope and module fidelity of the shared ECMAScript extractor, in
// JavaScript and TypeScript alike: a name a local declaration shadows, an
// import the program cannot resolve, a receiver without evidence never
// resolves Strong or Exact to a same-named declaration; aliases,
// re-exports and CommonJS bindings resolve to the declaration they denote.

// resolution is how Ark resolved one reference.
type resolution struct {
	conf       resolver.Confidence
	target     string   // "file:Qualified" of a unique Strong / Exact target
	candidates []string // "file:Qualified" of every candidate, at any confidence
	outside    bool
}

// resolveAll indexes the files of root as the index does and returns the
// resolution of every reference by "file:line:name".
func resolveAll(t *testing.T, files map[string]string) map[string][]resolution {
	t.Helper()
	root := t.TempDir()
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := languages.Registry()
	var fis []resolver.FileIndex
	for name, src := range files {
		desc, ok := reg.DetectByExtension(filepath.Ext(name))
		if !ok {
			continue
		}
		prov, _ := reg.Provider(desc.Language)
		ex, err := prov.Extract(context.Background(), source.FileID(name), []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		fis = append(fis, index.NewFileIndex(prov, source.FileID(name), ex))
	}
	byID := map[string]string{}
	for _, fi := range fis {
		for _, r := range fi.References {
			byID[string(r.ID)] = fmt.Sprintf("%s:%d:%s", fi.FileID, r.Location.Range.Start.Line, r.Name)
		}
	}
	out := map[string][]resolution{}
	for _, res := range resolver.NewInRoot(fis, "r").Resolve() {
		key := byID[string(res.ReferenceID)]
		r := resolution{conf: res.Confidence, outside: res.OutsideRepository}
		if res.HasUniqueTarget() {
			r.target = fmt.Sprintf("%s:%s", res.Candidates[0].File, res.Candidates[0].Qualified)
		}
		for _, c := range res.Candidates {
			r.candidates = append(r.candidates, fmt.Sprintf("%s:%s", c.File, c.Qualified))
		}
		out[key] = append(out[key], r)
	}
	return out
}

// expect is what one reference must resolve to: a target ("file:Qualified")
// it must reach Strong or Exact, or none ("") — then it must not reach any
// target Strong or Exact, and outside says whether it is outside.
type expect struct {
	key     string
	target  string
	outside bool
	conf    string // the confidence the target must have ("" = Strong or Exact)
	// cand: a target the reference must keep as a Candidate, never Strong
	// or Exact (a resolution through a CommonJS export: the source mapping
	// is known, the run-time value is not proven).
	cand string
	// absentOK: no reference at all is as good as one without a target
	// (a provider that states no reference a super it cannot type).
	absentOK bool
}

func check(t *testing.T, got map[string][]resolution, want []expect) {
	t.Helper()
	for _, w := range want {
		rs := got[w.key]
		if len(rs) == 0 {
			if !w.absentOK {
				t.Errorf("%s: no reference (have %v)", w.key, keys(got))
			}
			continue
		}
		for _, r := range rs {
			switch {
			case w.cand != "" && (r.conf != resolver.ConfidenceCandidate || !slices.Contains(r.candidates, w.cand)):
				t.Errorf("%s: resolved %v %v, want the Candidate %q", w.key, r.conf, r.candidates, w.cand)
			case w.cand != "":
			case w.target != "" && r.target != w.target:
				t.Errorf("%s: resolved %v %q, want Strong or Exact %q", w.key, r.conf, r.target, w.target)
			case w.conf != "" && r.conf.String() != w.conf:
				t.Errorf("%s: resolved %v, want %s", w.key, r.conf, w.conf)
			case w.target == "" && r.target != "":
				t.Errorf("%s: resolved %v %q, want no Strong or Exact target", w.key, r.conf, r.target)
			case w.target == "" && r.outside != w.outside:
				t.Errorf("%s: outside = %v, want %v", w.key, r.outside, w.outside)
			}
		}
	}
}

func keys(m map[string][]resolution) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return strings.Join(ks, " ")
}

// esCase is one fixture in both dialects: ext is replaced by .js and .ts.
type esCase struct {
	name  string
	files map[string]string
	want  []expect
}

func dialects(c esCase) map[string]esCase {
	out := map[string]esCase{}
	for _, ext := range []string{".js", ".ts"} {
		files := map[string]string{}
		for n, src := range c.files {
			files[strings.ReplaceAll(n, ".EXT", ext)] = strings.ReplaceAll(src, ".EXT", ext)
		}
		var want []expect
		for _, w := range c.want {
			w.key = strings.ReplaceAll(w.key, ".EXT", ext)
			w.target = strings.ReplaceAll(w.target, ".EXT", ext)
			want = append(want, w)
		}
		out[ext] = esCase{c.name, files, want}
	}
	return out
}

const util = "export function helper() { return 1 }\nexport function other() { return 2 }\n"

func TestECMAScriptScopes(t *testing.T) {
	cases := []esCase{
		{
			name: "parameter shadows an import",
			files: map[string]string{
				"util.EXT": util,
				"main.EXT": "import { helper } from './util.EXT'\nexport function f(helper) {\n  return helper()\n}\nexport function g() {\n  return helper()\n}\n",
			},
			want: []expect{{key: "main.EXT:3:helper"}, {key: "main.EXT:6:helper", target: "util.EXT:helper"}},
		},
		{
			name: "local variable, nested block and closure shadow a same-file function",
			files: map[string]string{
				"main.EXT": "export function helper() {}\nexport function f(x) {\n  const helper = x\n  return helper()\n}\nexport function g(x) {\n  if (x) {\n    let helper = x\n    helper()\n  }\n}\nexport function h(helper) {\n  return () => helper()\n}\nexport function k() {\n  return helper()\n}\n",
			},
			want: []expect{
				{key: "main.EXT:4:helper"}, {key: "main.EXT:9:helper"}, {key: "main.EXT:13:helper"},
				{key: "main.EXT:16:helper", target: "main.EXT:helper"},
			},
		},
		{
			name: "hoisted var and function declarations shadow before their declaration",
			files: map[string]string{
				"util.EXT": util,
				"main.EXT": "import { helper, other } from './util.EXT'\nexport function f(x) {\n  helper()\n  var helper = x\n}\nexport function g() {\n  other()\n  function other() {}\n}\n",
			},
			want: []expect{{key: "main.EXT:3:helper"}, {key: "main.EXT:7:other"}},
		},
		{
			name: "top-level destructuring and blocks are locals",
			files: map[string]string{
				"util.EXT": util,
				"main.EXT": "import { other } from './util.EXT'\nconst { helper } = globalThis\nhelper()\nfor (const other of []) {\n  other()\n}\nother()\n",
			},
			want: []expect{{key: "main.EXT:3:helper"}, {key: "main.EXT:5:other"}, {key: "main.EXT:7:other"}},
		},
		{
			// Each parameter and binding form shadows (catch, a lone arrow
			// parameter, rest, a default, a nested pattern, a named function
			// expression's own name, a class declared in a function).
			name: "every binding form shadows",
			files: map[string]string{
				"util.EXT": util,
				"main.EXT": "import { helper } from './util.EXT'\n" +
					"export function a() { try {} catch (helper) { helper() } }\n" +
					"export const b = helper => helper()\n" +
					"export function c(...helper) { helper() }\n" +
					"export function d(helper = 1) { helper() }\n" +
					"export function e({ x: { helper } }) { helper() }\n" +
					"export const f = function helper() { helper() }\n" +
					"export function g() { class helper {} new helper() }\n" +
					"export function h() { helper() }\n",
			},
			want: []expect{
				{key: "main.EXT:2:helper"}, {key: "main.EXT:3:helper"}, {key: "main.EXT:4:helper"},
				{key: "main.EXT:5:helper"}, {key: "main.EXT:6:helper"}, {key: "main.EXT:7:helper"},
				{key: "main.EXT:8:helper"}, {key: "main.EXT:9:helper", target: "util.EXT:helper"},
			},
		},
		{
			name: "a class expression's name and a default-exported class's static block shadow",
			files: map[string]string{
				"main.EXT": "export function Inner() {}\n" +
					"export const C = class Inner {\n  m() { return Inner() }\n  static s() { return new Inner() }\n}\n" +
					"export function helper() {}\n" +
					"export default class {\n  static {\n    const helper = () => 1\n    helper()\n  }\n}\n",
			},
			want: []expect{{key: "main.EXT:3:Inner"}, {key: "main.EXT:4:Inner"}, {key: "main.EXT:10:helper"}},
		},
		{
			name: "import alias and re-export resolve to the declaration",
			files: map[string]string{
				"util.EXT":   util,
				"barrel.EXT": "export { helper as h2 } from './util.EXT'\nexport * from './util.EXT'\n",
				"main.EXT":   "import { helper as h } from './util.EXT'\nimport { h2, other } from './barrel.EXT'\nexport function f() {\n  h()\n  h2()\n  other()\n}\n",
			},
			want: []expect{
				{key: "main.EXT:4:h", target: "util.EXT:helper"},
				{key: "main.EXT:5:h2", target: "util.EXT:helper"},
				{key: "main.EXT:6:other", target: "util.EXT:other"},
			},
		},
		{
			name: "an external package is never a repository declaration",
			files: map[string]string{
				"get.EXT":  "export function get() {}\n",
				"main.EXT": "import { get } from 'lodash'\nimport * as lib from 'get'\nexport function f() {\n  get()\n  lib.get()\n}\n",
			},
			want: []expect{{key: "main.EXT:4:get", outside: true}, {key: "main.EXT:5:get", outside: true}},
		},
		{
			name: "an ambiguous export-all and same-named declarations are no pick",
			files: map[string]string{
				"a.EXT":      "export function dup() {}\n",
				"b.EXT":      "export function dup() {}\n",
				"barrel.EXT": "export * from './a.EXT'\nexport * from './b.EXT'\n",
				"main.EXT":   "import { dup } from './barrel.EXT'\nexport function f() {\n  dup()\n}\n",
				"free.EXT":   "export function g() {\n  dup()\n}\n",
			},
			want: []expect{{key: "main.EXT:3:dup"}, {key: "free.EXT:2:dup"}},
		},
		{
			name: "a receiver without evidence, or a shadowed namespace, is no member's",
			files: map[string]string{
				"repo.EXT": "export class Repo {\n  save() {}\n}\n",
				"util.EXT": util,
				"main.EXT": "import * as lib from './util.EXT'\nexport function f(o) {\n  o.save()\n}\nexport function g(lib) {\n  lib.helper()\n}\nexport function h() {\n  lib.helper()\n}\n",
			},
			want: []expect{{key: "main.EXT:3:save"}, {key: "main.EXT:6:helper"}, {key: "main.EXT:9:helper", target: "util.EXT:helper"}},
		},
		{
			name: "a member of an exported const object is its member, Strong at most",
			files: map[string]string{
				"cache.EXT": "export const Cache = { get() { return 1 } }\n",
				"main.EXT":  "import { Cache } from './cache.EXT'\nexport function f() {\n  return Cache.get()\n}\n",
			},
			want: []expect{{key: "main.EXT:3:get", target: "cache.EXT:Cache.get", conf: "strong"}},
		},
		{
			name: "a relative import of a file no provider indexes is not outside",
			files: map[string]string{
				"main.EXT": "import data from './data.json'\nimport * as styles from './a.css'\nexport function f() {\n  data()\n  styles.x()\n}\n",
			},
			want: []expect{{key: "main.EXT:4:data", outside: false}, {key: "main.EXT:5:x", outside: false}},
		},
	}
	for _, c := range cases {
		for ext, d := range dialects(c) {
			t.Run(c.name+ext, func(t *testing.T) { check(t, resolveAll(t, d.files), d.want) })
		}
	}
}

func TestECMAScriptCommonJS(t *testing.T) {
	files := map[string]string{
		"layer.js": "function Layer() {}\nmodule.exports = Layer\n",
		"utils.js": "function a() {}\nfunction b() {}\nexports.a = a\nmodule.exports.b = b\n",
		"obj.js":   "function c() {}\nmodule.exports = { c, d: c }\n",
		"twice.js": "function e() {}\nfunction f() {}\nmodule.exports = e\nmodule.exports = f\n",
		"cond.js":  "function g() {}\nif (process.env.X) { module.exports = g }\n",
		"mixed.js": "function B() {}\nfunction f() {}\nmodule.exports = B\nexports.f = f\n",
		"l.js":     "function Layer() {}\nmodule.exports = Layer\n",
		"o.js":     "function Other() {}\nmodule.exports = Other\n",
		"re.js":    "let L = require('./l')\nif (process.env.X) { L = require('./o') }\nconst K = require('./l')\nfunction run() {\n  new L()\n  new K()\n}\n",
		"use.js":   "const { f } = require('./mixed')\nfunction run() {\n  f()\n}\n",
		"main.js": "const Layer = require('./layer')\nconst { a, b: bb } = require('./utils')\nconst c = require('./obj').c\nconst Twice = require('./twice')\nconst G = require('./cond')\nconst ext = require('express')\n" +
			"function run() {\n  new Layer()\n  a()\n  bb()\n  c()\n  Twice()\n  G()\n  ext()\n}\n",
	}
	check(t, resolveAll(t, files), []expect{
		{key: "main.js:8:Layer", cand: "layer.js:Layer"},
		{key: "main.js:9:a", cand: "utils.js:a"},
		{key: "main.js:10:bb", cand: "utils.js:b"},
		{key: "main.js:11:c", cand: "obj.js:c"},
		{key: "main.js:12:Twice"},              // two exports of one name: no pick
		{key: "main.js:13:G"},                  // a conditional export is no export
		{key: "main.js:14:ext", outside: true}, // a package
		{key: "re.js:5:L"},                     // a reassigned let: no binding
		{key: "re.js:6:K", cand: "l.js:Layer"},
		{key: "use.js:3:f"}, // module.exports replaced: exports.f is not stated
	})
}

// A named function expression's own name hides a receiver whose type the
// enclosing scope proves.
func TestECMAScriptReceiverShadowedByFunctionName(t *testing.T) {
	for _, ext := range []string{".js", ".ts"} {
		spec := "'./repo'"
		if ext == ".js" {
			spec = "'./repo.js'"
		}
		files := map[string]string{
			"repo" + ext: "export class Repo {\n  save() {}\n}\n",
			"main" + ext: "import { Repo } from " + spec + "\nexport function f() {\n  const r = new Repo()\n  const g = function r() { r.save() }\n  r.save()\n}\n" +
				"export function k() {\n  const r = new Repo()\n  r.save()\n}\n",
		}
		// A name declared twice in f (the const and the function's name)
		// is no evidence anywhere in f (types.go); k proves r.
		check(t, resolveAll(t, files), []expect{
			{key: "main" + ext + ":4:save"},
			{key: "main" + ext + ":5:save"},
			{key: "main" + ext + ":9:save", target: "repo" + ext + ":Repo.save"},
		})
	}
}

func TestTypeScriptTypeScopes(t *testing.T) {
	files := map[string]string{
		"user.ts": "export class User {\n  save() {}\n}\n",
		"main.ts": "import { User } from './user'\nexport function f(u: Partial<User>, v: User) {\n  u.save()\n  v.save()\n  type User2 = User\n}\nexport function g() {\n  interface User { x: number }\n  let u: User\n}\n",
	}
	got := resolveAll(t, files)
	check(t, got, []expect{
		{key: "main.ts:3:save", outside: false}, // Partial<User> is no type named Partial
		{key: "main.ts:4:save", target: "user.ts:User.save"},
		{key: "main.ts:9:User"}, // a local interface shadows the import
	})
}

func TestTypeScriptExportOnItsOwnLine(t *testing.T) {
	files := map[string]string{
		"a.ts":    "export\nfunction split() {}\n",
		"main.ts": "import { split } from './a'\nexport function f() {\n  split()\n}\n",
	}
	// The grammar does not read `export` on its own line as exporting the
	// declaration below: the import finds no export in a repository file —
	// unresolved, never outside the repository.
	check(t, resolveAll(t, files), []expect{{key: "main.ts:3:split", outside: false}})
}

// A namespace a module re-exports (export * as sub) is a namespace of its
// importers too: ns.sub.f() reaches the declaration through both exports.
func TestECMAScriptNamespaceReExportChain(t *testing.T) {
	for _, ext := range []string{".ts", ".mjs"} {
		spec := func(m string) string {
			if ext == ".mjs" {
				return "'./" + m + ".mjs'"
			}
			return "'./" + m + "'"
		}
		files := map[string]string{
			"s" + ext:     "export function f() { return 1 }\n",
			"index" + ext: "export * as sub from " + spec("s") + "\n",
			"main" + ext:  "import * as ns from " + spec("index") + "\nexport function run() {\n  return ns.sub.f()\n}\nexport function shadow(ns) {\n  return ns.sub.f()\n}\n",
		}
		check(t, resolveAll(t, files), []expect{
			{key: "main" + ext + ":3:f", target: "s" + ext + ":f"},
			{key: "main" + ext + ":6:f"}, // a parameter hides the import
		})
	}
}
