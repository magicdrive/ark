# Ark v6.0.0 — release notes (draft)

> Draft for the maintainer. Every number below was measured during the v6.0.0
> release audit (macOS x86_64, Go 1.27.1); nothing here is a general
> performance or token guarantee. Token counts are Ark's own estimate
> (`len(text)/4`), not any model's tokenizer, and they count tool responses
> only — not an agent's total token use.

## Overview

v6.0.0 makes Ark's answers about *which declaration* trustworthy, and adds a
discovery tool for agents that do not know a symbol's exact name.

- Every declaration is its own symbol, even when one file declares a name
  twice; nothing merges two declarations any more.
- `search_context` finds symbols from a partial identifier and returns the top
  candidate's context in one call, within one token budget.
- The MCP server resolves `--root` once at startup, accepts absolute paths
  inside it, and refuses symlinks that lead outside it.

The CLI and the MCP schema change only additively (one new tool, one new
optional parameter). The major version marks that **results change meaning
where they were wrong**, as in v5.0.0 — see Breaking changes.

## Highlights

- **`search_context` (new MCP tool).** A partial identifier (`auth`,
  `getUser`, `user_profile`, `Service.create`) returns up to `limit` ranked
  candidates (default 5) with context for the first `contextLimit` (default 1),
  all within `maxTokens` for the whole response (default 4000). Ranking is name
  similarity (`matchType`), never evidence of what code refers to.
- **Declaration identity.** Namesake declarations in one file (several Go
  `init`, a Python or JavaScript function defined twice) used to share one
  SymbolID: the index merged them, mixing their sources, callers and callees.
  Each now has its own ID, source and edges. A reference to a name declared
  twice stays ambiguous (no edge).
- **Absolute paths work with `--root ./`.** Claude Code launches Ark as
  `--root ./`; every tool rejected in-root absolute paths as "outside the server
  root". The root is now absolute from startup.

## New features

- `search_context` tool (`query`, `path`, `limit`, `contextLimit`, `maxTokens`,
  `includeContext`).
- Optional `symbolId` on `get_context`, `get_callers`, `get_callees`,
  `get_relations`, `analyze_change_impact`: selects one declaration among
  namesakes. Ambiguity listings now show each candidate's `file:line` and
  `symbolId`.
- `initialize` reports the binary's version in `serverInfo.version` (it was a
  fixed `0.1.0`).

## Reliability & correctness

- **Go reference resolution follows Go's scoping.** An unqualified name is a
  declaration of its own package (or a dot import), never "the only
  declaration with that name" elsewhere; `pkg.Name` is looked up in the package
  the import names (mapped by import path, exported, non-test files); a name a
  local declaration shadows at the call is never an edge. Measured against an
  independent `go/types` oracle (Strong/Exact resolutions the type checker
  confirms, over those it can verify):

  | Repository | Precision before → after | Wrong Strong/Exact | Correct resolutions | Recall |
  |---|---|---|---|---|
  | Ark | 98.15% → 100% | 132 → 0 | 7,010 → 7,741 | 78.1% → 86.3% |
  | golang.org/x/tools v0.36.0 | 75.96% → 100% | 3,968 → 0 | 12,540 → 13,684 | 76.1% → 83.0% |
  | fzf | 95.76% → 100% | 121 → 0 | 2,732 → 2,751 | 77.7% → 78.3% |

  19 correct resolutions were lost (x/tools 15, fzf 4): name-only matches that
  happened to be right (a variable shadowing an import, a variable named like
  a type); 1,913 were gained, mostly package-qualified calls and composite
  literals (`pkg.T{}`) the old import matching missed.

- Namesake declarations are separate symbols (`symbol.NewDeclarationID`: the
  first keeps its v5 ID; later ones carry a position-ordered ordinal).
- A reference's enclosing declaration is identified by lexical containment
  when several declarations of the file carry its container's name — exactly
  one must contain it, otherwise the reference stays unattributed.
- `Symbol.Parent` is the SymbolID of the real enclosing declaration (it was an
  ID no symbol had), or empty when that declaration cannot be singled out.
- SymbolID collisions (two distinct declarations, one 64-bit ID) stop the index
  build with an explicit `SymbolID collision` error instead of merging them.
- **Deterministic context ranking.** The ranker summed its score breakdown in
  map iteration order; near-equal scores could swap item order between
  identical `get_context` requests (measured: 2 distinct outputs in 240
  concurrent requests on v5.0.1, 1 on v6.0.0).
- Declaration ordinals and namesake query order are a function of file
  content, never of provider emission order.
