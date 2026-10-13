#!/usr/bin/env python3
"""The CI gate's test reports, one per job: the latest attempt of each.

  select-reports.py <downloaded-dir> <out-dir>

Every test job uploads its report as test-report-<job>-attempt-<n>, n its
run's attempt (github.run_attempt). A run re-run reruns some of its jobs: a
rerun job uploads under the new attempt, a job that is not rerun keeps the
artifact of the attempt that ran it, and the earlier attempts' artifacts stay
in the run. download-artifact, given a pattern, downloads each artifact into
a directory named after it; this copies, for every <job>, the directory of
its highest attempt to <out-dir>/test-report-<job>, so that the coverage
check reads the report of the attempt whose result the gate judges. (An
artifact name alone cannot tell: download-artifact keeps, of several
artifacts of one name, the one of the highest artifact id, and ids do not
follow attempts.)

A directory whose name has no -attempt-<n> suffix, or a file outside any
artifact's directory, fails: the gate never guesses which report counts.
"""
import os
import re
import shutil
import sys

NAME = re.compile(r"^(test-report-.+)-attempt-([1-9][0-9]*)$")


def select(src, dest):
    """Returns {job: attempt} chosen; raises ValueError on a malformed set."""
    latest = {}
    for entry in sorted(os.listdir(src)):
        if not os.path.isdir(os.path.join(src, entry)):
            # download-artifact unpacks a lone artifact without its
            # directory: no name, no attempt — not a set to choose from.
            raise ValueError(f"{entry}: a file, not an artifact's directory")
        m = NAME.match(entry)
        if not m:
            raise ValueError(f"{entry}: not a test-report-<job>-attempt-<n> artifact")
        job, attempt = m.group(1), int(m.group(2))
        if job not in latest or attempt > latest[job][0]:
            latest[job] = (attempt, entry)
    os.makedirs(dest, exist_ok=True)
    for job, (attempt, entry) in sorted(latest.items()):
        shutil.copytree(os.path.join(src, entry), os.path.join(dest, job))
    return {job: attempt for job, (attempt, _) in latest.items()}


def main():
    if len(sys.argv) != 3:
        sys.exit("usage: select-reports.py <downloaded-dir> <out-dir>")
    try:
        chosen = select(sys.argv[1], sys.argv[2])
    except (ValueError, OSError) as e:
        # No download (no artifact matched), an unreadable or existing
        # directory: fail with the reason, never select from less.
        sys.exit("select-reports: " + str(e))
    for job, attempt in sorted(chosen.items()):
        print(f"{job}: attempt {attempt}")


if __name__ == "__main__":
    main()
