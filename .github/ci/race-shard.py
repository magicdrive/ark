#!/usr/bin/env python3
"""Prints the packages of one race shard (.github/ci/race-shards.txt), one
import path per line; with "all", every package as "<shard> <package>".

Fails if a package is named twice or does not exist, or if no shard (or more
than one) holds "*", the shard of every package no other shard names — so a
package of `go list ./...` can never be left out.

Usage: race-shard.py <shard>|all
"""
import os
import re
import subprocess
import sys


def fail(msg):
    sys.exit("race-shards.txt: " + msg)


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: race-shard.py <shard>|all")
    want = sys.argv[1]
    module = subprocess.run(["go", "list", "-m"], check=True, capture_output=True, text=True).stdout.strip()
    packages = subprocess.run(["go", "list", "./..."], check=True, capture_output=True, text=True).stdout.split()
    exists = set(packages)

    owner, rest, shard = {}, None, None
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "race-shards.txt")
    with open(path) as f:
        for raw in f:
            line = raw.split("#", 1)[0].strip()
            if not line:
                continue
            m = re.fullmatch(r"\[(\d+)\]", line)
            if m:
                shard = m.group(1)
                continue
            if shard is None:
                fail("a package before any [shard]")
            if line == "*":
                if rest is not None:
                    fail('"*" in shards %s and %s' % (rest, shard))
                rest = shard
                continue
            pkg = module + "/" + line[2:] if line.startswith("./") else line
            if pkg not in exists:
                fail("no package " + line)
            if pkg in owner:
                fail("%s in shards %s and %s" % (line, owner[pkg], shard))
            owner[pkg] = shard
    if rest is None:
        fail('no shard holds "*"')

    for pkg in packages:
        s = owner.get(pkg, rest)
        if want == "all":
            print(s, pkg)
        elif s == want:
            print(pkg)


if __name__ == "__main__":
    main()
