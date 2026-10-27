#!/usr/bin/env python3
"""Failure-injection tests for the pinned install of make renovate-check.

The Renovate of `make renovate-check` (run.sh) comes from package-lock.json
with `npm ci`. These tests point npm at a fake registry on 127.0.0.1 and show
that:

  * the lockfile, not the registry's newest release, decides what is
    installed (a newer version published after the lock is not taken);
  * a version that is not in the lockfile is never installed: `npm ci` fails;
  * a registry answering 404 fails the install, and run.sh, visibly;
  * a tarball whose bytes do not match the lockfile's integrity fails;
  * a Node.js outside Renovate's engines fails (--engine-strict).

No test reaches the real registry: every npm process gets the fake registry,
a private cache and an empty user config.

Run: python3 .github/renovate-check/test_run.py   (or make renovate-check-test)
Needs: Node.js and npm.
"""

import base64
import hashlib
import io
import json
import os
import re
import shutil
import subprocess
import tarfile
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))


def tarball(name, version):
    """An npm package tarball (package/package.json, package/index.js)."""
    files = {
        "package/package.json": json.dumps({"name": name, "version": version, "main": "index.js"}).encode(),
        "package/index.js": f"module.exports = {json.dumps(version)};\n".encode(),
    }
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for path, data in files.items():
            info = tarfile.TarInfo(path)
            info.size = len(data)
            info.mtime = 0
            tar.addfile(info, io.BytesIO(data))
    return buf.getvalue()


def satisfies(version, rng):
    """Whether version (a tuple) satisfies an npm engines range: the forms the
    lockfile uses (||, space-joined comparators, ^, ~, >=, >, <, <=, =, x)."""
    def parse(v):
        parts = [p for p in v.lstrip("v=").split(".") if p not in ("x", "*", "X")]
        return tuple(int(p) for p in parts)

    def comparator(c):
        for op in (">=", "<=", ">", "<", "^", "~", "="):
            if c.startswith(op):
                want = parse(c[len(op):])
                break
        else:
            op, want = "=", parse(c)
        full = want + (0,) * (3 - len(want))
        if op == ">=":
            return version >= full
        if op == ">":
            return version > full
        if op == "<=":
            return version <= full
        if op == "<":
            return version < full
        if op == "^":
            nonzero = next((i for i, x in enumerate(want) if x), len(want) - 1)
            return version >= full and version[:nonzero + 1] == want[:nonzero + 1]
        if op == "~":
            n = 2 if len(want) > 1 else 1
            return version >= full and version[:n] == want[:n]
        return version[:len(want)] == want

    rng = re.sub(r"(>=|<=|>|<|=|\^|~)\s+", r"\1", rng)
    if rng.strip() in ("", "*"):
        return True
    return any(all(comparator(c) for c in alt.split()) for alt in rng.split("||"))


class FakeRegistry:
    """A minimal npm registry: packuments and tarballs, with injectable faults."""

    def __init__(self):
        self.packages = {}  # name -> {version: tarball bytes}
        self.tarball_override = {}  # (name, version) -> 404 or replacement bytes
        self.requests = []
        registry = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                registry.requests.append(self.path)
                status, body, ctype = registry.serve(self.path)
                self.send_response(status)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}/"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()

    def publish(self, name, version):
        self.packages.setdefault(name, {})[version] = tarball(name, version)

    def serve(self, path):
        path = path.split("?", 1)[0].lstrip("/")
        not_found = (404, b'{"error":"Not found"}', "application/json")
        if "/-/" in path:
            name, file = path.split("/-/", 1)
            for version, data in self.packages.get(name, {}).items():
                if file == f"{name}-{version}.tgz":
                    fault = self.tarball_override.get((name, version))
                    if fault == 404:
                        return not_found
                    return 200, fault if fault is not None else data, "application/octet-stream"
            return not_found
        versions = self.packages.get(path)
        if not versions:
            return not_found
        doc = {"name": path, "dist-tags": {"latest": list(versions)[-1]}, "versions": {}}
        for version, data in versions.items():
            doc["versions"][version] = {
                "name": path,
                "version": version,
                "dist": {
                    "tarball": f"{self.url}{path}/-/{path}-{version}.tgz",
                    "shasum": hashlib.sha1(data).hexdigest(),
                    "integrity": "sha512-" + base64.b64encode(hashlib.sha512(data).digest()).decode(),
                },
            }
        return 200, json.dumps(doc).encode(), "application/json"


