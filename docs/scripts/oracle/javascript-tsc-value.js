// Semantic value oracle (evaluation contract v2) for TestFidelityBaseline:
// javascript-tsc.js's sites, unchanged, each declaration site whose
// declaration is an object literal's property extended with the declaration
// of the value that property holds.
//
// usage: node javascript-tsc-value.js <typescript-package-dir> <corpus-dir> [<extensions>] > oracle.jsonl
//
// It runs javascript-tsc.js (the contract v1 oracle, kept as it is) on the
// same arguments and copies every line it writes; a line gains fields only,
// so scoring a v2 file without the value layer is scoring v1. Ark's output is
// never read.
//
// Why: the TypeScript checker's symbol at m.parse, where the module m ends
// with module.exports = { parse, X: 1 }, is the property { parse } of the
// object literal, while for module.exports = { parse } alone (shorthand
// properties only) the binder makes the property an alias and the checker
// answers the declaration of the value parse reads. The value is the same in
// both (ECMAScript evaluates each property of an object initializer on its
// own), so v1 scores one meaning by two layers depending on the siblings
// (docs/development/web-semantic-fidelity/f6b5/semantics.md). v2 states both
// layers: decl is the syntactic target (v1's), value the semantic value
// target, and a reference to either is correct.
//
// The value layer is stated for the CommonJS export object only: an object
// literal that is the right side of the one top-level statement
// module.exports = { ... } of its module, the subject of the disagreement;
// any other object literal (a local const, export default { ... }, a
// configuration object) is mutable data the program may change in ways no
// static rule bounds, and keeps v1's answer alone. For a site whose
// (alias-resolved) symbol S is a property of such an object:
//
//   value        the declaration of the value S's one declaration reads,
//                when that declaration is a shorthand property { a } or a
//                property assignment whose initializer is an identifier
//                { k: a } (TypeChecker.getShorthandAssignmentValueSymbol,
//                TypeChecker.getSymbolAtLocation of the initializer), the
//                value symbol followed through its aliases
//                (TypeChecker.getAliasedSymbol), and that symbol has one
//                declaration in a non-declaration file of the corpus (an
//                assignment declaring a member after the fact aside, as
//                javascript-tsc.js filters them); a value that is itself
//                such a property is followed the same way, at most 16 steps
//   value_kind   "function" or "class" (a function or class declaration, or
//                a variable initialized with a function, arrow function or
//                class expression), "data" for any other
//   value_via    the declarations the value's aliases pass through
//                (const a = require(...), an import binding) and the
//                properties a followed value passes through
//   value_rule   "shorthand" or "identifier_initializer", one per step
//                joined by ">": the provenance
//   value_unknown  why no value is stated, for a property site of an export
//                object that has none, with value_unknown_at (file:line:col
//                of the cause) where there is one:
//                "replaced": module.exports is assigned more than once, or
//                  anywhere but that statement, or the module declares
//                  module or exports at its top level;
//                "overwritten_in_literal": a spread, a computed key or the
//                  same key follows the property in the literal;
//                "multiple_declarations": S has more declarations;
//                "written": the program writes a property of S's name,
//                  whatever the object (o.a = b, o.a += b, o.a++, delete o.a,
//                  [o.a] = b, ({ x: o.a } = b), for (o.a of …), this.a = b,
//                  o["a"] = b, Object.defineProperty / Reflect.* /
//                  __defineGetter__ / __defineSetter__ with the key a,
//                  Object.assign / defineProperties with a literal source
//                  that has a); by name, so a cast, an any, an alias or this
//                  bound by a call cannot hide it;
//                "written_any": the program writes a property it does not
//                  name on an object whose type has S or is any (o[k] = b,
//                  Object.assign(x, src) with a source no literal), or does
//                  so on a function's or a method's this, or lets that this
//                  flow on (a class's constructor's, a field initializer's and
//                  a static block's aside; return this aside): the function
//                  may be called on an export object;
//                "escapes": an expression whose type has S is used as a
//                  value other than to read a member or compare it: an
//                  argument, an assigned value, an annotated initializer, a
//                  cast, an element, a property value, a spread, a returned,
//                  thrown or yielded value, an operand of && || ??, a branch
//                  of ?:, a template span (module.exports = X re-exports X:
//                  its users are typed as it, and are scanned in turn);
//                "dynamic_code": the program has with, eval or Function, or
//                  reaches the module wrapper's arguments, require.cache,
//                  require.main, module.constructor, process.mainModule,
//                  require('module') or require(__filename), or hands
//                  require on;
//                "alias_reassigned": an alias binding the value passes
//                  through (let a = require(...).a) is assigned;
//                "not_identifier" (a getter, a method, k: f(), k: a.b,
//                  k: require(...)), "module_value" (the value is a module),
//                "alias_unresolved", "chain_limit", "no_value_symbol",
//                "external_value", "declaration_file_value",
//                "ambiguous_value".
//
// The value layer assumes a closed world: code outside the corpus (a user of
// a published package) does not write the export object.
"use strict";
const { execFileSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const [tsdir, rootArg, extArg] = process.argv.slice(2);
const ts = require(tsdir);
const root = fs.realpathSync(rootArg);

const v1 = execFileSync(process.execPath, [path.join(__dirname, "javascript-tsc.js"), ...process.argv.slice(2)],
  { encoding: "utf8", maxBuffer: 1 << 30, stdio: ["ignore", "pipe", "inherit"] });

// The program, as javascript-tsc.js builds it.
const files = [];
(function walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : 1))) {
    if (e.name.startsWith(".") || e.name === "node_modules" || e.name === "vendor") continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p);
    else if (/\.(m|c)?(js|jsx|ts|tsx)$/.test(e.name) && !e.name.endsWith(".d.ts")) files.push(p);
  }
})(root);
const sources = new Set(files);
const program = ts.createProgram(files, {
  allowJs: true, checkJs: false, noEmit: true, jsx: ts.JsxEmit.Preserve,
  module: ts.ModuleKind.Preserve, moduleResolution: ts.ModuleResolutionKind.Bundler,
  target: ts.ScriptTarget.ES2022, types: [], skipLibCheck: true,
});
const checker = program.getTypeChecker();

