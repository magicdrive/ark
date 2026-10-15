package typescript_test

import "testing"

// Member fidelity of the shared ECMAScript extractor (values.go, the
// resolver's values with members and inherited lookup), in JavaScript and
// TypeScript alike: a member of a const object, a prototype method, an
// inherited member resolves to its declaration — a value's member Strong at
// most, code may reassign it; a receiver a local hides, a let object, a
// function's own property, an unidentified supertype never resolve Strong.

func TestECMAScriptValueMembers(t *testing.T) {
	cases := []esCase{
		{
			name: "members of a const object, here and through an import",
			files: map[string]string{
				"api.EXT": "export const api = { get() {}, put: function () {}, n: 1 }\n" +
					"export function here() {\n  api.get()\n}\n",
				"main.EXT": "import { api } from './api.EXT'\n" +
					"export function f() {\n  api.put()\n}\n" +
					"export function g(api) {\n  api.get()\n}\n",
			},
			want: []expect{
				{key: "api.EXT:3:get", target: "api.EXT:api.get", conf: "strong"},
				{key: "main.EXT:3:put", target: "api.EXT:api.put", conf: "strong"},
				{key: "main.EXT:6:get"}, // a parameter hides the import
			},
		},
		{
			name: "a let object and a member assigned later",
			files: map[string]string{
				"main.EXT": "let obj = { m() {} }\n" +
					"export const o = {}\n" +
					"o.m = function () {}\n" +
					"export function f() {\n  obj.m()\n  o.m()\n}\n",
			},
			want: []expect{
				{key: "main.EXT:5:m"}, // obj may hold another object
				{key: "main.EXT:6:m", target: "main.EXT:o.m", conf: "strong"},
			},
		},
		{
			name: "inherited members, super, and an unidentified supertype",
			files: map[string]string{
				"base.EXT": "export class A {\n  m() {}\n  k() {}\n}\n",
				"main.EXT": "import { A } from './base.EXT'\nimport { Ext } from 'pkg'\n" +
					"export class B extends A {\n  run() {\n    this.m()\n    super.k()\n  }\n}\n" +
					"export class C extends Ext {\n  run() {\n    this.m()\n  }\n}\n",
			},
			want: []expect{
				{key: "main.EXT:5:m", target: "base.EXT:A.m", conf: "exact"},
				{key: "main.EXT:6:k", target: "base.EXT:A.k", conf: "exact"},
				{key: "main.EXT:11:m"}, // Ext is outside the repository: the lookup stops
			},
		},
	}
	for _, c := range cases {
		for ext, d := range dialects(c) {
			t.Run(c.name+ext, func(t *testing.T) { check(t, resolveAll(t, d.files), d.want) })
		}
	}
}

func TestTypeScriptDeclaredObjectTypeIsNoValueOwner(t *testing.T) {
	files := map[string]string{
		"p.ts": "export interface Provider {\n  set(): void\n}\n" +
			"export const provider: Provider = { set() {} }\n" +
			"export function f() {\n  provider.set()\n}\n",
	}
	// The declared type is the object's type: no Strong target to the
	// literal's member (the TypeScript checker binds Provider.set).
	check(t, resolveAll(t, files), []expect{{key: "p.ts:6:set"}})
}

func TestECMAScriptCommonJSNamespace(t *testing.T) {
	files := map[string]string{
		"u.js":        "function a() {}\nexports.a = a\n",
		"replaced.js": "function a() {}\nfunction X() {}\nmodule.exports = X\nexports.a = a\n",
		"main.js": "const u = require('./u')\nconst r = require('./replaced')\n" +
			"function run() {\n  u.a()\n  r.a()\n}\n",
		"esm.mjs": "import u from './u.js'\nexport function f() {\n  u.a()\n}\n",
	}
	check(t, resolveAll(t, files), []expect{
		{key: "main.js:4:a", cand: "u.js:a"},
		{key: "main.js:5:a"},                 // module.exports replaced: exports.a is not stated
		{key: "esm.mjs:3:a", outside: false}, // an ES import of a CommonJS module: unresolved, not outside
	})
}

