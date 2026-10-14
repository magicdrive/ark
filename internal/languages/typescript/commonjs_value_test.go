package typescript_test

import (
	"fmt"
	"testing"
)

// TestECMAScriptCommonJSValueTargets pins the reference semantics of a
// CommonJS export object (docs/development/web-semantic-fidelity/f6b5/
// semantics.md): a reference through module.exports = { a, k: b } denotes the
// declaration of the value the property holds, whatever the object's other
// properties are (the TypeScript checker answers the property itself once one
// of them is not shorthand); a shorthand { a } is one read of a's binding; a
// property whose value is no top-level binding, a nested object, a property
// written again and an export inside a block state no target, and a member of
// an object literal (a getter among them) is that member, Strong at most.
func TestECMAScriptCommonJSValueTargets(t *testing.T) {
	files := map[string]string{
		// 1, 2, 3: shorthand, k: a and k: b mixed with a non-identifier value.
		"mixed.js": "function parse() {}\nconst MAX = 1\nmodule.exports = { parse, MAX, V: 2, again: parse, execute: parse }\n",
		// 4: a shorthand of a parameter in a non-exported object.
		"param.js": "function parse() {}\nfunction create(parse) {\n  return { parse }\n}\nmodule.exports = { create }\n",
		// 5: a let assigned again: the binding is one declaration.
		"let.js": "let parse = function first() {}\nparse = function second() {}\nmodule.exports = { parse, X: 1 }\n",
		// 6, 10: the value is an import binding.
		"impl.js":  "function parseImpl() {}\nmodule.exports = parseImpl\n",
		"impl2.js": "function parse() {}\nmodule.exports = { parse }\n",
		"alias.js": "const parse = require('./impl')\nconst { parse: p2 } = require('./impl2')\nmodule.exports = { parse, p2, X: 1 }\n",
		// 7: a member expression; 8: a getter; 9: a nested object.
		"member.js": "const api = { parse() {} }\nmodule.exports = { parse: api.parse, X: 1 }\n",
		"getter.js": "const api = { get parse() { return function () {} } }\nmodule.exports = api\n",
		"nested.js": "const parse = () => {}\nmodule.exports = { tools: { parse }, X: 1 }\n",
		// 11: written again; 15: no declaration; 16: inside a block.
		"again.js": "const parse = () => {}\nconst other = () => {}\nmodule.exports = { parse, X: 1 }\nmodule.exports.parse = other\n",
		"none.js":  "module.exports = { parse, X: 1 }\n",
		"block.js": "function parse() {}\n{\n  const parse = () => {}\n  module.exports = { parse, X: 1 }\n}\n",
		// 14: a non-exported object's member.
		"obj.js": "function parse() {}\nconst obj = { parse }\nfunction useObj() {\n  return obj.parse()\n}\n",
		"use.js": "const m = require('./mixed')\nconst { MAX, parse: p } = require('./mixed')\nconst param = require('./param')\nconst l = require('./let')\nconst a = require('./alias')\n" +
			"const mem = require('./member')\nconst g = require('./getter')\nconst n = require('./nested')\nconst ag = require('./again')\nconst no = require('./none')\nconst b = require('./block')\n" +
			"function run() {\n" + // 12
			"  m.parse()\n" + // 13
			"  m.again()\n" + // 14
			"  m.execute()\n" + // 15
			"  p()\n" + // 16
			"  if (MAX) {}\n" + // 17
			"  param.create()\n" + // 18
			"  l.parse()\n" + // 19
			"  a.parse()\n" + // 20
			"  a.p2()\n" + // 21
			"  mem.parse()\n" + // 22
			"  g.parse()\n" + // 23
			"  n.tools.parse()\n" + // 24
			"  ag.parse()\n" + // 25
			"  no.parse()\n" + // 26
			"  b.parse()\n" + // 27
			"}\n",
	}
	check(t, resolveAll(t, files), []expect{
		{key: "mixed.js:3:parse", target: "mixed.js:parse"}, // the shorthand reads parse
		{key: "mixed.js:3:MAX", target: "mixed.js:MAX"},
		{key: "use.js:13:parse", cand: "mixed.js:parse"},
		{key: "use.js:14:again", cand: "mixed.js:parse"},
		{key: "use.js:15:execute", cand: "mixed.js:parse"},
		{key: "use.js:16:p", cand: "mixed.js:parse"},
		{key: "use.js:17:MAX", cand: "mixed.js:MAX"},
		{key: "param.js:3:parse", absentOK: true}, // the parameter, not function parse
		{key: "use.js:18:create", cand: "param.js:create"},
		{key: "use.js:19:parse", cand: "let.js:parse"},
		{key: "use.js:20:parse", cand: "impl.js:parseImpl"},
		{key: "use.js:21:p2", cand: "impl2.js:parse"},
		{key: "use.js:22:parse", absentOK: true},
		{key: "use.js:23:parse", cand: "getter.js:api.parse"}, // the accessor, as the checker
		{key: "use.js:24:parse", absentOK: true},
		{key: "use.js:25:parse", absentOK: true},
		{key: "use.js:26:parse", absentOK: true},
		{key: "none.js:1:parse", absentOK: true},
		{key: "use.js:27:parse", absentOK: true},
		{key: "block.js:4:parse", absentOK: true}, // the inner const
		{key: "obj.js:4:parse", target: "obj.js:obj.parse", conf: "strong"},
		{key: "obj.js:4:obj", target: "obj.js:obj"},
	})
}

// TestECMAScriptCommonJSOverwrites pins that a module states no export of a
// name it may write another value to: a later spread, computed key or same
// key in the replacement object, a write anywhere but a top-level assignment
// statement, a write of the exports object as a whole, and module.exports
// replaced in a branch. The value at run time is another than the
// declaration the property names, so no Strong or Exact target is right.
func TestECMAScriptCommonJSOverwrites(t *testing.T) {
	files := map[string]string{
		"ov.js":       "function parse() {}\nmodule.exports = { parse }\n",
		"spread.js":   "function parse() {}\nmodule.exports = { parse, ...require('./ov') }\n",
		"spread2.js":  "function parse() {}\nconst ext = { parse() {} }\nmodule.exports = { parse, X: 1, ...ext }\n",
		"computed.js": "function parse() {}\nfunction other() {}\nconst k = 'parse'\nmodule.exports = { parse, [k]: other }\n",
		"method.js":   "function parse() {}\nmodule.exports = { parse, X: 1, parse() {} }\n",
		"strkey.js":   "function parse() {}\nfunction other() {}\nmodule.exports = { parse, 'parse': other }\n",
		"last.js":     "function parse() {}\nfunction other() {}\nmodule.exports = { parse, X: 1, parse: other }\n",
		"before.js":   "function parse() {}\nmodule.exports = { ...require('./ov'), parse }\n",
		"assign.js":   "function parse() {}\nfunction other() {}\nmodule.exports = { parse, X: 1 }\nObject.assign(module.exports, { parse: other })\n",
		"define.js":   "function parse() {}\nfunction other() {}\nmodule.exports = { parse, X: 1 }\nObject.defineProperty(exports, 'parse', { value: other })\n",
		"infn.js":     "function parse() {}\nfunction other() {}\nmodule.exports = { parse, X: 1 }\nfunction later() { module.exports.parse = other }\n",
		"plus.js":     "let parse = 1\nmodule.exports = { parse, X: 1 }\nmodule.exports.parse += 1\n",
		"del.js":      "function parse() {}\nmodule.exports = { parse, X: 1 }\ndelete module.exports.parse\n",
		"sub.js":      "function parse() {}\nfunction other() {}\nmodule.exports = { parse, X: 1 }\nconst k = 'parse'\nmodule.exports[k] = other\n",
		"subname.js":  "function parse() {}\nfunction keep() {}\nfunction other() {}\nmodule.exports = { parse, keep }\nmodule.exports['parse'] = other\n",
		"branch.js":   "function parse() {}\nfunction other() {}\nmodule.exports = { parse }\nif (process.env.X) { module.exports = { parse: other } }\n",
		"viaobj.js":   "function parse() {}\nconst obj = { parse }\nmodule.exports = obj\n",
		"pure.js":     "function parse() {}\nmodule.exports = { parse }\n",
		"samename.js": "function parse() {}\nmodule.exports = { parse: parse, X: 1 }\n",
		"destruct.js": "const { parse } = require('./pure')\nmodule.exports = { parse, X: 1 }\n",
		// exports.a.b = c writes a's value, not the export a.
		"deep.js": "function parse() {}\nmodule.exports = { parse }\nfunction f() { module.exports.parse.cache = 1 }\n",
		// The object written as a whole is still the module's value.
		"assignw.js": "function Api() {}\nconst more = {}\nmodule.exports = Api\nObject.assign(module.exports, more)\n",
		// An update writes one name.
		"inc.js": "let count = 0\nfunction parse() {}\nmodule.exports = { parse, count }\nmodule.exports.count++\n",
		"use.js": "const s = require('./spread')\nconst s2 = require('./spread2')\nconst c = require('./computed')\nconst me = require('./method')\nconst sk = require('./strkey')\n" +
			"const la = require('./last')\nconst be = require('./before')\nconst as = require('./assign')\nconst de = require('./define')\nconst fn = require('./infn')\n" +
			"const pl = require('./plus')\nconst dl = require('./del')\nconst sb = require('./sub')\nconst sn = require('./subname')\nconst br = require('./branch')\n" +
			"const vo = require('./viaobj')\nconst pu = require('./pure')\nconst sa = require('./samename')\nconst ds = require('./destruct')\n" +
			"const dp = require('./deep')\nconst AW = require('./assignw')\nconst inc = require('./inc')\nconst { count } = require('./inc')\n" +
			"function run() {\n" + // 24
			"  s.parse()\n" + // 25
			"  s2.parse()\n" + // 26
			"  c.parse()\n" + // 27
			"  me.parse()\n" + // 24
			"  sk.parse()\n" + // 29
			"  la.parse()\n" + // 30
			"  be.parse()\n" + // 31
			"  as.parse()\n" + // 32
			"  de.parse()\n" + // 33
			"  fn.parse()\n" + // 34
			"  pl.parse()\n" + // 35
			"  dl.parse()\n" + // 36
			"  sb.parse()\n" + // 37
			"  sn.parse()\n" + // 38
			"  sn.keep()\n" + // 39
			"  br.parse()\n" + // 40
			"  vo.parse()\n" + // 41
			"  pu.parse()\n" + // 42
			"  sa.parse()\n" + // 43
			"  ds.parse()\n" + // 44
			"  dp.parse()\n" + // 45
			"  new AW()\n" + // 46
			"  inc.parse()\n" + // 47
			"  if (count) {}\n" + // 48
			"}\n",
	}
	got := resolveAll(t, files)
	check(t, got, []expect{
		{key: "use.js:25:parse", absentOK: true},
		{key: "use.js:26:parse", absentOK: true},
		{key: "use.js:27:parse", absentOK: true},
		{key: "use.js:28:parse", absentOK: true},
		{key: "use.js:29:parse", absentOK: true},
		{key: "use.js:30:parse", cand: "last.js:other"}, // the last property stays
		{key: "use.js:31:parse", cand: "before.js:parse"},
		{key: "use.js:32:parse", absentOK: true},
		{key: "use.js:33:parse", absentOK: true},
		{key: "use.js:34:parse", absentOK: true},
		{key: "use.js:35:parse", absentOK: true},
		{key: "use.js:36:parse", absentOK: true},
		{key: "use.js:37:parse", absentOK: true},
		{key: "use.js:38:parse", absentOK: true},
		{key: "use.js:39:keep", cand: "subname.js:keep"},
		{key: "use.js:40:parse", absentOK: true},
		// A non-exported object exported through a variable: the member, as
		// for any object literal's member (the layer differs from
		// module.exports = { parse }, both are the checker's: semantics.md).
		{key: "use.js:41:parse", cand: "viaobj.js:obj.parse"},
		{key: "use.js:42:parse", cand: "pure.js:parse"},
		{key: "use.js:43:parse", cand: "samename.js:parse"},
		{key: "use.js:44:parse", cand: "pure.js:parse"},
		{key: "use.js:45:parse", cand: "deep.js:parse"},
		{key: "use.js:46:AW", cand: "assignw.js:Api"},
		{key: "use.js:47:parse", cand: "inc.js:parse"},
		{key: "use.js:48:count", absentOK: true},
	})
	// The shorthand { parse } is one reference: the read of parse's binding.
	if rs := got["pure.js:2:parse"]; len(rs) != 1 || rs[0].target != "pure.js:parse" {
		t.Errorf("pure.js:2:parse: %d references %+v, want one to pure.js:parse", len(rs), rs)
	}
}

