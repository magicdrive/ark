#!/usr/bin/env bash
# Builds one ark release binary: the build command of hephaestus.yaml, run by
# Hephaestus once per target with GOOS, GOARCH, CGO_ENABLED=0, ARK_VERSION
# ("v" + the version), ARK_TAG (the tag; empty for a snapshot) and
# HEPHAESTUS_OUTPUT (where the binary goes).
#
# The flags are those of the GoReleaser builds up to v6.7.0, which
# `go version -m` shows in every published binary:
#
#   go build -ldflags '-s -w -X "main.version=vX.Y.Z"
#     -X "github.com/magicdrive/ark/internal/cache.release=true"' ./main.go
#   CGO_ENABLED=0 (Ark is pure Go: gotreesitter needs no cgo), no -trimpath,
#   no build tags.
#
# internal/cache.release is "true" for a release build only (the index cache
# of a release is shared with other releases: internal/cache/build.go): a
# tagged build of a clean tree whose tag names HEAD. A snapshot leaves it
# empty, and a tagged build of a modified tree, or of a commit the tag does
# not name, fails instead of producing a binary that would claim to be the
# release. Nothing else sets it (release_workflow_test.go).
#
# The binary is built by the Go toolchain go.mod names (its toolchain line,
# else its go line), the one CI tests with, whichever go is on PATH: the
# Hephaestus action installs its own Go before it runs this.
set -euo pipefail

fail() { echo "build.sh: $*" >&2; exit 1; }

: "${HEPHAESTUS_OUTPUT:?build.sh runs from Hephaestus (HEPHAESTUS_OUTPUT is not set)}"
: "${GOOS:?GOOS is not set}" "${GOARCH:?GOARCH is not set}"
: "${ARK_VERSION:?ARK_VERSION is not set}"
tag="${ARK_TAG-}"

# Settings of the caller's environment that would change the binary.
unset GOFLAGS GOEXPERIMENT GOAMD64 GO386 GOARM64

[ "${CGO_ENABLED-}" = 0 ] || fail "CGO_ENABLED must be 0 (release binaries are pure Go), got '${CGO_ENABLED-}'"
case "$ARK_VERSION" in
v[0-9]*) ;;
*) fail "ARK_VERSION '$ARK_VERSION' is not v<version>" ;;
esac

toolchain=$(sed -n 's/^toolchain \(go[0-9][0-9.]*\)[[:space:]]*$/\1/p' go.mod)
gover=$(sed -n 's/^go \([0-9][0-9.]*\)[[:space:]]*$/\1/p' go.mod)
[ -n "$toolchain" ] || toolchain="go$gover"
[ "$toolchain" != go ] || fail "go.mod names no Go version"
export GOTOOLCHAIN="$toolchain"
got=$(go env GOVERSION)
[ "$got" = "$toolchain" ] || fail "go is $got, go.mod names $toolchain"

release=
if [ -n "$tag" ]; then
	[ "$ARK_VERSION" = "v${tag#v}" ] || fail "ARK_VERSION $ARK_VERSION does not match the tag $tag"
	tagged=$(git rev-parse -q --verify "refs/tags/$tag^{commit}") || fail "no tag $tag in this clone: a release is built from its tag"
	head=$(git rev-parse HEAD)
	[ "$tagged" = "$head" ] || fail "tag $tag is $tagged, HEAD is $head: a release is built from its tag"
	dirty=$(git status --porcelain)
	[ -z "$dirty" ] || fail "refusing to build release $tag from a modified tree:
$dirty"
	release=true
fi

echo "build.sh: $GOOS/$GOARCH $ARK_VERSION ($got, release=${release:-<empty>})"
exec go build \
	-ldflags "-s -w -X \"main.version=$ARK_VERSION\" -X \"github.com/magicdrive/ark/internal/cache.release=$release\"" \
	-o "$HEPHAESTUS_OUTPUT" ./main.go
