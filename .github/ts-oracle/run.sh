#!/usr/bin/env bash
# Runs Ark's TypeScript compiler differential tests against the compiler
# pinned by package.json / package-lock.json (installed with `npm ci`, outside
# the repository). Every test it names must PASS: a missing, unloadable or
# wrong-version compiler, a skip, or a mismatch fails.
#
# Environment (optional):
#   ARK_TYPEARGS_CASES  random type-argument cases (seed 1); default 1000000
#   ARK_TS_ORACLE_DIR   where to install the compiler; default a temp dir
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)

dir=${ARK_TS_ORACLE_DIR:-$(mktemp -d)}
cp "$here/package.json" "$here/package-lock.json" "$dir/"
npm ci --prefix "$dir" --ignore-scripts --no-audit --no-fund

expected=$(node -p "require(process.argv[1]).dependencies.typescript" "$here/package.json")
export ARK_TYPESCRIPT_MODULE="$dir/node_modules/typescript"
export ARK_TYPESCRIPT_VERSION="$expected"
export ARK_TS_ORACLE_REQUIRED=1
export ARK_TS_FIDELITY_ROOTS="$root/internal/golden/testdata:$root/internal/languages/typescript/testdata:$root/internal/mcp/testdata"
export ARK_TYPEARGS_CASES=${ARK_TYPEARGS_CASES:-1000000}

tests=(TestFidelity_TypeArgumentsMatchCompiler TestFidelity_TypeScriptCompiler)
pattern="^($(IFS='|'; echo "${tests[*]}"))\$"
log=$(mktemp)
(cd "$root" && go test -count=1 -v -run "$pattern" ./internal/languages/typescript/) | tee "$log"

for t in "${tests[@]}"; do
	if ! grep -q "^--- PASS: $t " "$log"; then
		echo "::error::$t did not run and pass"
		exit 1
	fi
done
echo "TypeScript compiler differential: ${tests[*]} passed against typescript@$expected"