// TestECMAScriptModuleValueTargets pins the ES module forms of the same
// export (semantics.md cases 12 and 13): the binding's declaration, Exact.
func TestECMAScriptModuleValueTargets(t *testing.T) {
	for ext, c := range dialects(esCase{name: "esm", files: map[string]string{
		"m.EXT":   "function parse() {}\nexport { parse }\n",
		"use.EXT": "import { parse } from './m.EXT'\nimport * as m from './m.EXT'\nexport function run() {\n  parse()\n  m.parse()\n}\n",
	}, want: []expect{
		{key: "use.EXT:4:parse", target: "m.EXT:parse", conf: "exact"},
		{key: "use.EXT:5:parse", target: "m.EXT:parse", conf: "exact"},
	}}) {
		t.Run(ext, func(t *testing.T) { check(t, resolveAll(t, c.files), c.want) })
	}
}

// TestECMAScriptCommonJSWriteScope pins the extent of the writes that drop
// an export: a write that fixes its key drops that name only (the
// Object.defineProperty(exports, "__esModule", …) every transpiled CommonJS
// module starts with drops none of its exports), and a write through a
// parameter named module or exports is the parameter's. A local of the name
// still counts (a write the scan cannot place is taken as the module's).
func TestECMAScriptCommonJSWriteScope(t *testing.T) {
	files := map[string]string{
		"babel.js":   "\"use strict\";\nObject.defineProperty(exports, \"__esModule\", { value: true });\nfunction bf() {}\nexports.bf = bf;\n",
		"babel2.js":  "Object.defineProperty(module.exports, '__esModule', { value: true })\nfunction mf() {}\nmodule.exports = { mf }\n",
		"assign1.js": "function a() {}\nfunction b() {}\nmodule.exports = { a, b }\nObject.assign(module.exports, { b: 1 })\n",
		"assign2.js": "function a() {}\nconst more = {}\nmodule.exports = { a }\nObject.assign(module.exports, { ...more })\n",
		"props.js":   "function a() {}\nfunction b() {}\nmodule.exports = { a, b }\nObject.defineProperties(exports, { b: { value: 1 } })\n",
		"param.js":   "function v1() {}\nmodule.exports = { v1 }\nfunction factory(module) { module.exports = {} }\n",
		"param2.js":  "function pa() {}\nexports.pa = pa\nfunction install(exports) { exports.pa = 1 }\nconst f = exports => { exports.pa = 2 }\ntry {} catch (exports) { exports.pa = 3 }\n",
		"local.js":   "function q() {}\nexports.q = q\nfunction g() { const exports = {}; exports.q = 1 }\n",
		"use.js": "const b = require('./babel')\nconst b2 = require('./babel2')\nconst a1 = require('./assign1')\nconst a2 = require('./assign2')\nconst pr = require('./props')\n" +
			"const p = require('./param')\nconst p2 = require('./param2')\nconst l = require('./local')\n" +
			"function run() {\n" + // 9
			"  b.bf()\n" + // 10
			"  b2.mf()\n" + // 11
			"  a1.a()\n" + // 12
			"  a1.b()\n" + // 13
			"  a2.a()\n" + // 14
			"  pr.a()\n" + // 15
			"  pr.b()\n" + // 16
			"  p.v1()\n" + // 17
			"  p2.pa()\n" + // 18
			"  l.q()\n" + // 19
			"}\n",
	}
	check(t, resolveAll(t, files), []expect{
		{key: "use.js:10:bf", cand: "babel.js:bf"},
		{key: "use.js:11:mf", cand: "babel2.js:mf"},
		{key: "use.js:12:a", cand: "assign1.js:a"},
		{key: "use.js:13:b", absentOK: true},
		{key: "use.js:14:a", absentOK: true}, // a spread source: any name
		{key: "use.js:15:a", cand: "props.js:a"},
		{key: "use.js:16:b", absentOK: true},
		{key: "use.js:17:v1", cand: "param.js:v1"},
		{key: "use.js:18:pa", cand: "param2.js:pa"},
		{key: "use.js:19:q", absentOK: true},
	})
}

// TestECMAScriptCommonJSOtherWrites pins the writes that reach the exports
// object other than through exports.a and module.exports.a: a second
// replacement, __defineGetter__, the object let out (an alias, an argument,
// an IIFE), the top-level this, parentheses and TypeScript's wrappers,
// module['exports'], an escaped name, a for-of target, exports bound to
// another object. Reads that let nothing out (require.main === module,
// typeof exports) keep the exports.
func TestECMAScriptCommonJSOtherWrites(t *testing.T) {
	const fns = "function parse() {}\nfunction other() {}\n"
	forms := []struct {
		name, src string
		kept      bool // parse stays the export (a Candidate: commonjs-boundary)
	}{
		{"twicefn", "module.exports = { parse }\nmodule.exports = function () {}\n", false},
		{"twicereq", "module.exports = { parse }\nmodule.exports = require('./base')\n", false},
		{"getter", "exports.parse = parse\nexports.__defineGetter__('parse', () => other)\n", false},
		{"alias", "exports.parse = parse\nconst e = exports\ne.parse = other\n", false},
		{"alias2", "module.exports = { parse }\nconst e = module.exports\ne.parse = other\n", false},
		{"escape", "exports.parse = parse\nfunction mix(o) { o.parse = other }\nmix(exports)\n", false},
		{"iife", "exports.parse = parse\n;(function (e) { e.parse = other })(exports)\n", false},
		{"this", "exports.parse = parse\nthis.parse = other\n", false},
		{"assignthis", "exports.parse = parse\nObject.assign(this, { parse: other })\n", false},
		{"arrowthis", "exports.parse = parse\nconst f = () => { this.parse = other }\n", false},
		{"paren", "exports.parse = parse\n;(exports).parse = other\n", false},
		{"paren2", "module.exports = { parse }\n;(module.exports).parse = other\n", false},
		{"bracket", "module.exports = { parse }\nmodule['exports'].parse = other\n", false},
		{"bracket2", "module.exports = { parse }\nmodule['exports'] = { parse: other }\n", false},
		{"escaped", "module.exports = { parse }\nmodule.exports.p\\u0061rse = other\n", false},
		{"escapedkey", "module.exports = { parse, p\\u0061rse: other }\n", false},
		{"forof", "exports.parse = parse\nfor (exports.parse of [other]);\n", false},
		{"rebound", "exports = {}\nexports.parse = parse\n", false},
		{"varexports", "var exports = {}\nexports.parse = parse\n", false},
		{"defmodule", "module.exports = { parse }\nObject.defineProperty(module, 'exports', { value: {} })\n", false},
		{"spaced", "exports.parse = parse\nObject . assign(exports, { parse: other })\n", false},
		{"destructured", "exports.parse = parse\nconst { assign } = Object\nassign(exports, { parse: other })\n", false},
		{"logical", "exports.parse = parse\nconst e = typeof x === 'object' && exports\n", false},
		{"proto", "module.exports = { parse, __proto__: other }\n", true},
		{"main", "exports.parse = parse\nif (require.main === module) {}\nif (typeof exports === 'object') {}\n", true},
		// A function's this is whatever object it is called on, the exports
		// object among them (exports.init()); a class's method's is an instance.
		{"thisinfn", "exports.parse = parse\nfunction F() { this.parse = other }\n", false},
		{"method", "module.exports = { parse, keep: 1, init() { this.parse = other } }\n", false},
		{"fnexport", "exports.parse = parse\nexports.init = function () { this.parse = other }\n", false},
		{"getterthis", "exports.parse = parse\nObject.defineProperty(exports, 'z', { get() { this.parse = other } })\n", false},
		{"thiskey", "module.exports = { parse, init(k) { this[k] = other } }\n", false},
		// A class's method may be put on the exports object by any route
		// (exports.m = C.prototype.m): its this counts. return this does not
		// (the returned value's writes are beyond the scan); a constructor's
		// this is a new instance.
		{"classthis", "exports.parse = parse\nclass C { m() { this.parse = other } }\n", false},
		{"classkey", "exports.parse = parse\nclass C { m(k, v) { this[k] = v } }\n", false},
		{"classassign", "exports.parse = parse\nclass C { m(o) { Object.assign(this, o) } }\n", false},
		{"classself", "exports.parse = parse\nclass C { m() { const self = this; self.parse = other } }\n", false},
		{"classreturn", "exports.parse = parse\nexports.C = C\nclass C { m() { this.buf = 1; return this } }\n", true},
		{"arrowreturn", "exports.parse = parse\nfunction f() { return () => this }\n", true},
		{"version", "function keep() {}\nconst pkg = { version: 1 }\nclass P { feed() { this.buf = 1; return this } }\nmodule.exports = { parse, keep, P, version: pkg.version }\n", true},
		{"requirevalue", "module.exports = { parse }\nconst r = require\nr.cache[__filename].exports.parse = other\n", false},
		{"requiremodule", "module.exports = { parse }\nconst Module = require('module')\nModule._cache[__filename].exports.parse = other\n", false},
		{"mainmodule", "module.exports = { parse }\nprocess.mainModule.exports.parse = other\n", false},
		{"requireself", "module.exports = { parse }\nrequire(__filename).parse = other\n", false},
		{"ctorextract", "exports.parse = parse\nclass C { constructor() { this.parse = other } m() {} }\nexports.m = C.prototype.m\n", true},
		{"protoextract", "exports.parse = parse\nclass C { m() { this.parse = other } }\nconst m = C.prototype.m\nexports.m = m\n", false},
		{"ctorthis", "exports.parse = parse\nclass C { constructor() { this.parse = other; Object.assign(this, {}) } }\n", true},
		{"fieldthis", "exports.parse = parse\nclass C { x = this; static { this.parse = other } }\n", true},
		{"selfthis", "exports.parse = parse\nexports.keep = 1\nfunction init() { var self = this; self.parse = other }\n", false},
		{"assignthisfn", "exports.parse = parse\nfunction init() { Object.assign(this, { parse: other }) }\n", false},
		{"returnthis", "exports.parse = parse\nfunction self() { return this }\n", true}, // writes through the returned value: a known limit
		{"staticextract", "exports.parse = parse\nclass A { static init() { this.parse = other } }\nexports.init = A.init\n", false},
		{"fieldobject", "exports.parse = parse\nclass A { static o = { init() { this.parse = other } } }\n", false},
		{"modulekey", "module.exports = { parse }\nconst k = 'exports'\nmodule[k] = { parse: other }\n", false},
		{"templatekey", "module.exports = { parse }\nmodule[`exports`].parse = other\n", false},
		{"requiremain", "module.exports = { parse }\nrequire.main.exports.parse = other\n", false},
		{"requirecache2", "module.exports = { parse }\nrequire['cache'][__filename].exports = {}\n", false},
		{"moduleconstructor", "module.exports = { parse }\nmodule.constructor._cache[__filename].exports.parse = other\n", false},
		{"tschelper", "var __awaiter = (this && this.__awaiter) || function () {}\nexports.parse = parse\n", true},
		{"parentidiom", "exports.parse = parse\nif (!module.parent) {}\n", true},
		{"with", "module.exports = { parse }\nwith (module.exports) { parse = other }\n", false},
		{"eval", "module.exports = { parse }\neval('module.exports.parse = other')\n", false},
		{"field", "module.exports = { parse }\nclass A { static e = module.exports }\n", false},
		{"objdefault", "module.exports = { parse }\nconst { e = module.exports } = {}\n", false},
		{"tagged", "module.exports = { parse }\nfunction tag(s, o) {}\ntag`${module.exports}`\n", false},
		{"wrapperargs", "module.exports = { parse }\narguments[2].exports = { parse: other }\n", false},
		{"requirecache", "module.exports = { parse }\nrequire.cache[__filename].exports = { parse: other }\n", false},
		{"subscriptgetter", "module.exports = { parse }\nmodule.exports['__defineGetter__']('parse', () => other)\n", false},
		{"fnmodule", "function module() {}\nmodule.exports = { parse }\n", false},
		{"fnexports", "function exports() {}\nexports.parse = parse\n", false},
		{"tsc", "exports.keep = exports.parse = void 0\nexports.parse = parse\nexports.keep = other\n", true},
		{"voidafter", "exports.parse = parse\nexports.parse = void 0\n", false},
		// The Function constructor runs global code, which may reach the
		// exports through process (release gate H3): the export stays a
		// Candidate, as every CommonJS export does (commonjs-boundary).
		{"newfunction", "exports.parse = parse\nconst f = new Function('return 1')\n", true},
	}
	files := map[string]string{"base.js": "module.exports = {}\n"}
	use, calls := "", "function run() {\n"
	var want []expect
	for i, f := range forms {
		files[f.name+".js"] = fns + f.src
		use += fmt.Sprintf("const m%d = require('./%s')\n", i, f.name)
		calls += fmt.Sprintf("  m%d.parse()\n", i)
		w := expect{key: fmt.Sprintf("use.js:%d:parse", len(forms)+2+i), absentOK: true}
		if f.kept {
			w = expect{key: w.key, cand: f.name + ".js:parse"} // a CommonJS export: Candidate at most
		}
		want = append(want, w)
	}
	files["use.js"] = use + calls + "}\n"
	files["ts.ts"] = fns + "module.exports = { parse }\n;(module.exports as any).parse = other\n"
	files["ts2.ts"] = fns + "module.exports = { parse }\nmodule.exports!.parse = other\n"
	files["usets.ts"] = "const t = require('./ts')\nconst t2 = require('./ts2')\nexport function run() {\n  t.parse()\n  t2.parse()\n}\n"
	want = append(want, expect{key: "usets.ts:4:parse", absentOK: true}, expect{key: "usets.ts:5:parse", absentOK: true})
	check(t, resolveAll(t, files), want)
	// TypeScript's CommonJS output declares its exports with void 0 first.
	check(t, resolveAll(t, map[string]string{
		"tsc.js": fns + "exports.keep = exports.parse = void 0\nexports.parse = parse\nexports.keep = other\n",
		"u.js":   "const m = require('./tsc')\nm.parse()\nm.keep()\n",
	}), []expect{{key: "u.js:2:parse", cand: "tsc.js:parse"}, {key: "u.js:3:keep", cand: "tsc.js:other"}})
	// __proto__: x sets the prototype and defines no property.
	check(t, resolveAll(t, map[string]string{
		"proto.js": fns + "module.exports = { parse, __proto__: other }\n",
		"u.js":     "const m = require('./proto')\nm.__proto__()\n",
	}), []expect{{key: "u.js:2:__proto__", absentOK: true}})
	// The second replacement keeps a name it exports too: two exports, no pick.
	got := resolveAll(t, map[string]string{
		"two.js": fns + "module.exports = { parse, other }\nmodule.exports = { parse: other }\n",
		"u.js":   "const m = require('./two')\nm.parse()\nm.other()\n",
	})
	check(t, got, []expect{{key: "u.js:2:parse", absentOK: true}, {key: "u.js:3:other", absentOK: true}})
}

