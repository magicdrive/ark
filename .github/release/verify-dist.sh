#!/usr/bin/env bash
# Verifies a Hephaestus dist/ directory before anything is published: exactly
# the archives of the eight shipped targets, the layout of the GoReleaser
# releases up to v6.7.0 inside each, ark_checksums.txt listing exactly them,
# and every binary built for its target with the release build flags
# (.github/release/build.sh).
#
# Usage: verify-dist.sh <dist-dir> --snapshot     (make snapshot, CI)
#        verify-dist.sh <dist-dir> --tag vX.Y.Z   (the release workflow)
#
# --tag requires the version of the tag and a release build
# (internal/cache.release=true); --snapshot requires 0.0.0-SNAPSHOT and the
# flag left empty. Each binary's build information is read from the file
# itself (no Go toolchain needed), as `go version -m` reads it.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
exec python3 - "$here/../.." "$@" <<'PY'
import hashlib, io, json, os, re, struct, sys, tarfile

def fail(msg):
    print(f"::error::verify-dist: {msg}")
    sys.exit(1)

root, args = sys.argv[1], sys.argv[2:]
if len(args) == 2 and args[1] == "--snapshot":
    dist, version, release = args[0], "0.0.0-SNAPSHOT", ""
elif len(args) == 3 and args[1] == "--tag" and re.fullmatch(r"v\d+\.\d+\.\d+([-+][0-9A-Za-z.+-]+)?", args[2]):
    dist, version, release = args[0], args[2][1:], "true"
else:
    fail("usage: verify-dist.sh <dist-dir> --snapshot | --tag vX.Y.Z")

TARGETS = ["darwin_amd64", "darwin_arm64", "linux_386", "linux_amd64",
           "linux_arm64", "windows_386", "windows_amd64", "windows_arm64"]
COMPLETIONS = {  # archive path -> source in the repository
    "completions/ark-completion.sh": "misc/completions/ark-completion.sh",
    "completions/bash/ark-completion.bash": "misc/completions/bash/ark-completion.bash",
    "completions/fish/ark.fish": "misc/completions/fish/ark.fish",
    "completions/zsh/_ark": "misc/completions/zsh/_ark",
}
DIRS = {"completions/", "completions/bash/", "completions/fish/", "completions/zsh/"}
LDFLAGS = ('-s -w -X \\"main.version=v{v}\\" '
           '-X \\"github.com/magicdrive/ark/internal/cache.release={r}\\"').format(v=version, r=release)

def sha256(b):
    return hashlib.sha256(b).hexdigest()

def read(p):
    with open(p, "rb") as f:
        return f.read()

names = {t: f"ark_{version}_{t}.tar.gz" for t in TARGETS}

# The manifest names this version and exactly these archives.
m = json.loads(read(os.path.join(dist, "hephaestus-manifest.json")))
if m.get("project") != "ark" or m.get("version") != version:
    fail(f"manifest is {m.get('project')} {m.get('version')}, want ark {version}")
archives = {a["name"]: a for a in m["artifacts"] if a["kind"] == "archive"}
if sorted(archives) != sorted(names.values()):
    fail(f"manifest archives {sorted(archives)}, want {sorted(names.values())}")

# dist/ holds no other archive (a zip, a stale version).
on_disk = sorted(f for f in os.listdir(dist) if re.search(r"\.(tar\.gz|tgz|zip)$", f))
if on_disk != sorted(names.values()):
    fail(f"archives in {dist}: {on_disk}, want {sorted(names.values())}")

# ark_checksums.txt lists exactly the archives, as sha256sum -c reads it.
lines = read(os.path.join(dist, "ark_checksums.txt")).decode().splitlines()
listed = {}
for line in lines:
    mm = re.fullmatch(r"([0-9a-f]{64})  (\S+)", line)
    if not mm:
        fail(f"ark_checksums.txt: bad line {line!r}")
    listed[mm.group(2)] = mm.group(1)
if sorted(listed) != sorted(names.values()) or len(lines) != len(names):
    fail(f"ark_checksums.txt lists {sorted(listed)}, want {sorted(names.values())}")

