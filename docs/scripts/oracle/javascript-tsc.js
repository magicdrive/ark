// JavaScript oracle for TestFidelityBaseline: the TypeScript checker's
// symbol at each identifier, over JavaScript (allowJs, checkJs off).
//
// usage: node javascript-tsc.js <typescript-package-dir> <corpus-dir> [<extensions>] > oracle.jsonl
//
//   <extensions>  the files whose identifiers are sites, comma-separated
//                 (default .js,.mjs,.cjs,.jsx); every script file of the corpus
//                 (.d.ts aside) is in the program either way, so a site may
//                 bind to a declaration in another language's file
//
// The sites are found independently of Ark: every identifier the TypeScript
// parser reads as a reference (an Identifier that is no declaration's own
// name, no property name of an object literal or class, no import or export
// specifier's name, no label, no closing JSX tag; a shorthand property { a }
// and export default a are references) and every property name of a property
// access (a.b: b), in every file of the extensions (directories starting with
// "." and node_modules skipped, as Ark skips them). For each, the checker's
// symbol (through an alias to the aliased symbol) gives the declarations. A
// site is
//
//   declaration       one declaration in a non-declaration file of the corpus
//                     (a class or function with static members assigned
//                     after it counts as its declaration alone); decl.name is
//                     the declaration's own name, which an alias import does
//                     not share, and via the declarations the alias passes
//                     through (an import binding, const x = require(...))
//   ambiguous         several, all in non-declaration files of the corpus
//   external          every declaration is outside the corpus (the runtime's
//                     lib files, a dependency), or none and the name comes
//                     through an import of a package the program cannot
//                     resolve (a bare specifier, a dependency not installed)
//                     that is not the corpus's own (package.json "name")
//   declaration_file  only declaration files (.d.ts) of the corpus: they type
//                     what the JavaScript assigns at run time, which is not
//                     decided
//   unknown           no symbol and no declaration
//
// The checker infers member types from assignments and JSDoc: a member's
// declaration may be one no syntactic rule can prove. Score Ark's Unresolved
// on such sites as recall lost, never its Strong as earned by the same
// inference. Nothing is classed "dynamic" (computed access a[k] has no
// identifier site).
//
// A site that is the name of a property access (a.b: b) carries
// "member": true — the oracle's own denominator of member recall.
//
// Positions are 1-based; columns are byte columns of the file as stored (a
// byte order mark counts), lines split at "\n" only, as Ark's. Pin the
// TypeScript version (npm install typescript@<version>) and record it.
"use strict";
const [tsdir, rootArg, extArg] = process.argv.slice(2);
const ts = require(tsdir);
const fs = require("fs");
const path = require("path");
const root = fs.realpathSync(rootArg);
const exts = (extArg || ".js,.mjs,.cjs,.jsx").split(",");

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

// position maps a position of sf.text to a 1-based line and byte column of
// the file as stored: TypeScript drops a byte order mark from sf.text and
// counts U+2028 / U+2029 as line breaks; Ark does neither.
const layouts = new Map();
function position(sf, pos) {
  let l = layouts.get(sf.fileName);
  if (!l) {
    const raw = fs.readFileSync(sf.fileName);
    const bom = raw.length >= 3 && raw[0] === 0xef && raw[1] === 0xbb && raw[2] === 0xbf && sf.text.charCodeAt(0) !== 0xfeff ? 3 : 0;
    const starts = [0];
    for (let i = 0; i < raw.length; i++) if (raw[i] === 0x0a) starts.push(i + 1);
    l = { bom, starts };
    layouts.set(sf.fileName, l);
  }
  const off = l.bom + Buffer.byteLength(sf.text.slice(0, pos), "utf8");
  let lo = 0, hi = l.starts.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (l.starts[mid] <= off) lo = mid; else hi = mid - 1;
  }
  return [lo + 1, off - l.starts[lo] + 1];
}

// isSite reports whether an identifier refers rather than declares.
function isSite(id) {
  const p = id.parent;
  if (!p) return false;
  if (ts.isPropertyAccessExpression(p)) return true; // both a and b of a.b
  if (ts.isShorthandPropertyAssignment(p) || ts.isExportAssignment(p)) return true; // { a }, export default a
  if (ts.isJsxClosingElement(p)) return false; // the opening tag is the site
  if (ts.getNameOfDeclaration(p) === id) return false; // a declaration's own name
  if (ts.isImportSpecifier(p) || ts.isExportSpecifier(p) || ts.isImportClause(p) || ts.isNamespaceImport(p)) return false;
  if (ts.isLabeledStatement(p) || ts.isBreakOrContinueStatement(p)) return false;
  if (ts.isPropertyAssignment(p) || ts.isMethodDeclaration(p) || ts.isPropertyDeclaration(p)) return p.name !== id;
  if (ts.isQualifiedName(p) || ts.isMetaProperty(p)) return false;
  return true;
}