// TestECMAScriptCommonJSRootAudit pins forms that reach a module's exports
// object (the counterexamples of the release gate's audit, archived; see
// docs/development/web-semantic-fidelity/README.md): no form keeps a Strong
// or Exact target, since a CommonJS export is a Candidate at most
// (commonjs-boundary); the listed reads keep the export as a Candidate.
func TestECMAScriptCommonJSRootAudit(t *testing.T) {
	const fns = "function parse() {}\nfunction other() {}\n"
	forms := []struct {
		name, src string
		kept      bool
	}{
		{"cachealias", "module.exports = { parse }\nconst cache = require.cache\ncache[__filename].exports.parse = other\n", false},
		{"ctoralias", "module.exports = { parse }\nconst M = module.constructor\nM._cache[__filename].exports.parse = other\n", false},
		{"parentalias", "module.exports = { parse }\nconst P = module.parent\nP.children[0].exports.parse = other\n", false},
		{"modulerequire", "module.exports = { parse }\nmodule.require(__filename).parse = other\n", false},
		{"patternexports", "({ exports } = { exports: {} })\nexports.parse = parse\n", false},
		{"varpattern", "var { exports } = { exports: {} }\nexports.parse = parse\n", false},
		{"modulepattern", "var { module } = { module: {} }\nmodule.exports = { parse }\n", false},
		{"superwrite", "module.exports = { parse, reset() { super.parse = other } }\n", false},
		{"arrowarguments", "exports.parse = parse\nconst f = () => { arguments[0].parse = other }\n", false},
		{"fieldkey", "exports.parse = parse\nclass A { [(this.parse = other, 'k')] = 1 }\n", false},
		{"protowrite", "exports.__proto__ = { set parse(v) {} }\nexports.parse = parse\n", false},
		{"setproto", "Object.setPrototypeOf(exports, {})\nexports.parse = parse\n", false},
		{"arrowself", "exports.parse = parse\nconst self = () => this\nself().parse = other\n", false},
		{"heritage", "exports.parse = parse\nconst mixin = (o) => class {}\nclass A extends mixin({ constructor() { this.parse = other } }) {}\n", false},
		{"dynamicrequire", "module.exports = { parse }\nconst name = './m'\nrequire(name).parse = other\n", false},
		// The release gate's final review (H1-H6, M1).
		{"computedcall", "exports.parse = parse\nconst k = '__define' + 'Getter__'\nexports[k]('parse', () => other)\n", false},
		{"thiscomputedcall", "exports.parse = parse\nconst k = '__define' + 'Getter__'\nthis[k]('parse', () => other)\n", false},
		{"valueofwrite", "exports.parse = parse\nexports.valueOf().parse = other\n", false},
		{"getbuiltin", "module.exports = { parse }\nprocess.getBuiltinModule('module')._cache[__filename].exports.parse = other\n", false},
		{"processalias", "module.exports = { parse }\nconst p = process\np.mainModule.exports.parse = other\n", false},
		{"processcomputed", "module.exports = { parse }\nprocess['main' + 'Module'].exports.parse = other\n", false},
		{"globalprocess", "module.exports = { parse }\nglobalThis.process.mainModule.exports.parse = other\n", false},
		{"functionctor", "module.exports = { parse }\nnew Function('f', 'o', 'process.mainModule.exports.parse = o')(__filename, other)\n", false},
		{"ctorcall", "module.exports = { parse }\nconst F = (() => {}).constructor\n;(() => {}).constructor('process.mainModule.exports.parse = 1')()\n", false},
		{"stringtimeout", "module.exports = { parse }\nsetTimeout('process.mainModule.exports.parse = 1', 0)\n", false},
		{"requirevm", "module.exports = { parse }\nconst vm = require('vm')\n", false},
		{"catchvar", "try { throw 0 } catch (e) { var exports = {} }\nexports.parse = parse\n", false},
		{"catchvarmodule", "try { throw 0 } catch (e) { var module = { exports: {} } }\nmodule.exports = { parse }\n", false},
		{"methodkey", "exports.parse = parse\nclass A { [(arguments[0].parse = other, 'k')]() {} }\n", false},
		{"objmethodkey", "exports.parse = parse\nconst o = { [(arguments[0].parse = other, 'k')]() {} }\n", false},
		{"instanceofleft", "exports.parse = parse\nclass Evil { static [Symbol.hasInstance](o) { o.parse = other; return false } }\nif (exports instanceof Evil) {}\n", false},
		{"caller", "module.exports = { parse }\nfunction g() { return g.caller.arguments[0] }\n", false},
		{"callee", "module.exports = { parse }\nfunction g() { return arguments.callee }\n", false},
		{"stacktrace", "module.exports = { parse }\nError.prepareStackTrace = (e, s) => s\n", false},
		{"moduleexportscomputed", "module.exports = { parse }\nconst k = '__define' + 'Getter__'\nmodule.exports[k]('parse', () => other)\n", false},
		{"indirecteval", "module.exports = { parse }\n;(0, eval)('process.mainModule.exports.parse = 1')\n", false},
		// The release gate's third review (N1-N9, R1, R2).
		{"reflectcaller", "module.exports = { parse }\nfunction g() { return Reflect.get(g, 'caller') }\n", false},
		{"fnarguments", "module.exports = { parse }\nfunction g() { return h.arguments }\nfunction h() {}\n", false},
		{"errorwrite", "module.exports = { parse }\nconst k = 'prepare' + 'StackTrace'\nError[k] = (e, s) => s\n", false},
		{"errordefine", "module.exports = { parse }\nObject.defineProperty(Error, 'prepareStack' + 'Trace', { value: (e, s) => s })\n", false},
		{"requireprocess", "module.exports = { parse }\nrequire('process').getBuiltinModule('module')._cache[__filename].exports.parse = other\n", false},
		{"sloppythis", "module.exports = { parse }\nfunction g() { this.process.getBuiltinModule('module')._cache[__filename].exports.parse = other }\n", false},
		{"dynamicimport", "module.exports = { parse }\nimport('node:module').then((M) => {})\n", false},
		{"dynamicimportself", "module.exports = { parse }\nconst u = __filename\nimport(u).then((ns) => {})\n", false},
		{"parenctor", "module.exports = { parse }\nconst f = () => {}\nnew (f.constructor)('return 1')\n", false},
		{"commactor", "module.exports = { parse }\nconst f = () => {}\n;(0, f.constructor)('return 1')\n", false},
		{"errorcomputed", "module.exports = { parse }\nconst k = '__define' + 'Getter__'\nError[k]('prepare' + 'StackTrace', () => (e, s) => s)\n", false},
		{"supervalueof", "module.exports = { parse, init() { super.valueOf().parse = other } }\n", false},
		{"extracteddefine", "exports.parse = parse\nexports.dg = Object.prototype.__defineGetter__\n", false},
		{"protoliteral", "exports.parse = parse\nconst set = Object.getOwnPropertyDescriptor(Object.prototype, '__proto__').set\n", false},
		{"shadowobject", "const Object = { assign(t) { t.parse = other } }\nexports.parse = parse\nObject.assign(exports, {})\n", false},
		{"inspector", "module.exports = { parse }\nconst inspector = require('inspector')\n", false},
		{"es5instanceof", "function Foo() { if (!(this instanceof Foo)) return new Foo() }\nexports.parse = parse\nexports.Foo = Foo\n", false},
		{"processflags", "exports.parse = parse\nif (process.browser || process.noDeprecation) {}\n", true},
		{"errorcalls", "exports.parse = parse\nfunction E(m) { const e = new Error(m); Error.captureStackTrace(e, E); if (e instanceof Error) {} return Error.stackTraceLimit }\n", true},
		{"errorclass", "exports.parse = parse\nclass E extends Error { constructor(m) { super(m); Error.captureStackTrace(this, this.constructor) } }\nfunction F() { Error.call(this) }\nF.prototype = Object.create(Error.prototype)\n", false},
		{"errorclassnothis", "exports.parse = parse\nclass E extends Error { constructor(m) { super(m); Error.captureStackTrace(this, this.constructor) } }\nconst p = Object.create(Error.prototype)\n", true},
		{"literalimport", "exports.parse = parse\nimport('./other.js').then(() => {})\n", true},
		{"processenv", "exports.parse = parse\nconst d = process.env.DEBUG\nconst a = process.argv.slice(2)\nif (typeof process === 'object') {}\nprocess.on('exit', () => {})\n", true},
		{"mainmoduleidiom", "exports.parse = parse\nif (process.mainModule === module) {}\n", true},
		{"globalread", "exports.parse = parse\nconst f = global.fetch\nconst g = typeof globalThis === 'object'\n", true},
		{"d7arrayobject", "const [Object] = [{ assign(t) { t.parse = other } }]\nexports.parse = parse\nObject.assign(exports, {})\n", false},
		{"d7catchobject", "exports.parse = parse\ntry { throw { assign(t) { t.parse = other } } } catch (Object) { Object.assign(exports, {}) }\n", false},
		{"d7destructobject", "const { Object } = { Object: { assign(t) { t.parse = other } } }\nexports.parse = parse\nObject.assign(exports, {})\n", false},
		{"d7errpstshorthand", "exports.parse = parse\nconst prepareStackTrace = (e, s) => s\nObject.assign(globalThis.Error, { prepareStackTrace })\nnew Error().stack[0].getThis().parse = other\n", false},
		{"d7globalerrorpst", "exports.parse = parse\nObject.assign(global.Error, { prepareStackTrace: (e, s) => s })\nconst s = new Error().stack\ns[0].getThis().parse = other\n", false},
		{"d7instofcall", "exports.parse = parse\nvar Foo = Object.defineProperty(function () {}, Object.getOwnPropertySymbols(Function.prototype)[0], { value(o) { o.parse = other; return false } })\nif (this instanceof Foo) {}\n", false},
		{"d7instofreassign", "exports.parse = parse\nfunction Foo() {}\nFoo = Object.defineProperty(function () {}, Object.getOwnPropertySymbols(Function.prototype)[0], { value(o) { o.parse = other; return false } })\nif (exports instanceof Foo) {}\n", false},
		{"d7instofreq", "exports.parse = parse\nvar Foo = require('./evil')\nif (exports instanceof Foo) {}\n", false},
		{"d7instofshadow", "exports.parse = parse\nfunction Foo() {}\nfunction g(Foo) { return exports instanceof Foo }\nconst sym = Object.getOwnPropertySymbols(Function.prototype)[0]\ng(Object.defineProperty(function () {}, sym, { value(o) { o.parse = other; return false } }))\n", false},
		{"d7paramobject", "exports.parse = parse\nfunction f(Object) { Object.assign(exports, {}) }\nf({ assign(t) { t.parse = other } })\n", false},
		{"d7paramreflect", "exports.parse = parse\nfunction f(Reflect) { Reflect.set(exports, 'x', 1) }\nf({ set(t) { t.parse = other } })\n", false},
		{"d7rangeerrorpst", "exports.parse = parse\nObject.assign(Object.getPrototypeOf(RangeError), { prepareStackTrace: (e, s) => s })\nconst s = new Error().stack\ns[0].getThis().parse = other\n", false},
		{"d7reqvalueof", "module.exports = { parse }\nconst name = __filename\nrequire(name).valueOf().parse = other\n", false},
		{"d7reqvalueofT", "module.exports = { parse }\nrequire(`${__filename}`).valueOf().parse = other\n", false},
		{"d7reqvalueofidx", "module.exports = { parse }\nconst name = __filename\nrequire(name)['valueOf']().parse = other\n", false},
		{"d7es5class", "var Foo = /** @class */ (function () {\n  function Foo() { if (!(this instanceof Foo)) return new Foo() }\n  Foo.prototype.m = function () { return 1 };\n  return Foo;\n}());\nexports.parse = parse\nexports.Foo = Foo\n", false},
		{"d7es5localbase", "var A = (function () { function A() {} return A }())\nvar B = (function (_super) { __extends(B, _super); function B() { if (!(this instanceof B)) {} } return B }(A))\nexports.parse = parse\n", false},
		{"d7es5outerbase", "var X = require('./x')\nvar B = (function (_super) { __extends(B, _super); function B() { if (!(this instanceof B)) {} } return B }(X.Base))\nexports.parse = parse\n", false},
		{"d7es5earlyreturn", "var B = (function () { function B() {} if (cond) return require('./evil'); return B }())\nif (exports instanceof B) {}\nexports.parse = parse\n", false},
		{"d7classouterbase", "const X = require('./x')\nclass B extends X {}\nif (exports instanceof B) {}\nexports.parse = parse\n", false},
		{"d7classerrorbase", "class B extends Error {}\nif (exports instanceof B) {}\nexports.parse = parse\n", false},
		{"d7ownkeys", "function Foo() {}\nconst k = Reflect.ownKeys(Function.prototype)\nif (exports instanceof Foo) {}\nexports.parse = parse\n", false},
		{"d7reqtostring", "module.exports = { parse }\nconst n = __filename\nconst s = require(n).toString()\n", false},
		{"d7objectread", "exports.parse = parse\nconst ks = Object.keys({ a: 1 })\nObject.freeze(ks)\n", true},
		// Recall: escapes the decoder reads, a comment naming a rule's word.
		{"escapedplain", "exports.parse = parse\nconst s = 'a\\x41\\u0042\\u{43}\\n\\'' + `\\x44`\n", true},
		{"midlineclose", "exports.parse = parse\n// ~1.2 --> >=1.2.0\nlet i = 3\nwhile (i --> 0) {}\n", true},
		{"jsdocctor", "class E extends Error {\n  /** @param {string} m */\n  constructor(m) { super(m); this.name = this.constructor.name }\n}\nexports.parse = parse\nexports.E = E\n", true},
		{"fieldctor", "class E {\n  x = 1;\n  // the constructor\n  constructor() { this.y = 2 }\n}\nexports.parse = parse\nexports.E = E\n", true},
		{"commentpst", "exports.parse = parse\n// Error.prepareStackTrace is not used here\n", true},
		{"functionread", "exports.parse = parse\nconst s = Function.prototype.toString.call(parse)\nif (parse instanceof Function) {}\n", true},
		{"newctor", "exports.parse = parse\nclass C { copy() { return new this.constructor(1) } }\n", true},
		{"fntimeout", "exports.parse = parse\nsetTimeout(() => {}, 0)\nconst h = exports.hasOwnProperty('parse')\n", true},
		// Reads that write nothing keep the exports.
		{"mainidiom", "exports.parse = parse\nif (require.main === module) {}\nif (!module.parent) {}\n", true},
		// The ts-14 scan drops the exports on a read of require.main (a
		// conservative loss of the Candidate).
		{"mainfilename", "exports.parse = parse\nconst f = require.main.filename\nconst r = require.resolve('./x')\n", false},
		{"typeofexports", "exports.parse = parse\nif (typeof exports === 'object' && typeof module === 'object') {}\nconst id = module.id\n", true},
		{"thisread", "exports.parse = parse\nconst v = this.version\n", true},
		{"fnreturnthis", "exports.parse = parse\nfunction chain() { return this }\n", true},
		{"literalrequire", "exports.parse = parse\nconst path = require('path')\n", true},
		{"dynamicread", "exports.parse = parse\nconst mod = 'x'\nconst fn = require(mod).__express\n", false},
		{"dynamiccache", "module.exports = { parse }\nconst mod = 'module'\nrequire(mod)._cache[__filename].exports.parse = other\n", false},
		{"dynamicvalue", "module.exports = { parse }\nconst mod = './m'\nconst me = require(mod)\nme.parse = other\n", false},
	}
	files := map[string]string{}
	use, calls := "", "function run() {\n"
	var want []expect
	for i, f := range forms {
		files[f.name+".js"] = fns + f.src
		use += fmt.Sprintf("const m%d = require('./%s')\n", i, f.name)
		calls += fmt.Sprintf("  m%d.parse()\n", i)
		w := expect{key: fmt.Sprintf("use.js:%d:parse", len(forms)+2+i), absentOK: true}
		if f.kept {
			w = expect{key: w.key, cand: f.name + ".js:parse"} // a CommonJS export: Candidate at most
		}
		want = append(want, w)
	}
	files["use.js"] = use + calls + "}\n"
	check(t, resolveAll(t, files), want)
}