- `ark mcp-server --help` / `--version` print help / the version (they started
  a server).

## Security

- Path arguments of every MCP tool go through one gate: relative paths resolve
  against the absolute root; absolute paths are accepted only inside it;
  `../` escapes are refused; an existing path whose symlink-resolved form leaves
  the root is refused (v5.0.1 returned the content of a file reached through
  such a symlink). See `SECURITY.md` for the remaining limits (TOCTOU; walks may
  read a symlinked *file* they encounter).

## Breaking changes

| Area | v5.0.1 | v6.0.0 | Migration |
|---|---|---|---|
| `mcp-server --root` | A missing or non-directory root started a server that failed every request | Startup error naming the resolved path | Pass an existing directory |
| Symlinks leaving the root | File tools followed them | Refused (`resolves through a symlink … outside the server root`) | Serve a root that contains the files, or copy them in |
| Same-file namesakes (`init`, redefinitions) | One merged symbol; tools answered with a mix of both | Separate symbols; a bare name is **ambiguous** for target tools | Pass the `symbolId` from the ambiguity listing (or from `search_context`) |
| SymbolIDs | — | Unchanged for every declaration except the 2nd+ namesake in a file | Re-read IDs of namesakes |
| `Symbol.Parent` (in `search_code` JSON) | Hash of a name (no such symbol) | Real parent ID or `""` | Treat as an ID reference |
| Containers named by several declarations | Unattributed | Attributed when exactly one contains the call | — (more edges, all lexically proven) |
| Go call graph | A unique name anywhere was Strong (x/tools: 2,437 cross-package edges); imports matched by path substring | Package scoping; on x/tools Go edges 11,179 → 9,604 with zero oracle-contradicted Strong/Exact | Expect fewer, correct edges; some former callers become Candidates |
| Go `pkg.Name` to a package outside the repository | Unresolved | `outsideRepository` | — |
| Error text | Root shown as given (`./`) | Absolute root; ambiguity listing has `file:line symbolId=` | Do not parse error text |

No CLI flag, MCP tool, required parameter or output field was removed.

## Migration guide (from v5.0.1)

1. **Upgrade the binary** (Homebrew, a release archive, or
   `go install github.com/magicdrive/ark@main`; see "Known limitations" about
   `go install …@latest`).
2. **MCP client configuration: no change needed.** Measured for all six
   clients (claude, cursor, cline, copilot-cli, copilot-vscode, codex): a
   configuration written by v5.0.1 is "No changes required" for v6.0.0, and other
   servers in the file are preserved. Restart the client so it re-reads the tool
   list (`search_context`).
