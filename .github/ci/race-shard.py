#!/usr/bin/env python3
"""The race shards of .github/ci/race-shards.txt.

  race-shard.py list <shard>        the shard's entries, in start order:
                                    "<package>" or "<package> -run|-skip <regexp>"
  race-shard.py all                 every entry as "<shard> <entry>"
  race-shard.py run <shard> <json>  runs the shard: its whole packages in one
                                    `go test -race -json` (in the order listed,
                                    with go test's usual parallelism) and each
                                    half of a split package in a `go test` of its
                                    own, all at once; their JSON streams go to
                                    <json> in that order. Exits non-zero if any
                                    of them does.

The file is checked first: every package of `go list ./...` runs in exactly
one shard, except a package split in two — once with `-run <re>` and once
with `-skip <re>`, the same non-empty <re>, in two different shards, so that
every one of its tests runs exactly once (a top-level regexp: no "/", so
a subtest runs with its parent); no shard is empty; every named
package exists. Any violation fails.
"""
import os
import re
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
SHARDS_FILE = os.path.join(HERE, "race-shards.txt")
COMMENT = re.compile(r"(^|\s)#.*$")


class ShardError(Exception):
    pass


def parse(path=SHARDS_FILE):
    """Returns ([shard], {shard: [(pkg, flag, regexp)]}, rest_shard) as written,
    with pkg as written ("./x", "." or "*"); checks only the file's shape."""
    order, entries, rest, shard = [], {}, None, None
    with open(path) as f:
        for n, raw in enumerate(f, 1):
            line = COMMENT.sub("", raw).strip()
            if not line:
                continue
            m = re.fullmatch(r"\[(\d+)\]", line)
            if m:
                shard = m.group(1)
                if shard in entries:
                    raise ShardError(f"line {n}: shard {shard} declared twice")
                order.append(shard)
                entries[shard] = []
                continue
            if shard is None:
                raise ShardError(f"line {n}: an entry before any [shard]")
            fields = line.split()
            if fields == ["*"]:
                if rest is not None:
                    raise ShardError(f'line {n}: "*" in shards {rest} and {shard}')
                rest = shard
                continue
            if len(fields) == 1:
                entries[shard].append((fields[0], None, None))
            elif len(fields) == 3 and fields[1] in ("-run", "-skip") and fields[2]:
                entries[shard].append((fields[0], fields[1], fields[2]))
            else:
                raise ShardError(f"line {n}: want <package>, <package> -run|-skip <regexp> or *: {line!r}")
    if rest is None:
        raise ShardError('no shard holds "*"')
    return order, entries, rest


def check_splits(entries):
    """Returns {pkg: regexp} of the split packages; raises unless every split
    package has exactly one -run and one -skip half, the same regexp, in two
    different shards, and no other package is named twice."""
    seen = {}
    for shard, items in entries.items():
        for pkg, flag, rx in items:
            seen.setdefault(pkg, []).append((shard, flag, rx))
    splits = {}
    for pkg, uses in seen.items():
        flags = sorted(str(f) for _, f, _ in uses)
        if len(uses) == 1 and uses[0][1] is None:
            continue
        if flags != ["-run", "-skip"]:
            where = ", ".join("shard %s %s" % (s, f or "whole") for s, f, _ in uses)
            raise ShardError(f"{pkg}: named {len(uses)} times ({where});"
                             " only a split in two (-run <re> and -skip <re>) may name a package twice")
        if uses[0][2] != uses[1][2]:
            raise ShardError(f"{pkg}: -run and -skip regexps differ ({uses[0][2]!r}, {uses[1][2]!r})")
        if "/" in uses[0][2]:
            raise ShardError(f"{pkg}: the split regexp {uses[0][2]!r} names subtests; split by top-level tests only")
        if uses[0][0] == uses[1][0]:
            raise ShardError(f"{pkg}: both halves in shard {uses[0][0]}")
        splits[pkg] = uses[0][2]
    return splits


def plan():
    """Returns ([shard], {shard: [(import_path, flag, regexp)]}) for the
    repository: the file checked against `go list ./...`, "*" expanded."""
    order, entries, rest = parse()
    check_splits(entries)
    module = subprocess.run(["go", "list", "-m"], check=True, capture_output=True, text=True).stdout.strip()
    packages = subprocess.run(["go", "list", "./..."], check=True, capture_output=True, text=True).stdout.split()
    exists = set(packages)

    def path(pkg):
        if pkg == ".":
            return module
        return module + "/" + pkg[2:] if pkg.startswith("./") else pkg

    out, named = {}, set()
    for shard in order:
        out[shard] = []
        for pkg, flag, rx in entries[shard]:
            p = path(pkg)
            if p not in exists:
                raise ShardError(f"no package {pkg}")
            out[shard].append((p, flag, rx))
            named.add(p)
    out[rest] += [(p, None, None) for p in packages if p not in named]
    for shard in order:
        if not out[shard]:
            raise ShardError(f"shard {shard} runs nothing")
    return order, out


def entry(p, flag, rx):
    return p if flag is None else f"{p} {flag} {rx}"


def run(shard, dest, shards):
    if shard not in shards:
        raise ShardError(f"no shard {shard}")
    items = shards[shard]
    whole = [p for p, flag, _ in items if flag is None]
    commands = []
    if whole:
        commands.append(["go", "test", "-race", "-json"] + whole)
    for p, flag, rx in items:
        if flag is not None:
            commands.append(["go", "test", "-race", "-json", flag, rx, p])
    tmp = tempfile.mkdtemp(prefix="race-shard-")
    procs = []
    start = time.time()
    for i, cmd in enumerate(commands):
        print("$ " + " ".join(cmd if len(cmd) < 12 else cmd[:6] + [f"... ({len(cmd) - 4} packages)"]), flush=True)
        out = open(os.path.join(tmp, f"{i}.json"), "w")
        procs.append((cmd, out, subprocess.Popen(cmd, stdout=out)))
    failed = 0
    for i, (cmd, out, proc) in enumerate(procs):
        code = proc.wait()
        out.close()
        print(f"command {i + 1}/{len(procs)} exited {code} after {time.time() - start:.0f}s", flush=True)
        failed += code != 0
    with open(dest, "w") as merged:
        for i in range(len(procs)):
            with open(os.path.join(tmp, f"{i}.json")) as part:
                merged.write(part.read())
    return 1 if failed else 0


def main():
    args = sys.argv[1:]
    try:
        if args[:1] == ["all"] and len(args) == 1:
            order, shards = plan()
            for s in order:
                for item in shards[s]:
                    print(s, entry(*item))
        elif args[:1] == ["list"] and len(args) == 2:
            _, shards = plan()
            if args[1] not in shards:
                raise ShardError(f"no shard {args[1]}")
            for item in shards[args[1]]:
                print(entry(*item))
        elif args[:1] == ["run"] and len(args) == 3:
            _, shards = plan()
            sys.exit(run(args[1], args[2], shards))
        else:
            sys.exit("usage: race-shard.py list <shard> | all | run <shard> <json>")
    except ShardError as e:
        sys.exit("race-shards.txt: " + str(e))


if __name__ == "__main__":
    main()
