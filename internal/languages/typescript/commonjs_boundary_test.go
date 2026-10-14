package typescript_test

import "testing"

// TestECMAScriptCommonJSConfidenceBoundary pins the boundary between lexical
// references and CommonJS export values
// (docs/development/web-semantic-fidelity/commonjs-boundary/contract.md):
//   - a lexical binding stays Exact, inside a CommonJS module too;
//   - a reference through a CommonJS export (require, an ESM import of a
//     CommonJS module, a re-export of one, CommonJS written in TypeScript)
//     keeps the exported declaration as a Candidate, never Strong or Exact,
//     whether or not code changes the exports object;
//   - ESM and TypeScript exports stay Exact.
func TestECMAScriptCommonJSConfidenceBoundary(t *testing.T) {
	files := map[string]string{
		// Lexical bindings (positive controls).
		"lex.js": "function original() {}\nconst fn = original\nconst value = 1\nfunction read() {\n  return value\n}\n",
		// A CommonJS module: its own reference to original is lexical.
		"cjs.js": "function original() {}\nmodule.exports = {\n  parse: original\n}\n",
		// Writes to the exports object: the boundary does not depend on them.
		"put.js":    "function parse() {}\nfunction replacement() {}\nmodule.exports = { parse, keep: parse }\nmodule.exports.parse = replacement\n",
		"assign.js": "function parse() {}\nfunction replacement() {}\nmodule.exports = { parse }\nObject.assign(module.exports, { parse: replacement })\n",
		"key.js":    "function parse() {}\nfunction replacement() {}\nconst key = 'parse'\nmodule.exports = { parse }\nmodule.exports[key] = replacement\n",
		// ESM and TypeScript exports.
		"esm.js": "export function parse() {}\n",
		"ts.ts":  "export function parse(): void {}\n",
		// CommonJS written in TypeScript, and an ESM re-export of CommonJS.
		"tscjs.ts":  "function parse(): void {}\nexports.parse = parse\n",
		"barrel.js": "export { parse } from './cjs2.js'\n",
		"cjs2.js":   "function parse() {}\nexports.parse = parse\n",
		"use.js": "const c = require('./cjs')\nconst p = require('./put')\nconst a = require('./assign')\nconst k = require('./key')\n" +
			"import { parse as e } from './esm.js'\nimport { parse as b } from './barrel.js'\nimport { parse as d } from './cjs2.js'\n" +
			"function run() {\n" + // 8
			"  c.parse()\n" + // 9
			"  p.parse()\n" + // 10
			"  p.keep()\n" + // 11
			"  a.parse()\n" + // 12
			"  k.parse()\n" + // 13
			"  e()\n" + // 14
			"  b()\n" + // 15
			"  d()\n" + // 16
			"}\n",
		"use.ts": "import { parse as t } from './ts'\nconst tc = require('./tscjs')\nfunction run(): void {\n  t()\n  tc.parse()\n}\n",
		// A class imported through a CommonJS export and extended: its members
		// stay Candidates (review 1 M-1: the relation was left unidentified).
		"lib/c.js": "class C {\n  cm() {}\n}\nmodule.exports = { C }\n",
		"app/s.js": "const { C } = require('../lib/c.js')\nclass S extends C {\n  sm() { this.cm(); super.cm() }\n}\n",
		// A module that replaces module.exports and has ESM exports too,
		// required: a CommonJS value (review 1 L-1: it was Exact).
		"mixt.ts":  "function tp(): void {}\nexport function te(): void {}\nmodule.exports = { tp }\n",
		"mixtu.ts": "const r = require('./mixt')\nr.te()\nr.tp()\n",
	}
	check(t, resolveAll(t, files), []expect{
		// 4.1 lexical bindings stay Exact.
		{key: "lex.js:2:original", target: "lex.js:original", conf: "exact"},
		{key: "lex.js:5:value", target: "lex.js:value", conf: "exact"},
		// 4.2 the export's own lexical reference stays Exact; the value through
		// the export is a Candidate.
		{key: "cjs.js:3:original", target: "cjs.js:original", conf: "exact"},
		{key: "use.js:9:parse", cand: "cjs.js:original"},
		// 4.3 writes to the exports object: never Strong or Exact. A property
		// export a later statement overwrites is not stated at all (the ts-14
		// scan); one it does not overwrite is a Candidate.
		{key: "use.js:10:parse", absentOK: true},
		{key: "use.js:11:keep", cand: "put.js:parse"},
		{key: "use.js:12:parse", absentOK: true},
		{key: "use.js:13:parse", absentOK: true},
		// 4.1 ESM and TypeScript exports stay Exact.
		{key: "use.js:14:e", target: "esm.js:parse", conf: "exact"},
		{key: "use.ts:4:t", target: "ts.ts:parse", conf: "exact"},
		// ESM over CommonJS: a re-export and a direct import of a CommonJS
		// module, and CommonJS written in TypeScript.
		{key: "use.js:15:b", cand: "cjs2.js:parse"},
		{key: "use.js:16:d", cand: "cjs2.js:parse"},
		{key: "use.ts:5:parse", cand: "tscjs.ts:parse"},
		{key: "app/s.js:3:cm", cand: "lib/c.js:C.cm"},
		{key: "mixtu.ts:2:te", cand: "mixt.ts:te"},
		{key: "mixtu.ts:3:tp", cand: "mixt.ts:tp"},
	})
}