// Positions: javascript-tsc.js writes 1-based lines and byte columns of the
// file as stored (a byte order mark counts, lines split at "\n" only);
// textPos maps one back to a position of sf.text.
const layouts = new Map();
function layout(sf) {
  let l = layouts.get(sf.fileName);
  if (!l) {
    const raw = fs.readFileSync(sf.fileName);
    const bom = raw.length >= 3 && raw[0] === 0xef && raw[1] === 0xbb && raw[2] === 0xbf && sf.text.charCodeAt(0) !== 0xfeff ? 3 : 0;
    const starts = [0];
    for (let i = 0; i < raw.length; i++) if (raw[i] === 0x0a) starts.push(i + 1);
    l = { raw, bom, starts };
    layouts.set(sf.fileName, l);
  }
  return l;
}
function textPos(sf, line, col) {
  const l = layout(sf);
  const off = l.starts[line - 1] + col - 1;
  return l.raw.subarray(l.bom, off).toString("utf8").length;
}
function position(sf, pos) {
  const l = layout(sf);
  const off = l.bom + Buffer.byteLength(sf.text.slice(0, pos), "utf8");
  let lo = 0, hi = l.starts.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (l.starts[mid] <= off) lo = mid; else hi = mid - 1;
  }
  return [lo + 1, off - l.starts[lo] + 1];
}
function identifierAt(sf, pos) {
  let found;
  (function find(n) {
    if (pos < n.getStart(sf) || pos >= n.getEnd()) return;
    if (ts.isIdentifier(n) && n.getStart(sf) === pos) found = n;
    ts.forEachChild(n, find);
  })(sf);
  return found;
}

// As javascript-tsc.js.
const inCorpus = (d) => d.getSourceFile().fileName.startsWith(root + path.sep);
const isSource = (d) => !d.getSourceFile().isDeclarationFile && sources.has(d.getSourceFile().fileName);
const isAssignment = (d) => ts.isBinaryExpression(d) || ts.isPropertyAccessExpression(d) || ts.isElementAccessExpression(d) ||
  (ts.isIdentifier(d) && d.parent && (ts.isPropertyAccessExpression(d.parent) || ts.isElementAccessExpression(d.parent)) && d.parent.expression === d);
function declOf(d) {
  const f = d.getSourceFile();
  const name = ts.getNameOfDeclaration(d) || d;
  const [line, col] = position(f, name.getStart(f));
  const out = { file: path.relative(root, f.fileName).split(path.sep).join("/"), line, col };
  if (ts.isIdentifier(name) || ts.isPrivateIdentifier(name)) out.name = name.text;
  return out;
}

// exportObjects: the object literals of a top-level module.exports = { ... }
// statement, and the modules that assign module.exports anywhere else or
// more than once (replaced).
const isModuleExports = (n) => n && ((ts.isPropertyAccessExpression(n) && ts.isIdentifier(n.expression) &&
  n.expression.text === "module" && n.name.text === "exports") || (ts.isElementAccessExpression(n) &&
  ts.isIdentifier(n.expression) && n.expression.text === "module" && [undefined, "exports"].includes(keyOf(skip(n.argumentExpression)))));
