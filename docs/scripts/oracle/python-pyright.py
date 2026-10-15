"""Python oracle for TestFidelityBaseline: pyright's go-to-definition.

usage: python3 -I python-pyright.py <pyright-langserver.index.js> <corpus-dir> <python> > oracle.jsonl

  <python>  the interpreter pyright resolves imports with: one of a virtual
            environment holding the corpus's dependencies at fixed versions
            (without them pyright answers nothing for their names, and Ark's
            false Strong targets for them are counted unknown, not false)

The sites are found independently of Ark: every identifier the standard
library's ast reads (a Name in load context, the attribute of an Attribute in
load context), in every .py file of the corpus (directories starting with "."
and named vendor, node_modules or __pycache__ are skipped, as Ark skips them).
For each, pyright's language server (textDocument/definition) gives the
declarations it denotes. A site is

  declaration       pyright names exactly one declaration, in a .py file of
                    the corpus (decl.name: the declaration's own name, which an
                    alias import does not share)
  ambiguous         several, all in .py files of the corpus (a name assigned in
                    several places, a conditional definition)
  external          every declaration it names is outside the corpus (the
                    standard library, a dependency, pyright's bundled typeshed)
  declaration_file  only stubs (.pyi) of the corpus — a stub is not the
                    implementation Ark indexes (a stub pyright names together
                    with its implementation, pkg/m.pyi with pkg/m.py, is
                    dropped for it)
  module            the name denotes a module of the corpus (pkg and m of
                    pkg.m.f): no declaration Ark models (a module outside the
                    corpus is external)
  unknown           pyright names none

Nothing is classed "dynamic": pyright names no declaration for getattr and the
like, and they are counted as unknown — never as Ark's success or error.

Columns are converted to 1-based byte columns (ast gives byte offsets; the
language server UTF-16 code units). The ast of the running interpreter parses
the corpus: use one at least as new as the corpus's syntax (3.12 for match).
Pin pyright (npm install pyright@<version>) and record its version, the
<python> interpreter's version and its installed packages with the results:
pyright reads them and the corpus's own configuration (pyrightconfig.json,
pyproject.toml), and answers differently with others.
"""

import ast
import json
import re
import os
import pathlib
import subprocess
import sys

SKIP = {"vendor", "node_modules", "__pycache__"}


def sites(root):
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames if not d.startswith(".") and d not in SKIP)
        for name in sorted(filenames):
            if not name.endswith(".py"):
                continue
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, root).replace(os.sep, "/")
            data = open(path, "rb").read()
            try:
                tree = ast.parse(data, filename=rel)
            except (SyntaxError, ValueError):
                continue
            lines = data.split(b"\n")
            for node in ast.walk(tree):
                if isinstance(node, ast.Name) and isinstance(node.ctx, ast.Load):
                    yield rel, lines, node.id, node.lineno, node.col_offset
                elif isinstance(node, ast.Attribute) and isinstance(node.ctx, ast.Load):
                    # The attribute's name ends where the node does.
                    line = lines[node.end_lineno - 1]
                    start = node.end_col_offset - len(node.attr.encode())
                    if line[start:node.end_col_offset] == node.attr.encode():
                        yield rel, lines, node.attr, node.end_lineno, start


def utf16_col(line, byte_col):
    return len(line[:byte_col].decode("utf-8", "replace").encode("utf-16-le")) // 2


def byte_col(line, utf16):
    text = line.decode("utf-8", "replace")
    units = 0
    for i, ch in enumerate(text):
        if units >= utf16:
            return len(text[:i].encode())
        units += len(ch.encode("utf-16-le")) // 2
    return len(line)


