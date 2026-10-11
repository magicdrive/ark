# Ark v6.0.0 — release notes (draft)

> Draft for the maintainer. Every number below was measured during the v6.0.0
> release work (macOS x86_64, Go 1.27.1; the release is built with Go
> 1.27.2); nothing here is a general performance or token guarantee. Token counts are Ark's own estimate
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
- **Fixed: excluded files reachable by another spelling of their path (C1).**
  On a case-insensitive file system (macOS by default, Windows) the
  `.arkignore` rules were matched against the path as a request spelled it,
  so `get_file_content "SECRET.TXT"` returned the file `secret.txt` the
  rules exclude — also through `get_files_arklite`, `file://`, a parent
  directory in another case (`SECRETS/key.txt` below `Secrets/`) and nested
  rules (`SRC/PRIVATE.TXT`); walks started from a directory named in another
  case (`search_in_files`, `list_files`) skipped its nested rules; and a
  symlink whose text spelled its target in another case exposed the target
  to walks and to the dump. No race was needed (v5.0.1 and earlier v6.0.0
  drafts). Paths are now decided by the names the file system lists — each
  component renamed, through directory handles, to the entry whose identity
  the lookup reached (`internal/fsroot`) — and the tool is handed that path;
  symlink targets in walks and in the dump are decided the same way. Paths
  whose listed name cannot be established (ambiguous hard links, Windows
  alternate data streams, names only the Windows path API accepts) are
  refused. The rules stay case-sensitive; outputs for paths spelled as
  listed are unchanged (compared over four corpora). The broader
  time-of-check/time-of-use limitation below remains.
- **Walks, the index and the dump read what the policy decided.** Listings,
  searches, trees, statistics, symbol and reference search, `directory://`,
  the repository index and the dump walked by path and opened each file by
  path after the policy check: a writer swapping a file for a symlink (to
  an excluded file, outside the root) or retargeting the root during a walk
  could make `search_in_files`, `find_symbol`, `search_code` or the dump
  return the other file (reproduced). They now walk the pinned root through
  directory handles under a policy snapshot (`internal/safewalk`) — an
  entry is decided before it is reported, a file read as decided — and an
  index is served only when its fingerprint equals what the request may
  read under its own snapshot (the same swapping: no leak). FIFOs and other
  special files are no longer opened (a FIFO blocked the server and the
  dump indefinitely). Outputs for unchanging repositories are unchanged,
  with one deliberate exception: in the dump's tree drawing, an entry the
  ignore rules exclude no longer counts when drawing its siblings — the
  last entry shown is drawn `└──` even when an excluded entry follows it
  (it was drawn `├──`, which showed that something hidden came after).
- **Files a client names are decided and read in one operation.** The path
  gate checked a path, then the tool opened it by path: a writer swapping a
  file for a symlink, or retargeting a symlinked root, during a request
  could make `get_file_content`, `get_files_arklite`, `file://`,
  `get_symbols` or `get_symbol` return an excluded file or a file outside
  the root (reproduced: hundreds of leaks in seconds of continuous
  swapping). These tools, `get_file_info`, `find_references` on a file, and
  the source text index-based tools read back (`get_context` and
  `search_context` snippets, `get_repository_map`'s generated-package
  check) now pin the root once per request and read each file through
  directory handles under a policy snapshot of that request, which reads
  only the `.arkignore` files that apply to the paths decided: the file read
  is the file admitted, or nothing is returned (the same swapping: none).
  `get_files_arklite` refuses the whole call when one of its files is
  refused while being read. Walks, the index and the dump are not migrated
  yet (Known limitations).
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

## Performance

Measured on the final build against the build before its performance work
(Phase 3-D), on one machine (macOS x86_64, Intel Xeon W-2140B, APFS; Go
1.27.1), alternating the two builds; medians of five runs unless stated.
Corpora: synthetic Go repositories (medium: 1,025 source files; large:
about 5,000) and Ark's own repository. These are Ark's own timings — they
say nothing about a model's inference time or token use.

| What | Before | After |
|---|---|---|
| Replayed code-investigation session (Ark's repository; 6 tasks, 22 tool calls, a new server per session) | 25.0 s | 14.3 s (−43%) |
| Warm `find_symbol` (medium) | 3.13 s | 42 ms |
| Warm `find_references` (medium) | 1.66 s | 43 ms |
| First index build, extraction cache off (medium) | 2.13 s | 0.67 s |
| First index build, extraction cache off (large) | 12.0 s | 4.7 s |
| Warm index-based tools (`search_code`, `get_callers`, `get_context`, …) | | 4–16% faster, by repository shape |
| `get_repository_map` | | 47–60% faster |

Where it comes from: files are read with fewer system calls, a file a walk
has just examined is read against the identity the walk found, a request
reads index snippets through its own freshness walk, `find_symbol` and
`find_references` keep their parse results by content, and the index build
extracts sources on four workers. Tool output is unchanged (below).

Trade-offs and limits:

- **Peak memory during the first index build is higher.** Extraction runs in
  parallel (four workers, at most 256 KB of source in flight), and each
  worker holds a parser's memory while it parses. Process peak RSS during a
  first build, extraction cache off: Ark's repository 236 MB → about
  380 MB; a deep synthetic repository 150 MB → about 180 MB; a repository
  of large files (18 files of about 250 KB) 333 MB → about 420 MB. The heap
  the server keeps after the build is unchanged.
- **The first `find_symbol` / `find_references` call still parses every
  file** (seconds on a repository of several hundred files); the speed-up
  above is for later calls, whose files have not changed.
- **Warm index requests still read and hash every source file**: an index
  is reused only after a walk under the request's own access policy finds
  every source's content unchanged — the freshness check is not relaxed for
  speed.
- The parse memo is bounded by the bytes of source it holds results for
  (64 MB); the results themselves take several times that.

Validation of the performance work: the MCP output of the final build and of
the build before it compared over 6 corpora, 21 tools and 9,688 calls (cold
and warm servers, error paths included) — no difference.

Go resolution (measured before the filesystem hardening; the resolver is
unchanged since): Go package scoping cut resolver time from 1.31 s to 0.19 s
on golang.org/x/tools (268 → 54 ms on Ark), with the heap retained by the
index unchanged.

`docs/performance.md` describes how to measure; its figures predate the
filesystem hardening and the performance work above.

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
- `.arkignore` rules match names as the file system lists them; a rule must
  spell a name as the file is listed (case, Unicode normalization) to
  exclude it. A misspelled path to a file with two hard links in one
  directory, or below a directory that can be entered but not listed, is
  refused on a case-insensitive file system.
- Within one request (and one index build, one dump), the `.arkignore`
  files that apply are read once; an edit made while it runs applies from
  the next one.
- The access policy decides by name: someone who can write to the
  repository can still rename an excluded file to an admitted name, or hard
  link it there, and it is then admitted — it is the file at an admitted
  name. The policy protects files from the agent, not from writers of the
  repository tree (`SECURITY.md`).
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

**Performance** — faster warm tools, `find_symbol` / `find_references`
on unchanged files and first index builds; higher peak memory during a
first index build (see Performance).

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

Final build: filesystem security and regression tests on macOS; CI on the
performance commit (run #180) green — the Linux test job (`go test`, race,
staticcheck, fuzz smoke) and the `filesystem-security` jobs on macOS and
Windows; the MCP differential above (9,688 calls); an independent security
review of Phases 3-A…3-D before tagging; `govulncheck` with the release's
Go (1.27.2): no known vulnerability.