const exportObjects = new Set(), replaced = new Map(), exportObjectOf = new Map();
const where = (n) => { const f = n.getSourceFile(); const [l, c] = position(f, n.getStart(f)); return `${path.relative(root, f.fileName).split(path.sep).join("/")}:${l}:${c}`; };
for (const sf of program.getSourceFiles()) {
  if (!sources.has(sf.fileName)) continue;
  const assigns = [];
  (function visit(n) {
    if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.EqualsToken && isModuleExports(skip(n.left))) assigns.push(n);
    ts.forEachChild(n, visit);
  })(sf);
  const top = assigns.filter((a) => ts.isExpressionStatement(a.parent) && a.parent.parent === sf);
  // A top-level declaration of module or exports (function module() {})
  // shadows the module wrapper's: module.exports is then no export.
  const shadowed = sf.statements.some((st) => ((ts.isFunctionDeclaration(st) || ts.isClassDeclaration(st)) && st.name &&
    ["module", "exports"].includes(st.name.text)) || (ts.isVariableStatement(st) &&
    st.declarationList.declarations.some((d) => ts.isIdentifier(d.name) && ["module", "exports"].includes(d.name.text))));
  if (shadowed && assigns.length > 0) { replaced.set(sf.fileName, where(assigns[0])); continue; }
  if (assigns.length === 1 && top.length === 1 && ts.isObjectLiteralExpression(skip(top[0].right))) {
    exportObjects.add(skip(top[0].right));
    exportObjectOf.set(sf.fileName, skip(top[0].right));
  }
  else if (assigns.some((a) => ts.isObjectLiteralExpression(skip(a.right)))) replaced.set(sf.fileName, where(assigns[assigns.length > 1 ? 1 : 0]));
}
function skip(n) {
  while (n && (ts.isParenthesizedExpression(n) || ts.isNonNullExpression(n) || ts.isAsExpression(n) ||
    ts.isTypeAssertionExpression(n) || (ts.isSatisfiesExpression && ts.isSatisfiesExpression(n)))) n = n.expression;
  return n;
}
const literalProperty = (s) => s && s.flags & ts.SymbolFlags.Property && (s.declarations || []).length > 0 &&
  (s.declarations || []).every((d) => d.parent && ts.isObjectLiteralExpression(d.parent));
const exportProperty = (s) => literalProperty(s) && s.declarations.some((d) => exportObjects.has(d.parent));
// The properties of the export objects, by declaration: the only symbols
// the scan below types expressions for. A type's property is one of them
// when it shares a declaration (the type of module.exports or of a require
// has its own symbols for the literal's properties).
const candidates = new Set(), candidateOf = new Map();
for (const o of exportObjects) {
  for (const d of o.properties) {
    const s = d.symbol;
    if (s) { candidates.add(s); candidateOf.set(d, s); }
  }
}
const propsOfObject = (o) => o.properties.map((d) => d.symbol).filter(Boolean);
// exports.a = … and module.exports.a = … (an export's declarations are the
// assignments, or their left sides): candidates too, by declaration.
const isExportsObject = (n) => (n = skip(n)) && ((ts.isIdentifier(n) && n.text === "exports") || isModuleExports(n));
function exportAssignment(d) {
  const a = ts.isBinaryExpression(d) ? d : d.parent && ts.isBinaryExpression(d.parent) && d.parent.left === d ? d.parent : undefined;
  return a && a.operatorToken.kind === ts.SyntaxKind.EqualsToken && ts.isPropertyAccessExpression(skip(a.left)) &&
    isExportsObject(skip(a.left).expression) ? a : undefined;
}
const exportPropsOf = new Map(); // file → the export symbols of its exports.a = …
for (const sf of program.getSourceFiles()) {
  if (!sources.has(sf.fileName)) continue;
  (function visit(n) {
    if (ts.isBinaryExpression(n) && exportAssignment(n)) {
      const sym = checker.getSymbolAtLocation(skip(n.left).name);
      if (sym) {
        candidates.add(sym);
        for (const d of sym.declarations || []) candidateOf.set(d, sym);
        if (!exportPropsOf.has(sf.fileName)) exportPropsOf.set(sf.fileName, new Set());
        exportPropsOf.get(sf.fileName).add(sym);
      }
    }
    ts.forEachChild(n, visit);
  })(sf);
}