class Server:
    def __init__(self, server, root, python):
        self.root = root
        self.python = python
        self.proc = subprocess.Popen(["node", server, "--stdio"], stdin=subprocess.PIPE,
                                     stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        self.id = 0
        self.opened = set()
        uri = pathlib.Path(root).as_uri()
        self.request("initialize", {"processId": os.getpid(), "rootUri": uri,
                                    "capabilities": {"workspace": {"configuration": True}},
                                    "workspaceFolders": [{"uri": uri, "name": "corpus"}]})
        self.send({"jsonrpc": "2.0", "method": "initialized", "params": {}})

    def send(self, msg):
        b = json.dumps(msg).encode()
        self.proc.stdin.write(b"Content-Length: %d\r\n\r\n" % len(b) + b)
        self.proc.stdin.flush()

    def read(self):
        headers = {}
        while True:
            line = self.proc.stdout.readline().decode().strip()
            if not line:
                break
            k, v = line.split(":", 1)
            headers[k.lower()] = v.strip()
        return json.loads(self.proc.stdout.read(int(headers["content-length"])))

    def request(self, method, params):
        self.id += 1
        self.send({"jsonrpc": "2.0", "id": self.id, "method": method, "params": params})
        while True:
            m = self.read()
            if m.get("id") == self.id and "method" not in m:
                return m.get("result")
            if "method" in m and "id" in m:  # a request of the server
                result = None
                if m["method"] == "workspace/configuration":
                    result = [{"pythonPath": self.python} if item.get("section") == "python" else None
                              for item in m["params"].get("items", [])]
                self.send({"jsonrpc": "2.0", "id": m["id"], "result": result})

    def definition(self, rel, line, utf16):
        uri = pathlib.Path(self.root, rel).as_uri()
        if uri not in self.opened:
            text = open(os.path.join(self.root, rel), encoding="utf-8", errors="replace").read()
            self.send({"jsonrpc": "2.0", "method": "textDocument/didOpen",
                       "params": {"textDocument": {"uri": uri, "languageId": "python", "version": 1, "text": text}}})
            self.opened.add(uri)
        return self.request("textDocument/definition",
                            {"textDocument": {"uri": uri}, "position": {"line": line - 1, "character": utf16}}) or []

    def close(self):
        self.request("shutdown", None)
        self.send({"jsonrpc": "2.0", "method": "exit"})


IDENT = re.compile(r"[^\W\d]\w*")


def main():
    server, root, python = sys.argv[1], os.path.realpath(sys.argv[2]), sys.argv[3]
    version = subprocess.run([python, "-c", "import sys; print(sys.version.split()[0])"],
                             capture_output=True, text=True).stdout.strip()
    print("python", python, version, file=sys.stderr)
    srv = Server(server, root, python)
    files = {}

    def decl_of(path, dline, dchar):
        drel = os.path.relpath(path, root).replace(os.sep, "/")
        if drel not in files:
            files[drel] = open(path, "rb").read().split(b"\n")
        line = files[drel][dline - 1]
        col = byte_col(line, dchar)
        m = IDENT.match(line[col:].decode("utf-8", "replace"))
        d = {"file": drel, "line": dline, "col": col + 1}
        if m:
            d["name"] = m.group()
        return d

    for rel, lines, name, line, col in sites(root):
        decls = []
        for d in srv.definition(rel, line, utf16_col(lines[line - 1], col)):
            uri = d.get("targetUri") or d.get("uri")
            rg = d.get("targetSelectionRange") or d.get("range")
            path = os.path.realpath(uri[len("file://"):]) if uri.startswith("file://") else uri
            decls.append((path, rg["start"]["line"] + 1, rg["start"]["character"],
                          rg["start"] == rg["end"] == {"line": 0, "character": 0}))
        site = {"lang": "python", "file": rel, "name": name,
                "start_line": line, "start_col": col + 1,
                "end_line": line, "end_col": col + 1 + len(name.encode())}
        if decls and all(d[3] for d in decls) and any(d[0].startswith(root + os.sep) for d in decls):
            # A module of the corpus (pyright points at its file's start).
            site["class"] = "module"
            print(json.dumps(site, sort_keys=True))
            continue
        decls = [d[:3] for d in decls]
        # A stub with its implementation (pkg/m.pyi and pkg/m.py): the
        # implementation is the declaration.
        stems = {d[0][:-3] for d in decls if d[0].startswith(root + os.sep) and d[0].endswith(".py")}
        decls = [d for d in decls if not (d[0].endswith(".pyi") and d[0][:-4] in stems)]
        inside = [d for d in decls if d[0].startswith(root + os.sep)]
        sources = [d for d in inside if d[0].endswith(".py")]
        if decls and not inside:
            site["class"] = "external"
        elif len(decls) == 1 and sources:
            site["class"] = "declaration"
            site["decl"] = decl_of(*sources[0])
        elif decls and len(sources) == len(decls):
            site["class"] = "ambiguous"
            site["decls"] = [decl_of(*d) for d in sources]
        elif inside and not sources:
            site["class"] = "declaration_file"
        else:
            site["class"] = "unknown"
        print(json.dumps(site, sort_keys=True))
    srv.close()


if __name__ == "__main__":
    main()
