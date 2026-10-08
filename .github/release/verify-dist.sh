#!/usr/bin/env bash
# Verifies a GoReleaser dist/ directory before anything is published:
#   - every expected archive is there, matches ark_checksums.txt, and holds the
#     binary (of the right format and architecture) and the completion files;
#   - the Homebrew formula is valid Ruby, points at those archives with their
#     checksums, and installs paths the archives contain;
#   - the archive for this machine runs: smoke.py (CLI, MCP, setup) against it,
#     expecting the version recorded in dist/metadata.json.
#
# Usage: verify-dist.sh <dist-dir>
set -euo pipefail

dist=$(cd "${1:?usage: verify-dist.sh <dist-dir>}" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
fail() { echo "::error::$*"; exit 1; }

version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$dist/metadata.json")
[ -n "$version" ] || fail "no version in metadata.json"
echo "version $version"

# Checksums: every listed file verifies, every archive is listed.
if command -v sha256sum >/dev/null; then sum="sha256sum"; else sum="shasum -a 256"; fi
(cd "$dist" && $sum -c ark_checksums.txt) || fail "checksum mismatch"
for a in "$dist"/*.tar.gz; do
	grep -q "  $(basename "$a")\$" "$dist/ark_checksums.txt" || fail "$(basename "$a") is not in ark_checksums.txt"
done

# Archives: target -> expected `file` description.
targets=(
	"darwin_amd64:Mach-O 64-bit executable x86_64"
	"darwin_arm64:Mach-O 64-bit executable arm64"
	"linux_386:ELF 32-bit LSB executable, Intel 80386"
	"linux_amd64:ELF 64-bit LSB executable, x86-64"
	"linux_arm64:ELF 64-bit LSB executable, ARM aarch64"
	"windows_386:PE32 executable (console) Intel 80386"
	"windows_amd64:PE32+ executable (console) x86-64"
	"windows_arm64:PE32+ executable (console) Aarch64"
)
completions="completions/ark-completion.sh completions/bash/ark-completion.bash completions/fish/ark.fish completions/zsh/_ark"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
for t in "${targets[@]}"; do
	target=${t%%:*} want=${t#*:}
	archive="$dist/ark_${version}_${target}.tar.gz"
	[ -f "$archive" ] || fail "missing $(basename "$archive")"
	bin=ark
	[[ $target == windows_* ]] && bin=ark.exe
	listing=$(tar -tzf "$archive" | sed 's#^\./##' | sort | tr '\n' ' ')
	[ "$listing" = "$(echo "$bin $completions" | tr ' ' '\n' | sort | tr '\n' ' ')" ] || fail "$target archive holds: $listing"
	mkdir -p "$work/$target"
	tar -xzf "$archive" -C "$work/$target"
	desc=$(file -b "$work/$target/$bin")
	[[ $desc == "$want"* ]] || fail "$target binary is: $desc"
	echo "ok $target: $desc"
done

# Homebrew formula.
formula="$dist/homebrew/ark.rb"
[ -f "$formula" ] || fail "no Homebrew formula"
ruby -c "$formula" >/dev/null || fail "the formula is not valid Ruby"
grep -q "^  version \"$version\"\$" "$formula" || fail "formula version is not $version"
urls=$(grep -c '^ *url "' "$formula")
[ "$urls" -eq 4 ] || fail "formula has $urls urls, want 4 (macOS / Linux, amd64 / arm64)"
while read -r url; do
	name=$(basename "$url")
	sha=$(grep -A1 "url \"$url\"" "$formula" | sed -n 's/^ *sha256 "\(.*\)"$/\1/p')
	grep -q "^$sha  $name\$" "$dist/ark_checksums.txt" || fail "formula sha256 for $name does not match"
done < <(sed -n 's/^ *url "\(.*\)"$/\1/p' "$formula")
for p in $(sed -n 's/^ *[a-z_]*\.install "\([^"]*\)".*$/\1/p' "$formula" | sort -u); do
	[ -e "$work/darwin_arm64/$p" ] || fail "the formula installs $p, which the archive lacks"
done
grep -q 'system "#{bin}/ark", "--version"' "$formula" || fail "formula test does not run ark --version"
echo "ok Homebrew formula"

# Run this machine's binary.
host="$(go env GOOS)_$(go env GOARCH)"
[ -x "$work/$host/ark" ] || fail "no runnable archive for $host"
python3 "$here/smoke.py" "$work/$host/ark" "v$version"
