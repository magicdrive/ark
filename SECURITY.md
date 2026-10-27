# Ark Security Model

This document describes the security posture of the Ark code intelligence
engine when analyzing untrusted repositories, and the data boundary between
Ark, the coding agent and the agent's model provider.

## Scope

Ark is a static analysis tool. It reads source files and builds an
in-memory symbol graph. It does NOT:

- execute any code from the analyzed repository;
- invoke build systems, package managers, or compilers of the repository;
- evaluate scripts or configuration files;
- load native plugins or dynamic libraries from the repository;
- open outbound network connections. The binary contains no network
  client; the only listening socket is the optional HTTP transport, bound to
  `localhost` ([HTTP transport](#http-transport)).

The one external program Ark runs is the Codex CLI (`codex mcp add`), and
only for `ark setup codex`.

The following applies to all analysis paths: CLI, MCP server, and
programmatic API.

## Data boundary

Ark returns repository content to the MCP client that calls it: file
contents, declarations' source, search results, context. What the client does
with that content — including sending it to a model provider — is outside
Ark's control and governed by the client's own configuration and terms.
Ark itself sends nothing anywhere.

See [Secret masking](#secret-masking) for what Ark removes from that
content before it leaves.

## Secret masking

Ark masks the secrets its rules detect in everything the MCP server returns:
every tool result, resource read and error message, over stdio and HTTP.
The same rules mask the repository dump (`ark <dir>`).

- **On by default; the operator decides.** `ark mcp-server --mask-secrets
  off` turns it off for the whole server and logs one warning on standard
  error (never on the protocol stream): `WARNING: MCP secret masking is
  disabled. Source content may contain credentials or other sensitive
  information.` A request cannot change the setting: the `maskSecrets`
  argument of `get_file_content` and `get_files_arklite` is accepted and has
  no effect, so neither an agent nor text in the repository steering it can
  unmask what the operator chose to mask.
- **What is masked.** Repository text in responses — source snippets
  (`get_symbol`, `get_context`, `search_context`), file contents, search
  result lines, receiver expressions of references, signatures, messages —
  has each detected secret replaced by `*****MASKED*****`.
- **What is not masked.** File paths, symbol names, symbol IDs, evidence
  text built from them, and Ark's own enumerations (kinds, confidence
  levels, ...) are returned as they are, so a client can pass them back. A
  secret embedded in a file or directory name or an identifier is therefore
  not masked. JSON keys, numbers and structure are never changed.
- **Recommended rules, adjustable.** The rules below are Ark's recommended
  default, each with a stable ID. A repository adjusts them with
  `.ark-secrets-rules.toml` (or `ark-secrets-rules.toml`) at its root: it
  disables built-in rules and adds its own ([Secret masking
  rules](docs/secret-rules.md)). The dump, `ark secrets-scan` and the MCP
  server read it the same way — a regular file at the root, never through a
  symlink — and apply it the same way, with one exception: **the MCP server
  applies the entries that disable built-in rules only when its operator
  starts it with `--secret-rule-overrides on`.** The file is text in the
  repository, which an agent may write; without the operator's flag the
  server leaves those entries out, applies the rest, and logs once which
  entries it left out. Adding rules is never restricted. A configuration
  that cannot be read or compiled is an error, never the recommended rules in
  its place: the dump and the scan fail, and the MCP server replaces every
  result with repository text in it by the error (fail closed) until it is
  fixed; results built only from identifiers still go out, and a failing
  request's error message is withheld rather than masked by fewer rules. No
  error, log or scan output holds a configured pattern's text or a masked
  value.
- **Rules.** Whole PEM private-key blocks; known token formats (AWS access
  key IDs, GitHub, GitLab, Slack, Stripe, SendGrid and Google keys and
  tokens, JWTs, Azure account keys); and the value of an assignment whose
  key names a secret (`password = "…"`, `API_TOKEN=…`, `api_key: …`), the
  key bare, quoted or an index (`"api_key": "…"`, `'password' => '…'`,
  `$cfg['token'] = …`). Secret patterns apply to a line as written, before
  any value in it is masked.
- **No part of a value is left out.** An unquoted value runs to the end of
  its line — `password: correct horse battery` is masked whole, a comment
  after it too — and ends earlier only before a `,` or `;` that ends the
  line or starts another secret-like assignment (`password=a;token=b`). A
  quoted value ends at its closing quote only when a blank, a comment, a
  closing bracket or such a separator follows; a value the quote does not
  end (`"a"b`, `"a" "b"`, `'a''b'`, `"a" + "b"`, an unclosed quote) is
  masked to the end of the line. A value with nothing in it is left as is.
- **Not detected** (examples): an
  `Authorization: Bearer …` header value, a password inside a connection
  URL, a value whose variable name does not end in a secret keyword
  (`secretHash := "…"`), non-ASCII keys, and any secret format the rules do
  not know.
- **References are kept, values are not.** A value that only names another
  value is not a secret and is returned as written: a GitHub Actions context
  reference (`${{ secrets.DEPLOY_TOKEN }}`, `${{ steps.x.outputs.y }}` — a
  context, dot-separated identifiers, nothing else), and, in the
  `get_file_content` or `get_symbol` result of a Terraform `.tf` file, a
  Terraform reference (`var.db_password`, `random_password.x.result`,
  `data.T.N.attr`, `module.M.out`). Whether a file is a `.tf` file is decided
  by the file actually read, after symlinks, never by the name asked for.
  Anything else is masked whole: an expression with a literal, an operator, a
  function or an index (`${{ secrets.A || 'fallback' }}`), a value next to a
  reference, a bare word (`token: abcdefg`, `password = hunter2`). A GitHub
  Actions value runs to the end of its line or to a comment (` #`), so
  nothing after a kept reference is kept with it; a Terraform reference is
  kept only when a comment, a `,` or a closing bracket follows it, otherwise
  the value is masked to the end of the line. Every secret pattern still
  applies to a kept reference's line.
- **Values over several lines are masked on every line.** The body of a
  heredoc (`<<EOT`, `<<-EOT`, `<<~EOT`, `<<<'EOT'`) or of a YAML block scalar
  (`|`, `>-`, …) given to a secret-like key is masked line by line, up to the
  heredoc's terminator or the first line indented no deeper than the key. A
  line break is `\n` or `\r\n`: a value never runs past one, nor ends
  before one. A `␤` in a file is a character of its line, and a value runs
  through it; `get_files_arklite` masks each file before it writes its line
  breaks as `␤`, and masks its result again treating those as line breaks.
- **False positives.** The assignment rule also masks ordinary code such as
  `token := next()`; masked responses show less of such code.
- **Analysis is unaffected.** Parsing, the index, resolution, the graph,
  symbol IDs and the extraction cache use the source as written; masking is
  applied only when a response is produced. The cache in `<root>/.ark/index`
  therefore holds unmasked extraction data (symbols and references).

- **Scanning.** `ark secrets-scan` reports where the rules mask and by
  which rule, with the masking's own matching (`Policy.Find`, the pipeline
  `Policy.Mask` masks by), never the values. A scan without findings is no
  proof that a repository holds no secret. `--include-ignored` also scans
  the files the ignore rules exclude, inside the directory, without
  following a symlink to reach them.
- **Not masked: `ark syntax` and `ark symbol`.** They inspect one file the
  user names, outside the masking; `ark syntax --format json` prints the
  source text of the file's leaf nodes as written.
- **Masking never decides access.** A secret rules configuration changes what
  is masked in output, nothing else: not which files can be read
  (`.arkignore`, root and symlink checks), not the index, symbols, the
  repository map or search results.

Masking is a safety net, not data-loss prevention. To keep a file away from
an agent entirely, exclude it with `.arkignore` — which keeps it out of Ark's
output only: git, editors and agents with other tools still read it.

## File access policy (`.arkignore`)

The MCP server never returns anything from a file the repository's
`.arkignore` files exclude.

- **Rules.** Every `.arkignore` file under the server root, plus files named
  by `--additionally-ignorerule`, with the repository dump's pattern syntax
  (`.gitignore` syntax: a pattern is relative to the directory of its
  `.arkignore`; `dir/` excludes a directory; a leading `/` anchors; `!`
  re-includes what an earlier pattern excluded; matching is case-sensitive).
  A path is excluded when it or a directory above it matches. `.gitignore`
  files play no part.
- **Names as the file system lists them.** The rules match the names the
  file system lists, not the spelling a request uses. On a case-insensitive
  file system (macOS by default, Windows) `SECRET.TXT`, `Secrets/key.txt`
  for `secrets/key.txt`, a Unicode name in another normalization, a Windows
  8.3 short name or a name with trailing dots or spaces reach the same file
  as the listed name and are decided as that name; so is the target of a
  symlink whose text spells it in another case, or reaches the root through
  another spelling of its path. A path whose listed name cannot be
  established — the tree changed during the lookup, two hard links in one
  directory fit a misspelled name, a directory that can be entered but not
  listed, a Windows alternate data stream (`file.txt::$DATA`) — is refused.
  A rule itself must spell a name as the file is listed: a rule in another
  case, or another Unicode normalization, than the file does not match it,
  on any file system.
- **Direct access.** A tool path or `file://` resource naming an excluded
  file or directory, or a symlink that resolves to one, is answered like a
  path that does not exist.
- **Indirect access.** File listings, directory trees, statistics, text
  search, symbol and reference search, and the repository index skip
  excluded files and directories (and symlinks resolving to them). The
  index never reads an excluded file, so no symbol, reference, graph edge,
  context item, repository-map entry, impact entry or diagnostic of it
  exists; the extraction cache is not consulted for it.
- **Updates.** The rule files are read on every request: an edited, added,
  removed or moved `.arkignore` file, or an edited additional rule file,
  applies to the next request, of the same server or a restarted one. One
  request reads each rule file once and uses that version for all its
  decisions. A path a client names is decided by the rule files that can
  apply to it (the root's, those of the directories above it and its own),
  read without walking the repository; walks decide by the whole
  repository's rules. The compiled rules are reused while the rule files'
  paths and SHA-256 contents are unchanged. An index whose file set a
  change alters is rebuilt. Cache entries written before a file was
  excluded stay on disk (and are never served); delete `<root>/.ark/index`
  to remove them.
- **Rule files rewritten while read.** A request never decides by a rule
  file read half-written. Each rule file is read whole between two `fstat`
  calls, twice; both readings must find the same file (device and inode),
  size, modification time, change time (`ctime`) and bytes, the bytes read
  must be the size `fstat` reports, and the second reading must be
  *settled*: the file's change time more than the timestamp granularity
  (100ms; 2.1s on file systems with whole-second times) away from the
  reading itself, so that a change during it would have changed the
  `ctime` it saw — the change time cannot be restored, so a rewrite of the
  same size with the modification time put back (`touch -r`, `os.Chtimes`)
  is still seen. A rule file changed less than that before the request
  delays the request until it is (about 100ms, once); a rule file that
  keeps changing refuses the request with `ignore rules changed while being
  read` rather than deciding by a mix of versions. Not detected, as no
  reader can detect it alone: a single `write` call still copying bytes in
  place, at the same size, through both readings; writes through a shared
  memory mapping; a system clock set back during the reading; network file
  systems whose clock is not this machine's. Tools that rewrite rule files
  should replace them atomically — write a temporary file in the same
  directory and rename it over the rule file — which every reading sees as
  one version or the other.
- **Symlinked root.** A server root (or dump target) given through a
  symlink behaves as the directory it leads to: the same rules, files and
  answers ([Symlink policy](#symlink-policy)).
- **Independent of masking.** `--mask-secrets off` never makes an excluded
  file readable.
- **Failure.** Rules that cannot be read fail closed, even when a compiled
  version of them exists: a tool that walks the repository (listing,
  search, index) refuses every path but the root when any rule file or
  directory under the root cannot be read, and a named path is refused when
  a rule file or directory that could apply to it cannot be read — with
  `the .arkignore rules could not be read, so file access is refused`.

The repository dump reads `.arkignore` the same way — rules from the dumped
directory down, regardless of the working directory and of `.gitignore`
files ([Ignore rules](docs/cli.md#ignore-rules)) — but it is not an access
control: whoever runs it reads the files anyway. Symlinks that lead outside
the root are covered by the [Symlink policy](#symlink-policy).

## Files Ark writes

| What | Where | When |
|---|---|---|
| Extraction cache | `<root>/.ark/index` (mode `0700`) | MCP server, unless `--no-cache` |
| MCP client configuration | the client's configuration file ([Getting started](docs/getting-started.md#3-connect-a-coding-agent)) | `ark setup`, `ark mcp-init` |
| Skills and a Claude Code slash command | the repository's existing skills directory, else `skills/<name>`; `.claude/commands/` | `ark setup claude`, `ark skill` |
| Repository dump | `ark-output.*` in the current directory, or `--output-filename` | `ark <dir>` |

`ark setup` changes only Ark's own entry of a client configuration, never
repairs a file it cannot parse, and replaces files atomically
([safety contract](docs/getting-started.md#safety-contract)).

## Repository boundary enforcement

The MCP server resolves `--root` once, at startup: a relative root is
resolved against the launch working directory, and the result is an
absolute, clean path that must be an existing directory (otherwise the
server exits with an error). Later working-directory changes do not alter
the root.

Every path argument of every MCP tool (and the `file://` / `directory://`
resources) goes through one gate, `resolveToolPath`. Relative paths are
resolved against the root; absolute paths are accepted when they lie inside
it.

**Containment invariant:**

```
resolved_path must satisfy:
  rel, err := filepath.Rel(root, resolved_path)
  err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
```

This check rejects:

- `../` relative escapes;
- `../../` deep relative escapes;
- absolute paths outside root;
- prefix collisions such as `/repo-other` when root is `/repo`.

String-prefix checks (`strings.HasPrefix(path, root)`) are intentionally
**not** used, because they are vulnerable to prefix collisions.

## Symlink policy

**The root itself.** A root given through a symlink (`--root ~/repo-link`,
`ark ~/repo-link`; chains of links too) is the repository it leads to:
listings, searches, the index and the dump walk that directory, and report
paths under the root as given (the logical path), which the `.arkignore`
rules are anchored at. Only the root is followed this way: it does not make
symlinks below it, or `--allow-external-symlinks`, behave differently. The
link is re-resolved for every request, so retargeting it while the server
runs serves the new directory, with its own rules and index. A dangling or
looping root is an error at startup (`root directory does not exist`,
`cannot access root`) or for the dump (`a directory not found`), never an
empty repository.

**Inside the repository.** A symlink whose target lies inside the root
works, and `.arkignore` applies both to the path it is reached by and to
its target: a link cannot expose an excluded file under another name. An
absolute path that reaches the root through another spelling of a
symlinked prefix (for example macOS `/var` vs `/private/var`) is accepted
and reported in the root's own spelling. Index-based tools build their
index over the canonical directory (`canonicalDir`).

**Leading outside the repository: refused by default.** A tool path or
resource that resolves through a symlink to a location outside the root is
refused, and walks (listing, trees, statistics, search, symbol and
reference search, index) skip a symlink whose target lies outside the
root, so its content is neither read nor indexed nor cached.

**`ark mcp-server --allow-external-symlinks on`** lets the server read
through symlinks in the repository that lead outside it. Only the operator
can set it; no request argument changes it, and `--mask-secrets` does not
affect it. With it on:

| Access | Behaviour |
|---|---|
| A file symlink in the repository (`lib.go → /opt/shared/lib.go`) | Read by name and by walks; indexed like any file. |
| A path below a directory symlink (`shared/pub.txt`, `shared → /opt/shared`) | Readable by naming it. |
| A directory symlink named as a tool's path | `get_directory_tree` and `directory://` list below it; `list_files`, `search_in_files` and `get_files_arklite` return nothing below it; index-based tools refuse it (the index covers the root only). |
| Walks from the root | Do not descend into directory symlinks, as before. |
| An absolute or `../` path outside the root, even to a linked target (`/opt/shared/lib.go`) | Refused: only paths inside the root are accepted. |
| `.arkignore` | Applies to the path in the repository the link is reached by (`shared/private/` excludes `shared/private/key.txt`); `.arkignore` files outside the root are not read. |

Each symlink is judged by its own path: if two links lead to the same
outside file and only one is excluded, the other still reads it. Symlink
chains are followed to their final target; a dangling or looping symlink
reads nothing. Turning the setting off again (and restarting) removes the
outside files from the index: they are no longer walked, and extraction
cache entries are looked up only for files a walk admits.

**Risks of turning it on.** Every file reachable through a symlink in the
repository becomes readable by the agent — including anything a
repository author links to (`~/.ssh`, `/etc`). Enable it only for
repositories whose symlinks you trust, and exclude links you do not want
read with `.arkignore`.

**Known limitations:**

- **Fixed in v6.0.0: other spellings of an excluded path.** Before v6.0.0
  (and in v6.0.0 drafts until the fix), the MCP server and the repository
  dump matched the rules against the path as the request or the symlink
  spelled it. On a case-insensitive file system `get_file_content`,
  `get_files_arklite` and `file://` returned an excluded file named in
  another case (`SECRET.TXT` for `secret.txt`, `SECRETS/key.txt` below
  `Secrets/`, `SRC/PRIVATE.TXT` against `src/.arkignore`); a walk started
  from a directory named in another case (`search_in_files`, `list_files`)
  applied no nested `.arkignore` of it; and a symlink whose target was
  spelled in another case (`alias -> SECRET.TXT`, or an absolute link
  through the root's path in another case) exposed its target to walks and
  to the dump. No race was needed. Names are now decided as listed (above).
- **Concurrent changes (time-of-check/time-of-use).** Whoever can change the repository tree,
  or retarget a symlinked root, *while requests run* could win the race
  between the check of a path and its opening by path: a file swapped for
  a symlink was read through — including one outside the root, with
  `--allow-external-symlinks off` — and a root link retargeted during a
  request was read with the rules of the directory it led to before.
  - *Files a client names:* the tools that read a file the client names —
    `get_file_content`, `get_files_arklite`, `file://`, `get_file_info`,
    `get_symbols`, `get_symbol`, `find_references` on a file — and the
    source text index-based tools read back (`get_context` and
    `search_context` snippets, `get_repository_map`'s generated-package
    check) pin the root once per request and decide and read each file in
    one operation, through directory handles: the object read is the object
    the policy admitted, or nothing is returned. `get_files_arklite`
    returns nothing when any of its files is refused. Reproduced before,
    none since: continuous swapping (root link, symlink to an outside file,
    symlink to an excluded file) while requesting.
  - *Walks:* `list_files`, `search_in_files`, `get_directory_tree`,
    `get_project_stats`, `find_symbol`, `find_references` on a directory and
    `directory://` walk the request's pinned root through directory handles
    under the request's policy snapshot (`internal/safewalk`): every entry
    is decided before the tool sees it — an excluded one is not reported,
    name included — and every file is read as the walk decided it (the
    directories and the file reached again, identity by identity), never
    opened by path again. An entry that changes while it is walked is left
    out. Reproduced before (`search_in_files`, `find_symbol`), none since.
  - *The index:* a build reads every source through a walker of its own
    (the root pinned for the build, a snapshot of the rules); a request
    serves an index only when the index's fingerprint — every source's path
    and content — equals the fingerprint of what that request may read,
    computed by a walk under the request's own snapshot. A file a rule
    excludes, or that a race swapped out, changes that fingerprint: the
    index is rebuilt, never served with it. (An index whose post-build check
    began after the request arrived is served without the request's own
    walk: its check used a snapshot taken then.) Reproduced before
    (`search_code`), none since.
  - *The dump:* the target is pinned once and its ignore files captured once
    (`.arkignore`, the additional rule files and, with `-a on`,
    `.gitignore`); the tree it prints and the files it writes are listed and
    read through them (reproduced before, none since).
  - *Special files:* FIFOs, sockets and devices are never opened for reading
    (a FIFO used to block the server or the dump indefinitely).
  - *Within a request,* the `.arkignore` files that apply to a path are
    read once, the first time a decision needs them, and that version
    decides for the rest of the request; an edit made later applies from
    the next request.

  What remains is by design: the policy decides by name, so a writer who
  renames an excluded file to an admitted name, or hard links it there,
  makes it admitted — it is the file at that name. All of this needs write
  access to the repository tree or to the directory holding the root link,
  and the results go to the MCP client the operator runs; Ark itself never
  writes there.

The access policy (`.arkignore`, symlinks) protects files from the agent
and the MCP client. It is not a boundary against someone who can write to
the repository or its root link: run Ark where neither is writable by an
untrusted party (your own checkout, a read-only checkout, a container with
a restricted filesystem).

## HTTP transport

`ark mcp-server --type http` listens on `localhost` only (default port
8522, endpoint `/mcp`). Each request must name a loopback host in its
`Host` header and, when it carries an `Origin` header, a loopback `http(s)`
origin; this blocks DNS-rebinding and cross-origin browser requests. The
endpoint accepts only `POST` with `Content-Type: application/json` and
bounded request bodies, and sends no CORS allow-origin header.

There is **no authentication**: any local process that can connect to the
port can call every tool. Use the default stdio transport unless a client
requires HTTP, and do not run the HTTP transport on a multi-user machine
whose other users must not read the repository.

## Oversized file handling

Ark reads source files into memory for Tree-sitter parsing. There is
currently no per-file size limit. Repositories containing extremely large
files (multi-GB) may exhaust available memory.

Recommended mitigation: configure OS-level memory limits for the Ark
process when analyzing untrusted repositories.

## Cache trust model

Cache entries are stored as JSON files in `<root>/.ark/index`; the location
is not configurable. The cache parser is defensive:

- corrupt JSON → graceful miss, file removed;
- schema version mismatch → graceful miss;
- an entry of another build (found in this build's directory but written by
  another) → miss, file removed;
- content hash mismatch → miss;
- provider version mismatch → miss.

Each build reads only its own directory under `<root>/.ark/index`:
`release/` for published releases, `dev-<SHA-256 of the executable>` for
any other build ([Operations](docs/operations.md#extraction-cache)). The
directory separates builds; it is no secret, so it does not change the
threat below. At start the server deletes, in `<root>/.ark/index`, retired
`dev-*` directories and entries of the layout before build directories; it
deletes nothing when `.ark` or `.ark/index` is a symbolic link.

A maliciously crafted cache file cannot cause Ark to panic (verified by
fuzz testing). It may, however, cause Ark to use wrong extraction results
if an attacker can write files matching the expected cache path — for
example, a repository that ships a prepared `.ark/` directory.

Mitigation: for untrusted repositories, run the server with `--no-cache`,
or delete `<root>/.ark` before serving and make sure no untrusted party can
write to it.

## Output bounds

Ark does not impose hard limits on MCP response sizes. A repository with
very many symbols may produce large responses. MCP clients should apply
their own size limits. Token budgets (`maxTokens`) bound `get_context` and
`search_context` by an estimate, not a hard byte limit.

## Determinism and reproducibility

Ark's analysis is fully deterministic for a given set of source files and
Ark version. It does not call external services, use random seeds, or
depend on wall-clock time for analysis results.

## Responsible disclosure

To report a security issue, please open a GitHub issue with the label
`security` or contact the maintainers directly. Do not disclose
vulnerabilities publicly before coordinating with maintainers.
