#!/bin/sh
# Compares HTML parsers for Ark's HTML provider (Phase 3 of
# docs/development/web-semantic-fidelity; results in its verification.md).
#
#   docs/scripts/html-parser-eval/run.sh WORKDIR [REAL_HTML_DIR]
#
# WORKDIR receives a Go module with the pinned parsers, the corpora and the
# outputs. Corpora: the html5lib tree-construction inputs (#data of
# x/net@v0.50.0/html/testdata/webkit/*.dat), synth/ (hand-written cases;
# cx-* are Reviewer B's counterexamples: an attribute whose value contains
# "fake" is one a browser does not see — FAKE counts any verify keeps),
# and REAL_HTML_DIR's *.html (default: every .html under GOMODCACHE,
# deduplicated by content).
set -eu
here=$(cd "$(dirname "$0")" && pwd)
w=$1
real=${2:-$(go env GOMODCACHE)}
mkdir -p "$w/cmd" "$w/corpus/html5lib" "$w/corpus/real" "$w/corpus/synth" "$w/out"
cd "$w"
[ -f go.mod ] || go mod init parsers >/dev/null 2>&1
go get golang.org/x/net@v0.50.0 github.com/tdewolff/parse/v2@v2.8.16 github.com/odvcencio/gotreesitter@v0.55.1 >/dev/null 2>&1
for c in htmltok attrcheck ctxcheck tstok eoftail; do
	mkdir -p "cmd/$c"
	sed '1,2d' "$here/$c.go" >"cmd/$c/main.go" # drop //go:build ignore
	case $c in attrcheck | ctxcheck) sed '1,2d' "$here/attrlib.go" >"cmd/$c/attrlib.go" ;; esac
	go build -o "$c" "./cmd/$c"
done
cp "$here"/synth/*.html corpus/synth/
python3 - "$(go env GOMODCACHE)/golang.org/x/net@v0.50.0/html/testdata" corpus/html5lib <<'PY'
import sys, os, glob
src, dst = sys.argv[1], sys.argv[2]
n = 0
for f in sorted(glob.glob(os.path.join(src, 'webkit', '*.dat'))):
    lines = open(f, encoding='utf-8', errors='surrogateescape').read().split('\n')
    i = 0
    while i < len(lines):
        if lines[i] == '#data':
            j, buf = i + 1, []
            while j < len(lines) and not lines[j].startswith('#errors') and lines[j] not in ('#document-fragment', '#script-off', '#script-on'):
                buf.append(lines[j]); j += 1
            n += 1
            open(os.path.join(dst, '%04d.html' % n), 'w', encoding='utf-8', errors='surrogateescape').write('\n'.join(buf))
            i = j
        else:
            i += 1
print('html5lib inputs', n)
PY
find "$real" -name '*.html' -size -3000k 2>/dev/null | while read -r f; do
	h=$(shasum -a 256 "$f" | cut -c1-16)
	[ -e "corpus/real/$h.html" ] || cp "$f" "corpus/real/$h.html"
done
echo "== time over html5lib + real"
for p in xnet tdewolff; do
	/usr/bin/time -p ./htmltok $p corpus/real/*.html corpus/html5lib/*.html >/dev/null 2>out/time.$p </dev/null || true
	echo "$p: $(grep real out/time.$p)"
done
/usr/bin/time -p ./tstok corpus/real/*.html corpus/html5lib/*.html >/dev/null 2>out/time.ts </dev/null || true
echo "gotreesitter: $(grep real out/time.ts)"
for s in synth html5lib real; do
	echo "== $s ($(ls corpus/$s | wc -l | tr -d ' ') files)"
	./htmltok xnet corpus/$s/*.html >out/$s.xnet.jsonl </dev/null
	./htmltok tdewolff corpus/$s/*.html >out/$s.tdewolff.jsonl </dev/null
	./tstok corpus/$s/*.html >out/$s.ts.jsonl </dev/null
	echo "-- attribute spans (x/net + HTML5 attribute scanner over Raw)"
	./attrcheck corpus/$s/*.html </dev/null | tail -1
	echo "-- mutation: value spans one byte late (must mismatch)"
	SHIFT=1 ./attrcheck corpus/$s/*.html </dev/null | tail -1
	echo "-- bare tokenizer vs html.Parse tree, (element, attribute, value) multisets"
	./ctxcheck plain corpus/$s/*.html </dev/null | tail -1
	echo "-- attributes verified on the element their own tag produced (markers)"
	FAKE=1 ./ctxcheck verified corpus/$s/*.html </dev/null | tail -1
	echo "-- eof-in-tag"
	./eoftail corpus/$s/*.html </dev/null | tail -1
	echo "-- x/net vs tdewolff attributes"
	python3 "$here/cmp.py" out/$s.xnet.jsonl out/$s.tdewolff.jsonl 0
	echo "-- gotreesitter vs x/net"
	python3 "$here/tscount.py" $s
done
