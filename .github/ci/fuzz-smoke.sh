#!/usr/bin/env bash
# Runs the fuzz smoke targets of one group (.github/ci/fuzz-smoke.txt), each
# for 10 seconds, without re-running the package's unit tests. Any failing
# target fails the group; the group still runs its other targets, so one
# failure does not hide another.
#
# Usage: fuzz-smoke.sh <group> [<diagnostics-dir>]
#
# For every failing target, <diagnostics-dir> (default: a new temporary
# directory, printed) keeps <target>.log, the
# full go test output, and <target>/, the inputs the run added to the
# package's testdata/fuzz/<target> — the crash input go test writes there —
# for the job to upload; the log and the step summary name the package, the
# target, the command, its exit status and duration. A target's log is
# written there as it runs, so a step stopped mid-target (its timeout)
# still leaves it.
set -euo pipefail

group=${1:?usage: fuzz-smoke.sh <group> [<diagnostics-dir>]}
diag=${2:-$(mktemp -d "${TMPDIR:-/tmp}/fuzz-diagnostics.XXXXXX")}
dir=$(cd "$(dirname "$0")" && pwd)
summary=${GITHUB_STEP_SUMMARY:-/dev/null}
mkdir -p "$diag"
diag=$(cd "$diag" && pwd)

echo "fuzz group $group: diagnostics in $diag"
echo "fuzz group $group: $(getconf _NPROCESSORS_ONLN 2>/dev/null || echo '?') CPUs, GOMAXPROCS=${GOMAXPROCS:-default}, $(go version)"
ran=0 failed=""
while read -r g pkg target rest; do
	case "$g" in ''|'#'*) continue ;; esac
	[ "$g" = "$group" ] || continue
	corpus="$pkg/testdata/fuzz/$target"
	log="$diag/$target.log"
	: > "$log" # there before go test starts: a log that cannot be written fails here
	before=$(ls -A "$corpus" 2>/dev/null || true)
	echo "::group::fuzz $target ($pkg)"
	start=$(date +%s)
	set +e
	go test -run '^$' -fuzz="^${target}\$" -fuzztime=10s "$pkg" </dev/null 2>&1 | tee "$log"
	rc=${PIPESTATUS[0]}
	set -e
	# go test also passes when nothing was fuzzed: -fuzz matching no fuzz
	# test ("no fuzz tests to fuzz"), or no test file built ("[no test
	# files]"). A pass counts only with the fuzzer's own progress lines.
	if [ "$rc" -eq 0 ] && ! grep -q '^fuzz: elapsed: ' "$log"; then
		echo "fuzz $target ($pkg): passed without fuzzing (no such fuzz test, or no test file built)"
		rc=1
	fi
	echo "::endgroup::"
	took=$(( $(date +%s) - start ))
	echo "fuzz $target ($pkg): exit $rc, ${took}s"
	ran=$((ran + 1))
	if [ "$rc" -eq 0 ]; then
		rm -f "$log"
		continue
	fi

	failed="$failed $target"
	# Keeping the diagnostics never stops the group: a copy that fails is a
	# warning, and the target has failed the group already.
	new=""
	while IFS= read -r f; do
		case $'\n'"$before"$'\n' in *$'\n'"$f"$'\n'*) continue ;; esac
		if mkdir -p "$diag/$target" && cp -R "$corpus/$f" "$diag/$target/"; then
			new="$new $f"
		else
			echo "::warning::fuzz $target: could not keep the input $corpus/$f"
		fi
	done < <(ls -A "$corpus" 2>/dev/null || true)
	echo "::error title=fuzz smoke::$target ($pkg) failed: exit $rc after ${took}s; new input(s) in $corpus:${new:- none}"
	{
		echo "### fuzz $target ($pkg): exit $rc after ${took}s"
		echo
		echo "Command: \`go test -run '^\$' -fuzz='^$target\$' -fuzztime=10s $pkg\`"
		echo
		if [ -n "$new" ]; then
			echo "New input(s) of $corpus (in the fuzz-diagnostics artifact):$new"
		else
			echo "No new input in $corpus (no crash recorded: not an input's failure, or the run stopped first)."
		fi
		echo
		echo '```'
		grep -E -m 20 -e '^--- FAIL' -e 'panic:' -e 'Failing input written' -e 'To re-run' -e 'go test -run=' \
			-e 'hung or terminated' -e 'signal' -e 'deadline exceeded' -e 'build failed' -e '^FAIL' "$log" | cut -c1-300 || true
		echo '```'
	} >> "$summary"
done < "$dir/fuzz-smoke.txt"
[ "$ran" -gt 0 ] || { echo "fuzz-smoke.txt: no target in group $group" >&2; exit 1; }
if [ -n "$failed" ]; then
	echo "fuzz group $group: failed:$failed (of $ran targets; diagnostics: $diag)" >&2
	exit 1
fi
echo "fuzz group $group: $ran targets"
