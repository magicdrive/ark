#!/usr/bin/env python3
"""Release smoke test: exercises a built ark binary the way a user would.

Usage: smoke.py <ark-binary> <expected-version>

  CLI    --version / --help / a bad flag / a missing directory / the text
         output with an .arkignore rule (exit codes and streams)
  MCP    a real JSON-RPC session over stdio: initialize, tools/list,
         tools/call (a symbol search, diagnostics, an unknown tool), an
         unknown method, malformed JSON, a notification, clean exit on EOF
  setup  every supported client in an isolated HOME: first run, re-run
         (idempotent), other servers kept, conflict refused without
         --force, --force replaces only Ark's entry

Every check prints PASS/FAIL; any FAIL exits 1. Standard library only, and
nothing outside the temporary directories it creates is touched.
"""

import json
import os
import select
import shutil
import subprocess
import sys
import tempfile
import time

failures = []


def check(cond, what, detail=""):
    print(("PASS " if cond else "FAIL ") + what + ("" if cond else f"  -- {detail}"))
    if not cond:
        failures.append(what)
    return cond


def run(argv, cwd, env=None, stdin=None):
    p = subprocess.run(argv, cwd=cwd, env=env, input=stdin, capture_output=True, text=True, timeout=120)
    return p.returncode, p.stdout, p.stderr


def cli(ark, version, tmp):
    proj = os.path.join(tmp, "proj")
    os.makedirs(proj)
    with open(os.path.join(proj, "a.go"), "w") as f:
        f.write("package p\n\nfunc Hello() {}\n")
    with open(os.path.join(proj, "skip.txt"), "w") as f:
        f.write("must-not-appear\n")
    with open(os.path.join(proj, ".arkignore"), "w") as f:
        f.write("skip.txt\n")

    code, out, err = run([ark, "--version"], tmp)
    check(code == 0 and out.strip() == f"ark version {version}", "cli: --version", f"exit {code}, {out!r}")
    code, out, err = run([ark, "--help"], tmp)
    check(code == 0 and out.startswith("Usage: ark"), "cli: --help", f"exit {code}")
    code, out, err = run([ark, "--no-such-flag"], tmp)
    check(code == 2 and out == "" and "flag provided but not defined" in err, "cli: bad flag fails on stderr", f"exit {code}, stdout {out!r}")
    code, out, err = run([ark, os.path.join(tmp, "no-such-dir")], tmp)
    check(code == 1 and "directory not found" in out + err, "cli: missing directory fails", f"exit {code}")
    code, out, err = run([ark, "-S", "-o", "out.txt", "proj"], tmp)
    text = open(os.path.join(tmp, "out.txt")).read() if os.path.exists(os.path.join(tmp, "out.txt")) else ""
    check(code == 0 and "func Hello() {}" in text, "cli: text output", f"exit {code}, {err!r}")
    # (.arkignore itself, a dotfile, is listed; the file it names is not)
    check(code == 0 and "must-not-appear" not in text, "cli: .arkignore applied")


class MCP:
    def __init__(self, ark, root):
        self.p = subprocess.Popen([ark, "mcp-server", "--root", root], cwd=root, stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1)

    def send(self, raw):
        self.p.stdin.write(raw + "\n")
        self.p.stdin.flush()

    def recv(self, timeout=60):
        ready, _, _ = select.select([self.p.stdout], [], [], timeout)
        if not ready:
            return None
        line = self.p.stdout.readline()
        return json.loads(line) if line else None

    def call(self, id_, method, params=None):
        msg = {"jsonrpc": "2.0", "id": id_, "method": method}
        if params is not None:
            msg["params"] = params
        self.send(json.dumps(msg))
        return self.recv()