3. **Cache: no action needed.** The first run re-extracts Go files (the Go
   provider's cache version changed); other entries are reused, and a cache
   gives the same answers as a cold build (measured). `--no-cache` and deleting `<root>/.ark/index` remain safe.
4. **Agents:** regenerate instructions/skills (`ark instruction <target>`,
   `ark setup <client> --force` for skills) to pick up `search_context`
   guidance — optional.
5. **Callers of target tools** that relied on a bare name for a same-file
   namesake must pass `symbolId`.

## Performance (measured, v5.0.1 vs v6.0.0, same machine and corpus)

| Corpus | Files | Cold first call | Warm get_context | Cache-restore first call | Max RSS |
|---|---|---|---|---|---|
| small fixture | 4 | 97 → 102 ms | 0.4 → 0.4 ms | 21 → 21 ms | 48 → 49 MB |
| Ark repository | 585 | 4.09 → 4.12 s | 30.7 → 28.7 ms | 570 → 570 ms | 248 → 249 MB |
| fzf | 89 | 1.58 → 1.56 s | 6.7 → 6.4 ms | 149 → 147 ms | 228 → 234 MB |
| golang.org/x/tools v0.36.0 | 1216 | 14.7–16.1 → 15.2–15.9 s | 79 → 74 ms | 2.15 → 2.25 s | 832–947 → 781–1029 MB |

Timings and RSS overlap between versions across repeated runs (RSS varies with
GC timing). Go package scoping (measured against the v6 audit build, same
corpora): resolver time 1.31 s → 0.19 s on x/tools (268 → 54 ms on Ark: the
repository-wide name stages no longer run for Go), index build 14.9 → 13.6 s,
heap retained by the index unchanged (70.5 → 68.4 MB on x/tools); warm tool
latency within run-to-run variation. In-process on x/tools: allocations +1.6% (3.64M → 3.70M), heap
304 → 305 MB. `search_context` warm latency: 0.5 ms / 32 ms / 8 ms / 96 ms on the
four corpora.

Tool-level workflow benchmark (12 tasks: exact, partial and ambiguous names,
callers, callees, impact, error path, another Go repository, TypeScript; no
model involved — fixed procedures, same success criteria):

| Workflow | Tasks solved | Tool calls | Tool-response tokens | Warm wall-clock |
|---|---|---|---|---|
| A: grep + read files (no Ark) | 9/12 | 50 | 213,264 | 1.0 s |
| B: find_symbol / get_context / graph tools (v5.0.1) | 10/12 | 16 | 21,699 | 35.4 s |
| B: same (v6.0.0) | 10/12 | 16 | 21,699 | 35.3 s |
| C: search_context first (v6.0.0) | 12/12 | 16 | 51,190 | 0.4 s |

C solves the two tasks B misses (a name in another naming style; a
case-insensitive partial name) and avoids `find_symbol`'s per-call file scan,
but returns more tokens: 31k of its 51k come from one task whose target
function is very large (`get_context` always includes the target). These are
tool-response tokens, not a model's total token use.

## Known limitations

- **`go install github.com/magicdrive/ark@latest` installs the legacy v1.2.3.**
  The module path has no major-version suffix, so `@vX` tags above v1 are not
  installable with `go install` (this predates v6). Use Homebrew, a release
  archive, or `@main` (reports a pseudo-version).
- **Go: what still stays a Candidate or Unresolved** (never a wrong edge):
  method calls on a variable whose type is not written down locally (or is
  package-qualified: `var s pkg.T; s.M()`), methods of named non-struct types
  (`type g map[...]`), interface dispatch, promoted (embedded) methods, and
  declarations duplicated across build-tagged files. With one internal import
  only, imports map into the repository only if the root directory is named
  like the module path's last element.
- A SymbolID collision makes the whole index unavailable (every index tool
  answers `SymbolID collision`) until the colliding declarations change;
  file tools keep working. Never observed; tested by injection.
- `symbolId` values depend on the indexed path: `search_context`'s IDs are
  valid with `path: "."`.
- `get_context` includes the target even beyond `maxTokens`
  (`TargetTruncated`); `search_context` instead marks such a candidate
  `omitted_budget`.
- A function passed as a value (`ids: symbol.NewDeclarationID`) is no call:
  `analyze_change_impact` does not list the code that calls it through the
  value.
- Python methods are not extracted; Terraform `.tf.json` is not read.

## Changelog (v5.0.1 → v6.0.0)

**Added** — `search_context` MCP tool; optional `symbolId` on five target
tools; `symbol_id_collision` diagnostic code; `serverInfo.version` from the
binary; CLI help documents `--no-cache`.

**Changed** — Go references resolve by package scoping (no repository-wide
unique-name stage; import paths mapped to directories; local shadowing
capped); namesake declarations are separate symbols; containment
identifies enclosing namesakes; `Symbol.Parent` is a real ID; `--root` is
validated and made absolute at startup; ambiguity listings show
`file:line symbolId=`; error messages show the absolute root.

**Fixed** — absolute in-root paths rejected under a relative root; merged
namesakes' sources and edges; dangling `Parent` IDs; nondeterministic context
item order; order-dependent declaration ordinals for conflicting drafts;
`mcp-server --help/--version`; README install instructions; Go dot imports
were read as ordinary imports; `var x I = &T{}` proved type T for x.

**Security** — symlink escapes through tool path arguments are refused.

**Deprecated / Removed** — none.

**Performance** — no measurable change (see above).

**Internal** — `language.Extraction.Package` / `PackageScoped`,
`resolver.NewInRoot`; Go provider cache version go-7 (re-extracts Go files
once); a `go/types` oracle test for Go resolution; `index.IDFunc` /
`index.NewWithIDs` (collision tests);
`common.ResolveRootDir` shared by `setup` and `mcp-server`; total order in
`index.sortSymbols`; CI fuzz smoke anchored and extended to the new fuzz tests.

## Validation

gofmt, `go vet ./...`, `go test ./...`, `go test -race ./...`, staticcheck
(CI set), golden, conformance, contextquality, every CI fuzz smoke step (13),
the TypeScript compiler differential (typescript@5.6.3: 1,000,094 type-argument
cases, 0 disagreements), GoReleaser snapshot build + `verify-dist.sh` (8
archives), MCP JSON-RPC integration tests on the real binary, setup upgrade from
v5.0.1 for all six clients, cache cold / warm / corrupted / read-only /
v5.0.1-written, 240 concurrent HTTP requests.
