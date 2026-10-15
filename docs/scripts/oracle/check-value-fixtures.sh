#!/bin/sh
# Checks javascript-tsc-value.js's value layer on value-fixtures/ (spec/, the
# cases of semantics.md; cases/*, one program each, since a write by name
# reaches every export object of its program) against
# value-fixtures/expected.tsv (docs/development/web-semantic-fidelity/f6b5/
# oracle-design.md). Ark's output is not involved.
#
# usage: check-value-fixtures.sh <typescript-package-dir>   (TypeScript 5.6.3)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
ts=$1
layer() { # dir [extensions]
  node "$here/javascript-tsc-value.js" "$ts" "$here/value-fixtures/$1" ${2:+"$2"} 2>/dev/null |
    node -e '
      const lines = require("fs").readFileSync(0, "utf8").split("\n").filter(Boolean);
      for (const l of lines) {
        const x = JSON.parse(l);
        if (!x.value && !x.value_unknown) continue;
        const v = x.value ? `${x.value.file}:${x.value.line}:${x.value.col}` : "-";
        const via = (x.value_via || []).map((d) => `${d.file}:${d.line}:${d.col}`).join(",") || "-";
        console.log([process.argv[1], `${x.file}:${x.start_line}:${x.start_col}`, x.name, v, x.value_kind || "-", x.value_rule || "-", via, x.value_unknown || "-", x.value_unknown_at || "-", x.export_unsafe ? "unsafe" : "-"].join("\t"));
      }' "$1"
}
{
  layer spec .js,.mjs,.cjs,.ts
  for c in "$here"/value-fixtures/cases/*/; do layer "cases/$(basename "$c")" .js,.ts; done
} | sort > "${TMPDIR:-/tmp}/value-fixtures.$$"
if diff -u "$here/value-fixtures/expected.tsv" "${TMPDIR:-/tmp}/value-fixtures.$$"; then
  echo "value fixtures: ok"
  rm "${TMPDIR:-/tmp}/value-fixtures.$$"
else
  echo "value fixtures: DIFFERENT (actual: ${TMPDIR:-/tmp}/value-fixtures.$$)" >&2
  exit 1
fi