def mcp(ark, tmp):
    root = os.path.join(tmp, "mcp")
    os.makedirs(root)
    with open(os.path.join(root, "a.go"), "w") as f:
        f.write("package p\n\nfunc Hello() { World() }\n\nfunc World() {}\n")
    s = MCP(ark, root)
    try:
        r = s.call(1, "initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                                      "clientInfo": {"name": "release-smoke", "version": "1"}})
        res = (r or {}).get("result", {})
        check(r is not None and r.get("id") == 1 and res.get("protocolVersion") and
              res.get("serverInfo", {}).get("name") and "tools" in res.get("capabilities", {}),
              "mcp: initialize", f"{r}")

        s.send(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}))
        r = s.call(2, "tools/list")
        names = {t.get("name") for t in (r or {}).get("result", {}).get("tools", [])}
        check({"find_symbol", "get_callers", "analyze_change_impact", "get_diagnostics"} <= names,
              "mcp: tools/list (and no reply to the notification)", f"{r if r is None or r.get('id') != 2 else sorted(names)}")

        r = s.call(3, "tools/call", {"name": "find_symbol", "arguments": {"pattern": "Hello", "path": "."}})
        res = (r or {}).get("result", {})
        text = "".join(c.get("text", "") for c in res.get("content", []))
        check(r is not None and r.get("id") == 3 and not res.get("isError") and "Hello" in text,
              "mcp: tools/call find_symbol", f"{r}")

        r = s.call(4, "tools/call", {"name": "get_callers", "arguments": {"path": ".", "symbol": "World"}})
        res = (r or {}).get("result", {})
        text = "".join(c.get("text", "") for c in res.get("content", []))
        check(r is not None and not res.get("isError") and "Hello" in text, "mcp: tools/call get_callers", f"{r}")

        r = s.call(5, "tools/call", {"name": "get_diagnostics", "arguments": {"path": "."}})
        res = (r or {}).get("result", {})
        body = json.loads("".join(c.get("text", "") for c in res.get("content", [])) or "{}")
        check(not res.get("isError") and body.get("total") == 0 and "summary" in body,
              "mcp: tools/call get_diagnostics", f"{r}")

        r = s.call(6, "tools/call", {"name": "no_such_tool", "arguments": {}})
        check(r is not None and r.get("id") == 6 and ("error" in r or r.get("result", {}).get("isError")),
              "mcp: unknown tool is an error", f"{r}")

        r = s.call(7, "no/such/method")
        check(r is not None and r.get("id") == 7 and r.get("error", {}).get("code") == -32601,
              "mcp: unknown method -32601", f"{r}")

        s.send("{not json")
        r = s.recv()
        check(r is not None and r.get("error", {}).get("code") == -32700, "mcp: malformed JSON -32700", f"{r}")

        s.p.stdin.close()
        try:
            code = s.p.wait(timeout=30)
        except subprocess.TimeoutExpired:
            code = None
        check(code == 0, "mcp: exits 0 on EOF", f"exit {code}")
    except (OSError, ValueError) as e:
        # The server died or answered with something that is not JSON-RPC.
        check(False, "mcp: session", f"{e!r}; server exit {s.p.poll()}")
    finally:
        try:
            s.p.stdin.close()
        except OSError:
            pass  # the pipe is already broken: the failure is recorded
        if s.p.poll() is None:
            s.p.kill()


# client: (config path relative to HOME or the project, servers key, scope)
CLIENTS = {
    "claude": (".mcp.json", "mcpServers", "project"),
    "cursor": (".cursor/mcp.json", "mcpServers", "project"),
    "cline": (".cline/mcp.json", "mcpServers", "home"),
    "copilot-vscode": (".vscode/mcp.json", "servers", "project"),
    "copilot-cli": (".github/mcp.json", "mcpServers", "project"),
}

CODEX_STUB = r'''#!/usr/bin/env python3
# Stand-in for the Codex CLI's `codex mcp list --json | add | remove`.
import json, os, sys
db = os.path.join(os.environ["HOME"], ".codex-stub.json")
servers = json.load(open(db)) if os.path.exists(db) else {}
a = sys.argv[1:]
if a[:3] == ["mcp", "list", "--json"]:
    print(json.dumps([{"name": n, "transport": {"type": "stdio", "command": s[0], "args": s[1:]}} for n, s in sorted(servers.items())]))
elif a[:2] == ["mcp", "add"] and "--" in a:
    servers[a[2]] = a[a.index("--") + 1:]
elif a[:2] == ["mcp", "remove"]:
    servers.pop(a[2], None)
else:
    sys.exit("unsupported: " + " ".join(a))
json.dump(servers, open(db, "w"))
'''


