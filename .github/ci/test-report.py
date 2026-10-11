#!/usr/bin/env python3
"""Summarizes a `go test -json` stream for a CI log and keeps the details.

Prints the counts, the full output of every failed test (and of packages that
failed to build), every skipped test with its reason, and every KNOWN-GAP
line; writes <out>/<label>.tests.tsv (package, test, result, seconds — every
test and subtest) and <out>/<label>.packages.tsv for the CI artifact, and a
summary to $GITHUB_STEP_SUMMARY when it is set. It never decides the step's
result: the caller exits with `go test`'s status.

Usage: test-report.py <go-test.json> <label> <out-dir>
"""
import collections
import json
import os
import sys


def main():
    src, label, out = sys.argv[1:4]
    os.makedirs(out, exist_ok=True)
    tests = {}  # (pkg, test) -> (action, elapsed)
    pkgs = {}   # pkg -> (action, elapsed)
    output = collections.defaultdict(list)
    cached = set()
    stray = []
    with open(src, encoding="utf-8", errors="replace") as f:
        for line in f:
            try:
                e = json.loads(line)
            except ValueError:
                stray.append(line.rstrip("\n"))  # build errors and the like
                continue
            pkg, test, action = e.get("Package", ""), e.get("Test"), e.get("Action")
            if action == "output":
                text = e.get("Output", "")
                output[(pkg, test)].append(text)
                if test is None and "(cached)" in text:
                    cached.add(pkg)
            elif action in ("pass", "fail", "skip"):
                if test is None:
                    pkgs[pkg] = (action, e.get("Elapsed", 0.0))
                else:
                    tests[(pkg, test)] = (action, e.get("Elapsed", 0.0))

    def count(top_only, action):
        return sum(1 for (p, t), (a, _) in tests.items() if a == action and (not top_only or "/" not in t))

    failed_pkgs = sorted(p for p, (a, _) in pkgs.items() if a == "fail")
    summary = (
        f"{label}: {len(pkgs)} packages ({len(failed_pkgs)} failed, {len(cached)} cached); "
        f"top-level tests {count(True, 'pass')} pass / {count(True, 'fail')} fail / {count(True, 'skip')} skip; "
        f"with subtests {count(False, 'pass')} pass / {count(False, 'fail')} fail / {count(False, 'skip')} skip"
    )
    print(summary)

    for (p, t), (a, _) in sorted(tests.items()):
        if a == "fail" and not any(k[0] == p and k[1].startswith(t + "/") and tests[k][0] == "fail" for k in tests):
            print(f"\n--- FAIL {p} {t}")
            print("".join(output[(p, t)][-200:]), end="")
    for p in failed_pkgs:
        if not any(k[0] == p and v[0] == "fail" for k, v in tests.items()):
            print(f"\n--- FAIL {p} (no test failed: build or setup)")
            print("".join(output[(p, None)][-200:]), end="")
    if stray:
        print("\n--- output outside the JSON stream")
        print("\n".join(stray[-200:]))

    skips = [(p, t) for (p, t), (a, _) in sorted(tests.items()) if a == "skip"]
    print(f"\n{len(skips)} skipped:" if skips else "\nno skipped tests")
    for p, t in skips:
        reason = " ".join(
            l.strip() for l in output[(p, t)]
            if l.strip() and not l.startswith(("=== ", "--- SKIP")) and not l.strip().startswith("--- SKIP")
        )
        print(f"  SKIP {p} {t}: {reason or '(no reason given)'}")
    gaps = [l.strip() for lines in output.values() for l in lines if "KNOWN-GAP" in l]
    for g in gaps:
        print("  " + g)

    with open(os.path.join(out, label + ".tests.tsv"), "w") as f:
        for (p, t), (a, el) in sorted(tests.items()):
            f.write(f"{p}\t{t}\t{a}\t{el:.2f}\n")
    with open(os.path.join(out, label + ".packages.tsv"), "w") as f:
        for p, (a, el) in sorted(pkgs.items()):
            f.write(f"{p}\t{a}\t{el:.2f}\t{'cached' if p in cached else ''}\n")

    step_summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if step_summary:
        slow = sorted(((el, p, t) for (p, t), (a, el) in tests.items() if "/" not in t), reverse=True)[:10]
        with open(step_summary, "a") as f:
            f.write(f"### {label}\n\n{summary}\n\n")
            if skips:
                f.write("Skipped: " + ", ".join(f"`{t}`" for _, t in skips if "/" not in t) + "\n\n")
            f.write("| slowest tests | s |\n|---|---:|\n")
            for el, p, t in slow:
                f.write(f"| `{p.rsplit('/', 1)[-1]}` {t} | {el:.1f} |\n")
            f.write("\n")


if __name__ == "__main__":
    main()