// Reviewer A (Phase 2): a local hiding a const object, a spread, static
// contexts, a class's prototype assignment, static and instance namesakes,
// extending a constructor function, super in an object literal's method.
func TestECMAScriptMemberNegatives(t *testing.T) {
	cases := []esCase{
		{
			name: "a local of the name hides a const object",
			files: map[string]string{
				"s.EXT": "export const X = { m() {} }\n" +
					"export function f(o) { const X = o; X.m() }\n" +
					"export function g(X) { X.m() }\n" +
					"export const k = (X) => X.m()\n" +
					"export function h(xs) { for (const X of xs) X.m() }\n" +
					"export function c(o) { try { o() } catch (X) { X.m() } }\n" +
					"export function u() { X.m() }\n",
			},
			want: []expect{
				{key: "s.EXT:2:m"}, {key: "s.EXT:3:m"}, {key: "s.EXT:4:m"}, {key: "s.EXT:5:m"}, {key: "s.EXT:6:m"},
				{key: "s.EXT:7:m", target: "s.EXT:X.m", conf: "strong"},
			},
		},
		{
			name: "a spread may replace a member",
			files: map[string]string{
				"v.EXT": "const base = { m() {} }\nexport const S = { m() {}, ...base }\nexport function f() { S.m() }\n",
			},
			want: []expect{{key: "v.EXT:3:m"}},
		},
		{
			name: "static contexts are no instance, a class prototype assignment no static member",
			files: map[string]string{
				"b.EXT": "export class Base {\n  bm() {}\n}\n" +
					"export class K extends Base {\n  static st() { super.bm(); this.bm() }\n  run() { this.bm() }\n}\n" +
					"K.prototype.b = function () {}\n" +
					"export function f() { K.b() }\n",
			},
			want: []expect{
				{key: "b.EXT:5:bm"}, // super and this in a static method
				{key: "b.EXT:6:bm", target: "b.EXT:Base.bm", conf: "exact"},
				{key: "b.EXT:9:b"},
			},
		},
		{
			// Phase 9 (F9A-3): a member's placement (symbol.Placement) tells
			// them apart — this is an instance, Clock the class itself.
			name: "static and instance members of one name, by the receiver",
			files: map[string]string{
				"n.EXT": "export class Clock {\n  static now = 1\n  now = 2\n  run() { this.now() }\n  static s() { Clock.now() }\n  i() {}\n}\nClock.i()\nfunction f() { const c = new Clock(); c.s() }\n",
			},
			want: []expect{
				{key: "n.EXT:4:now", target: "n.EXT:Clock.now", conf: "exact"},
				{key: "n.EXT:5:now"}, // the static one only, a Candidate (by name)
				{key: "n.EXT:8:i"},
				{key: "n.EXT:9:s"},
			},
		},
		{
			name: "extending a constructor function, super in an object literal's method",
			files: map[string]string{
				"q.EXT": "export function F() {}\nF.prototype.pm = function () {}\n" +
					"export class Q extends F {\n  run() { this.pm() }\n}\n" +
					"export class Base {\n  bm() {}\n}\n" +
					"export class R extends Base {\n  run() { return { m() { super.bm() } } }\n}\n",
			},
			want: []expect{{key: "q.EXT:4:pm"}, {key: "q.EXT:10:bm", absentOK: true}},
		},
	}
	for _, c := range cases {
		for ext, d := range dialects(c) {
			t.Run(c.name+ext, func(t *testing.T) { check(t, resolveAll(t, d.files), d.want) })
		}
	}
}

// A script's extends name a local hides is no supertype for super.
func TestECMAScriptSuperShadowedInScript(t *testing.T) {
	files := map[string]string{
		"base.js": "class Base {\n  x() {}\n}\n",
		"main.js": "const { Base } = globalThis.lib\nclass C extends Base {\n  m() { super.x() }\n}\n",
	}
	check(t, resolveAll(t, files), []expect{{key: "main.js:3:x", absentOK: true}})
}

// JavaScript reads a function with prototype assignments as a constructor;
// TypeScript does not (no Strong target in a .ts file).
func TestJavaScriptPrototypeMembers(t *testing.T) {
	c := esCase{
		name: "prototype methods, the constructor's this, and no static members",
		files: map[string]string{
			"f.EXT": "export function F() {\n  this.m()\n}\n" +
				"F.prototype.m = function () {\n  this.n()\n}\n" +
				"F.prototype.n = function () {}\n" +
				"F.s = function () {}\n" +
				"export function G() {}\n" +
				"G.prototype = { q() {} }\n" +
				"export function use() {\n  const x = new F()\n  x.m()\n  F.s()\n  const y = new G()\n  y.q()\n  x.s()\n}\n",
		},
		want: []expect{
			{key: "f.EXT:2:m", target: "f.EXT:F.m", conf: "strong"},
			{key: "f.EXT:5:n", target: "f.EXT:F.n", conf: "strong"},
			{key: "f.EXT:13:m", target: "f.EXT:F.m", conf: "strong"},
			{key: "f.EXT:14:s"}, // a function's own property is no member
			{key: "f.EXT:16:q", target: "f.EXT:G.q", conf: "strong"},
			{key: "f.EXT:17:s"}, // nor of an instance
		},
	}
	ds := dialects(c)
	t.Run("js", func(t *testing.T) { check(t, resolveAll(t, ds[".js"].files), ds[".js"].want) })
	var none []expect
	for _, w := range ds[".ts"].want {
		none = append(none, expect{key: w.key})
	}
	t.Run("ts", func(t *testing.T) { check(t, resolveAll(t, ds[".ts"].files), none) })
}

// Reviewer A (Phase 2 re-review): static and instance accessors of one name,
// an arrow function assigned to a prototype, a JSDoc-typed object, a static
// block.
func TestECMAScriptMemberNegatives2(t *testing.T) {
	cases := []esCase{
		{
			name: "static and instance accessors of one name are two members",
			files: map[string]string{
				"a.EXT": "export class A {\n  static get v() { return 1 }\n  get v() { return 2 }\n}\n" +
					"export class S extends A {\n  run() { this.v() }\n}\n",
			},
			want: []expect{{key: "a.EXT:6:v", target: "a.EXT:A.v", conf: "exact"}},
		},
		{
			name: "a static block's this is the class",
			files: map[string]string{
				"b.EXT": "export class B {\n  m() {}\n  static {\n    this.m()\n    helper()\n  }\n}\nexport function helper() {}\n",
			},
			want: []expect{{key: "b.EXT:4:m"}, {key: "b.EXT:5:helper", target: "b.EXT:helper"}},
		},
	}
	for _, c := range cases {
		for ext, d := range dialects(c) {
			t.Run(c.name+ext, func(t *testing.T) { check(t, resolveAll(t, d.files), d.want) })
		}
	}
	js := map[string]string{
		"p.js": "export function F() {}\nF.prototype.m = () => { this.x() }\nF.prototype.x = function () {}\n" +
			"/** @type {Options} */\nexport const opts = { go() {} }\nexport function f() { opts.go() }\n",
	}
	check(t, resolveAll(t, js), []expect{
		{key: "p.js:2:x"},  // an arrow function's this is the module's
		{key: "p.js:6:go"}, // a JSDoc @type declares the object's type
	})
}
