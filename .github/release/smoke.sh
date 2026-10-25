#!/usr/bin/env bash
# Runs a release binary on the system it was built for: it starts, reports
# the expected version, prints its help, parses a Go file with tree-sitter
# (ark symbol) and dumps a directory. Run from the repository root.
#
# Usage: smoke.sh <ark-binary> <version>     e.g. smoke.sh ./ark v0.0.0-SNAPSHOT
set -euo pipefail
ark=${1:?usage: smoke.sh <ark-binary> <version>}
version=${2:?usage: smoke.sh <ark-binary> <version>}
fail() { echo "::error::smoke: $*"; exit 1; }

out=$("$ark" --version | tr -d '\r')
[ "$out" = "ark version $version" ] || fail "--version: '$out', want 'ark version $version'"

help=$("$ark" --help)
grep -q '^Usage: ark ' <<<"$help" || fail "--help prints no usage"

symbols=$("$ark" symbol main.go | tr -d '\r')
grep -q 'function main ' <<<"$symbols" || fail "ark symbol main.go:
$symbols"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
"$ark" -o "$work/dump.txt" misc/completions >/dev/null
# Each file's header; the dump writes paths with the system's separator.
headers=$(tr '\134' / <"$work/dump.txt" | tr -d '\r' | grep '^=== ')
for f in ark-completion.sh bash/ark-completion.bash fish/ark.fish zsh/_ark; do
	grep -qx "=== misc/completions/$f ===" <<<"$headers" || fail "the dump of misc/completions has no file $f:
$headers"
done

echo "ok: $("$ark" --version | tr -d '\r') ($(uname -s)/$(uname -m)): --version, --help, symbol, dump"