// Writes by name, writes of no name, escapes, dynamic code (see the header);
// the first cause of each, for value_unknown_at.
const writtenNames = new Map(), writtenAny = new Map(), escaped = new Map(), assignedSymbols = new Set();
let dynamicCode;
// thisAnyAt: a function's or a method's this (a class's constructor's, a
// field initializer's and a static block's aside) is written under a key no
// name fixes (this[k] = v, Object.assign(this, src)) or flows on: the
// function may be called on an export object (exports.m = A.prototype.m, by
// any route), so no export's value is stated.
let thisAnyAt;
function functionThis(n) {
  if (!n || n.kind !== ts.SyntaxKind.ThisKeyword) return false;
  const c = ts.getThisContainer(n, false, false);
  return !(ts.isConstructorDeclaration(c) || ts.isPropertyDeclaration(c) || ts.isClassStaticBlockDeclaration(c));
}
const thisAny = (n) => { if (!thisAnyAt) thisAnyAt = where(n); };
const writeSites = new Map(); // name → every write of it (the nodes)
const first = (m, k, n) => {
  if (m === writtenNames) { if (!writeSites.has(k)) writeSites.set(k, []); writeSites.get(k).push(n); }
  if (!m.has(k)) m.set(k, where(n));
};
const mutators = new Set(["Object.assign", "Object.defineProperty", "Object.defineProperties",
  "Reflect.set", "Reflect.defineProperty", "Reflect.deleteProperty"]);
