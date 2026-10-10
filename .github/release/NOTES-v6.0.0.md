# Ark v6.0.0 — release notes (draft)

> Draft for the maintainer. Every number below was measured during the v6.0.0
> release audit (macOS x86_64, Go 1.27.1); nothing here is a general
> performance or token guarantee. Token counts are Ark's own estimate
> (`len(text)/4`), not any model's tokenizer, and they count tool responses
> only — not an agent's total token use.

## Overview

Ark is a code intelligence engine for AI coding agents: it resolves what the
references in a repository point to and serves the resulting graph over MCP,
with the confidence of every answer. v6.0.0 makes those answers about *which
declaration* trustworthy, and adds a discovery tool for agents that do not
know a symbol's exact name.

- **Go references follow Go's scoping** (improved): package scope, imports
  and local shadowing decide a call's target; an unrelated declaration that
  happens to share a name is no longer an edge.
- **Names never cross languages** (improved): a Python, JavaScript or PHP
  call no longer resolves to another language's declaration. TSX and
  TypeScript, one language, still share names.
- **Every declaration is its own symbol** (improved), even when one file
  declares a name twice; nothing merges two declarations any more.
- **`search_context`** (new) finds symbols from a partial identifier and
  returns the top candidate's context in one call, within one token budget.
- The MCP server resolves `--root` once at startup, accepts absolute paths
  inside it, and neither reads nor indexes through symlinks that lead outside
  it unless the operator starts it with `--allow-external-symlinks on` (new).
- **Secrets are masked in MCP responses** (new): the repository dump's
  masking rules apply at the MCP output boundary — source snippets, search
  lines, receiver expressions, resources and errors — on by default;
  `ark mcp-server --mask-secrets off` turns it off, with a warning.
- **`.arkignore` applies to the MCP server** (new): files it excludes are
  never read, listed, searched or indexed by any tool, directly or through a
  symlink, whatever the masking setting.
- **Documentation** (new): a `docs/` set — getting started, MCP tool
  reference, resolution model, language support, operations, performance,
  troubleshooting — and a rewritten README.

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

- **Names no longer cross languages.** A Python, JavaScript or PHP call
  resolved to the repository's only declaration of its name even in another
  language (Python `GoOnly()` → Go `GoOnly`, Strong), and a Go method call on
  a value of unknown type listed other languages' methods as Candidates.
  Every name-based rule now considers only declarations of the referencing
  file's language (TSX and TypeScript are one); explicit evidence (module
  bindings, qualified identity) is unchanged. Measured on the v6 corpora
  (Ark, ky, zod, express, requests, Slim, guzzle, terraform-aws-vpc/-eks and
  mixed-language fixtures): cross-language Strong edges 8 → 0, cross-language
  candidate pairs 991 → 0 (all name coincidences, e.g. zod's
  `entry.name.endsWith()` in a build script → `ZodString.endsWith`);
  same-language resolutions unchanged except one Candidate set reached by a
  later rule once a cross-language match no longer stopped it; TSX → TypeScript
  imports kept (9 Exact edges).

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

- **Secret masking at the MCP output boundary.** v5.0.1 masked secrets only in
  `get_file_content`, `get_files_arklite` and `file://`; `get_symbol`,
  `get_context`, `search_context`, `search_in_files` and the receiver
  expressions of `find_references`, `get_callees` and `analyze_change_impact`
  returned them as written. Every tool result, resource and error now passes
  one sanitizer that applies the existing rules (`internal/secrets`, unchanged)
  to repository text, keeps JSON structure, keys, numbers, paths, symbol names
  and IDs unchanged, and returns a response with nothing to mask byte for
  byte. It is on by default; `ark mcp-server --mask-secrets off` turns it off
  for the server and logs a warning on standard error once. A request cannot
  turn it off: `maskSecrets` on `get_file_content` / `get_files_arklite`
  stays accepted without effect, as before. Analysis, index and cache are
  unaffected.
