# Troubleshooting

Messages are quoted as Ark prints them; `…` stands for the variable part.

## Installation and setup

| Symptom | Cause and fix |
|---|---|
| `ark --version` prints `v1.2.3` | Installed with `go install …@latest`, which resolves to the legacy v1 line. Install with Homebrew, a release archive, or `go install github.com/magicdrive/ark@main` ([Getting started](getting-started.md#go-toolchain)). |
| `ark command "…" not found on PATH` during `setup` | Put `ark` on `PATH`, or pass `--ark-path /full/path/to/ark`. |
| `found a different Ark MCP configuration for …` | An Ark entry exists with other settings. Re-run with `--force` to replace Ark's entry (nothing else is touched). |
| `cannot parse …` / `Ark did not modify the file` | The client configuration is malformed. Fix it by hand; Ark never overwrites a file it cannot parse. |
| ``the Codex CLI (`codex`) was not found on PATH`` | Install Codex, or register Ark by hand: `codex mcp add ark -- ark mcp-server --root /abs/path`. |
| `ark setup copilot-cli` refuses | The repository's `.mcp.json` already defines `ark`, which Copilot CLI prefers over `.github/mcp.json`. Use that entry, or remove it. |
| `configuration … changed while Ark was updating it; no changes were written, retry the command` | Another program wrote the file at the same time. Retry. |
| Permission denied writing the configuration | Fix the file's permissions, or use the other scope (`--global` or project). |

## The agent does not see Ark

1. Fully restart or reload the client; most clients read MCP configuration
   only at startup.
2. Approve the server if the client asks.
3. Check that the client can start `ark`: GUI applications often run with a
   shorter `PATH` than your shell. Configure the absolute path
   (`ark setup <client> --force --ark-path "$(command -v ark)"`).
4. Run the server by hand with the same arguments to see its error
   ([Getting started](getting-started.md#2-check-the-installation)).

## Server errors

| Message | Cause and fix |
|---|---|
| `root directory does not exist: …` / `root is not a directory: …` | `--root` does not name a directory. A relative root is resolved against the directory the client starts the server in. |
| `ark: warning: serving root "…", but CLAUDE_PROJECT_DIR is "…"` | Claude Code started the server in another directory than its project. Pass an absolute `--root`. |
| `path "…" is outside the server root "…"; use a path inside the repository` | Tool paths are relative to the root or absolute inside it. The message shows the root in use. |
| `path "…" resolves through a symlink to "…", outside the server root "…"` | Ark does not follow a symlink out of the repository by default. Serve a root that contains the target, or start the server with `--allow-external-symlinks on` if the repository's symlinks are trusted ([Symlink policy](../SECURITY.md#symlink-policy)). |
| `--allow-external-symlinks "…": must be 'on' or 'off'` | The server option takes `on` or `off`. |
| `indexing …: sources kept changing during 3 builds; retry when they settle` | Files changed during three index builds in a row (a build or code generator is writing). Retry when it is done. |
| `index: … SymbolID collision(s) between distinct declarations …` | Two declarations produced the same 64-bit ID. Index tools are unavailable until one of them changes; file tools keep working. Please report it. |
| `ark: cache disabled (could not create store: …)` | `<root>/.ark/index` cannot be created. The server works without the cache; fix the directory's permissions to restore it. |
| `Forbidden: invalid Host` / `Forbidden: invalid Origin` (HTTP transport) | The HTTP endpoint accepts only loopback hosts and origins. Connect to `localhost` from a local client. |

## Unexpected answers

**`symbol "…" not found in …`**

- The name may be in a file Ark does not index: an unsupported language or
  format (`get_language_support`), or a directory named `vendor`,
  `node_modules` or starting with `.`.
- The declaration may not be extracted: JavaScript and Python class methods,
  Go type aliases and function-local declarations are not symbols
  ([Language support](language-support.md)).
- The `path` argument may index a subdirectory that does not contain it.
- Use `search_context` with part of the name to see what Ark knows.

**`Ambiguous symbol "…" — N matches found`**

Several declarations have the name. Pass one of the listed `symbolId`
values, a qualified name, or `filePattern`. Ark does not choose one.

**No callers, or fewer edges than expected**

An empty edge list is not proof of absence. Check, in the same response:

- `unattributed` and `candidates`: references Ark saw but could not resolve
  to one declaration — for example, a Go method call through a struct field,
  or a TypeScript method call on a receiver without a type annotation;
- `indexDiagnostics`: some files were not fully analyzed;
- the language's support level: JavaScript and Python graph results are not
  certified.

**`parse_error` diagnostics on valid code**

The parser (a pure-Go Tree-sitter runtime) rejects some valid syntax. Ark
retries alternative parse routes and keeps a tree only if it parses cleanly;
the remaining region is reported and not analyzed as written. The rest of the
file is analyzed normally.

**`*****MASKED*****` in source code**

Ark masks what its secret rules detect in MCP responses. The rule for
assignments also matches ordinary code whose variable names end in
`secret`, `password`, `passwd`, `pass`, `pw`, `token`, `api_key`,
`access_key` or `private_key` (`token := next()`, `bypass = true`). Only the
operator can turn masking off — start the server with `--mask-secrets off`
(it logs a warning); the `maskSecrets` argument of a tool call has no
effect.

**`path "…" does not exist` for a file that exists**

The file, or a directory above it, is excluded by an `.arkignore` file;
excluded paths are reported as not existing. Remove the pattern to make the
file available again (the next request sees the change). If paths are
refused with `the .arkignore rules could not be read`, a rule file or a
directory under the root cannot be read; fix its permissions. Listings,
searches and index tools then refuse everything; a named path is refused
when the unreadable rule file or directory is above it.

**Files behind a symlink are missing**

A symlink leading outside the root is skipped by listings, searches and the
index, and refused when named, unless the server runs with
`--allow-external-symlinks on`. Even then, walks do not descend into a
directory symlink; name a path below it instead
([Symlink policy](../SECURITY.md#symlink-policy)).

**The dump leaves out, or includes, files unexpectedly**

The dump reads the `.arkignore` and `.gitignore` files at and below the
directory it dumps — not above it, and not where it is run from — and either
kind excludes (`-a off` leaves `.gitignore` out). A subdirectory dump does not
see its parents' ignore files; dump the parent or add the rules to the
subdirectory ([Ignore rules](cli.md#ignore-rules)). `ignore rules: …` means an
ignore file cannot be read; fix its permissions.

**Answers seem stale**

They should not: every request re-checks the source files' contents and
rebuilds the index if anything changed, and cached extractions are keyed by
file content. If you suspect the cache, delete `<root>/.ark/index` or run the
server with `--no-cache`, and please report the case.

## Reporting a problem

Open an issue at <https://github.com/magicdrive/ark/issues> with the Ark
version (`ark --version`), the client, the tool call and its response, and —
if possible — a small repository that reproduces it. For security issues,
see [SECURITY.md](../SECURITY.md#responsible-disclosure).