const inCorpus = (d) => d.getSourceFile().fileName.startsWith(root + path.sep);
const isSource = (d) => !d.getSourceFile().isDeclarationFile && sources.has(d.getSourceFile().fileName);
// An assignment declaring a member after the fact (C.NAMES = ..., Fn.prototype.m = ...):
// TypeScript records C.X = 1 as a declaration of C at the identifier C.
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

// The corpus's own package names (package.json "name"): an import of one of
// them is the corpus itself, not a dependency, though the program may not
// resolve it (its build output left out of the corpus).
const ownPackages = new Set();
(function names(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    if (e.name.startsWith(".") || e.name === "node_modules") continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) names(p);
    else if (e.name === "package.json") {
      try { const n = JSON.parse(fs.readFileSync(p, "utf8")).name; if (n) ownPackages.add(n); } catch (err) {}
    }
  }
})(root);
const ownPackage = (spec) => [...ownPackages].some((n) => spec === n || spec.startsWith(n + "/"));

// unresolvedPackage reports whether n's leftmost name is bound by an import
// (or require) of a non-relative module the program cannot resolve, other
// than the corpus's own package.
function unresolvedPackage(n) {
  let e = n;
  while (e.parent && ts.isPropertyAccessExpression(e.parent) && e.parent.name === e) e = e.parent.expression;
  while (ts.isPropertyAccessExpression(e)) e = e.expression;
  if (!ts.isIdentifier(e)) return false;
  const sym = checker.getSymbolAtLocation(e);
  for (const d of (sym && sym.declarations) || []) {
    let spec;
    const imp = ts.findAncestor(d, ts.isImportDeclaration);
    if (imp && ts.isStringLiteral(imp.moduleSpecifier)) spec = imp.moduleSpecifier;
    else if (ts.isVariableDeclaration(d) && d.initializer && ts.isCallExpression(d.initializer) &&
      ts.isIdentifier(d.initializer.expression) && d.initializer.expression.text === "require" &&
      d.initializer.arguments.length === 1 && ts.isStringLiteral(d.initializer.arguments[0])) spec = d.initializer.arguments[0];
    if (!spec || spec.text.startsWith(".") || spec.text.startsWith("/") || ownPackage(spec.text)) continue;
    const mod = checker.getSymbolAtLocation(spec);
    if (!mod || !(mod.declarations || []).length) return true;
  }
  return false;
}

const out = [];
for (const sf of program.getSourceFiles()) {
  if (!sources.has(sf.fileName) || !exts.some((e) => sf.fileName.endsWith(e))) continue;
  const rel = path.relative(root, sf.fileName).split(path.sep).join("/");
  const visit = (n) => {
    if (ts.isIdentifier(n) && isSite(n)) {
      const [sl, sc] = position(sf, n.getStart(sf));
      const [el, ec] = position(sf, n.getEnd());
      const site = { lang: "javascript", file: rel, name: n.text, start_line: sl, start_col: sc, end_line: el, end_col: ec };
      // A member access's name (a.b: b): member recall's oracle-side denominator.
      if (ts.isPropertyAccessExpression(n.parent) && n.parent.name === n) site.member = true;
      let sym = checker.getSymbolAtLocation(n);
      if (ts.isShorthandPropertyAssignment(n.parent)) sym = checker.getShorthandAssignmentValueSymbol(n.parent);
      const via = [];
      if (sym && sym.flags & ts.SymbolFlags.Alias) {
        for (const d of sym.declarations || []) if (isSource(d)) via.push(declOf(d));
        try { sym = checker.getAliasedSymbol(sym); } catch (e) { sym = undefined; }
      }
      let decls = (sym && sym.declarations) || [];
      const real = decls.filter((d) => !isAssignment(d));
      if (real.length === 1 && real.length < decls.length) decls = real;
      if (decls.length === 0) {
        site.class = unresolvedPackage(n) ? "external" : "unknown";
      } else if (!decls.some(inCorpus)) {
        site.class = "external";
      } else if (decls.length === 1 && isSource(decls[0])) {
        site.class = "declaration";
        site.decl = declOf(decls[0]);
      } else if (decls.every(isSource)) {
        site.class = "ambiguous";
        site.decls = decls.map(declOf);
      } else if (!decls.some(isSource)) {
        site.class = "declaration_file";
      } else {
        site.class = "unknown";
      }
      if (via.length && site.class !== "unknown") site.via = via;
      out.push(JSON.stringify(site));
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
}
process.stdout.write(out.join("\n") + (out.length ? "\n" : ""));
console.error("typescript", ts.version, "program files", files.length, "sites", out.length);