- **`.arkignore` is enforced by the MCP server.** Every `.arkignore` under the
  root (with the dump's pattern syntax and matcher) now decides what the
  server may read: an excluded path, or a symlink to one, is answered as not
  existing; listings, searches, trees, statistics and the index skip excluded
  files, so no symbol, edge, context, map entry or diagnostic of them exists.
  Rule changes apply to the next request, of the same or a restarted server;
  the extraction cache cannot serve an excluded file. Independent of masking;
  rules that cannot be read fail closed. Each request reads every rule file
  once; a named path reads only the rule files above it; compiled rules are
  reused while the rule files' SHA-256 contents are unchanged.
- **The dump reads ignore files as the MCP server does.** Rules come from the
  ignore files at and below the dumped directory, wherever `ark` is run from
  (v5.0.1 rooted them at the working directory: `ark /abs/repo` from
  elsewhere applied none of the repository's rules). `.arkignore` and
  `.gitignore` are separate sources — either excludes, and a `!` in one never
  re-includes what the other excludes (v5.0.1 read a directory's
  `.arkignore` only when it had no `.gitignore`, so with the default `-a on`
  a root `.gitignore` disabled the root `.arkignore`). A symlink to an
  excluded file in the repository is excluded too; symlinks to directories
  are listed, not followed; dangling and looping links no longer stop the
  dump (v5.0.1 exited with `read …: is a directory` / `open …: no such
  file`). An ignore file the dump cannot read stops it (`ignore rules: …`)
  instead of being skipped.
- **A symlinked root no longer loses its `.arkignore` rules.** When
  `mcp-server --root` (or a dump target) was itself a symlink, the walk that
  reads the rule files did not enter it, so no rule applied while the index —
  built over the resolved directory — included excluded files
  (`get_context` and `search_context` returned them). The walk now enters the
  directory the root leads to.
- **A symlinked root works as the directory.** `list_files`,
  `search_in_files`, `get_project_stats`, `find_symbol`, `find_references`
  and `get_files_arklite` returned nothing for a `--root` given through a
  symlink, and the plaintext, markdown and arklite dumps of such a target
  were empty (v5.0.1 too). They now walk the directory the root leads to and
  report paths under the root as given; symlinks below the root keep their
  policy.
- Path arguments of every MCP tool go through one gate: relative paths resolve
  against the absolute root; absolute paths are accepted only inside it;
  `../` escapes are refused; an existing path whose symlink-resolved form leaves
  the root is refused (v5.0.1 returned the content of a file reached through
  such a symlink), and walks and the index skip symlinks whose target lies
  outside the root.
- **`mcp-server --allow-external-symlinks <on|off>`** (new, default `off`):
  with `on`, files reached through symlinks in the repository that lead
  outside it are read and indexed; `.arkignore` still applies to the path in
  the repository, paths outside the root stay refused, and no request can
  change the setting. See `SECURITY.md` (Symlink policy) for the behaviour
  per access and the remaining limit (TOCTOU).

## Breaking changes

| Area | v5.0.1 | v6.0.0 | Migration |
|---|---|---|---|
| `mcp-server --root` | A missing or non-directory root started a server that failed every request | Startup error naming the resolved path | Pass an existing directory |
| Symlinks leaving the root | File tools followed them; walks read file symlinks | Refused when named (`resolves through a symlink … outside the server root`); skipped by walks and the index | Serve a root that contains the files, or start the server with `--allow-external-symlinks on` |
| Same-file namesakes (`init`, redefinitions) | One merged symbol; tools answered with a mix of both | Separate symbols; a bare name is **ambiguous** for target tools | Pass the `symbolId` from the ambiguity listing (or from `search_context`) |
| SymbolIDs | — | Unchanged for every declaration except the 2nd+ namesake in a file | Re-read IDs of namesakes |
| `Symbol.Parent` (in `search_code` JSON) | Hash of a name (no such symbol) | Real parent ID or `""` | Treat as an ID reference |
| Containers named by several declarations | Unattributed | Attributed when exactly one contains the call | — (more edges, all lexically proven) |
| Go call graph | A unique name anywhere was Strong (x/tools: 2,437 cross-package edges); imports matched by path substring | Package scoping; on x/tools Go edges 11,179 → 9,604 with zero oracle-contradicted Strong/Exact | Expect fewer, correct edges; some former callers become Candidates |
| Go `pkg.Name` to a package outside the repository | Unresolved | `outsideRepository` | — |
| A name declared only in another language | Strong / Candidate to that declaration | Unresolved | Expect no callers, impact or context across languages |
| Secrets in MCP responses | Masked only by the file tools | Masked in every response by default; `mcp-server --mask-secrets off` turns it off, with a warning | Start the server with `--mask-secrets off` where unmasked text is required |
| Files listed in `.arkignore` | Read, listed, searched and indexed by the MCP server | Invisible to every MCP tool and resource | Remove patterns for files agents must see |
| Dump ignore rules | Read under the working directory; a directory's `.arkignore` skipped when it had a `.gitignore` (default `-a on`) | Read at and below the dumped directory; `.arkignore` and `.gitignore` both apply | Expect more files excluded where both exist or when run from elsewhere; a subdirectory target no longer uses its parents' rules — put rules in it, or dump the parent |
| Dump with an unreadable ignore file / a dangling or directory symlink | Rules silently skipped / exit 1 | Exit 1 with `ignore rules: …` / dump completes | Fix the file's permissions |
| Source in code-intelligence responses | As written | Detected secrets, and code the assignment rule matches (`token := next()`), show `*****MASKED*****` | — |
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
GC timing). This table compares versions on the audit's corpus copies (x/tools:
1,216 indexed files); `docs/performance.md` measures v6.0.0 alone on the full
release archives (x/tools: 1,875 files), with a reproducible procedure. Go package scoping (measured against the v6 audit build, same
corpora): resolver time 1.31 s → 0.19 s on x/tools (268 → 54 ms on Ark: the
repository-wide name stages no longer run for Go), index build 14.9 → 13.6 s,
heap retained by the index unchanged (70.5 → 68.4 MB on x/tools); warm tool
latency within run-to-run variation. In-process on x/tools: allocations +1.6% (3.64M → 3.70M), heap
304 → 305 MB. `search_context` warm latency: 0.5 ms / 32 ms / 8 ms / 96 ms on the
four corpora.

