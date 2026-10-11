#!/usr/bin/env bash
# Runs the fuzz smoke targets of one group (.github/ci/fuzz-smoke.txt), each
# for 10 seconds, without re-running the package's unit tests. The first
# failure fails the group.
#
# Usage: fuzz-smoke.sh <group>
set -euo pipefail

group=${1:?usage: fuzz-smoke.sh <group>}
dir=$(cd "$(dirname "$0")" && pwd)
ran=0
while read -r g pkg target rest; do
	case "$g" in ''|'#'*) continue ;; esac
	[ "$g" = "$group" ] || continue
	echo "::group::fuzz $target ($pkg)"
	start=$(date +%s)
	go test -run '^$' -fuzz="^${target}\$" -fuzztime=10s "$pkg"
	echo "::endgroup::"
	echo "fuzz $target ($pkg): $(( $(date +%s) - start ))s"
	ran=$((ran + 1))
done < "$dir/fuzz-smoke.txt"
[ "$ran" -gt 0 ] || { echo "fuzz-smoke.txt: no target in group $group" >&2; exit 1; }
echo "fuzz group $group: $ran targets"
