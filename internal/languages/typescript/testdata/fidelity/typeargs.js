// For each case `<args>`, reports how the TypeScript compiler parses the
// statement `f<args>(x);`: whether it is a call of f with type arguments
// and argument x and nothing else, without parse errors, and the type
// references in the type arguments (byte offsets into the statement).
// Used by TestFidelity_TypeArgumentsMatchCompiler; never by Ark itself.
//
// Usage: node typeargs.js <typescript module dir> <cases.json> <out.json>
"use strict";
const [tsdir, input, out] = process.argv.slice(2);
const ts = require(tsdir);
const fs = require("fs");
const cases = JSON.parse(fs.readFileSync(input, "utf8"));
const res = cases.map((args) => {
  const text = "f" + args + "(x);";
  const sf = ts.createSourceFile("c.ts", text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const byte = (pos) => Buffer.byteLength(text.slice(0, pos), "utf8");
  let call = false;
  const refs = [];
  if (sf.parseDiagnostics.length === 0 && sf.statements.length === 1) {
    const st = sf.statements[0];
    if (ts.isExpressionStatement(st) && ts.isCallExpression(st.expression)) {
      const c = st.expression;
      call = ts.isIdentifier(c.expression) && c.expression.text === "f" && !!c.typeArguments &&
        c.arguments.length === 1 && ts.isIdentifier(c.arguments[0]) && c.arguments[0].text === "x";
      if (call) {
        const visit = (n) => {
          if (ts.isTypeReferenceNode(n)) {
            const t = n.typeName;
            if (ts.isIdentifier(t)) refs.push([t.text, "", byte(t.getStart(sf))]);
            else refs.push([t.right.text, t.left.getText(sf), byte(t.getStart(sf))]);
          }
          ts.forEachChild(n, visit);
        };
        c.typeArguments.forEach(visit);
      }
    }
  }
  return { call, refs };
});
fs.writeFileSync(out, JSON.stringify(res));
