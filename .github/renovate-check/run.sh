#!/usr/bin/env bash
# Validates renovate.json with renovate-config-validator --strict of the
# Renovate pinned by package.json / package-lock.json: the whole dependency
# tree comes from the lockfile, with its integrity checked (`npm ci`), outside
# the repository. Nothing is resolved at run time, so a dependency published
# minutes ago (and not yet served by every registry node) is never fetched.
# Node.js must satisfy Renovate's engines (.node-version; --engine-strict).
#
# Usage: run.sh [config...]   (default: renovate.json at the repository root)
#
# Environment (optional):
#   ARK_RENOVATE_CHECK_DIR  where to install Renovate (an existing directory);
#                           default a temporary directory, removed afterwards
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)

if [ -n "${ARK_RENOVATE_CHECK_DIR:-}" ]; then
	dir=$ARK_RENOVATE_CHECK_DIR
else
	dir=$(mktemp -d)
	trap 'rm -rf "$dir"' EXIT
fi
# The real path: npm mistakes a prefix reached through a symbolic link (macOS
# /var -> /private/var) for a link to a workspace, and `npm ci` then fails.
dir=$(cd "$dir" && pwd -P)
cp "$here/package.json" "$here/package-lock.json" "$dir/"
npm ci --prefix "$dir" --ignore-scripts --engine-strict --no-audit --no-fund

expected=$(node -p "require(process.argv[1]).dependencies.renovate" "$here/package.json")
actual=$(node -p "require(process.argv[1]).version" "$dir/node_modules/renovate/package.json")
if [ "$actual" != "$expected" ]; then
	echo "::error::installed renovate@$actual, package.json pins $expected" >&2
	exit 1
fi

if [ $# -eq 0 ]; then
	set -- renovate.json
fi
cd "$root"
"$dir/node_modules/.bin/renovate-config-validator" --strict "$@"
