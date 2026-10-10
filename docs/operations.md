# Operations

How the Ark MCP server runs, what it keeps on disk and in memory, and how it
fails.

## Process model

An agent starts `ark mcp-server` as a child process, normally one per agent
session, and talks to it over standard input and output (JSON-RPC, MCP
protocol version `2024-11-05`). The server exits when its input closes.
Diagnostics and warnings go to standard error.

`--type http` serves the same protocol at `http://localhost:<port>/mcp`
(default port 8522) for clients that cannot spawn processes, with a health
endpoint at `/health`. See [SECURITY.md](../SECURITY.md#http-transport) before
using it.

At startup the server resolves `--root` once to an absolute, clean path and
exits with an error if it does not exist or is not a directory. Every tool
path is resolved against that root.

## What the index covers

Graph tools build an index of the directory given as their `path` argument —
usually the root, `.`. The index contains every file whose extension a
language provider handles (`get_language_support`), except files under
directories named `vendor` or `node_modules`, or whose name starts with `.`.

- Files and directories excluded by `.arkignore` are not indexed (nor read
  by any tool;
  [SECURITY.md](../SECURITY.md#file-access-policy-arkignore)). A change to
  `.arkignore` applies to the next request.
- Symlinks leading outside the root are not indexed unless the server runs
  with `--allow-external-symlinks on`
  ([Symlink policy](../SECURITY.md#symlink-policy)); changing the setting
  and restarting changes the indexed file set accordingly.
- `.gitignore` and the server's filter options do **not** narrow the index;
  they apply to the file tools.
- Test files and `testdata` directories are indexed; tools that should leave
  tests out (`get_context`, `search_code`) do so by default.
- Pointing a graph tool at a subdirectory (`path: "services/billing"`) indexes
  only that subtree, which is the way to bound the work on a very large
  repository. Symbol IDs depend on the indexed path.

## Index lifecycle

- The first graph request for a directory builds its index: every source file
  is parsed (or read from the extraction cache) and every reference is
  resolved. Concurrent requests for the same directory share one build.
- Later requests reuse the index after re-computing a fingerprint of the
  source files (their paths and contents, not timestamps). Any added,
  removed, renamed or edited source file triggers a rebuild, so answers always
  describe the files as they are.
- A build is published only if the sources did not change while it ran; if
  they keep changing for three builds in a row, the request fails with
  `sources kept changing during 3 builds; retry when they settle`.
- The server keeps at most three indexes in memory (for example, the root and
  two subdirectories), least recently used first out.
- An index is immutable and shared by concurrent requests.

Each index request walks the repository once: the walk that re-reads the
`.arkignore` rule files (only their compilation is reused while they are
unchanged) also lists the source files, and the fingerprint then reads and
hashes every listed source file. On a 1,875-file repository this is the bulk
of a warm request. A tool given a single path reads only the rule files above
that path and walks nothing ([Performance](performance.md)).

## Extraction cache

Parsing is the expensive part of a build, so the server stores each file's
extraction result in `<root>/.ark/index` (one JSON file per source file
version). A restarted server reads them back and only resolves; on the
measured repositories this is 5–13 times faster than a cold build.

- **Location:** `<root>/.ark/index`, created with mode `0700`. Add `.ark/` to
  `.gitignore`. The location is not configurable.
- **Key:** file path, content hash, cache schema version and the language
  provider's version. An entry is used only if all match; a provider whose
  analysis changed in a new Ark release gets a new version, so its files are
  re-extracted once after an upgrade.
- **Failure handling:** an unreadable or corrupt entry is a cache miss (a
  corrupt one is deleted). If the directory cannot be created, the server
  logs `ark: cache disabled …` and runs without a cache.
- **Growth:** an edited file gets a new entry; old entries are not removed —
  including those of a file later excluded by `.arkignore`, which are never
  read again.
  Delete `<root>/.ark/index` whenever you like — it is always safe; the next
  start rebuilds it.
- **Disable:** `ark mcp-server --no-cache`.

The cache stores extraction results, never resolution results or symbol IDs:
a cached build gives the same answers as a cold one. It holds names and
reference text as written in the source — secret masking applies to MCP
responses, not to the cache ([SECURITY.md](../SECURITY.md#secret-masking)).

## Resource usage

Memory grows with the size of the indexed source. Peak resident memory of
the server process, from the [performance measurements](performance.md):

| Repository | Source files indexed | Peak RSS |
|---|---|---|
| ky (TypeScript) | 34 | 50 MB |
| express (JavaScript) | 152 | 56 MB |
| zod (TypeScript) | 170 | 162 MB |
| Ark (Go) | 600 | 246 MB |
| golang.org/x/tools (Go) | 1,875 | 959 MB |

There is no per-file size limit and no limit on response size; set operating
system limits when analyzing untrusted repositories
([SECURITY.md](../SECURITY.md)).

## Determinism

For the same files and Ark version, every answer is identical: resolution
order, candidate lists, samples, context ranking and ambiguity listings are
deterministic. Results do not depend on request order, concurrency or the
cache.

## Failure modes

| Situation | Behavior |
|---|---|
| `--root` missing or not a directory | The server does not start; the error names the resolved path |
| A file cannot be read or parsed | The file, or the rejected region, is skipped and reported by `get_diagnostics`; other files are unaffected |
| Sources change during a build | The build is discarded and retried |
| Two declarations produce the same `symbolId` | The index is not built; index tools answer `SymbolID collision`, file tools keep working |
| Cache unreadable or corrupt | Cache miss; the file is re-extracted |
| A tool cannot answer | MCP result with `isError: true`; the server keeps running |

## Supported platforms

Release binaries are built with `CGO_ENABLED=0` for Linux and Windows (386,
amd64, arm64) and macOS (amd64, arm64). The test suite runs in CI on Linux
only; the other platforms are cross-compiled and not tested in CI.
