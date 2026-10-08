#!/usr/bin/env bash
# Verifies a GoReleaser dist/ directory before anything is published: the
# archive of every shipped target is there, and every archive matches
# ark_checksums.txt (and is listed in it).
#
# Usage: verify-dist.sh <dist-dir>
set -euo pipefail

dist=$(cd "${1:?usage: verify-dist.sh <dist-dir>}" && pwd)
fail() { echo "::error::$*"; exit 1; }

version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$dist/metadata.json")
[ -n "$version" ] || fail "no version in metadata.json"
echo "version $version"

for target in darwin_amd64 darwin_arm64 linux_386 linux_amd64 linux_arm64 windows_386 windows_amd64 windows_arm64; do
	[ -f "$dist/ark_${version}_${target}.tar.gz" ] || fail "missing ark_${version}_${target}.tar.gz"
done

if command -v sha256sum >/dev/null; then sum="sha256sum"; else sum="shasum -a 256"; fi
(cd "$dist" && $sum -c ark_checksums.txt) || fail "checksum mismatch"
for a in "$dist"/*.tar.gz; do
	grep -q "  $(basename "$a")\$" "$dist/ark_checksums.txt" || fail "$(basename "$a") is not in ark_checksums.txt"
done
echo "ok: 8 archives, checksums verified"
