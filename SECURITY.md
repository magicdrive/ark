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
- **Rules.** Whole PEM private-key blocks; known token formats (AWS access
  key IDs, GitHub, GitLab, Slack, Stripe, SendGrid and Google keys and
  tokens, JWTs, Azure account keys); and the value of an assignment whose
  key names a secret (`password = "…"`, `API_TOKEN=…`, `api_key: …`).
- **Not detected** (examples): a JSON member such as `"api_key": "…"`, an
  `Authorization: Bearer …` header value, a password inside a connection
  URL, a value whose variable name does not end in a secret keyword
  (`secretHash := "…"`), non-ASCII keys, and any secret format the rules do
  not know.
- **False positives.** The assignment rule also masks ordinary code such as
  `token := next()`; masked responses show less of such code.
- **Analysis is unaffected.** Parsing, the index, resolution, the graph,
  symbol IDs and the extraction cache use the source as written; masking is
  applied only when a response is produced. The cache in `<root>/.ark/index`
  therefore holds unmasked extraction data (symbols and references).

Masking is a safety net, not data-loss prevention. To keep a file away from
an agent entirely, exclude it with `.arkignore`.

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
- **Symlinked root.** A server root (or dump target) given through a
  symlink reads the rules of the directory it leads to.
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

- Checks are made when a request arrives and when a walk reaches an entry.
  They do not guard against the repository being changed concurrently
  (time-of-check/time-of-use): a symlink retargeted between the check and
  the read can be read through.

Mitigation: run Ark in an environment where the repository tree is not
controlled by an untrusted party (e.g., read-only checkout, container
with restricted filesystem).

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
- content hash mismatch → miss;
- provider version mismatch → miss.

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