// TestECMAScriptReleaseGateCases pins the release gate's mandatory cases: a
// Candidate only where the value a use reads is the one stated, the
// module's own lexical references kept Exact.
func TestECMAScriptReleaseGateCases(t *testing.T) {
	files := map[string]string{
		"c1.js":  "function f() { return this.value }\nmodule.exports = { f }\n",
		"c2.js":  "const obj = { f() { return this.value } }\nmodule.exports = obj\n",
		"c3.js":  "const f = () => this.value\nmodule.exports = { f }\n",
		"c4.js":  "function original() {}\nfunction replacement() {}\nmodule.exports = { parse: original }\nObject.assign(module.exports, { parse: replacement })\n",
		"c5.js":  "const first = () => {}\nconst second = () => {}\nmodule.exports = { parse: first, ...{ parse: second } }\n",
		"c6.js":  "const parse = () => {}\nconst other = () => {}\nconst key = 'parse'\nmodule.exports = { parse, [key]: other }\n",
		"c8.js":  "const parse = () => {}\nmodule.exports = { parse }\nconst cache = require.cache\n",
		"c9.js":  "const parse = () => {}\nconst other = () => {}\nlet key\nmodule.exports = { parse }\nmodule.exports[key] = other\n",
		"c10.js": "const parse = () => {}\nmodule.exports = { get parse() { return parse } }\n",
		"c11.js": "function original() {}\nfunction replacement() {}\nmodule.exports = { parse: original }\nmodule.exports.parse = replacement\nfunction local() { return original() }\n",
		"use.js": "const m1 = require('./c1')\nconst m2 = require('./c2')\nconst m3 = require('./c3')\nconst m4 = require('./c4')\nconst m5 = require('./c5')\nconst m6 = require('./c6')\n" +
			"const m8 = require('./c8')\nconst m9 = require('./c9')\nconst m10 = require('./c10')\nconst m11 = require('./c11')\n" +
			"function run() {\n" + // 11
			"  m1.f()\n" + // 12
			"  m2.f()\n" + // 13
			"  m3.f()\n" + // 14
			"  m4.parse()\n" + // 15
			"  m5.parse()\n" + // 16
			"  m6.parse()\n" + // 17
			"  m8.parse()\n" + // 18
			"  m9.parse()\n" + // 19
			"  m10.parse()\n" + // 20
			"  m11.parse()\n" + // 21
			"}\n",
	}
	check(t, resolveAll(t, files), []expect{
		{key: "use.js:12:f", cand: "c1.js:f"},
		{key: "use.js:13:f", cand: "c2.js:obj.f"}, // the member (values.go, D11)
		{key: "use.js:14:f", cand: "c3.js:f"},
		{key: "use.js:15:parse", absentOK: true},
		{key: "use.js:16:parse", absentOK: true},
		{key: "use.js:17:parse", absentOK: true},
		{key: "use.js:18:parse", absentOK: true}, // require.cache let out: the exports may be replaced
		{key: "use.js:19:parse", absentOK: true},
		{key: "use.js:20:parse", absentOK: true},
		{key: "use.js:21:parse", absentOK: true},
		{key: "c11.js:5:original", target: "c11.js:original", conf: "exact"}, // the lexical reference stays
	})
}

