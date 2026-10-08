// Lists, per file, the references the TypeScript compiler's own parser (and
// binder, for type parameters) sees, in Ark's terms. Used by
// TestFidelity_TypeScriptCompiler; never by Ark itself.
//
// Usage: node tsrefs.js <typescript module dir> <file list> <out.jsonl>
"use strict";
const [tsdir, list, out] = process.argv.slice(2);
const ts = require(tsdir);
const fs = require("fs");
const o = fs.openSync(out, "w");
for (const file of fs.readFileSync(list, "utf8").split("\n").filter(Boolean)) {
  const text = fs.readFileSync(file, "utf8");
  const kind = file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const host = ts.createCompilerHost({ noLib: true, noResolve: true });
  host.getSourceFile = (fn) =>
    fn === file ? ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind) : undefined;
  const prog = ts.createProgram([file], { noLib: true, noResolve: true, jsx: ts.JsxEmit.Preserve }, host);
  const sf = prog.getSourceFile(file);
  const checker = prog.getTypeChecker();
  const lines = text.split("\n");
  // 1-based line and byte column, as Ark reports positions.
  const loc = (pos) => {
    const lc = sf.getLineAndCharacterOfPosition(pos);
    return [lc.line + 1, Buffer.byteLength(lines[lc.line].slice(0, lc.character), "utf8") + 1];
  };
  const unwrap = (n) => {
    while (ts.isNonNullExpression(n) || ts.isParenthesizedExpression(n)) n = n.expression;
    return n;
  };
  const refs = [];
  const callee = (f) => {
    if (ts.isIdentifier(f)) refs.push(["call", f.text, "", ...loc(f.getStart(sf))]);
    else if (ts.isPropertyAccessExpression(f) && f.expression.kind !== ts.SyntaxKind.SuperKeyword)
      refs.push(["call", f.name.text, unwrap(f.expression).getText(sf), ...loc(f.name.getStart(sf))]);
  };
  const visit = (n) => {
    if (ts.isCallExpression(n)) callee(n.expression);
    else if (ts.isTaggedTemplateExpression(n)) callee(n.tag);
    else if (ts.isNewExpression(n)) {
      const c = n.expression;
      if (ts.isIdentifier(c)) refs.push(["construction", c.text, "", ...loc(c.getStart(sf))]);
      else if (ts.isPropertyAccessExpression(c))
        refs.push(["construction", c.name.text, c.expression.getText(sf), ...loc(c.getStart(sf))]);
    } else if (ts.isJsxOpeningElement(n) || ts.isJsxSelfClosingElement(n)) {
      const t = n.tagName;
      if (ts.isIdentifier(t) && /^[A-Z]/.test(t.text)) refs.push(["call", t.text, "", ...loc(t.getStart(sf))]);
      else if (ts.isPropertyAccessExpression(t))
        refs.push(["call", t.name.text, t.expression.getText(sf), ...loc(t.getStart(sf))]);
    } else if (ts.isTypeReferenceNode(n)) {
      const t = n.typeName;
      if (ts.isIdentifier(t) && t.text === "const") {
        // `as const` is a const assertion, not a type reference.
      } else if (ts.isIdentifier(t)) {
        const sym = checker.getSymbolAtLocation(t);
        const tp = sym && sym.flags & ts.SymbolFlags.TypeParameter;
        refs.push([tp ? "type_parameter" : "type_use", t.text, "", ...loc(t.getStart(sf))]);
      } else refs.push(["type_use", t.right.text, t.left.getText(sf), ...loc(t.getStart(sf))]);
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
  fs.writeSync(o, JSON.stringify({ file, errors: sf.parseDiagnostics.length, refs }) + "\n");
}