Access policy (`.arkignore` and symlinks) — warm latency per tool, median of
9, `.arkignore` integration's first version → this release; the build before
any `.arkignore` enforcement in brackets:

| Corpus | `get_diagnostics` | `list_files` | `search_in_files` | `get_file_content` |
|---|---|---|---|---|
| Ark (1 pattern) | 40.3 → 34.2 ms [26.8] | 112.9 → 87.0 ms [103.3] | 138.8 → 109.6 ms [120.3] | 6.4 → 0.4 ms [0.2] |
| golang.org/x/tools (no `.arkignore`) | 130.6 → 119.6 ms [108.0] | 115.0 → 102.3 ms [90.5] | 92.2 → 69.3 ms [72.8] | 25.3 → 0.4 ms [0.4] |
| x/tools + 300 nested `.arkignore` (900 patterns) | 1,810 → 192 ms [104] | 4,953 → 277 ms [2,491] | 1,447 → 126 ms [733] | 34.0 → 0.5 ms [0.3] |

An index request walks the repository once: the walk that re-reads the rule
files also lists the sources its freshness fingerprint reads (warm
`get_diagnostics` 43.4 → 34.2 ms on Ark, 137.1 → 119.6 ms on x/tools against
two walks). What remains above the build without enforcement is the rule
files' reading and the policy checks; compiling the rules takes microseconds
and is reused while they are unchanged.

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