// TestECMAScriptReleaseGateD8 pins the counterexamples of the release gate's
// independent reviews D8-D12 (archived; see the ledger's README): forms that
// change the export at run time (checked with node; the TypeScript ones by
// their JavaScript semantics) and must keep no target.
func TestECMAScriptReleaseGateD8(t *testing.T) {
	const fns = "function parse() {}\nfunction other() {}\n"
	forms := []struct {
		name, ext, src string
		extra          map[string]string
	}{
		{"a1parenobject", ".js", "exports.parse = parse\n;(Object) = { assign(t) { t.parse = other } }\nObject.assign(exports, {})\n", nil},
		{"a2arrparen", ".js", "exports.parse = parse\n;[(Object)] = [{ assign(t) { t.parse = other } }]\nObject.assign(exports, {})\n", nil},
		{"a3pairparen", ".js", "exports.parse = parse\n;({ a: (Reflect) } = { a: { set(t) { t.parse = other } } })\nReflect.set(exports, 'x', 1)\n", nil},
		{"a4forofparen", ".js", "exports.parse = parse\nfor ((Object) of [{ assign(t) { t.parse = other } }]) Object.assign(exports, {})\n", nil},
		{"a5parenctor", ".js", "exports.parse = parse\nfunction Foo() {}\n;(Foo) = require('./evil')\nif (exports instanceof Foo) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"a6objdefault", ".js", "exports.parse = parse\nconst { Object = { assign(t) { t.parse = other } } } = {}\nObject.assign(exports, {})\n", nil},
		{"a7paramdefault", ".js", "exports.parse = parse\nfunction f({ Reflect = { set(t) { t.parse = other } } }) { Reflect.set(exports, 'x', 1) }\nf({})\n", nil},
		{"a8ctordefault", ".js", "exports.parse = parse\nfunction Foo() {}\nvar { Foo = require('./evil') } = {}\nif (exports instanceof Foo) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"a9assigndefault", ".js", "exports.parse = parse\n;({ Object = { assign(t) { t.parse = other } } } = {})\nObject.assign(exports, {})\n", nil},
		{"b1escexports", ".js", "exports.parse = parse\nexports.parse = other\n", nil},
		{"b2escmodule", ".js", "module.exports = { parse }\nmodule.exports.parse = other\n", nil},
		{"b2bescvalueof", ".js", "exports.parse = parse\nexports.valueOf().parse = other\n", nil},
		{"b3eschasinst", ".js", "exports.parse = parse\nclass Evil { static [Symbol.hasInstance](o) { o.parse = other; return false } }\nif (exports instanceof Evil) {}\n", nil},
		{"b4escpst", ".js", "exports.parse = parse\nObject.getPrototypeOf(RangeError).prepareStackTrace = (e, s) => s\nnew Error().stack[0].getThis().parse = other\n", nil},
		{"b5strpst", ".js", "exports.parse = parse\nReflect.set(Object.getPrototypeOf(RangeError), 'prepare\\x53tackTrace', (e, s) => s)\nnew Error().stack[0].getThis().parse = other\n", nil},
		{"b5bassignpst", ".js", "exports.parse = parse\nObject.assign(Object.getPrototypeOf(RangeError), { prepareStackTrace: (e, s) => s })\nnew Error().stack[0].getThis().parse = other\n", nil},
		{"b8escimport", ".js", "module.exports = { parse }\nimport('node:mod\\x75le').then((M) => { M.default._cache[__filename].exports.parse = other })\n", nil},
		{"b10escsyms", ".js", "exports.parse = parse\nfunction Foo() {}\nconst sym = Object.getOwnPropertySymbols(Function.prototype)[0]\nObject.defineProperty(Foo, sym, { value(o) { o.parse = other; return false } })\nif (exports instanceof Foo) {}\n", nil},
		{"b12escrequire", ".js", "module.exports = { parse }\nrequire.cache[__filename].exports.parse = other\n", nil},
		{"b14esceval", ".js", "module.exports = { parse }\neval('module.exports.parse = other')\n", nil},
		{"c1setproto", ".js", "exports.parse = parse\nfunction Foo() {}\nObject.setPrototypeOf(Foo, require('./evil'))\nif (exports instanceof Foo) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"c2createclass", ".js", "exports.parse = parse\nfunction _createClass() { return require('./evil') }\nvar Foo = (function () { function Foo() {} return _createClass(Foo) }())\nif (exports instanceof Foo) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"c3namedfnexpr", ".js", "exports.parse = parse\nconst x = function Foo() {}\nglobal.Foo = require('./evil')\nif (exports instanceof Foo) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"c3bnamedclassexpr", ".js", "exports.parse = parse\nconst x = class Foo {}\nglobal.Foo = require('./evil')\nif (exports instanceof Foo) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"c4staticblock", ".js", "exports.parse = parse\nclass Foo { static { Object.setPrototypeOf(this, require('./evil')) } }\nif (exports instanceof Foo) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"c5gopn", ".js", "exports.parse = parse\nconst sym = Object.getOwnPropertyNames(Symbol).map((n) => Symbol[n]).filter((s) => typeof s === 'symbol')\nclass Foo {}\nfor (const s of sym) try { Object.defineProperty(Foo, s, { value(o) { o.parse = other; return false } }) } catch (e) {}\nif (exports instanceof Foo) {}\n", nil},
		{"c6extendsetproto", ".js", "exports.parse = parse\nclass A {}\nclass B extends A {}\nObject.setPrototypeOf(A, require('./evil'))\nif (exports instanceof B) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"c7iifeextends", ".js", "exports.parse = parse\nvar __extends = function (d, b) { Object.setPrototypeOf(d, require('./evil')) }\nvar B = (function () { function B() {} __extends(B, null); return B }())\nif (exports instanceof B) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"b6bstrcaller", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, 'call\\x65r'), 'argum\\x65nts')[0].parse = other }\ng()\n", nil},
		{"b6ctplcaller", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, `caller`), `arguments`)[0].parse = other }\ng()\n", nil},
		{"b15escdefine", ".js", "exports.parse = parse\nexports.dg = Object.prototype['__defineG\\x65tter__']\nexports.dg('parse', () => other)\n", nil},
		{"b16tpldefine", ".js", "exports.parse = parse\nexports.dg = Object.prototype[`__defineGetter__`]\nexports.dg('parse', () => other)\n", nil},
		{"b17escprotostr", ".js", "exports.parse = parse\nconst set = Object.getOwnPropertyDescriptor(Object.prototype, '\\x5f_proto__').set\nexports.sp = set\nexports.sp({ get parse() { return other } })\ndelete exports.parse\n", nil},
		{"b18escrequiremod", ".js", "module.exports = { parse }\nrequire('mod\\x75le')._cache[__filename].exports.parse = other\n", nil},
		{"b19escrequireproc", ".js", "module.exports = { parse }\nrequire('proc\\x65ss').getBuiltinModule('module')._cache[__filename].exports.parse = other\n", nil},
		{"b20escimportproc", ".js", "module.exports = { parse }\nimport('proc\\x65ss').then((P) => { P.default.getBuiltinModule('module')._cache[__filename].exports.parse = other })\n", nil},
		{"a10errdefault", ".js", "exports.parse = parse\nconst { Error = require('./evil') } = {}\nclass B extends Error {}\nif (exports instanceof B) {}\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"a11objdefaultcatch", ".js", "exports.parse = parse\ntry { throw {} } catch ({ Object = { assign(t) { t.parse = other } } }) { Object.assign(exports, {}) }\n", nil},
		{"a12arrowparen", ".js", "exports.parse = parse\nconst f = (Object) => Object.assign(exports, {})\nf({ assign(t) { t.parse = other } })\n", nil},
		{"a13fordefault", ".js", "exports.parse = parse\nfor (const { Object = { assign(t) { t.parse = other } } } of [{}]) Object.assign(exports, {})\n", nil},
		{"d23arrdestruct", ".js", "exports.parse = parse\n;[exports.parse] = [other]\n", nil},
		{"d24forof", ".js", "exports.parse = parse\nfor (exports.parse of [other]);\n", nil},
		{"d26andassign", ".js", "exports.parse = parse\nexports.parse &&= other\n", nil},
		{"d27destruct", ".js", "module.exports = { parse }\n;({ parse: module.exports.parse } = { parse: other })\n", nil},
		{"d29assignthis", ".js", "exports.parse = parse\nObject.assign(this, { parse: other })\n", nil},
		{"d9assignsrc", ".js", "exports.parse = parse\nconst src = { parse: other }\nObject.assign(exports, src)\n", nil},
		{"d13delete", ".js", "exports.parse = parse\ndelete exports.parse\n", nil},
		{"d30spreadassign", ".js", "exports.parse = parse\nconst src = { parse: other }\nObject.assign(exports, { ...src })\n", nil},
		{"d31definegetter", ".js", "exports.parse = parse\nObject.defineProperties(exports, { parse: { get: () => other } })\n", nil},
		{"d32tostring", ".js", "exports.parse = parse\nexports.toString = function () { exports.parse = other; return '' }\nconst s = '' + exports\n", nil},
		{"d33reqname", ".js", "module.exports = { parse }\nconst n = __filename\nrequire(n).hasOwnProperty('x')\nObject.assign(require(n), { parse: other })\n", nil},
		{"d34moduleexportsparen", ".js", "module.exports = { parse }\n;(module.exports).parse = other\n", nil},
		{"d35superassign", ".js", "module.exports = { parse, init() { Object.assign(super.valueOf(), { parse: other }) } }\nmodule.exports.init()\n", nil},
		{"d36thisarrowmutate", ".js", "exports.parse = parse\nconst f = () => Object.assign(this, { parse: other })\nf()\n", nil},
		{"d37exportsinstanceofparam", ".js", "exports.parse = parse\nfunction Foo() {}\nconst g = (x) => exports instanceof x\ng(require('./evil'))\n", map[string]string{"evil.js": "module.exports = class Evil { static [Symbol.hasInstance](o) { o.parse = function other() {}; return false } }\n"}},
		{"t1namespace", ".ts", "exports.parse = parse\nnamespace Object { export function assign(t: any, s: any) { t.parse = other } }\nObject.assign(exports, {})\n", nil},
		{"t2enumobj", ".ts", "exports.parse = parse\nimport Object = require('./evilobj')\nObject.assign(exports, {})\n", map[string]string{"evilobj.ts": "exports.assign = function (t: any) { t.parse = 1 }\n"}},
		{"t3parenas", ".ts", "exports.parse = parse\n;(Object as any) = { assign(t: any) { t.parse = other } }\nObject.assign(exports, {})\n", nil},
		{"t2b", ".ts", "exports.parse = parse\nimport Object = require('./evilobj.ts')\nObject.assign(exports, {})\n", map[string]string{"evilobj.ts": "exports.assign = function (t: any) { t.parse = function other() {} }\n"}},
		{"e1dynmodule", ".js", "module.exports = { parse }\nconst n = 'module'\nrequire(n).Module._cache[__filename].exports.parse = other\n", nil},
		{"e2dynprocess", ".js", "module.exports = { parse }\nconst n = 'process'\nrequire(n).getBuiltinModule('module')._cache[__filename].exports.parse = other\n", nil},
		{"e3dyncreaterequire", ".js", "module.exports = { parse }\nconst n = 'module'\nrequire(n).createRequire(__filename)(__filename).parse = other\n", nil},
		// The seventh review (A3f): line continuations after LS / PS, plain
		// literals of frame keys held in constants, templates around them.
		{"ctlbad", ".js", "exports.parse = parse\nexports.parse = other\n", nil},
		{"d1ls", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, 'call\\\u2028er'), 'argum\\\u2028ents')[0].parse = other }\ng()\n", nil},
		{"d2ps", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, 'call\\\u2029er'), 'argum\\\u2029ents')[0].parse = other }\ng()\n", nil},
		{"d3tplesc", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, `call\\x65r`), `argum\\x65nts`)[0].parse = other }\ng()\n", nil},
		{"d4ubrace", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, 'call\\u{000065}r'), 'argum\\u{65}nts')[0].parse = other }\ng()\n", nil},
		{"d5lc", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, 'call\\\ner'), 'argum\\\nents')[0].parse = other }\ng()\n", nil},
		{"d6crlf", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, 'call\\\r\ner'), 'argum\\\r\nents')[0].parse = other }\ng()\n", nil},
		{"d7tplls", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, `call\\\u2028er`), `argum\\\u2028ents`)[0].parse = other }\ng()\n", nil},
		{"d8tagraw", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, String.raw`caller`), String.raw`arguments`)[0].parse = other }\ng()\n", nil},
		{"d9surr", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, 'caller'), 'arguments')[0].parse = other }\ng()\n", nil},
		{"i1escexports", ".js", "exports.parse = parse\nexports.parse = other\n", nil},
		{"i2escmodule", ".js", "module.exports = { parse }\nmodule.exports.parse = other\n", nil},
		{"i3escprop", ".js", "exports.parse = parse\nfunction g() { g.caller.arguments[0].parse = other }\ng()\n", nil},
		{"i4escpst", ".js", "exports.parse = parse\nObject.getPrototypeOf(RangeError).prepareStackTrace = (e, s) => s\nnew Error().stack[0].getThis().parse = other\n", nil},
		{"i5escrequire", ".js", "module.exports = { parse }\nrequire.cache[__filename].exports.parse = other\n", nil},
		{"i6escobject", ".js", "exports.parse = parse\nconst Object = { assign(t) { t.parse = other } }\nObject.assign(exports, {})\n", nil},
		{"i7escdefget", ".js", "exports.parse = parse\nexports.dg = Object.prototype.__defineGetter__\nexports.dg('parse', () => other)\n", nil},
		{"i8escthis", ".js", "exports.parse = parse\nthis.parse = other\n", nil},
		{"i9escarguments", ".js", "exports.parse = parse\narguments[0].parse = other\n", nil},
		{"i10escshort", ".js", "exports.parse = parse\nconst { exports: e2 } = { exports }\ne2.parse = other\n", nil},
		{"i11escpropid", ".js", "module.exports = { parse }\nmodule.exports.parse = other\n", nil},
		{"k1constcaller", ".js", "exports.parse = parse\nconst a = 'caller', b = 'arguments'\nfunction g() { Reflect.get(Reflect.get(g, a), b)[0].parse = other }\ng()\n", nil},
		{"k2constcaller2", ".js", "exports.parse = parse\nconst a = 'caller', b = 'arguments'\nfunction g() { g[a][b][0].parse = other }\ng()\n", nil},
		{"k3constdefget", ".js", "exports.parse = parse\nconst k = '__defineGetter__'\nexports.dg = Object.prototype[k]\nexports.dg('parse', () => other)\n", nil},
		{"k5arrcaller", ".js", "exports.parse = parse\nconst [a, b] = ['caller', 'arguments']\nfunction g() { g[a][b][0].parse = other }\ng()\n", nil},
		{"k7forin", ".js", "exports.parse = parse\nconst o = { caller: 1, arguments: 2 }\nconst ks = Object.keys(o)\nfunction g() { g[ks[0]][ks[1]][0].parse = other }\ng()\n", nil},
		{"k8importlit", ".js", "module.exports = { parse }\nconst s = 'node:module'\nimport(s).then((M) => { M.default._cache[__filename].exports.parse = other })\n", nil},
		{"s2wholesub", ".js", "exports.parse = parse\nfunction g() { Reflect.get(Reflect.get(g, `${'caller'}`), `${'arguments'}`)[0].parse = other }\ng()\n", nil},
		{"s3subdefget", ".js", "exports.parse = parse\nexports.dg = Object.prototype[`${'__defineGetter__'}`]\nexports.dg('parse', () => other)\n", nil},
		{"s4subrequire", ".js", "module.exports = { parse }\nrequire(`${'module'}`)._cache[__filename].exports.parse = other\n", nil},
		{"s5subimport", ".js", "module.exports = { parse }\nimport(`node:${'module'}`).then((M) => { M.default._cache[__filename].exports.parse = other })\n", nil},
		{"s7subkeyexports", ".js", "exports.parse = parse\nexports[`${'parse'}`] = other\n", nil},
		{"s8tplkey", ".js", "exports.parse = parse\nexports[`parse`] = other\n", nil},
		{"s9tplkeyesc", ".js", "exports.parse = parse\nexports[`pars\\x65`] = other\n", nil},
		{"w1withobject", ".js", "exports.parse = parse\nwith ({ Object: { assign(t) { t.parse = other } } }) Object.assign(exports, {})\n", nil},
		{"w2withreflect", ".js", "exports.parse = parse\nwith ({ Reflect: { set(t) { t.parse = other } } }) Reflect.set(exports, 'x', 1)\n", nil},
		{"w3withreq", ".js", "exports.parse = parse\nwith (require('./evil')) assign(exports)\n", map[string]string{"evil.js": "module.exports = { parse: function other() {}, assign(t) { t.parse = function other() {} } }\n"}},
		{"w4withvar", ".js", "exports.parse = parse\nconst o = { Object: { assign(t) { t.parse = other } } }\nwith (o) { Object.assign(exports, {}) }\n", nil},
		{"w5withfn", ".js", "exports.parse = parse\nfunction f(o) { with (o) Object.assign(exports, {}) }\nf({ Object: { assign(t) { t.parse = other } } })\n", nil},
		{"r2call", ".js", "module.exports = { parse }\nconst n = 'module'\nrequire.call(null, n)._cache[__filename].exports.parse = other\n", nil},
		{"r3seq", ".js", "module.exports = { parse }\nconst n = 'module'\n;(0, require)(n)._cache[__filename].exports.parse = other\n", nil},
		{"r4alias", ".js", "module.exports = { parse }\nconst r = require\nconst n = 'module'\nr(n)._cache[__filename].exports.parse = other\n", nil},
		{"r6paren", ".js", "module.exports = { parse }\nconst n = 'module'\n;(require)(n)._cache[__filename].exports.parse = other\n", nil},
		{"o1catch", ".js", "exports.parse = parse\ntry { throw { assign(t) { t.parse = other } } } catch (Object) { Object.assign(exports, {}) }\n", nil},
		{"o4class", ".js", "exports.parse = parse\nclass Object { static assign(t) { t.parse = other } }\nObject.assign(exports, {})\n", nil},
		{"o6funcname", ".js", "exports.parse = parse\nfunction Object() {}\nObject.assign = (t) => { t.parse = other }\nObject.assign(exports, {})\n", nil},
		{"m1dupgetter", ".js", "module.exports = { parse, get parse() { return other } }\n", nil},
		{"m2dupcomputed", ".js", "module.exports = { parse, ['parse']: other }\n", nil},
		{"m3dupstr", ".js", "module.exports = { parse, 'parse': other }\n", nil},
		{"m4dupesc", ".js", "module.exports = { parse, 'pars\\x65': other }\n", nil},
		{"m5duptpl", ".js", "module.exports = { parse, [`parse`]: other }\n", nil},
		{"m6dupspread", ".js", "module.exports = { parse, ...{ parse: other } }\n", nil},
		{"m7proxy", ".js", "module.exports = new Proxy({ parse }, { get: () => other })\n", nil},
		{"m8create", ".js", "module.exports = Object.create({ parse: other }, {})\n", nil},
		{"m9protokey", ".js", "module.exports = { __proto__: { parse: other } }\n", nil},
		{"m10setter", ".js", "module.exports = { parse, set parse(v) {} }\n", nil},
		{"m12dupsubtpl", ".js", "module.exports = { parse, [`pars${'e'}`]: other }\n", nil},
		{"m13dupls", ".js", "module.exports = { parse, 'pars\\\u2028e': other }\n", nil},
		{"n1assignspread", ".js", "exports.parse = parse\nObject.assign(exports, ...[{ parse: other }])\n", nil},
		{"n2assignesc", ".js", "exports.parse = parse\nObject.assign(exports, { 'pars\\x65': other })\n", nil},
		{"n3assignls", ".js", "exports.parse = parse\nObject.assign(exports, { 'pars\\\u2028e': other })\n", nil},
		{"n4assignreq", ".js", "exports.parse = parse\nObject.assign(exports, require('./evil'))\n", map[string]string{"evil.js": "module.exports = { parse: function other() {} }\n"}},
		{"n5assignget", ".js", "exports.parse = parse\nObject.assign(exports, { get parse() { return other } })\n", nil},
		{"n6defineesc", ".js", "exports.parse = parse\nObject.defineProperty(exports, 'pars\\x65', { value: other })\n", nil},
		{"n7definels", ".js", "exports.parse = parse\nObject.defineProperty(exports, 'pars\\\u2028e', { value: other })\n", nil},
		{"n8reflectsetls", ".js", "exports.parse = parse\nReflect.set(exports, 'pars\\\u2028e', other)\n", nil},
		{"n9subkeyls", ".js", "exports.parse = parse\nexports['pars\\\u2028e'] = other\n", nil},
		{"n10modkeyls", ".js", "module.exports = { parse }\nmodule['exp\\\u2028orts'].parse = other\n", nil},
		{"n11reqls", ".js", "module.exports = { parse }\nrequire('mod\\\u2028ule')._cache[__filename].exports.parse = other\n", nil},
		{"n12importls", ".js", "module.exports = { parse }\nimport('node:mod\\\u2028ule').then((M) => { M.default._cache[__filename].exports.parse = other })\n", nil},
		{"n13protols", ".js", "exports.parse = parse\nconst set = Object.getOwnPropertyDescriptor(Object.prototype, '__pro\\\u2028to__').set\nexports.sp = set\nexports.sp({ get parse() { return other } })\ndelete exports.parse\n", nil},
		{"n14defgetls", ".js", "exports.parse = parse\nexports.dg = Object.prototype['__defineG\\\u2028etter__']\nexports.dg('parse', () => other)\n", nil},
		{"n15pstls", ".js", "exports.parse = parse\nReflect.set(Object.getPrototypeOf(RangeError), 'prepare\\\u2028StackTrace', (e, s) => s)\nnew Error().stack[0].getThis().parse = other\n", nil},
		{"n16valueofls", ".js", "exports.parse = parse\nexports['value\\\u2028Of']().parse = other\n", nil},
		{"k9constctor", ".js", "exports.parse = parse\nconst k = 'constructor'\n;(function () {})[k]('exports.parse = other')()\n", nil},
		{"k10reflectctor", ".js", "exports.parse = parse\nReflect.get(function () {}, 'constructor')('exports.parse = other')()\n", nil},
		{"k11escctor", ".js", "exports.parse = parse\n;(function () {})['constr\\x75ctor']('exports.parse = other')()\n", nil},
		{"k12lsctor", ".js", "exports.parse = parse\n;(function () {})['constr\\\u2028uctor']('exports.parse = other')()\n", nil},
		{"k15lscaller", ".js", "exports.parse = parse\nfunction g() { g['call\\\u2028er']['argum\\\u2028ents'][0].parse = other }\ng()\n", nil},
		{"k9constctor", ".js", "exports.parse = parse\nconst k = 'constructor'\n;(function () {})[k]('f', 'process.getBuiltinModule(\"module\")._cache[f].exports.parse = function other() {}')(__filename)\n", nil},
		{"k10reflectctor", ".js", "exports.parse = parse\nReflect.get(function () {}, 'constructor')('f', 'process.getBuiltinModule(\"module\")._cache[f].exports.parse = function other() {}')(__filename)\n", nil},
		{"k12lsctor", ".js", "exports.parse = parse\n;(function () {})['constr\\\u2028uctor']('f', 'process.getBuiltinModule(\"module\")._cache[f].exports.parse = function other() {}')(__filename)\n", nil},
		{"k11escctor", ".js", "exports.parse = parse\n;(function () {})['constr\\x75ctor']('f', 'process.getBuiltinModule(\"module\")._cache[f].exports.parse = function other() {}')(__filename)\n", nil},
		// The eighth review (A3g): escaped identifiers, frame keys as names,
		// Annex B HTML-like comments.
		{"ctlbad", ".js", "exports.parse = parse\nexports.parse = other\n", nil},
		{"a1classfield", ".js", "exports.parse = parse\nclass K { caller = 0; arguments = 0 }\nconst [c, a] = Object.keys(new K())\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a2classmethod", ".js", "exports.parse = parse\nclass K { caller() {} arguments() {} }\nconst [, c, a] = Object.getOwnPropertyNames(K.prototype)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a3objmethod", ".js", "exports.parse = parse\nconst [c, a] = Object.keys({ caller() {}, arguments() {} })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a4getter", ".js", "exports.parse = parse\nconst [c, a] = Object.keys({ get caller() { return 1 }, get arguments() { return 1 } })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a5fnname", ".js", "exports.parse = parse\nconst caller = () => 0\nfunction mk() { const arguments = () => 0; return arguments.name }\nconst c = caller.name, a = mk()\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a5bfnname", ".js", "exports.parse = parse\nfunction caller() {}\nfunction mk() { function arguments() {} return arguments.name }\nconst c = caller.name, a = mk()\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a6regex", ".js", "exports.parse = parse\nconst c = /caller/.source, a = /arguments/.source\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a9shpattern", ".js", "exports.parse = parse\nfunction h({ arguments }) { arguments[0].parse = other }\nfunction g() { const { caller } = g; h(caller) }\ng()\n", nil},
		{"a9bshpattern", ".js", "exports.parse = parse\nfunction h(w) { const { arguments } = w; arguments[0].parse = other }\nfunction g() { const { caller } = g; h(caller) }\ng()\n", nil},
		{"a10ctorpattern", ".js", "exports.parse = parse\nconst { constructor } = function () {}\nconstructor('f', 'process.getBuiltinModule(\"module\")._cache[f].exports.parse = function other() {}')(__filename)\n", nil},
		{"a11classstatic", ".js", "exports.parse = parse\nclass K { static caller = 0; static arguments = 0 }\nconst [, , , c, a] = Reflect.ownKeys(K)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a12defget", ".js", "exports.parse = parse\nconst [k] = Object.keys({ __defineGetter__() {} })\nexports.dg = Object.prototype[k]\nexports.dg('parse', () => other)\n", nil},
		{"a13classname", ".js", "exports.parse = parse\nconst c = (class caller {}).name\nfunction mk() { return (function arguments() {}).name }\nconst a = mk()\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"a14forin", ".js", "exports.parse = parse\nconst ks = []\nfor (const k in { caller() {}, arguments() {} }) ks.push(k)\nconst [c, a] = ks\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"z1computed", ".js", "exports.parse = parse\nconst [c, a] = Object.keys({ ['caller']: 1, ['arguments']: 1 })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"z2strkey", ".js", "exports.parse = parse\nconst [c, a] = Object.keys({ 'caller': 1, 'arguments': 1 })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"z3strfield", ".js", "exports.parse = parse\nclass K { 'caller' = 0; 'arguments' = 0 }\nconst [c, a] = Object.keys(new K())\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"z4pair", ".js", "exports.parse = parse\nconst [c, a] = Object.keys({ caller: 1, arguments: 1 })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"z5shorthand", ".js", "exports.parse = parse\nconst caller = 1, argumentz = 1\nconst [c] = Object.keys({ caller })\nconst a = 'argum' + 'ents'\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"z6tplesc", ".js", "exports.parse = parse\nfunction g() { g[`call\\x65r`][`argum\\x65nts`][0].parse = other }\ng()\n", nil},
		{"z7tplmix", ".js", "exports.parse = parse\nfunction g() { g[`c\\x61ll\\x65r`][`a\\u0072guments`][0].parse = other }\ng()\n", nil},
		{"z8tplplain", ".js", "exports.parse = parse\nfunction g() { g[`caller`][`arguments`][0].parse = other }\ng()\n", nil},
		{"h1htmlopen", ".js", "exports.parse = parse\nvoid 0 <!-- `\nexports.parse = other\n// `\n", nil},
		{"h2htmlclose", ".js", "exports.parse = parse\n/*\n*/ --> `\nexports.parse = other\n// `\n", nil},
		{"h3htmlopen2", ".js", "exports.parse = parse\nlet z = 1\nz <!-- \"\nexports.parse = other\n", nil},
		{"h4htmlstr", ".js", "exports.parse = parse\nconst s = 0 <!-- `\nfunction g() { g.caller.arguments[0].parse = other }\ng()\n// `\n", nil},
		{"d1oct", ".js", "exports.parse = parse\nfunction g() { g['\\143aller']['\\141rguments'][0].parse = other }\ng()\n", nil},
		{"d2u6", ".js", "exports.parse = parse\nfunction g() { g['\\u{0000000063}aller']['argum\\u{65}nts'][0].parse = other }\ng()\n", nil},
		{"d4ident8", ".js", "exports.parse = parse\nfunction g() { g['c\\aller']['\\arguments'][0].parse = other }\ng()\n", nil},
		{"d5crlf", ".js", "exports.parse = parse\nfunction g() { g['call\\\r\ner']['argum\\\r\nents'][0].parse = other }\ng()\n", nil},
		{"d6tplcrlf", ".js", "exports.parse = parse\nfunction g() { g[`call\\\r\ner`][`argum\\\r\nents`][0].parse = other }\ng()\n", nil},
		{"d7lstpl", ".js", "exports.parse = parse\nfunction g() { g[`call\\\u2028er`][`argum\\\u2029ents`][0].parse = other }\ng()\n", nil},
		{"d9idesc", ".js", "exports.parse = parse\nfunction g() { g.c\\u0061ller.\\u0061rguments[0].parse = other }\ng()\n", nil},
		{"d10keyesc", ".js", "exports.parse = parse\nconst [c, a] = Object.keys({ c\\u0061ller: 1, \\u0061rguments: 1 })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d11shesc", ".js", "exports.parse = parse\nfunction h({ \\u0061rguments }) { \\u0061rguments[0].parse = other }\nfunction g() { const { c\\u0061ller } = g; h(c\\u0061ller) }\ng()\n", nil},
		{"d12methesc", ".js", "exports.parse = parse\nconst [c, a] = Object.keys({ c\\u0061ller() {}, \\u0061rguments() {} })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"i1exports", ".js", "exports.parse = parse\n\\u0065xports.parse = other\n", nil},
		{"i2module", ".js", "exports.parse = parse\nmod\\u0075le.exports.parse = other\n", nil},
		{"i3require", ".js", "exports.parse = parse\nrequir\\u0065('module')._cache[__filename].exports.parse = other\n", nil},
		{"i4eval", ".js", "exports.parse = parse\n\\u0065val('exports.parse = other')\n", nil},
		{"i5Function", ".js", "exports.parse = parse\nF\\u0075nction('f', 'process.getBuiltinModule(\"module\")._cache[f].exports.parse = function other() {}')(__filename)\n", nil},
		{"i6ctor", ".js", "exports.parse = parse\n;(function () {}).c\\u006fnstructor('f', 'process.getBuiltinModule(\"module\")._cache[f].exports.parse = function other() {}')(__filename)\n", nil},
		{"i7proto", ".js", "exports.parse = parse\nexports.__pr\\u006fto__ = { parse: other }\nexports.parse = undefined\ndelete exports.parse\n", nil},
		{"i8defget", ".js", "exports.parse = parse\ndelete exports.parse\nexports.__d\\u0065fineGetter__('parse', () => other)\n", nil},
		{"i9process", ".js", "exports.parse = parse\npr\\u006fcess.getBuiltinModule('module')._cache[__filename].exports.parse = other\n", nil},
		{"i11argtop", ".js", "exports.parse = parse\n\\u0061rguments[0].parse = other\n", nil},
		{"i13mexports", ".js", "module.exports = { parse }\nmodule.\\u0065xports.parse = other\n", nil},
		{"i14global", ".js", "exports.parse = parse\nglob\\u0061lThis.process.getBuiltinModule('module')._cache[__filename].exports.parse = other\n", nil},
		{"i15shorthand", ".js", "exports.parse = parse\nconst o = { \\u0065xports }\no.exports.parse = other\n", nil},
		// The ninth review (A3h): inferred names, named groups, a static
		// method named constructor, LS in a comment, an enum.
		{"m1default", ".js", "function f(caller = () => 0) { return caller.name }\nconst [x = function () {}] = []\nfunction h({ y: arguments = () => 0 } = {}) { return arguments.name }\nconst c = f(), a = h()\nexports.parse = parse\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"m2regex", ".js", "const [a] = Object.keys(/(?<arguments>.)/.exec('x').groups), [c] = Object.keys(/(?<caller>.)/.exec('x').groups)\nexports.parse = parse\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"m4staticctor", ".js", "class A { static constructor() { this.parse = other } }\nexports.parse = parse\nexports.f = A.constructor\nexports.f()\n", nil},
		{"m5lscomment", ".js", "exports.parse = parse\n/*\u2028*/ --> `\nexports.parse = other\n// `\n", nil},
		{"m3enum", ".ts", "enum E { caller, arguments }\nexports.parse = parse\nfunction g() { (g as any)[E[0]][E[1]][0].parse = other }\ng()\n", nil},
		{"htmlclosey", ".js", "exports.parse = parse\nlet y = 0\ny\n--> `\nexports.parse = other\n// `\n", nil},
		// The tenth review (A3i): static constructor spellings, regex group
		// names with escapes, TypeScript class names, earlier forms re-run.
		{"d12s1staticcmt", ".js", "exports.parse = parse\nclass A { static/**/constructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12s2staticnl", ".js", "exports.parse = parse\nclass A { static\nconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12s3staticplain", ".js", "exports.parse = parse\nclass A { static constructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12s4staticcmt2", ".js", "exports.parse = parse\nclass A { static/*\n*/constructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12s5staticasync", ".js", "exports.parse = parse\nclass A { static async constructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12s6staticgen", ".js", "exports.parse = parse\nclass A { static *constructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k().next()\n", nil},
		{"d12s7staticlinecmt", ".js", "exports.parse = parse\nclass A { static//x\nconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12r1grpesc", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<call\\u0065r>.)(?<argument\\u0073>.)/.exec(\"ab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r2grpescu", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<\\u{63}aller>.)(?<\\u{61}rguments>.)/u.exec(\"ab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r3grpplain", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<caller>.)(?<arguments>.)/.exec(\"ab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r4grpv", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<call\\u0065r>.)(?<argum\\u0065nts>.)/v.exec(\"ab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r5grpsurrogate", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<c\\u0061ller>.)(?<\\u0061rguments>.)/d.exec(\"ab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r8grpctor", ".js", "module.exports = { parse }\nconst [k] = Object.keys(/(?<constructo\\u0072>.)/.exec(\"a\").groups)\nconst F = Reflect.get(function () {}, k)\nF(\"m\", \"o\", \"process.getBuiltinModule('module')._cache[m].exports.parse = o\")(__filename, other)\n", nil},
		{"d12r9grpbackref", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<call\\u0065r>.)\\k<call\\u0065r>(?<argument\\u0073>.)/.exec(\"aab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12p2protosetter", ".js", "module.exports = { __proto__: { set parse(v) {} } }\nmodule.exports.parse = parse\n", nil},
		{"d12p3protosetter2", ".js", "module.exports = { __proto__: { set parse(v) { } } }\nObject.assign(module.exports, { parse })\n", nil},
		{"d12p4protogetter", ".js", "module.exports = { __proto__: { get parse() { return other } } }\n", nil},
		{"d12p5protoobj", ".js", "module.exports = { __proto__: { parse: other } }\n", nil},
		{"d12p6protoexp", ".js", "module.exports = { __proto__: { set parse(v) { } }, other }\nexports.x = 1\nmodule.exports.parse = parse\n", nil},
		{"d12p7protoproxy", ".js", "module.exports = { __proto__: new Proxy({}, { set() { return true } }) }\nmodule.exports.parse = parse\n", nil},
		{"d12p8protodefine", ".js", "const base = { set parse(v) {} }\nmodule.exports = { __proto__: base }\nmodule.exports.parse = parse\n", nil},
		{"d12c1crcomment", ".js", "exports.parse = parse\n// x\rexports.parse = other\n", nil},
		{"d12c2lscomment", ".js", "exports.parse = parse\n// x\u2028exports.parse = other\n", nil},
		{"d12k1classasi", ".js", "module.exports = class { static parse = parse\n static\nparse = other }\n", nil},
		{"d12k4classblock", ".js", "class A { static\nconstructor() { exports.k = 1; this.parse = other } }\nexports.parse = parse\nexports.k2 = A.constructor\nexports.k2()\n", nil},
		{"d12k5statictab", ".js", "exports.parse = parse\nclass A { static\tconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12k6staticcr", ".js", "exports.parse = parse\nclass A { static\rconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12k7staticexpr", ".js", "exports.parse = parse\nconst A = class { static\nconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12k8staticsuper", ".js", "exports.parse = parse\nclass A { static\nconstructor() { super.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12k9staticmodule", ".js", "module.exports = { parse }\nclass A { static\nconstructor() { this.parse = other } }\nmodule.exports.k = A.constructor\nmodule.exports.k()\n", nil},
		{"d12r10grpdefget", ".js", "exports.parse = parse\nconst [k] = Object.keys(/(?<__defin\\u0065Getter__>.)/.exec(\"a\").groups)\nexports.dg = ({})[k]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12r11grpcallee", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<call\\u0065r>.)(?<argument\\u0073>.)/.exec(\"ab\").groups)\nconst h = function () { return Reflect.get(Reflect.get(h, c), a) }\nh()[0].parse = other\n", nil},
		{"d12r12grpindices", ".js", "exports.parse = parse\nconst m = /(?<call\\u0065r>.)(?<argument\\u0073>.)/d.exec(\"ab\")\nconst [c, a] = Object.keys(m.indices.groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r13grpreplace", ".js", "exports.parse = parse\nlet c, a\n\"ab\".replace(/(?<call\\u0065r>.)(?<argument\\u0073>.)/, (...x) => { [c, a] = Object.keys(x[x.length - 1]); return \"\" })\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r14grpmatchall", ".js", "exports.parse = parse\nconst [c, a] = Object.keys([...\"ab\".matchAll(/(?<call\\u0065r>.)(?<argument\\u0073>.)/g)][0].groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r15grpnew", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(new RegExp(/(?<call\\u0065r>.)(?<argument\\u0073>.)/).exec(\"ab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12r16grpmixed", ".js", "exports.parse = parse\nconst [c, a] = Object.keys(/(?<cal\\u006Cer>.)(?<a\\u0072guments>.)/.exec(\"ab\").groups)\nfunction g() { g[c][a][0].parse = other }\ng()\n", nil},
		{"d12v2varre", ".js", "var parse = other\nexports.parse = parse\n", nil},
		{"d12v7spread", ".js", "module.exports = { parse, ...{ parse: other } }\n", nil},
		{"d12v8getter", ".js", "module.exports = { parse, get parse() { return other } }\n", nil},
		{"d12v9compkey", ".js", "const k = \"parse\"\nmodule.exports = { parse, [k]: other }\n", nil},
		{"d12v10strkey", ".js", "module.exports = { parse, \"parse\": other }\n", nil},
		{"d12v11assignval", ".js", "module.exports = { parse: parse = other }\n", nil},
		{"d12v12seq", ".js", "module.exports = { parse: (parse, other) }\n", nil},
		{"d12v13and", ".js", "exports.parse = parse && other\n", nil},
		{"d12v14chain", ".js", "exports.parse = parse = other\n", nil},
		{"d12v15bind", ".js", "exports.parse = parse.bind(null)\n", nil},
		{"d12v16cond", ".js", "exports.parse = 1 ? other : parse\n", nil},
		{"d12v17catch", ".js", "try { throw other } catch (parse) { exports.parse = parse }\n", nil},
		{"d12v18param", ".js", "function f(parse) { exports.parse = parse }\nf(other)\n", nil},
		{"d12v19letblock", ".js", "{ let parse = other; exports.parse = parse }\n", nil},
		{"d12v20argsalias", ".js", "function f(parse) { arguments[0] = other; exports.parse = parse }\nf(parse)\n", nil},
		{"d12v27definepropexp", ".js", "exports.parse = parse\nObject.defineProperty(module.exports, \"parse\", { get: () => other })\n", nil},
		{"d12v28freezefirst", ".js", "Object.freeze(exports)\nexports.parse = parse\n", nil},
		{"d12v29preventext", ".js", "Object.preventExtensions(module.exports)\nexports.parse = parse\n", nil},
		{"d12v30definesetter", ".js", "Object.defineProperty(exports, \"parse\", { set(v) {} })\nexports.parse = parse\n", nil},
		{"d12v31nonwritable", ".js", "Object.defineProperty(exports, \"parse\", { value: other, writable: false })\nexports.parse = parse\n", nil},
		{"d12v32sealafter", ".js", "module.exports = Object.freeze({ parse: other })\nmodule.exports.parse = parse\n", nil},
		{"d12v33assignsrc", ".js", "module.exports = Object.assign({ parse }, { parse: other })\n", nil},
		{"d12v34arguments2", ".js", "function f(parse) { arguments[0] = other; return parse }\nexports.parse = f(parse)\n", nil},
		{"d12a1objget", ".js", "module.exports = { parse, get\nparse() { return other } }\n", nil},
		{"d12a7staticfieldthen", ".js", "exports.parse = parse\nclass A { x\nstatic\nconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a8staticws", ".js", "exports.parse = parse\nclass A { static\u00a0constructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a9staticfeff", ".js", "exports.parse = parse\nclass A { static\ufeffconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a10staticvt", ".js", "exports.parse = parse\nclass A { static\u000bconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a11staticff", ".js", "exports.parse = parse\nclass A { static\fconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a12staticnbsp2", ".js", "exports.parse = parse\nclass A { static\u3000constructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a13staticdecor", ".js", "exports.parse = parse\nclass A { static\n/* c */\nconstructor() { this.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a14staticexprctor", ".js", "exports.parse = parse\nexports.k = (class { static\nconstructor() { this.parse = other } }).constructor\nexports.k()\n", nil},
		{"d12a15staticfieldarrow", ".js", "exports.parse = parse\nclass A { static\nconstructor() { (() => { this.parse = other })() } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a16staticsuperset", ".js", "exports.parse = parse\nclass A { static\nconstructor() { super.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a17staticthiscomp", ".js", "exports.parse = parse\nclass A { static\nconstructor() { this[\"par\" + \"se\"] = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a18staticthisvalof", ".js", "exports.parse = parse\nclass A { static\nconstructor() { const t = this; t.parse = other } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a19staticthiscall", ".js", "exports.parse = parse\nclass A { static\nconstructor() { Object.assign(this, { parse: other }) } }\nexports.k = A.constructor\nexports.k()\n", nil},
		{"d12a20staticargs", ".js", "exports.parse = parse\nclass A { static\nconstructor() { return this } }\nexports.k = A.constructor\nexports.k().parse = other\n", nil},
		{"d12b4ifregex", ".js", "exports.parse = parse\nif (1) /x/.test(\"x\"), exports.parse = other\n", nil},
		{"d12b5blockregex", ".js", "exports.parse = parse\n{}\n/x/.test(\"x\") && (exports.parse = other)\n", nil},
		{"d12b12regexcomment", ".js", "exports.parse = parse\nconst r = /a\\//; exports.parse = other\n", nil},
		{"d12t1classdgjs", ".js", "exports.parse = parse\nclass __defineGetter__ {}\nexports.dg = ({})[__defineGetter__.name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t2classexprjs", ".js", "exports.parse = parse\nconst C = class __defineGetter__ {}\nexports.dg = ({})[C.name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t3abstractjs", ".js", "exports.parse = parse\nclass __defineGetter__ {}\nexports.dg = ({})[__defineGetter__.name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t7lookupjs", ".js", "exports.parse = parse\nconst C = class __defineSetter__ {}\nexports.ds = ({})[C.name]\nexports.ds(\"parse\", () => {})\n", nil},
		{"d12t8argexprjs", ".js", "exports.parse = parse\nfunction n(c) { return c.name }\nexports.dg = ({})[n(class __defineGetter__ {})]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t9extendsjs", ".js", "exports.parse = parse\nclass X extends class __defineGetter__ {} {}\nexports.dg = ({})[Object.getPrototypeOf(X).name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t10staticblockjs", ".js", "exports.parse = parse\nlet K\nclass __defineGetter__ { static { K = this.name } }\nexports.dg = ({})[K]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t11staticmethjs", ".js", "exports.parse = parse\nlet K\nclass __defineGetter__ { static n() { K = this.name } static x = this.n() }\nexports.dg = ({})[K]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t1classdgts", ".ts", "exports.parse = parse\nclass __defineGetter__ {}\nexports.dg = ({} as any)[__defineGetter__.name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t2classexprts", ".ts", "exports.parse = parse\nconst C = class __defineGetter__ {}\nexports.dg = ({} as any)[C.name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t3abstractts", ".ts", "exports.parse = parse\nabstract class __defineGetter__ {}\nexports.dg = ({} as any)[__defineGetter__.name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t4callerargsts", ".ts", "exports.parse = parse\nclass caller {}\nconst [a] = Object.keys({ x: 1 })\nfunction g(): any { return (g as any)[caller.name] }\nconst w = g()\n", nil},
		{"d12t6callerexprts", ".ts", "exports.parse = parse\nconst C = class callee {}\nconst D = class caller {}\nexports.n = [C.name, D.name]\n", nil},
		{"d12t7lookupts", ".ts", "exports.parse = parse\nconst C = class __defineSetter__ {}\nexports.ds = ({} as any)[C.name]\nexports.ds(\"parse\", () => {})\n", nil},
		{"d12t8argexprts", ".ts", "exports.parse = parse\nfunction n(c: any) { return c.name }\nexports.dg = ({} as any)[n(class __defineGetter__ {})]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t9extendsts", ".ts", "exports.parse = parse\nclass X extends class __defineGetter__ {} {}\nexports.dg = ({} as any)[Object.getPrototypeOf(X).name]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t10staticblockts", ".ts", "exports.parse = parse\nlet K: any\nclass __defineGetter__ { static { K = this.name } }\nexports.dg = ({} as any)[K]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t11staticmethts", ".ts", "exports.parse = parse\nlet K: any\nclass __defineGetter__ { static n() { K = this.name } static x = this.n() }\nexports.dg = ({} as any)[K]\nexports.dg(\"parse\", () => other)\n", nil},
		{"d12t12newtargetts", ".ts", "exports.parse = parse\nlet K: any\nclass __defineGetter__ { constructor() { K = new.target.name } }\nconst o = new (class extends (null as any) {})\n", nil},
		{"semiincomment", ".js", "class A { static /* ; */\nconstructor() { this.parse = other } }\nexports.parse = parse\nexports.f = A.constructor\nexports.f()\n", nil},
		// Positive controls: the harness resolves both languages (a form of a
		// language whose uses resolve nothing would pass vacuously).
		{"posjs", ".js", "exports.parse = parse\n", nil},
		{"posts", ".ts", "exports.parse = parse\n", nil},
	}
	files := map[string]string{}
	var want []expect
	// One user per language: a TypeScript form is required from TypeScript.
	for _, ext := range []string{".js", ".ts"} {
		var mine []int
		for i, f := range forms {
			if f.ext == ext {
				mine = append(mine, i)
			}
		}
		use, calls := "", "function run() {\n"
		for j, i := range mine {
			f := forms[i]
			files[f.name+f.ext] = fns + f.src
			for k, v := range f.extra {
				files[k] = v
			}
			use += fmt.Sprintf("const m%d = require('./%s')\n", j, f.name)
			calls += fmt.Sprintf("  m%d.parse()\n", j)
			w := expect{key: fmt.Sprintf("use%s:%d:parse", ext, len(mine)+2+j), absentOK: true}
			if f.name == "posjs" || f.name == "posts" {
				w = expect{key: w.key, cand: f.name + f.ext + ":parse"} // a CommonJS export: Candidate at most
			}
			want = append(want, w)
		}
		files["use"+ext] = use + calls + "}\n"
	}
	check(t, resolveAll(t, files), want)
}
