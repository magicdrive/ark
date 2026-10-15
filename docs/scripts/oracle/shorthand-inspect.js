// Inspects, with the TypeScript checker, what a site's symbol is and how it
// reaches a declaration: the identifier's AST node and parent, the symbol at
// the location, every alias step (TypeChecker.getImmediateAliasedSymbol)
// with its declarations, the final aliased symbol's declarations and, for a
// declaration that is a shorthand property assignment ({ a }), the value
// symbol the shorthand reads (TypeChecker.getShorthandAssignmentValueSymbol)
// followed through its own aliases. Ark's output is never read.
//
// usage: node shorthand-inspect.js <typescript-package-dir> <corpus-dir> <file:line:col> ...
//
// (Hardening F6B-5: docs/development/web-semantic-fidelity/f6b5/cases.md.)
// Positions are 1-based lines and byte columns, as javascript-tsc.js writes
// them; the program is javascript-tsc.js's.
"use strict";
const [tsdir, rootArg, ...siteArgs] = process.argv.slice(2);
const ts = require(tsdir);
const fs = require("fs");
const path = require("path");
const root = fs.realpathSync(rootArg);

const files = [];
(function walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : 1))) {
    if (e.name.startsWith(".") || e.name === "node_modules" || e.name === "vendor") continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p);
    else if (/\.(m|c)?(js|jsx|ts|tsx)$/.test(e.name) && !e.name.endsWith(".d.ts")) files.push(p);
  }
})(root);
const program = ts.createProgram(files, {
  allowJs: true, checkJs: false, noEmit: true, jsx: ts.JsxEmit.Preserve,
  module: ts.ModuleKind.Preserve, moduleResolution: ts.ModuleResolutionKind.Bundler,
  target: ts.ScriptTarget.ES2022, types: [], skipLibCheck: true,
});
const checker = program.getTypeChecker();

// offsetOf maps a 1-based line and byte column (no BOM in the corpora
// inspected) to a position of sf.text.
function offsetOf(sf, line, col) {
  const raw = fs.readFileSync(sf.fileName);
  let l = 1, i = 0;
  while (l < line) { if (raw[i] === 0x0a) l++; i++; }
  const byteOff = i + col - 1;
  return Buffer.from(raw.subarray(0, byteOff)).toString("utf8").length;
}
function where(d) {
  const f = d.getSourceFile();
  const n = ts.getNameOfDeclaration(d) || d;
  const p = f.getLineAndCharacterOfPosition(n.getStart(f));
  return `${path.relative(root, f.fileName)}:${p.line + 1}:${p.character + 1}`;
}
function kind(n) { return n ? ts.SyntaxKind[n.kind] : "-"; }
function describeDecl(d) {
  const text = d.getText().replace(/\s+/g, " ").slice(0, 70);
  return `${kind(d)} @ ${where(d)} parent=${kind(d.parent)} ${JSON.stringify(text)}`;
}
function flags(sym) {
  const out = [];
  for (const k of ["Alias", "Property", "Function", "Variable", "BlockScopedVariable", "FunctionScopedVariable", "Class", "Assignment"]) {
    if (sym.flags & ts.SymbolFlags[k]) out.push(k);
  }
  return out.join("|");
}
function followValue(sym, indent) {
  // The value a shorthand reads, through its aliases.
  let s = sym, step = 0;
  while (s) {
    console.log(`${indent}value[${step}] ${s.name} flags=${flags(s)}`);
    for (const d of s.declarations || []) console.log(`${indent}  decl ${describeDecl(d)}`);
    if (!(s.flags & ts.SymbolFlags.Alias) || step > 8) break;
    s = checker.getImmediateAliasedSymbol(s);
    step++;
  }
}

for (const arg of siteArgs) {
  const m = /^(.*):(\d+):(\d+)$/.exec(arg);
  const sf = program.getSourceFile(path.join(root, m[1]));
  const pos = offsetOf(sf, +m[2], +m[3]);
  const node = (function find(n) {
    if (pos >= n.getStart(sf) && pos < n.getEnd()) {
      const c = ts.forEachChild(n, find);
      return c || n;
    }
  })(sf);
  console.log(`== ${arg} node=${kind(node)} text=${JSON.stringify(node.getText(sf))} parent=${kind(node.parent)} grandparent=${kind(node.parent && node.parent.parent)}`);
  let sym = checker.getSymbolAtLocation(node);
  if (!sym) { console.log("  no symbol"); continue; }
  let step = 0;
  while (sym) {
    console.log(`  symbol[${step}] ${sym.name} flags=${flags(sym)}`);
    for (const d of sym.declarations || []) {
      console.log(`    decl ${describeDecl(d)}`);
      if (ts.isShorthandPropertyAssignment(d)) {
        const v = checker.getShorthandAssignmentValueSymbol(d);
        console.log(`    shorthand value symbol (getShorthandAssignmentValueSymbol): ${v ? v.name : "none"}`);
        if (v) followValue(v, "      ");
      }
    }
    if (!(sym.flags & ts.SymbolFlags.Alias) || step > 8) break;
    sym = checker.getImmediateAliasedSymbol(sym);
    step++;
  }
}