class NpmTestCase(unittest.TestCase):
    def setUp(self):
        self.registry = FakeRegistry()
        self.addCleanup(self.registry.close)
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp, True)
        userconfig = os.path.join(self.tmp, "npmrc")
        open(userconfig, "w").close()
        self.env = dict(os.environ)
        for key in list(self.env):
            if key.lower().startswith("npm_config_"):
                del self.env[key]
        self.env.update({
            "npm_config_registry": self.registry.url,
            "npm_config_cache": os.path.join(self.tmp, "cache"),
            "npm_config_userconfig": userconfig,
            "npm_config_fetch_retries": "0",
            "npm_config_audit": "false",
            "npm_config_fund": "false",
            "npm_config_update_notifier": "false",
        })

    def npm(self, cwd, *args, env=None):
        return subprocess.run(["npm", *args], cwd=cwd, env=env or self.env,
                              capture_output=True, text=True, timeout=300)

    def project(self, spec):
        d = tempfile.mkdtemp(dir=self.tmp)
        self.write_package(d, spec)
        return d

    @staticmethod
    def write_package(d, spec):
        with open(os.path.join(d, "package.json"), "w") as f:
            json.dump({"name": "demo-project", "private": True, "dependencies": {"demo": spec}}, f)

    @staticmethod
    def installed(d):
        p = os.path.join(d, "node_modules", "demo", "package.json")
        if not os.path.exists(p):
            return None
        with open(p) as f:
            return json.load(f)["version"]

    def locked(self, spec="^1.0.0"):
        """A project whose lockfile pins demo@1.0.0, the only version published when it was made."""
        self.registry.publish("demo", "1.0.0")
        d = self.project(spec)
        r = self.npm(d, "install", "--package-lock-only", "--ignore-scripts")
        self.assertEqual(r.returncode, 0, r.stderr)
        return d

    def assertFailed(self, r, *needles):
        out = r.stdout + r.stderr
        self.assertNotEqual(r.returncode, 0, "the failure was hidden (exit 0):\n" + out)
        for needle in needles:
            self.assertIn(needle, out)


class LockfileInstall(NpmTestCase):
    def test_lockfile_decides_not_the_newest_release(self):
        d = self.locked()
        self.registry.publish("demo", "1.0.1")  # newer, and "latest", after the lock
        r = self.npm(d, "ci", "--ignore-scripts")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.installed(d), "1.0.0")
        self.assertNotIn("/demo/-/demo-1.0.1.tgz", self.registry.requests)
        # What the old `npx --package` did: resolve the range at run time, so
        # whatever was published last (minutes ago, maybe) is fetched.
        floating = self.project("^1.0.0")
        r = self.npm(floating, "install", "--ignore-scripts", "--no-package-lock")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.installed(floating), "1.0.1")

    def test_version_not_in_lockfile_is_not_installed(self):
        d = self.locked()
        self.registry.publish("demo", "1.0.1")
        self.write_package(d, "1.0.1")  # package.json asks for what the lockfile lacks
        r = self.npm(d, "ci", "--ignore-scripts")
        self.assertFailed(r, "EUSAGE")
        self.assertIsNone(self.installed(d))
        self.assertNotIn("/demo/-/demo-1.0.1.tgz", self.registry.requests)

    def test_registry_404_fails_the_install(self):
        d = self.locked()
        self.registry.tarball_override[("demo", "1.0.0")] = 404
        r = self.npm(d, "ci", "--ignore-scripts")
        self.assertFailed(r, "E404")
        self.assertIsNone(self.installed(d))

    def test_integrity_mismatch_fails_the_install(self):
        d = self.locked()
        self.registry.tarball_override[("demo", "1.0.0")] = tarball("demo", "6.6.6")
        r = self.npm(d, "ci", "--ignore-scripts")
        self.assertFailed(r, "EINTEGRITY")
        self.assertIsNone(self.installed(d))


