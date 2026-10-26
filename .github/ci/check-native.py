#!/usr/bin/env python3
"""The CI gate's check of the macOS native race shards (job native-tests),
over the reports of one run: for every shard of race-shards.txt,
native-<shard>-macos.packages.tsv (test-report.py) and
native-<shard>-macos.plan.txt (`race-shard.py all`, run by the shard on
macOS, so planned from macOS's own `go list ./...`).

  - a report and a plan exist for every shard;
  - the shards' plans are identical, and every package of the plan is in
    exactly one shard, except a split package: its two halves in two shards;
  - each shard ran exactly the packages its plan gives it, none missing and
    none other: together the shards ran `go test -race ./...` on macOS, every
    package once (a split package once per half);
  - no package failed.

A package passed from go test's cache counts as run, as it always did for
this job's `go test -race ./...`: the job's flags are unchanged (no -count).
(check-coverage.py's MUST_RUN_PACKAGES rule is the Linux jobs'; here the
cache a shard saves on its first run would make a rerun with the same
sources fail it.)

Usage: check-native.py <reports-dir>
"""
import collections
import glob
import importlib.util
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def load(name, file):
    sys.dont_write_bytecode = True  # no __pycache__ in the checkout
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def main():
    reports = sys.argv[1]
    order, _, _ = load("race_shard", "race-shard.py").parse()
    errors = []

    def one(label, suffix):
        hits = glob.glob(os.path.join(reports, "**", label + suffix), recursive=True)
        if len(hits) != 1:
            errors.append(f"{label}: {len(hits)} {suffix[1:]} files (want 1)")
            return None
        return hits[0]

    plans, ran = {}, {}
    for s in order:
        label = f"native-{s}-macos"
        plan, packages = one(label, ".plan.txt"), one(label, ".packages.tsv")
        if plan:
            with open(plan) as f:
                plans[s] = [line.split() for line in f if line.strip()]
        if packages:
            ran[s] = {}
            with open(packages) as f:
                for line in f:
                    parts = line.rstrip("\n").split("\t")
                    if len(parts) >= 2:
                        ran[s][parts[0]] = parts
    if len(plans) == len(order) and len(set(map(repr, plans.values()))) != 1:
        errors.append("the shards' plans differ: " + ", ".join(f"shard {s} {len(p)} entries" for s, p in plans.items()))

    if plans:
        plan = plans[min(plans)]
        assigned = collections.defaultdict(set)  # shard -> packages
        halves = collections.defaultdict(list)   # package -> [(shard, flag)]
        for entry in plan:
            if len(entry) not in (2, 4):
                errors.append(f"plan entry {' '.join(entry)!r}: want <shard> <package> [-run|-skip <regexp>]")
                continue
            shard, pkg = entry[0], entry[1]
            assigned[shard].add(pkg)
            halves[pkg].append((shard, entry[2] if len(entry) == 4 else None))
        if sorted(assigned) != sorted(order):
            errors.append(f"the plan's shards {sorted(assigned)}, race-shards.txt declares {sorted(order)}")
        for pkg, uses in sorted(halves.items()):
            whole = len(uses) == 1 and uses[0][1] is None
            split = sorted(str(f) for _, f in uses) == ["-run", "-skip"] and uses[0][0] != uses[1][0]
            if not (whole or split):
                errors.append(f"plan: {pkg} in {uses} (want one shard, or two halves of a split in two shards)")
        for s in order:
            if s not in ran:
                continue
            got, want = set(ran[s]), assigned.get(s, set())
            for pkg in sorted(want - got):
                errors.append(f"shard {s}: {pkg} did not run (the plan gives it to this shard)")
            for pkg in sorted(got - want):
                errors.append(f"shard {s}: {pkg} ran, but the plan does not give it to this shard")
            for pkg, parts in sorted(ran[s].items()):
                if parts[1] == "fail":
                    errors.append(f"shard {s}: {pkg} failed")
        print(f"macOS native: {len(order)} shards, {len(halves)} packages planned on macOS; "
              + ", ".join(f"shard {s}: {len(assigned.get(s, ()))} planned, {len(ran.get(s, ()))} ran" for s in order))

    if errors:
        print("\nnative check failed:")
        for e in errors:
            print("  " + e)
        sys.exit(1)
    print("native check passed")


if __name__ == "__main__":
    main()
