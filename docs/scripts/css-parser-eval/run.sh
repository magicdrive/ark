#!/bin/sh
# Compares CSS parsers for Ark's CSS provider (Phase 5 of
# docs/development/web-semantic-fidelity; results in its verification.md).
#
#   docs/scripts/css-parser-eval/run.sh WORKDIR [REAL_CSS_DIR]
#
# WORKDIR receives a Go module with the pinned parsers, the corpora and the
# outputs. Corpora: the css-parsing-tests suite (CSS Syntax Level 3,
# SimonSapin/css-parsing-tests at a pinned commit), the stylesheets of
# pinned npm packages, synth/ (hand-written cases; synth/expected.txt is
# their expected outline), and REAL_CSS_DIR's *.css (default: GOMODCACHE).
set -eu
here=$(cd "$(dirname "$0")" && pwd)
w=$1
real=${2:-$(go env GOMODCACHE)}
tests_commit=203ce36bffd617db7f118c551e32794561fb273d
mkdir -p "$w/cmd/csseval" "$w/cmd/tscss" "$w/corpus/real" "$w/corpus/pkg" "$w/npm" "$w/out"
cd "$w"
[ -f go.mod ] || go mod init csseval >/dev/null 2>&1
go get github.com/tdewolff/parse/v2@v2.8.16 github.com/odvcencio/gotreesitter@v0.55.1 >/dev/null 2>&1
for f in csseval syntax parse walk rulecmp; do sed '1,2d' "$here/$f.go" >"cmd/csseval/$f.go"; done # drop //go:build ignore
sed '1,2d' "$here/tscss.go" >cmd/tscss/main.go
go build -o csseval ./cmd/csseval
go build -o tscss ./cmd/tscss
if [ ! -d css-parsing-tests ]; then
	curl -sfL -o tests.tgz "https://codeload.github.com/SimonSapin/css-parsing-tests/tar.gz/$tests_commit" ||
		{ echo "cannot fetch css-parsing-tests" >&2; exit 1; }
	tar -xzf tests.tgz && mv "css-parsing-tests-$tests_commit" css-parsing-tests
fi
for p in bootstrap@5.3.3 bulma@1.0.2 animate.css@4.1.1 open-props@1.7.7 normalize.css@8.0.1 \
	modern-normalize@3.0.1 @picocss/pico@2.0.6 @fontsource/inter@5.1.0 tailwindcss@3.4.14 \
	@primer/css@21.5.1 water.css@2.1.1 milligram@1.4.1 tachyons@4.12.0 font-awesome@4.7.0 \
	@fortawesome/fontawesome-free@6.6.0; do
	name=${p%@*} ver=${p##*@} base=$(basename "${p%@*}")
	[ -f "npm/$base-$ver.tgz" ] || curl -sfL -o "npm/$base-$ver.tgz" "https://registry.npmjs.org/$name/-/$base-$ver.tgz" ||
		{ echo "cannot fetch $p" >&2; exit 1; }
	mkdir -p "npm/$base-$ver" && tar -xzf "npm/$base-$ver.tgz" -C "npm/$base-$ver"
done
dedupe() { # dir dest
	find "$1" -name '*.css' -size -3000k 2>/dev/null | while read -r f; do
		h=$(shasum -a 256 "$f" | cut -c1-16)
		[ -e "$2/$h.css" ] || cp "$f" "$2/$h.css"
	done
}
dedupe npm corpus/pkg
dedupe "$real" corpus/real
ls corpus/real/*.css corpus/pkg/*.css >out/files.txt
echo "== css-parsing-tests ($tests_commit)"
./csseval conform css-parsing-tests </dev/null
echo "== synth: outline against synth/expected.txt"
(cd "$here/synth" && "$w/csseval" outline *.css </dev/null) >out/synth.txt
if diff -u "$here/synth/expected.txt" out/synth.txt; then echo "synth: as expected"; fi
echo "== token boundaries, spec vs tdewolff ($(wc -l <out/files.txt | tr -d ' ') files)"
xargs ./csseval tokens <out/files.txt >out/tokens.txt
tail -1 out/tokens.txt
echo "== structure, spec vs gotreesitter"
xargs ./csseval structure <out/files.txt >out/spec.tsv
xargs ./tscss <out/files.txt >out/ts.tsv
python3 - out/spec.tsv out/ts.tsv <<'PY'
import sys, os
a = {l.split('\t')[0]: l.rstrip().split('\t')[1:] for l in open(sys.argv[1])}
b = {l.split('\t')[0]: l.rstrip().split('\t')[1:] for l in open(sys.argv[2])}
err = sum(1 for v in b.values() if v[3] == 'true')
short = [f for f, v in b.items() if int(v[2]) != os.path.getsize(f)]
fewer = [f for f in a if f in b and int(b[f][1]) < int(a[f][1])]
silent = [f for f in fewer if b[f][3] != 'true']
print(f'files={len(a)} gotreesitter_error={err} short={len(short)} fewer_declarations={len(fewer)} fewer_without_error={len(silent)}')
print('declarations spec', sum(int(v[1]) for v in a.values()), 'gotreesitter', sum(int(v[1]) for v in b.values()),
      '| blocks spec', sum(int(v[0]) for v in a.values()), 'gotreesitter', sum(int(v[0]) for v in b.values()))
PY
echo "== problems the outline reports over the stylesheets (by kind)"
xargs ./csseval outline <out/files.txt >out/outline.txt
grep -o '^ *![a-z-]*' out/outline.txt | sed 's/^ *//' | sort | uniq -c | sort -rn
echo "== time over the stylesheets"
for c in "csseval structure" "tscss"; do
	/usr/bin/time -p sh -c "xargs ./$c <out/files.txt >/dev/null" 2>out/time.txt </dev/null || true
	echo "$c: $(grep real out/time.txt)"
done
echo "== worst cases (time, rules and declarations; each must end in seconds, not crash)"
mkdir -p worst
python3 - worst <<'PY'
import sys
d = sys.argv[1]
cases = {
    "media32000": "@media x{" + "div{color:red}\n" * 32000 + "}",
    "nest4000": "a{" * 4000,
    "declnest4000": ".x{" + "a:b{" * 4000,
    "custom4000": ".x{" + "--a:{" * 4000,
    "paren1m": ".x{a:" + "(" * 1000000,
    "brace1m": "{" * 1000000,
    "blockvalue": ".x{" + "a:{" * 250 + "b " * 400000 + "}x" * 250 + "}",
    "blockvalue_rules": ".x{" + "a:{" * 200 + "p{c:d}" * 20000 + "} b" * 200 + "}",
}
for k, v in cases.items():
    open(f"{d}/{k}.css", "w").write(v)
PY
for f in worst/*.css; do
	/usr/bin/time -p ./csseval structure "$f" >out/worst.txt 2>out/time.txt </dev/null || echo "FAILED $f"
	echo "$(basename "$f") $(cut -f2,3 out/worst.txt | tr '\t' ' ') $(grep real out/time.txt)"
done