class RunScript(NpmTestCase):
    """run.sh itself, on a copy of this directory and renovate.json."""

    def copy(self):
        root = tempfile.mkdtemp(dir=self.tmp)
        dst = os.path.join(root, ".github", "renovate-check")
        os.makedirs(dst)
        for name in ("run.sh", "package.json", "package-lock.json", ".node-version"):
            shutil.copy2(os.path.join(HERE, name), dst)
        shutil.copy2(os.path.join(HERE, "..", "..", "renovate.json"), root)
        return root, dst

    def run_sh(self, root, env=None):
        return subprocess.run([os.path.join(root, ".github", "renovate-check", "run.sh")], cwd=root,
                              env=env or self.env, capture_output=True, text=True, timeout=300)

    def test_404_from_the_registry_fails_run_sh(self):
        root, _ = self.copy()
        r = self.run_sh(root)  # the fake registry has no package: every fetch is a 404
        self.assertFailed(r, "E404")
        self.assertNotIn("Config validated", r.stdout + r.stderr)
        # The lockfile's registry.npmjs.org URLs went to the fake registry.
        self.assertTrue(any(p.endswith(".tgz") for p in self.registry.requests), self.registry.requests)

    def test_renovate_not_in_the_lockfile_fails_run_sh(self):
        root, dst = self.copy()
        pkg = os.path.join(dst, "package.json")
        with open(pkg) as f:
            doc = json.load(f)
        doc["dependencies"]["renovate"] = "0.0.1"
        with open(pkg, "w") as f:
            json.dump(doc, f)
        self.registry.publish("renovate", "0.0.1")  # published, but not locked
        r = self.run_sh(root)
        self.assertFailed(r, "EUSAGE")
        self.assertFalse(any(p.endswith(".tgz") for p in self.registry.requests), self.registry.requests)

    def test_node_version_satisfies_renovates_engines(self):
        # CI's Node.js (.node-version) must satisfy the engines of every locked
        # package; run.sh installs with --engine-strict, so a Node.js outside
        # them fails the install instead of warning (EBADENGINE).
        with open(os.path.join(HERE, "run.sh")) as f:
            self.assertIn("npm ci --prefix \"$dir\" --ignore-scripts --engine-strict", f.read())
        with open(os.path.join(HERE, ".node-version")) as f:
            node = tuple(int(x) for x in f.read().strip().split("."))
        with open(os.path.join(HERE, "package-lock.json")) as f:
            packages = json.load(f)["packages"]
        for path, entry in packages.items():
            rng = entry.get("engines", {}).get("node") if isinstance(entry.get("engines"), dict) else None
            if path and rng:
                self.assertTrue(satisfies(node, rng), f"{path}: Node.js {node} outside engines {rng!r}")

    def test_lockfile_pins_every_package_with_integrity(self):
        with open(os.path.join(HERE, "package.json")) as f:
            pkg = json.load(f)
        with open(os.path.join(HERE, "package-lock.json")) as f:
            lock = json.load(f)
        packages = lock["packages"]
        self.assertEqual(packages[""]["dependencies"], pkg["dependencies"])
        want = pkg["dependencies"]["renovate"]
        self.assertEqual(packages["node_modules/renovate"]["version"], want)
        for path, entry in packages.items():
            if path and not entry.get("link"):
                self.assertTrue(entry.get("integrity", "").startswith("sha512-"), path)
                self.assertTrue(entry.get("resolved", "").startswith("https://registry.npmjs.org/"), path)


if __name__ == "__main__":
    unittest.main(verbosity=2)
