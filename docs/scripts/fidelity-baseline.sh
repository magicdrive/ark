#!/bin/sh
# Measure Ark's reference resolution over one corpus, per language, with
# TestFidelityBaseline of internal/index (opt-in, not in CI). See
# docs/research/multilang-fidelity/evaluation.md.
#
# usage: sh docs/scripts/fidelity-baseline.sh <corpus-dir> <out-dir> [<langs>] [<oracle.jsonl>]
#
#   <corpus-dir>    the corpus (a fixed, unpacked archive: evaluation.md §8)
#   <out-dir>       a directory for the results (created)
#   <langs>         languages to measure, comma-separated ("" = all)
#   <oracle.jsonl>  an oracle's sites to score Ark against (optional)
#
# Writes <out-dir>/report.txt (the test's log), report.json (the counts) and
# report.sites (one line per reference: language, span, kind, name,
# confidence, outside, first evidence, unique target) and, with an oracle,
# report.false (every false Strong or Exact it shows) and report.scored (every
# oracle site, keyed by the oracle's own site, with its class and Ark's
# verdict). Series 1 is the sites representable in the report.scored of both
# runs being compared, joined on that key; Series 2 the sites representable in
# the later run only (evaluation.md §6). Two runs over one corpus and one Ark
# give the same report.sites. Nothing is fetched or built; the corpus is only
# read.
set -eu

[ $# -ge 2 ] || { echo "usage: $0 <corpus-dir> <out-dir> [<langs>] [<oracle.jsonl>]" >&2; exit 2; }
corpus=$(cd "$1" && pwd)
mkdir -p "$2"
out=$(cd "$2" && pwd)
langs=${3:-}
oracle=${4:-}
if [ -n "$oracle" ]; then
	oracle=$(cd "$(dirname "$oracle")" && pwd)/$(basename "$oracle")
fi
here=$(cd "$(dirname "$0")/../.." && pwd)

start=$(date +%s)
(cd "$here/internal/index" &&
	ARK_FIDELITY_ROOT="$corpus" ARK_FIDELITY_LANG="$langs" ARK_FIDELITY_ORACLE="$oracle" \
		ARK_FIDELITY_JSON="$out/report.json" ARK_FIDELITY_SITES="$out/report.sites" ARK_FIDELITY_FALSE="$out/report.false" ARK_FIDELITY_SCORED="$out/report.scored" \
		go test -count=1 -run 'TestFidelityBaseline$' -v . >"$out/report.txt" 2>&1)
echo "elapsed $(($(date +%s) - start))s" >>"$out/report.txt"
grep -E '^ +fidelity_baseline_test.go:[0-9]+: ' "$out/report.txt" | sed 's/^ *fidelity_baseline_test.go:[0-9]*: //'