## Documentation

- `README.md` / `README_ja.md` rewritten around what Ark is for: the
  confidence model, a worked example with real output, language levels,
  measured performance, security and limitations.
- New in `docs/`: `getting-started.md`, `mcp-tools.md` (every tool's
  parameters, generated from the server's schema and checked by a test),
  `resolution-model.md`, `language-support.md`, `cli.md`, `operations.md`,
  `performance.md` (with `docs/scripts/ark-mcp-timing.py` to reproduce the
  measurements), `troubleshooting.md`.
- `SECURITY.md` now states the data boundary (which tools mask secrets), the
  files Ark writes, the HTTP transport's protections and lack of
  authentication, and a cache mitigation that is actually available
  (`--no-cache`).
- Corrected: the comment-stripping flag is `--delete-comment` (`-D`); the dump
  file is `ark-output.<ext>`; generated skills grant 19 of the 21 tools;
  JavaScript class methods are not extracted (previously undocumented).
- `ark --help` describes Ark as a code intelligence engine for AI coding
  agents, states the real dump defaults (`ark-output.txt`, line numbers
  `off`) and links `https://github.com/magicdrive/ark#readme`; the Homebrew
  formula description matches. A test checks the help's defaults against the
  flags.

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
- **No explicit cross-language references yet.** Names resolve only within
  one language (TSX shares TypeScript's); JavaScript emits no module bindings,
  so a JavaScript `import { f } from './util'` of a TypeScript `util.ts` does
  not resolve (it used to, by name coincidence, at Strong).
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
- Python and JavaScript class methods are not extracted; Terraform
  `.tf.json` is not read.
- Secret masking is pattern-based: it misses formats its rules do not know
  (a JSON `"api_key": "…"` member, `Authorization: Bearer …`, passwords in
  connection URLs), masks some ordinary code (`token := next()`), and never
  masks paths or symbol names. It is not data-loss prevention (`SECURITY.md`).
- A source file the dump cannot read stops it with a non-zero exit after part
  of the output file is written (predates v6). An unreadable *ignore* file
  stops it before anything is written.
- With `--allow-external-symlinks on`, walks still do not descend into a
  directory symlink; files below it are readable by naming them. A symlink
  retargeted between the check and the read can be read through (TOCTOU).
- The access policy is checked before files are opened by path: someone who
  can write to the repository, or retarget a symlinked root, while requests
  run can make a request return an excluded file or a file outside the root
  (reproduced; `SECURITY.md`, Symlink policy). It protects files from the
  agent, not from writers of the repository tree.
- `get_file_content`'s `withLineNumbers` argument has no effect (predates
  v6); responses carry no line numbers.
- The extraction cache in `<root>/.ark/index` gains an entry per edited file
  version and is never pruned; deleting it is always safe (predates v6).

## Changelog (v5.0.1 → v6.0.0)

**Added** — `search_context` MCP tool; optional `symbolId` on five target
tools; `symbol_id_collision` diagnostic code; `serverInfo.version` from the
binary; CLI help documents `--no-cache`.

**Changed** — name-based resolution stays within one language (TSX with
TypeScript); Go references resolve by package scoping (no repository-wide
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

**Security** — symlink escapes through tool path arguments are refused, and
walks skip symlinks leading outside the root unless
`mcp-server --allow-external-symlinks on`;
detected secrets are masked in MCP responses by default (tools, resources,
errors); `.arkignore` exclusions are enforced by the MCP server, and the
dump reads ignore rules from the dumped directory with `.arkignore` and
`.gitignore` as separate sources.

**Deprecated / Removed** — none.

**Performance** — no measurable change (see above).

**Internal** — `language.Dialect` / `language.NameSpace`,
`resolver.FileIndex.NameSpace` (set by the index; no cache change);
`index.NewFileIndex` takes the provider instead of a language name;
`language.Extraction.Package` / `PackageScoped`,
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
