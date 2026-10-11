#!/usr/bin/env python3
"""The CI gate's coverage check, over the test reports of one run
(test-report.py's *.tests.tsv and *.packages.tsv):

  - a report exists for unit, for every race shard of race-shards.txt and for
    every filesystem-security system;
  - the race shards share no package, and together ran exactly the packages
    and the top-level tests the unit job ran — a test dropped from the race
    run (a shard, a filter) fails the gate;
  - per system, every skipped filesystem-security test with its result, and
    the state of the binary-level case tests (TestMCPCase_, TestCLICase_).

Usage: check-coverage.py <reports-dir> <system>...
"""
import collections
import glob
import os
import re
import sys


def load(path):
    rows = []
    with open(path) as f:
        for line in f:
            parts = line.rstrip("\n").split("\t")
            if len(parts) >= 3:
                rows.append(parts)
    return rows


def main():
    reports, systems = sys.argv[1], sys.argv[2:]
    errors = []
    here = os.path.dirname(os.path.abspath(__file__))
    with open(os.path.join(here, "race-shards.txt")) as f:
        shards = re.findall(r"^\[(\d+)\]", f.read(), re.M)

    def report(label):
        hits = glob.glob(os.path.join(reports, "**", label + ".tests.tsv"), recursive=True)
        if len(hits) != 1:
            errors.append(f"{label}: {len(hits)} test reports (want 1)")
            return None
        return hits[0]

    unit = report("unit-linux")
    race = {s: report(f"race-{s}-linux") for s in shards}
    if unit and all(race.values()):
        top = lambda rows: {(r[0], r[1]) for r in rows if "/" not in r[1]}
        unit_tests = top(load(unit))
        unit_pkgs = {r[0] for r in load(unit[: -len(".tests.tsv")] + ".packages.tsv")}
        race_tests, owner = set(), {}
        for s, path in race.items():
            race_tests |= top(load(path))
            for r in load(path[: -len(".tests.tsv")] + ".packages.tsv"):
                if r[0] in owner:
                    errors.append(f"package {r[0]} ran in race shards {owner[r[0]]} and {s}")
                owner[r[0]] = s
        for p in sorted(unit_pkgs - set(owner)):
            errors.append(f"package {p} ran in unit but in no race shard")
        for p in sorted(set(owner) - unit_pkgs):
            errors.append(f"package {p} ran in a race shard but not in unit")
        for p, t in sorted(unit_tests - race_tests):
            errors.append(f"{p} {t} ran in unit but in no race shard")
        for p, t in sorted(race_tests - unit_tests):
            errors.append(f"{p} {t} ran in a race shard but not in unit")
        print(f"unit: {len(unit_pkgs)} packages, {len(unit_tests)} top-level tests; "
              f"race: {len(shards)} shards, {len(owner)} packages, {len(race_tests)} top-level tests")

    for system in systems:
        path = report(f"filesystem-security-{system}")
        if not path:
            continue
        rows = load(path)
        tally = collections.Counter(r[2] for r in rows)
        print(f"\nfilesystem-security {system}: {tally['pass']} pass, {tally['fail']} fail, {tally['skip']} skip")
        for r in rows:
            if r[2] == "skip" and "/" not in r[1]:
                print(f"  SKIP {r[0]} {r[1]}")
        case = [r for r in rows if re.match(r"Test(MCP|CLI)Case_", r[1]) and "/" not in r[1]]
        if not case:
            print("  binary-level case tests (TestMCPCase_, TestCLICase_): none ran")
        for r in case:
            print(f"  {r[2].upper():4} {r[1]}")

    if errors:
        print("\ncoverage check failed:")
        for e in errors:
            print("  " + e)
        sys.exit(1)
    print("\ncoverage check passed")


if __name__ == "__main__":
    main()