def setup(ark, tmp):
    home = os.path.join(tmp, "home")
    stubs = os.path.join(tmp, "stubs")
    os.makedirs(home)
    os.makedirs(stubs)
    with open(os.path.join(stubs, "codex"), "w") as f:
        f.write(CODEX_STUB)
    os.chmod(os.path.join(stubs, "codex"), 0o755)
    env = dict(os.environ, HOME=home, USERPROFILE=home, PATH=stubs + os.pathsep + os.environ.get("PATH", ""))

    def entry_of(cfg, key):
        return (cfg.get(key) or {}).get("ark") or {}

    for client, (rel, key, scope) in CLIENTS.items():
        proj = os.path.join(tmp, "setup-" + client)
        os.makedirs(proj)
        path = os.path.join(home if scope == "home" else proj, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            json.dump({key: {"other": {"command": "other-server", "args": []}}, "keep": True}, f)
        argv = [ark, "setup", client, "--ark-path", ark, "--root", proj]

        code, out, err = run(argv, proj, env)
        cfg = json.load(open(path))
        e = entry_of(cfg, key)
        check(code == 0 and e.get("command") == ark and "other" in cfg[key] and cfg.get("keep") is True,
              f"setup {client}: first run writes Ark, keeps other config", f"exit {code} {err.strip()[:200]} {cfg}")
        first = open(path, "rb").read()
        code, out, err = run(argv, proj, env)
        check(code == 0 and open(path, "rb").read() == first, f"setup {client}: re-run is idempotent", f"exit {code} {err.strip()[:200]}")

        cfg[key]["ark"] = {"command": "/somewhere/else/ark", "args": ["mcp-server"]}
        with open(path, "w") as f:
            json.dump(cfg, f)
        before = open(path, "rb").read()
        code, out, err = run(argv, proj, env)
        check(code != 0 and open(path, "rb").read() == before, f"setup {client}: a foreign ark entry is refused, file untouched", f"exit {code}")
        code, out, err = run(argv + ["--force"], proj, env)
        cfg = json.load(open(path))
        check(code == 0 and entry_of(cfg, key).get("command") == ark and "other" in cfg[key],
              f"setup {client}: --force replaces only Ark's entry", f"exit {code} {err.strip()[:200]}")

    # Codex: configured through its CLI (the stub), never by editing files.
    proj = os.path.join(tmp, "setup-codex")
    os.makedirs(proj)
    argv = [ark, "setup", "codex", "--ark-path", ark, "--root", proj]
    db = os.path.join(home, ".codex-stub.json")
    code, out, err = run(argv, proj, env)
    servers = json.load(open(db)) if os.path.exists(db) else {}
    check(code == 0 and servers.get("ark", [None])[0] == ark, "setup codex: registers Ark via `codex mcp add`", f"exit {code} {err.strip()[:200]} {servers}")
    code, out, err = run(argv, proj, env)
    check(code == 0 and json.load(open(db)) == servers, "setup codex: re-run is idempotent", f"exit {code} {err.strip()[:200]}")
    json.dump({"ark": ["/somewhere/else/ark", "mcp-server"]}, open(db, "w"))
    code, out, err = run(argv, proj, env)
    check(code != 0 and json.load(open(db))["ark"][0] == "/somewhere/else/ark", "setup codex: a foreign ark entry is refused", f"exit {code}")


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    ark, version = os.path.abspath(sys.argv[1]), sys.argv[2]
    tmp = tempfile.mkdtemp(prefix="ark-release-smoke-")
    try:
        cli(ark, version, tmp)
        mcp(ark, tmp)
        setup(ark, tmp)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    print(f"{len(failures)} failure(s)")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
