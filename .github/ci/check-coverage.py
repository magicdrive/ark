#!/usr/bin/env python3
"""The CI gate's coverage check, over the test reports of one run
(test-report.py's *.tests.tsv and *.packages.tsv):

  - a report exists for unit, for every race shard of race-shards.txt and for
    every filesystem-security system;
  - every package the unit job ran ran in exactly one race shard, except a
    package race-shards.txt splits in two, which ran in exactly the two shards
    holding its halves;
  - the race shards together ran every test and subtest the unit job ran, as
    many times as it did — a test missing from the race run, or run twice
    (two shards, two halves of a split), fails the gate;
  - no test failed, and no test the unit job ran (passed) was skipped in the
    race shards: a failed, skipped or cancelled run is not coverage;
  - per system, every skipped filesystem-security test, and the state of the
    binary-level case tests (TestMCPCase_, TestCLICase_).

Usage: check-coverage.py <reports-dir> <system>...
"""
import collections
import glob
import importlib.util
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def shard_file():
    sys.dont_write_bytecode = True  # no __pycache__ in the checkout
    spec = importlib.util.spec_from_file_location("race_shard", os.path.join(HERE, "race-shard.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    order, entries, _ = mod.parse()
    splits = mod.check_splits(entries)
    # A split package's shards, by the package's ./ path as written.
    split_shards = {pkg: sorted(s for s, items in entries.items() for p, f, _ in items if p == pkg and f) for pkg in splits}
    return order, split_shards


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
    shards, split_shards = shard_file()

    def report(label):
        hits = glob.glob(os.path.join(reports, "**", label + ".tests.tsv"), recursive=True)
        if len(hits) != 1:
            errors.append(f"{label}: {len(hits)} test reports (want 1)")
            return None
        return hits[0]

    def packages(tests_tsv):
        return {r[0] for r in load(tests_tsv[: -len(".tests.tsv")] + ".packages.tsv")}

    def module_relative(pkg, module):
        if pkg == module:
            return "."
        return "./" + pkg[len(module) + 1:] if pkg.startswith(module + "/") else pkg

    unit = report("unit-linux")
    race = {s: report(f"race-{s}-linux") for s in shards}
    if unit and all(race.values()):
        unit_rows = load(unit)
        unit_tests = collections.Counter((r[0], r[1]) for r in unit_rows)
        results = collections.defaultdict(collections.Counter)  # (pkg, test) -> {(job, result): n}
        for r in unit_rows:
            results[(r[0], r[1])][("unit", r[2])] += 1
        unit_pkgs = packages(unit)
        module = min(unit_pkgs, key=len) if unit_pkgs else ""
        race_tests = collections.Counter()
        where = collections.defaultdict(set)
        for s, path in race.items():
            rows = load(path)
            race_tests.update((r[0], r[1]) for r in rows)
            for r in rows:
                results[(r[0], r[1])][("race", r[2])] += 1
            for p in packages(path):
                where[p].add(s)
        for p in sorted(unit_pkgs | set(where)):
            got = sorted(where.get(p, ()))
            want = split_shards.get(module_relative(p, module))
            if p not in unit_pkgs:
                errors.append(f"package {p} ran in race shard(s) {got} but not in unit")
            elif want is not None and got != want:
                errors.append(f"split package {p} ran in race shards {got}, want its halves' shards {want}")
            elif want is None and len(got) != 1:
                errors.append(f"package {p} ran in race shards {got or 'none'} (want exactly one)")
        for key in sorted(unit_tests.keys() | race_tests.keys()):
            u, r = unit_tests[key], race_tests[key]
            if r < u:
                errors.append(f"{key[0]} {key[1]}: ran {u}x in unit, {r}x in the race shards (missing)")
            elif r > u:
                errors.append(f"{key[0]} {key[1]}: ran {u}x in unit, {r}x in the race shards (duplicated)")
        for key, got in sorted(results.items()):
            if got[("unit", "fail")] or got[("race", "fail")]:
                errors.append(f"{key[0]} {key[1]}: failed ({got[('unit', 'fail')]}x in unit, {got[('race', 'fail')]}x in the race shards)")
            elif got[("race", "skip")] > got[("unit", "skip")]:
                errors.append(f"{key[0]} {key[1]}: skipped {got[('race', 'skip')]}x in the race shards, {got[('unit', 'skip')]}x in unit")
        top = sum(n for (p, t), n in unit_tests.items() if "/" not in t)
        print(f"unit: {len(unit_pkgs)} packages, {top} top-level tests, {sum(unit_tests.values())} with subtests; "
              f"race: {len(shards)} shards, {len(where)} packages, {sum(race_tests.values())} tests with subtests; "
              f"split packages: {', '.join(f'{p} (shards {s})' for p, s in split_shards.items()) or 'none'}")

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
