#!/usr/bin/env bash
# Tests of verify-tag-provenance.sh against local git repositories (no
# network): every case builds what a release run would see and checks the
# exit status and the reason.
#
#   verify-tag-provenance_test.sh [path/to/verify-tag-provenance.sh]
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
script=${1:-$here/verify-tag-provenance.sh}
script=$(cd "$(dirname "$script")" && pwd)/$(basename "$script")
[ -f "$script" ] || { echo "no script at $script" >&2; exit 2; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export HOME=$work/home GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=$work/home/.gitconfig
mkdir -p "$HOME"
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.com
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.com
git config --global init.defaultBranch main
git config --global advice.detachedHead false
git config --global protocol.file.allow always

failures=0
pass=0

# check NAME WANT(0|1|2) PATTERN DIR ARGS...: runs the script in DIR and
# requires the exit status WANT and, in its output, PATTERN (grep -E).
check() {
	local name=$1 want=$2 pattern=$3 dir=$4
	shift 4
	local out rc=0
	out=$(cd "$dir" && bash "$script" "$@" 2>&1) || rc=$?
	if [ "$rc" -ne "$want" ]; then
		echo "FAIL $name: exit $rc, want $want"; printf '    %s\n' "$out"
		failures=$((failures + 1))
	elif ! grep -Eq -- "$pattern" <<<"$out"; then
		echo "FAIL $name: output does not match /$pattern/"; printf '    %s\n' "$out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
		pass=$((pass + 1))
	fi
}

commit() { # commit DIR MESSAGE: an empty commit, prints its SHA
	git -C "$1" commit --quiet --allow-empty -m "$2"
	git -C "$1" rev-parse HEAD
}

# The upstream: main, a maintenance branch v1.x, a feature branch and a
# commit on no branch, each tagged; one annotated tag.
up=$work/upstream.git
git init --quiet --bare "$up"
src=$work/src
git clone --quiet "$up" "$src" 2>/dev/null
base=$(commit "$src" base)
git -C "$src" tag v1.0.0 "$base"
git -C "$src" branch v1.x
main1=$(commit "$src" "main 1")
git -C "$src" tag v2.0.0 "$main1"
git -C "$src" tag -a -m "annotated" v2.0.1 "$main1"
git -C "$src" switch --quiet v1.x
maint=$(commit "$src" "maintenance fix")
git -C "$src" tag v1.0.1 "$maint"
git -C "$src" switch --quiet -c feature main
feat=$(commit "$src" "feature")
git -C "$src" tag v2.1.0-feature "$feat"
git -C "$src" switch --quiet --detach main
detached=$(commit "$src" "on no branch")
git -C "$src" tag v2.2.0 "$detached"
git -C "$src" switch --quiet main
git -C "$src" push --quiet origin main v1.x feature --tags 2>/dev/null

# clone_at NAME SHA [git clone options]: a clone whose HEAD is SHA, the way
# actions/checkout leaves it (detached at the event's commit).
clone_at() {
	local d=$work/$1 sha=$2
	shift 2
	git clone --quiet "$@" "file://$up" "$d" 2>/dev/null
	git -C "$d" checkout --quiet --detach "$sha"
	echo "$d"
}

# 1. A tag on the default branch passes, lightweight and annotated.
d=$(clone_at c1 "$main1")
check "default branch, lightweight tag" 0 "is in the history of main" "$d" --ref refs/tags/v2.0.0 --sha "$main1" main
check "default branch, annotated tag (peeled)" 0 "is in the history of main" "$d" --ref refs/tags/v2.0.1 --sha "$main1" main
check "default branch, older tag" 0 "is in the history of main" "$(clone_at c1b "$base")" --ref refs/tags/v1.0.0 --sha "$base" main

# 2. A tag of a commit on no branch fails.
check "detached commit" 1 "not in the history of any allowed release branch" "$(clone_at c2 "$detached")" --ref refs/tags/v2.2.0 --sha "$detached" main v1.x

# 3. A tag on a branch that is not allowed fails, even if it exists.
d=$(clone_at c3 "$feat")
check "branch not allowed" 1 "not in the history of any allowed release branch \(main\)" "$d" --ref refs/tags/v2.1.0-feature --sha "$feat" main

# 4. A tag on an allowed maintenance branch passes; not allowed, it fails.
d=$(clone_at c4 "$maint")
check "allowed maintenance branch" 0 "is in the history of v1.x" "$d" --ref refs/tags/v1.0.1 --sha "$maint" main v1.x
check "maintenance branch not allowed" 1 "not in the history of any allowed release branch" "$d" --ref refs/tags/v1.0.1 --sha "$maint" main

# 5. A shallow clone fails before anything is decided.
d=$(clone_at c5 "$main1" --depth 1)
check "shallow clone" 1 "the clone is shallow" "$d" --ref refs/tags/v2.0.0 --sha "$main1" main

# 6. A tag that moved, or does not match the run, fails.
d=$(clone_at c6 "$main1")
check "SHA is not the tag's commit" 1 "points to $main1, but this run releases $feat" "$(clone_at c6b "$feat")" --ref refs/tags/v2.0.0 --sha "$feat" main feature
check "HEAD is not the SHA" 1 "HEAD is $main1, not the commit being released $base" "$d" --ref refs/tags/v1.0.0 --sha "$base" main
git -C "$src" tag -f v2.0.0 "$feat" >/dev/null
git -C "$src" push --quiet -f origin v2.0.0 2>/dev/null
check "tag moved after the run started" 1 "tag v2.0.0 on origin points to $feat, but this run releases $main1" "$d" --ref refs/tags/v2.0.0 --sha "$main1" main
git -C "$src" push --quiet origin --delete v2.0.1 2>/dev/null
check "tag deleted after the run started" 1 "cannot fetch tag v2.0.1" "$d" --ref refs/tags/v2.0.1 --sha "$main1" main
# The stale local copy of the tag in the clone is not trusted either.
git -C "$d" tag -f v2.0.0 "$main1" >/dev/null
check "local tag ignored, remote tag decides" 1 "points to $feat" "$d" --ref refs/tags/v2.0.0 --sha "$main1" main

# 7. git failures fail closed.
d=$(clone_at c7 "$base")
check "allowed branch missing on the remote" 1 "cannot fetch tag v1.0.0 and branches main release/9.x" "$d" --ref refs/tags/v1.0.0 --sha "$base" main release/9.x
git -C "$d" remote set-url origin "file://$work/no-such-repo.git"
check "remote unreachable" 1 "cannot fetch tag v1.0.0" "$d" --ref refs/tags/v1.0.0 --sha "$base" main
git -C "$d" remote set-url origin "file://$up"
# merge-base failing with an error (128) is an error, never "not an
# ancestor" (1) and never a pass.
mkdir -p "$work/fakebin"
real_git=$(command -v git)
cat >"$work/fakebin/git" <<EOF
#!/usr/bin/env bash
if [ "\${1:-}" = merge-base ]; then echo "fatal: simulated failure" >&2; exit 128; fi
exec "$real_git" "\$@"
EOF
chmod +x "$work/fakebin/git"
PATH=$work/fakebin:$PATH check "merge-base error (exit 128)" 1 "merge-base --is-ancestor .* failed \(exit 128\)" "$d" --ref refs/tags/v1.0.0 --sha "$base" main
cat >"$work/fakebin/git" <<EOF
#!/usr/bin/env bash
if [ "\${1:-}" = merge-base ]; then exit 1; fi
exec "$real_git" "\$@"
EOF
PATH=$work/fakebin:$PATH check "merge-base says no on every branch" 1 "not in the history of any allowed release branch" "$d" --ref refs/tags/v1.0.0 --sha "$base" main
check "not a git repository" 1 "not a git repository" "$work" --ref refs/tags/v1.0.0 --sha "$base" main

# Inputs: only a tag ref, a full SHA and exact branch names.
check "branch ref instead of a tag" 1 "is not a tag ref" "$d" --ref refs/heads/main --sha "$base" main
check "short SHA" 1 "full lowercase SHA-1" "$d" --ref refs/tags/v1.0.0 --sha "${base:0:12}" main
check "branch pattern" 1 "is not an exact branch name" "$d" --ref refs/tags/v1.0.0 --sha "$base" 'release/*'
check "no branch" 2 "usage" "$d" --ref refs/tags/v1.0.0 --sha "$base"

echo "$pass passed, $failures failed"
[ "$failures" -eq 0 ]
