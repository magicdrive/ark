#!/bin/sh
# Measure Go reference resolution against the go/types oracles of
# internal/languages/golang (TestGoRefOracleMeasure; opt-in, not in CI).
#
# usage: sh docs/scripts/go-oracle.sh <out-dir> [<ark-checkout>]
#
#   <out-dir>       a directory for the results (created)
#   <ark-checkout>  an Ark source tree to measure as a corpus too (optional;
#                   e.g. a `git archive` of a fixed commit)
#
# The corpus is fixed: the module versions below, taken from the module
# cache (`go mod download` fetches a missing one — the only network access).
# The oracle type-checks every package of a corpus with go/types from source
# (go/parser, go/types, the standard library's source importer, and the
# corpus's go.mod requirements read from the module cache), under the host's
# build constraints; nothing of a corpus is built or run. What it cannot
# judge is reported, never counted as Ark's error nor as its success: an
# identifier without an object (a dependency absent from the cache, a type
# error), a file the host's build constraints exclude, a repository
# declaration Ark has no symbol for (a field, a local).
#
# Per corpus: <out-dir>/<name>.txt (the report), .json (the counts: by role,
# by method tag — a call, a method value or expression, promoted, generic,
# on an interface, ... — and by where a method's receiver's type comes
# from), .sites (one line per representable site: key, role, target,
# ok|missed, method tags, receiver origin — two runs compare site by site).
set -eu

out=$1
ark=${2:-}
mkdir -p "$out"
here=$(cd "$(dirname "$0")/../.." && pwd)
cache=$(go env GOMODCACHE)

corpora="
xtools golang.org/x/tools@v0.36.0
xexp golang.org/x/exp@v0.0.0-20250210185358-939b2ce775ac
cobra github.com/spf13/cobra@v1.9.1
gocmp github.com/google/go-cmp@v0.6.0
"

measure() { # name dir
	(cd "$here/internal/languages/golang" &&
		ARK_GO_ORACLE_MODCACHE="$cache" ARK_GO_ORACLE_ROOT="$2" ARK_GO_ORACLE_NAME="$1" \
			ARK_GO_ORACLE_JSON="$out/$1.json" ARK_GO_ORACLE_SITES="$out/$1.sites" \
			go test -count=1 -run 'TestGoRefOracleMeasure$' -v . >"$out/$1.txt" 2>&1)
	grep -m1 'TOTAL' "$out/$1.txt" | sed "s/^ */$1: /"
}

echo "$corpora" | while read -r name module; do
	[ -n "$name" ] || continue
	dir=$(go mod download -json "$module" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')
	measure "$name" "$dir"
done
if [ -n "$ark" ]; then
	measure ark "$ark"
fi
