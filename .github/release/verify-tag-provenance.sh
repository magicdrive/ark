#!/usr/bin/env bash
# verify-tag-provenance.sh: fail unless the release tag's commit is in the
# history of an allowed release branch.
#
#   verify-tag-provenance.sh --ref REF --sha SHA [--remote NAME] BRANCH...
#
# REF is the pushed ref (GITHUB_REF, refs/tags/<tag>), SHA the commit the
# run builds (GITHUB_SHA) and BRANCH... the branches releases may be tagged
# on (normally only the default branch; add a maintenance branch only when
# releases are really cut from it). Branch names are exact: no patterns.
#
# Run it in a full (non-shallow) clone whose HEAD is SHA (actions/checkout
# with fetch-depth: 0). It fetches the tag and every allowed branch from the
# remote in one fetch (one consistent ref advertisement) into a private
# namespace, so neither the checkout's view of the refs nor a tag moved since
# the run started is trusted, then requires:
#
#   - the tag, peeled to its commit (annotated or lightweight), is SHA and
#     HEAD is SHA: the run builds what the tag names now;
#   - git merge-base --is-ancestor SHA <branch> succeeds for at least one
#     allowed branch. Exit status 1 means "not an ancestor"; any other
#     status is an error and fails the check (it is never read as "no").
#
# Anything it cannot establish (a shallow clone, a missing branch or tag, a
# failed fetch, a git error) fails the check: exit status 1. Usage errors
# exit 2.
set -euo pipefail

ns=refs/release-provenance

die() {
	echo "::error::release tag provenance: $*" >&2
	exit 1
}

usage() {
	echo "usage: $0 --ref refs/tags/<tag> --sha <commit> [--remote <name>] <branch>..." >&2
	exit 2
}

ref="" sha="" remote=origin
branches=()
while [ $# -gt 0 ]; do
	case "$1" in
	--ref) [ $# -ge 2 ] || usage; ref=$2; shift 2 ;;
	--sha) [ $# -ge 2 ] || usage; sha=$2; shift 2 ;;
	--remote) [ $# -ge 2 ] || usage; remote=$2; shift 2 ;;
	--) shift; branches+=("$@"); break ;;
	-*) usage ;;
	*) branches+=("$1"); shift ;;
	esac
done

# The inputs: a tag ref, a full commit SHA, exact branch names.
case "$ref" in
refs/tags/?*) tag=${ref#refs/tags/} ;;
*) die "$ref is not a tag ref (refs/tags/<tag>): only a pushed tag can be released" ;;
esac
git check-ref-format "refs/tags/$tag" || die "invalid tag name: $tag"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || die "the commit must be a full lowercase SHA-1, got '$sha'"
[ "${#branches[@]}" -gt 0 ] || usage
for b in "${branches[@]}"; do
	case "$b" in
	*[*?[]* | -*) die "allowed branch '$b' is not an exact branch name" ;;
	esac
	git check-ref-format "refs/heads/$b" || die "allowed branch '$b' is not a valid branch name"
done

# A shallow clone lacks the history that ancestry needs: never guess.
shallow=$(git rev-parse --is-shallow-repository) || die "not a git repository"
[ "$shallow" = false ] || die "the clone is shallow (is-shallow-repository: $shallow); check out with fetch-depth: 0"

head=$(git rev-parse --verify "HEAD^{commit}") || die "cannot resolve HEAD"
[ "$head" = "$sha" ] || die "HEAD is $head, not the commit being released $sha"

# Fresh copies of the tag and the allowed branches, from one fetch.
refspecs=("+refs/tags/$tag:$ns/tag")
for b in "${branches[@]}"; do
	refspecs+=("+refs/heads/$b:$ns/heads/$b")
done
git fetch --quiet --no-tags --no-write-fetch-head "$remote" "${refspecs[@]}" ||
	die "cannot fetch tag $tag and branches ${branches[*]} from $remote"

tagged=$(git rev-parse --verify --quiet "$ns/tag^{commit}") || die "tag $tag on $remote does not name a commit"
[ "$tagged" = "$sha" ] || die "tag $tag on $remote points to $tagged, but this run releases $sha (the tag moved or the run is inconsistent)"

found=""
for b in "${branches[@]}"; do
	rc=0
	git merge-base --is-ancestor "$sha" "$ns/heads/$b" || rc=$?
	case "$rc" in
	0) found=$b; break ;;
	1) echo "release tag provenance: $sha is not in the history of $b" >&2 ;;
	*) die "git merge-base --is-ancestor $sha $b failed (exit $rc)" ;;
	esac
done
[ -n "$found" ] || die "tag $tag ($sha) is not in the history of any allowed release branch (${branches[*]})"
echo "release tag provenance: tag $tag ($sha) is in the history of $found"