function keyOf(n) { return n && (ts.isStringLiteralLike(n) || ts.isNumericLiteral(n)) ? n.text : undefined; }
function typedProps(n) {
  if (candidates.size === 0) return [];
  const out = [];
  const m = skip(n);
  if (m && isModuleExports(m) && exportObjectOf.has(m.getSourceFile().fileName)) out.push(...propsOfObject(exportObjectOf.get(m.getSourceFile().fileName)));
  if (m && isExportsObject(m) && exportPropsOf.has(m.getSourceFile().fileName)) out.push(...exportPropsOf.get(m.getSourceFile().fileName));
  let t;
  try { t = checker.getTypeAtLocation(n); } catch (e) { return []; }
  if (!t) return out;
  if (t.flags & (ts.TypeFlags.Any | ts.TypeFlags.Unknown)) return [...out, "*any*"];
  const add = (p) => {
    if (candidates.has(p)) out.push(p);
    for (const d of p.declarations || []) if (candidateOf.has(d)) out.push(candidateOf.get(d));
  };
  for (const p of t.getProperties()) add(p);
  for (const u of t.isUnionOrIntersection() ? t.types : []) for (const p of u.getProperties()) add(p);
  return out;
}
function literalKeys(src, n) { // the names an object literal source defines, or no name
  src = skip(src);
  if (!src || !ts.isObjectLiteralExpression(src)) return false;
  for (const p of src.properties) {
    if (ts.isSpreadAssignment(p) || !p.name || ts.isComputedPropertyName(p.name)) return false;
    first(writtenNames, p.name.text, n);
  }
  return true;
}
function markTarget(t, n) {
  t = skip(t);
  if (!t) return;
  if (ts.isPropertyAccessExpression(t)) {
    first(writtenNames, t.name.text, n);
    const s = resolved(checker.getSymbolAtLocation(t.name));
    if (s && candidates.has(s)) first(writtenNames, s.name, n);
  } else if (ts.isElementAccessExpression(t)) {
    const k = keyOf(skip(t.argumentExpression));
    if (k !== undefined) first(writtenNames, k, n);
    else {
      for (const p of typedProps(t.expression)) first(writtenAny, p, n);
      if (functionThis(skip(t.expression))) thisAny(n);
    }
  } else if (ts.isObjectLiteralExpression(t)) { // ({ x: o.a } = b), ({ ...o.a } = b)
    for (const e of t.properties) {
      if (ts.isPropertyAssignment(e)) markTarget(e.initializer, n);
      else if (ts.isShorthandPropertyAssignment(e)) markTarget(e.name, n);
      else if (ts.isSpreadAssignment(e)) markTarget(e.expression, n);
    }
  } else if (ts.isArrayLiteralExpression(t)) { // [o.a] = b
    for (const e of t.elements) markTarget(ts.isSpreadElement(e) ? e.expression : e, n);
  } else if (ts.isBinaryExpression(t) && t.operatorToken.kind === ts.SyntaxKind.EqualsToken) { // [o.a = 1] = b
    markTarget(t.left, n);
  } else if (ts.isIdentifier(t)) {
    const s = checker.getSymbolAtLocation(t);
    if (s) assignedSymbols.add(s);
  }
}
function resolved(sym) {
  if (sym && sym.flags & ts.SymbolFlags.Alias) {
    try { return checker.getAliasedSymbol(sym); } catch (e) { return undefined; }
  }
  return sym;
}
// usedAsValue reports whether the expression n, whose value may be an export
// object, flows on: anything but reading a member, calling, comparing,
// testing, typeof / void / !, an unannotated const's initializer, the left
// side of an assignment and the right side of module.exports = X.
function usedAsValue(n) {
  let c = n, p = n.parent;
  while (p && (ts.isParenthesizedExpression(p) || ts.isNonNullExpression(p))) { c = p; p = p.parent; }
  if (!p) return false;
  if ((ts.isPropertyAccessExpression(p) || ts.isElementAccessExpression(p)) && p.expression === c) return false;
  if (ts.isCallExpression(p) && p.expression === c) return false;
  if (ts.isExpressionStatement(p) || ts.isIfStatement(p) || ts.isWhileStatement(p) || ts.isDoStatement(p)) return false;
  if (ts.isForStatement(p) && p.condition === c) return false;
  if (ts.isConditionalExpression(p) && p.condition === c) return false;
  if (ts.isTypeOfExpression(p) || ts.isVoidExpression(p)) return false;
  if (ts.isPrefixUnaryExpression(p) && p.operator === ts.SyntaxKind.ExclamationToken) return false;
  if (ts.isBinaryExpression(p)) {
    const k = p.operatorToken.kind;
    if ([ts.SyntaxKind.EqualsEqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsEqualsToken, ts.SyntaxKind.EqualsEqualsToken,
      ts.SyntaxKind.ExclamationEqualsToken, ts.SyntaxKind.InstanceOfKeyword, ts.SyntaxKind.InKeyword].includes(k)) return false;
    if (k >= ts.SyntaxKind.FirstAssignment && k <= ts.SyntaxKind.LastAssignment && p.left === c) return false;
    // a && b is a only when a is falsy, never an object.
    if (k === ts.SyntaxKind.AmpersandAmpersandToken && p.left === c) return false;
    // module.exports = X exports the object X itself: its users are typed as
    // it, so their writes and escapes are found where they are.
    if (k === ts.SyntaxKind.EqualsToken && p.right === c && isModuleExports(skip(p.left))) return false;
  }
  if (ts.isVariableDeclaration(p) && p.initializer === c && !p.type && !ts.getJSDocType(p) &&
    ts.getCombinedNodeFlags(p) & ts.NodeFlags.Const) return false;
  if (ts.isForInStatement(p) || (ts.isForOfStatement(p) && p.expression === c)) return false;
  // return this hands the receiver back (writes through the returned value
  // are beyond the scan, as for Ark).
  if (c.kind === ts.SyntaxKind.ThisKeyword && (ts.isReturnStatement(p) || (ts.isArrowFunction(p) && p.body === c))) return false;
  return true;
}
for (const sf of program.getSourceFiles()) {
  if (!sources.has(sf.fileName)) continue;
  (function visit(n) {
    // The module wrapper's other ways to the export: its arguments
    // (arguments[2].exports) and require.cache[…].exports.
    const via = (x) => { // require.cache / require.main / module.constructor …, reached through
      x = skip(x);
      if (!x || !(ts.isPropertyAccessExpression(x) || ts.isElementAccessExpression(x)) || !ts.isIdentifier(x.expression)) return false;
      const k = ts.isPropertyAccessExpression(x) ? x.name.text : keyOf(skip(x.argumentExpression));
      return (x.expression.text === "require" && [undefined, "cache", "main"].includes(k)) ||
        (x.expression.text === "module" && ["constructor", "parent", "children", "__proto__"].includes(k)) ||
        (x.expression.text === "process" && k === "mainModule");
    };
    if ((ts.isIdentifier(n) && n.text === "arguments" && !ts.findAncestor(n.parent, (a) => ts.isFunctionLike(a) && !ts.isArrowFunction(a))) ||
      ((ts.isPropertyAccessExpression(n) || ts.isElementAccessExpression(n)) && via(n.expression))) {
      if (!dynamicCode) dynamicCode = where(n);
    }
    if (ts.isWithStatement(n) || (ts.isCallExpression(n) && ts.isIdentifier(skip(n.expression)) &&
      ["eval", "Function"].includes(skip(n.expression).text)) || (ts.isNewExpression(n) && ts.isIdentifier(n.expression) &&
      n.expression.text === "Function")) {
      if (!dynamicCode) dynamicCode = where(n);
    }
    if (ts.isBinaryExpression(n) && n.operatorToken.kind >= ts.SyntaxKind.FirstAssignment &&
      n.operatorToken.kind <= ts.SyntaxKind.LastAssignment) markTarget(n.left, n);
    else if ((ts.isPrefixUnaryExpression(n) || ts.isPostfixUnaryExpression(n)) &&
      (n.operator === ts.SyntaxKind.PlusPlusToken || n.operator === ts.SyntaxKind.MinusMinusToken)) markTarget(n.operand, n);
    else if (ts.isDeleteExpression(n)) markTarget(n.expression, n);
    else if ((ts.isForInStatement(n) || ts.isForOfStatement(n)) && !ts.isVariableDeclarationList(n.initializer)) markTarget(n.initializer, n);
    if (ts.isCallExpression(n) && n.arguments.length > 0) {
      const callee = skip(n.expression);
      const name = callee.getText(sf).replace(/\s+/g, "");
      if (mutators.has(name)) {
        if (["Object.defineProperty", "Reflect.defineProperty", "Reflect.set", "Reflect.deleteProperty"].includes(name)) {
          const k = keyOf(skip(n.arguments[1]));
          if (k !== undefined) first(writtenNames, k, n);
          else {
            for (const p of typedProps(n.arguments[0])) first(writtenAny, p, n);
            if (functionThis(skip(n.arguments[0]))) thisAny(n);
          }
        } else {
          const sources = name === "Object.defineProperties" ? n.arguments.slice(1, 2) : n.arguments.slice(1);
          if (!sources.every((a) => literalKeys(a, n))) {
            for (const p of typedProps(n.arguments[0])) first(writtenAny, p, n);
            if (functionThis(skip(n.arguments[0]))) thisAny(n);
          }
        }
      } else if ((ts.isPropertyAccessExpression(callee) && ["__defineGetter__", "__defineSetter__"].includes(callee.name.text)) ||
        (ts.isElementAccessExpression(callee) && ["__defineGetter__", "__defineSetter__"].includes(keyOf(skip(callee.argumentExpression))))) {
        const k = keyOf(skip(n.arguments[0]));
        if (k !== undefined) first(writtenNames, k, n);
        else for (const p of typedProps(callee.expression)) first(writtenAny, p, n);
      }
    }
    // A first argument of a mutator is no flow of it (its writes are
    // counted above).
    const mutated = n.parent && ts.isCallExpression(n.parent) && n.parent.arguments[0] === n &&
      mutators.has(skip(n.parent.expression).getText(sf).replace(/\s+/g, ""));
    // An alias of this or of require (const self = this) is typed apart:
    // an initializer is a flow for them.
    const aliased = (x) => { let c = x; while (c.parent && ts.isParenthesizedExpression(c.parent)) c = c.parent;
      return c.parent && ts.isVariableDeclaration(c.parent) && c.parent.initializer === c; };
    if (functionThis(n) && !mutated && (usedAsValue(n) || aliased(n))) thisAny(n);
    // require handed on (const r = require), require('module') (Module._cache),
    // require(__filename) (this module): the module's other ways to its
    // exports.
    if (ts.isIdentifier(n) && n.text === "require" && !(n.parent && ts.isCallExpression(n.parent) && n.parent.expression === n) &&
      !(n.parent && ts.isPropertyAccessExpression(n.parent) && n.parent.name === n) && (usedAsValue(n) || aliased(n))) {
      if (!dynamicCode) dynamicCode = where(n);
    }
    if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === "require" && n.arguments.length > 0) {
      const a = skip(n.arguments[0]);
      if ((ts.isIdentifier(a) && a.text === "__filename") || ["module", "node:module"].includes(keyOf(a))) {
        if (!dynamicCode) dynamicCode = where(n);
      }
    }
    if (candidates.size > 0 && (ts.isIdentifier(n) || n.kind === ts.SyntaxKind.ThisKeyword || ts.isPropertyAccessExpression(n) ||
      ts.isElementAccessExpression(n) || ts.isCallExpression(n) || ts.isAsExpression(n) || ts.isTypeAssertionExpression(n)) &&
      ts.isExpression(n) && !(n.parent && ts.getNameOfDeclaration(n.parent) === n && !ts.isShorthandPropertyAssignment(n.parent)) &&
      !(n.parent && ts.isPropertyAccessExpression(n.parent) && n.parent.name === n) && usedAsValue(n)) {
      for (const p of typedProps(n)) if (p !== "*any*") first(escaped, p, n);
    }
    ts.forEachChild(n, visit);
  })(sf);
}