# Build information of a Go binary (debug/buildinfo, the inline format of
# Go 1.18 and later).
def uvarint(b, i):
    x = s = 0
    while True:
        c = b[i]; i += 1
        x |= (c & 0x7f) << s
        if c < 0x80:
            return x, i
        s += 7

def buildinfo(b):
    i = b.find(b"\xff Go buildinf:")
    if i < 0 or not (b[i + 15] & 2):
        fail("no Go build information")
    n, j = uvarint(b, i + 32)
    gover = b[j:j + n].decode(); j += n
    n, j = uvarint(b, j)
    mod = b[j:j + n][16:-16].decode()
    settings = {}
    for line in mod.splitlines():
        if line.startswith("build\t"):
            k, _, v = line[6:].partition("=")
            settings[k] = v
    return gover, settings

# The executable format and machine of a binary.
def machine(b):
    if b[:4] == b"\x7fELF":
        return "linux", {3: "386", 62: "amd64", 183: "arm64"}.get(struct.unpack_from("<H", b, 18)[0])
    if b[:4] == b"\xcf\xfa\xed\xfe":
        return "darwin", {0x01000007: "amd64", 0x0100000c: "arm64"}.get(struct.unpack_from("<I", b, 4)[0])
    if b[:2] == b"MZ":
        pe = struct.unpack_from("<I", b, 0x3c)[0]
        if b[pe:pe + 4] == b"PE\0\0":
            return "windows", {0x14c: "386", 0x8664: "amd64", 0xaa64: "arm64"}.get(struct.unpack_from("<H", b, pe + 4)[0])
    return None, None

gomod = read(os.path.join(root, "go.mod")).decode()
tc = re.search(r"(?m)^toolchain (go\S+)\s*$", gomod)
want_go = tc.group(1) if tc else "go" + re.search(r"(?m)^go (\S+)\s*$", gomod).group(1)

for t in TARGETS:
    name = names[t]
    data = read(os.path.join(dist, name))
    if sha256(data) != listed[name] or sha256(data) != archives[name]["sha256"]:
        fail(f"{name}: sha256 differs from ark_checksums.txt or the manifest")
    goos, goarch = t.split("_")
    exe = "ark.exe" if goos == "windows" else "ark"
    want = {exe: 0o755, **{p: 0o644 for p in COMPLETIONS}}
    files = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as tf:
        for e in tf.getmembers():
            if e.isdir():
                if e.name.rstrip("/") + "/" not in DIRS:
                    fail(f"{name}: unexpected directory {e.name}")
                continue
            if not e.isfile():
                fail(f"{name}: {e.name} is not a regular file")
            if e.name in files:
                fail(f"{name}: {e.name} twice")
            if e.mode & 0o7777 != want.get(e.name, -1):
                fail(f"{name}: {e.name} mode {oct(e.mode)}, want {oct(want.get(e.name, 0))}")
            files[e.name] = tf.extractfile(e).read()
    if sorted(files) != sorted(want):
        fail(f"{name}: files {sorted(files)}, want {sorted(want)}")
    for path, src in COMPLETIONS.items():
        if files[path] != read(os.path.join(root, src)):
            fail(f"{name}: {path} differs from {src}")
    binary = files[exe]
    if machine(binary) != (goos, goarch):
        fail(f"{name}: {exe} is a {machine(binary)} executable, want {goos}/{goarch}")
    gover, s = buildinfo(binary)
    want_settings = {"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0", "-ldflags": '"' + LDFLAGS + '"'}
    for k, v in want_settings.items():
        if s.get(k) != v:
            fail(f"{name}: build setting {k}={s.get(k)!r}, want {v!r}")
    for k in ("-tags", "-trimpath", "-race", "-asan", "-msan", "-cover"):
        if k in s:
            fail(f"{name}: unexpected build setting {k}={s[k]}")
    if gover != want_go:
        fail(f"{name}: built by {gover}, go.mod names {want_go}")

print(f"ok: 8 archives of ark {version}: checksums, layout, completions, targets and "
      f"build flags (release={release or '<empty>'}, {want_go}) verified")
PY
