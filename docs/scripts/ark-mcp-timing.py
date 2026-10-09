#!/usr/bin/env python3
"""Time Ark's MCP server on one repository (the procedure of docs/performance.md).

usage: python3 docs/scripts/ark-mcp-timing.py <ark-binary> <repository> [runs]

cold     first get_diagnostics of a server started with --no-cache
         (builds the index: parse, extract, resolve)
warm     a second get_diagnostics on the same server (index reused after
         re-fingerprinting the sources)
restore  first get_diagnostics of a server started with the persistent cache
         after one priming run (extraction read from <repo>/.ark/index;
         resolution still runs)
rss      peak resident set size of the cold server process

Writes <repo>/.ark/index (the cache) during the restore runs.
"""
import json, os, resource, shutil, statistics, subprocess, sys, time

def serve(ark, repo, *flags):
    p = subprocess.Popen([ark, "mcp-server", "--root", repo, *flags], stdin=subprocess.PIPE,
                         stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    def call(i, method, params):
        p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": i, "method": method, "params": params}) + "\n")
        p.stdin.flush()
        return json.loads(p.stdout.readline())
    call(0, "initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                           "clientInfo": {"name": "timing", "version": "1"}})
    p.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
    p.stdin.flush()
    return p, call

def timed(call, i):
    t = time.perf_counter()
    r = call(i, "tools/call", {"name": "get_diagnostics", "arguments": {"path": "."}})
    elapsed = time.perf_counter() - t
    summary = json.loads(r["result"]["content"][0]["text"])["summary"]
    return elapsed, summary

def stop(p):
    p.stdin.close()
    p.wait()

def main():
    ark, repo = sys.argv[1], os.path.abspath(sys.argv[2])
    runs = int(sys.argv[3]) if len(sys.argv) > 3 else 5
    cold, warm, restore, rss = [], [], [], []
    summary = None
    for _ in range(runs):
        before = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss
        p, call = serve(ark, repo, "--no-cache")
        c, summary = timed(call, 1)
        w, _ = timed(call, 2)
        stop(p)
        cold.append(c); warm.append(w)
        # ru_maxrss of children is the maximum over all waited-for children;
        # it is exact for this run only when it rises, so report the maximum.
        rss.append(max(before, resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss))
    cache = os.path.join(repo, ".ark")
    shutil.rmtree(cache, ignore_errors=True)
    p, call = serve(ark, repo)
    timed(call, 1)
    stop(p)
    for _ in range(runs):
        p, call = serve(ark, repo)
        r, _ = timed(call, 1)
        stop(p)
        restore.append(r)
    shutil.rmtree(cache, ignore_errors=True)
    scale = 1 if sys.platform == "darwin" else 1024  # ru_maxrss: bytes on macOS, KiB on Linux
    fmt = lambda xs, unit=1000: f"{statistics.median(xs)*unit:.0f} [{min(xs)*unit:.0f}-{max(xs)*unit:.0f}]"
    print(json.dumps({
        "repository": os.path.basename(repo), "files_indexed": summary["filesIndexed"],
        "cold_ms": fmt(cold), "warm_ms": fmt(warm, 1000), "restore_ms": fmt(restore),
        "peak_rss_mb": round(max(rss) * scale / 2**20), "runs": runs}))

main()