const valueKind = (d) => {
  if (ts.isFunctionDeclaration(d) || ts.isClassDeclaration(d)) return ts.isClassDeclaration(d) ? "class" : "function";
  const init = ts.isVariableDeclaration(d) && d.initializer && skip(d.initializer);
  if (init && (ts.isFunctionExpression(init) || ts.isArrowFunction(init))) return "function";
  if (init && ts.isClassExpression(init)) return "class";
  return "data";
};

// valueLayer returns the fields the value layer adds to the site at id.
// UNSAFE: the reasons that leave the value of an export open (the module, or
// the program, may write another): a site through such a property is
// export_unsafe, and its Strong or Exact targets are counted apart.
const UNSAFE = new Set(["replaced", "overwritten_in_literal", "multiple_declarations", "written", "written_any",
  "escapes", "dynamic_code", "alias_reassigned"]);
function valueLayer(id) {
  const r = valueLayerOf(id);
  if (UNSAFE.has(r.value_unknown)) r.export_unsafe = true;
  return r;
}
// exportOnChain returns the export property an alias chain from sym passes
// through — a property of an export object, or exports.a = … — with the
// writes that declare it, or undefined.
function exportOnChain(sym) {
  for (let s = sym, i = 0; s && i < 16; i++) {
    for (const d of s.declarations || []) {
      if ((ts.isShorthandPropertyAssignment(d) || ts.isPropertyAssignment(d)) && exportObjects.has(d.parent)) {
        return { name: d.name.text, file: d.getSourceFile().fileName, own: [], cand: candidateOf.get(d) || d.symbol, aliases: [] };
      }
      const a = exportAssignment(d);
      if (a) {
        return { name: skip(a.left).name.text, file: d.getSourceFile().fileName, cand: candidateOf.get(d) || s,
          own: (s.declarations || []).map((x) => exportAssignment(x)).filter(Boolean) };
      }
    }
    if (!(s.flags & ts.SymbolFlags.Alias)) break;
    if (assignedSymbols.has(s)) return { reassigned: s };
    try { s = checker.getImmediateAliasedSymbol(s); } catch (e) { break; }
  }
  return undefined;
}
// exportSafety returns why the value of the export e is open, or undefined.
function exportSafety(e) {
  if (e.reassigned) return { value_unknown: "alias_reassigned" };
  if (dynamicCode) return { value_unknown: "dynamic_code", value_unknown_at: dynamicCode };
  if (thisAnyAt) return { value_unknown: "written_any", value_unknown_at: thisAnyAt };
  if (replaced.has(e.file)) return { value_unknown: "replaced", value_unknown_at: replaced.get(e.file) };
  const other = (writeSites.get(e.name) || []).filter((n) => !e.own.includes(n));
  if (other.length) return { value_unknown: "written", value_unknown_at: where(other[0]) };
  if (e.own.length > 1) return { value_unknown: "multiple_declarations" };
  if (writtenAny.has(e.cand)) return { value_unknown: "written_any", value_unknown_at: writtenAny.get(e.cand) };
  if (escaped.has(e.cand)) return { value_unknown: "escapes", value_unknown_at: escaped.get(e.cand) };
  return undefined;
}
function valueLayerOf(id) {
  // exports or module itself: the checker answers the property an
  // exports.a = … assigns at exports, but the site is the module's object.
  if (id.text === "exports" || id.text === "module") return {};
  let sym = checker.getSymbolAtLocation(id);
  if (ts.isShorthandPropertyAssignment(id.parent)) return {}; // the site reads the value itself (v1 already)
  // A site written, not read (a = b, m.a = b, a++): no value is read there.
  const t = id.parent && ts.isPropertyAccessExpression(id.parent) && id.parent.name === id ? id.parent : id;
  if (ts.isAssignmentTarget(t) || (t.parent && (ts.isPrefixUnaryExpression(t.parent) || ts.isPostfixUnaryExpression(t.parent)) &&
    [ts.SyntaxKind.PlusPlusToken, ts.SyntaxKind.MinusMinusToken].includes(t.parent.operator)) ||
    (t.parent && ts.isDeleteExpression(t.parent))) return {};
  // A site whose alias chain passes through an export the checker resolves
  // to its value (module.exports = { a } of shorthands alone, exports.a = a):
  // v1 states the value; whether the module or the program may write another
  // is checked as for a property.
  if (sym && sym.flags & ts.SymbolFlags.Alias && !literalProperty(resolved(sym))) {
    const e = exportOnChain(sym);
    if (e) return exportSafety(e) || {};
  }
  if (sym && sym.flags & ts.SymbolFlags.Alias) {
    if (assignedSymbols.has(sym)) return literalProperty(resolved(sym)) ? { value_unknown: "alias_reassigned" } : {};
    try { sym = checker.getAliasedSymbol(sym); } catch (e) { return {}; }
  }
  if (!literalProperty(sym)) return {};
  const decl0 = sym.declarations[0];
  if (!exportProperty(sym)) {
    const f = decl0.getSourceFile().fileName;
    return replaced.has(f) && (sym.declarations || []).length === 1 ? { value_unknown: "replaced", value_unknown_at: replaced.get(f) } : {};
  }
  if (dynamicCode) return { value_unknown: "dynamic_code", value_unknown_at: dynamicCode };
  if (thisAnyAt) return { value_unknown: "written_any", value_unknown_at: thisAnyAt };
  const via = [], rules = [];
  for (let step = 0; ; step++) {
    if (step === 16) return { value_unknown: "chain_limit" };
    if (!exportProperty(sym)) return { value_unknown: "not_export_object" };
    const decls = sym.declarations || [];
    if (decls.length !== 1) return { value_unknown: "multiple_declarations" };
    const d = decls[0];
    const props = d.parent.properties, i = props.indexOf(d);
    for (const later of props.slice(i + 1)) {
      if (ts.isSpreadAssignment(later) || !later.name || ts.isComputedPropertyName(later.name) || later.name.text === sym.name) {
        return { value_unknown: "overwritten_in_literal", value_unknown_at: where(later) };
      }
    }
    if (writtenNames.has(sym.name)) return { value_unknown: "written", value_unknown_at: writtenNames.get(sym.name) };
    const S = candidateOf.get(d) || sym;
    if (writtenAny.has(S)) return { value_unknown: "written_any", value_unknown_at: writtenAny.get(S) };
    if (escaped.has(S)) return { value_unknown: "escapes", value_unknown_at: escaped.get(S) };
    let vsym;
    if (ts.isShorthandPropertyAssignment(d)) {
      vsym = checker.getShorthandAssignmentValueSymbol(d);
      rules.push("shorthand");
    } else if (ts.isPropertyAssignment(d) && ts.isIdentifier(d.initializer)) {
      vsym = checker.getSymbolAtLocation(d.initializer);
      rules.push("identifier_initializer");
    } else {
      return { value_unknown: "not_identifier" };
    }
    if (!vsym) return { value_unknown: "no_value_symbol" };
    if (vsym.flags & ts.SymbolFlags.Alias) {
      for (const a of vsym.declarations || []) {
        if (assignedSymbols.has(vsym)) return { value_unknown: "alias_reassigned", value_unknown_at: where(a) };
        if (isSource(a)) via.push(declOf(a));
      }
      try { vsym = checker.getAliasedSymbol(vsym); } catch (e) { return { value_unknown: "alias_unresolved" }; }
      if (!vsym || vsym === checker.getUnknownSymbol?.()) return { value_unknown: "alias_unresolved" };
    }
    let vdecls = (vsym && vsym.declarations) || [];
    const real = vdecls.filter((v) => !isAssignment(v));
    if (real.length === 1 && real.length < vdecls.length) vdecls = real;
    if (vdecls.length === 0) return { value_unknown: "no_value_symbol" };
    if (!vdecls.some(inCorpus)) return { value_unknown: "external_value" };
    if (vdecls.length > 1) return { value_unknown: vdecls.every(isSource) ? "ambiguous_value" : "declaration_file_value" };
    if (!isSource(vdecls[0])) return { value_unknown: "declaration_file_value" };
    const vd = vdecls[0];
    if (ts.isSourceFile(vd) || ts.isExportAssignment(vd) || (ts.isBinaryExpression(vd) && isModuleExports(skip(vd.left))) ||
      vsym.flags & ts.SymbolFlags.ValueModule) return { value_unknown: "module_value" };
    if (literalProperty(vsym)) { // another property: its value, through it
      via.push(declOf(vd));
      sym = vsym;
      continue;
    }
    const out = { value: declOf(vd), value_kind: valueKind(vd), value_rule: rules.join(">") };
    if (via.length) out.value_via = via;
    return out;
  }
}

const out = [];
let added = 0, unknown = 0;
for (const line of v1.split("\n")) {
  if (!line) continue;
  const site = JSON.parse(line);
  if (site.class === "declaration") {
    const sf = program.getSourceFile(path.join(root, site.file));
    const id = sf && identifierAt(sf, textPos(sf, site.start_line, site.start_col));
    if (!id || id.text !== site.name) throw new Error(`no identifier ${site.name} at ${site.file}:${site.start_line}:${site.start_col}`);
    const v = valueLayer(id);
    if (v.value) added++;
    if (v.value_unknown) unknown++;
    Object.assign(site, v);
  }
  out.push(JSON.stringify(site));
}
process.stdout.write(out.join("\n") + (out.length ? "\n" : ""));
console.error("value layer: contract v2, sites", out.length, "with value", added, "value_unknown", unknown);
