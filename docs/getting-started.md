# Getting started

This guide installs Ark, connects it to a coding agent, and walks through a
first session. It takes about five minutes.

1. [Install](#1-install)
2. [Check the installation](#2-check-the-installation)
3. [Connect a coding agent](#3-connect-a-coding-agent)
4. [Teach the agent to use Ark](#4-teach-the-agent-to-use-ark)
5. [First session](#5-first-session)
6. [Upgrade](#6-upgrade)

## 1. Install

### Homebrew (macOS, Linux)

```bash
brew install magicdrive/tap/ark
```

### Release archive

Download an archive for your platform from
[GitHub Releases](https://github.com/magicdrive/ark/releases), verify it
against `ark_checksums.txt`, and put `ark` on your `PATH`. Archives are built
for Linux and Windows (386, amd64, arm64) and macOS (amd64, arm64), without
CGO; each contains shell completions under `completions/`.

### Go toolchain

```bash
go install github.com/magicdrive/ark@main
```

Install from `main`, not `@latest`. Ark's module path,
`github.com/magicdrive/ark`, has no major-version suffix, so the Go module
proxy treats the v5 and v6 tags as incompatible and resolves `@latest` to the
legacy v1.2.3. A binary installed from `@main` reports a pseudo-version such
as `v0.0.0-20261008160233-f5b70445d208`.

## 2. Check the installation

```bash
ark --version
```

Release builds print `ark version v6.0.0`. To check the MCP server without an
agent, send it two requests on standard input; it answers each and exits at
the end of input:

```bash
cd /path/to/your/repository
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_repository_map","arguments":{"path":"."}}}' \
  | ark mcp-server --root . --no-cache
```

The second line of output contains the repository map.

## 3. Connect a coding agent

Run `ark setup <client>` once in the repository root, then restart or reload
the client and approve the Ark server when it asks.

```bash
cd /path/to/your/repository
ark setup claude
```

| Client | Command | Configuration written |
|---|---|---|
| Claude Code | `ark setup claude` | `.mcp.json` (project); the user scope in `~/.claude.json` (`--global`, through the `claude` CLI: `claude mcp add-json --scope user`); also a project skill and slash command |
| Cursor | `ark setup cursor` | `.cursor/mcp.json` (project), `~/.cursor/mcp.json` (`--global`) |
| Codex | `ark setup codex` | Codex's own configuration, through the `codex` CLI (`codex mcp add`) |
| Cline CLI | `ark setup cline` | `~/.cline/data/settings/cline_mcp_settings.json` (or where `CLINE_MCP_SETTINGS_PATH`, `CLINE_DATA_DIR` or `CLINE_DIR` puts it) |
| GitHub Copilot in VS Code | `ark setup copilot-vscode` | `.vscode/mcp.json` (project only) |
| GitHub Copilot CLI | `ark setup copilot-cli` | `.github/mcp.json` (project only) |

Only these clients have a tested `setup` path. Others can be configured by
hand ([below](#manual-configuration)).

Client-specific notes:

- **Cline:** only the Cline CLI's MCP settings file is managed, found as the
  CLI finds it: `$CLINE_MCP_SETTINGS_PATH`, else
  `$CLINE_DATA_DIR/settings/cline_mcp_settings.json`, else
  `$CLINE_DIR/data/settings/cline_mcp_settings.json`, else
  `~/.cline/data/settings/cline_mcp_settings.json` (home is `$HOME`, then
  `%USERPROFILE%`, as for Cline). A relative value of one of these variables
  is refused. If you start Cline with `--config <dir>` or `--data-dir <dir>`,
  set `CLINE_DIR` or `CLINE_DATA_DIR` to the same directory when you run
  `ark setup cline`. An entry in the nested form `cline mcp add` writes
  (`"transport": {"type": "stdio", …}`) is recognized as the same server.
  The settings of Cline's editor extensions live in editor-specific storage
  that Ark does not touch; configure them by hand.
- **Claude Code `--global`** registers Ark in Claude Code's user scope, the
  top-level `mcpServers` of `~/.claude.json`
  (`$CLAUDE_CONFIG_DIR/.claude.json` when that is set). That file is Claude
  Code's own, so Ark never writes it: it reads it to decide whether anything
  needs doing and changes it only by running `claude mcp add-json --scope
  user ark …` (and `claude mcp remove --scope user ark` for `--force`). The
  `claude` CLI must be on `PATH`; without it, setup prints the command to run
  yourself. If `~/.claude.json` is malformed, a symlink, not a regular file
  or larger than 16 MiB, setup stops without starting `claude` (Claude Code
  would replace a malformed file with a default one and drop your other
  servers). The user-scope entry names the repository's absolute path. The
  project scope (`.mcp.json`) is unaffected.
- **Copilot in VS Code** and **Copilot CLI:** project scope only (`--global`
  is refused). The generated `--root` is the absolute path of the
  repository, so the file is specific to your machine: do not commit it; let
  each developer run `setup`. Neither command configures the GitHub-hosted
  Copilot agent.
- **Copilot CLI** prefers a `.mcp.json` server of the same name over
  `.github/mcp.json`. If the repository's `.mcp.json` already defines `ark`,
  `ark setup copilot-cli` refuses (even with `--force`) and changes nothing.
  Running `ark setup claude` afterwards creates such an entry.

### Configuration written by earlier versions

Up to v6.11.1, two `setup` targets were files their client does not read,
so those entries never took effect:

- `ark setup claude --global` and `ark mcp-init --global` wrote
  `~/.claude/settings.json`. Claude Code does not read MCP servers from it.
- `ark setup cline` wrote `~/.cline/mcp.json`. No Cline version reads it.

Re-run `ark setup claude --global` or `ark setup cline` to configure the file
the client reads. Ark never changes or deletes the old files; each run
reports what it finds there (an `ark` entry, other servers, or a file it
could not inspect). Delete the `ark` entry, or the whole old file, by hand.
Servers you added to `~/.cline/mcp.json` yourself were not loaded either:
move them to Cline's MCP settings file. Other servers in
`~/.claude/settings.json` can be added with `claude mcp add-json --scope
user`. The project scope of Claude Code (`.mcp.json`) and the other clients
are unchanged. `ark mcp-init --global` is now refused and writes nothing; use
`ark setup claude --global`.

### Safety contract

`ark setup` only ever touches Ark's own entry:

- **Idempotent.** Running it again with the same options changes nothing and
  does not rewrite the file.
- **Conflict-safe.** If an Ark entry exists with different settings, setup
  stops and asks for `--force`. `--force` replaces Ark's entry only.
- **Preserving.** Other MCP servers and unknown fields are kept.
- **Never repairs.** A malformed configuration file is reported, never
  overwritten, even with `--force`.
- **Atomic.** Files are replaced through a temporary file and rename, with a
  check that nobody modified the file in between and a read-back after
  writing.
- **Client-owned files through the client.** Codex and Claude Code's user
  scope are changed only through their own CLI (`codex mcp`, `claude mcp`),
  with the result read back; a failed `--force` replacement restores the
  previous entry.

### Manual configuration

The server command is always `ark mcp-server --root <repository>`.

Claude Code, project scope (`.mcp.json`):

```json
{
  "mcpServers": {
    "ark": {
      "type": "stdio",
      "command": "ark",
      "args": ["mcp-server", "--root", "${CLAUDE_PROJECT_DIR:-.}/"],
      "env": {}
    }
  }
}
```

Claude Code starts the server in the project directory, so the argument
resolves to the project. Use an absolute `--root` to serve another directory.

Claude Code, user scope (every project; through the CLI, not by editing
`~/.claude.json`):

```bash
claude mcp add-json --scope user ark '{"type":"stdio","command":"ark","args":["mcp-server","--root","/abs/path/to/repo"],"env":{}}'
```

Cursor (`.cursor/mcp.json`) and Cline CLI
(`~/.cline/data/settings/cline_mcp_settings.json`):

```json
{ "mcpServers": { "ark": { "command": "ark", "args": ["mcp-server", "--root", "/abs/path/to/repo"], "env": {} } } }
```

GitHub Copilot in VS Code (`.vscode/mcp.json`):

```json
{ "servers": { "ark": { "type": "stdio", "command": "ark", "args": ["mcp-server", "--root", "/abs/path/to/repo"], "env": {} } } }
```

Codex:

```bash
codex mcp add ark -- ark mcp-server --root /abs/path/to/repo
```

Add `.ark/` to the repository's `.gitignore`: the server keeps its cache in
`<root>/.ark/index` ([Operations](operations.md#extraction-cache)).

To keep files away from the agent altogether — credentials, generated or
vendored code — list them in an `.arkignore` file (`.gitignore` syntax) at
the repository root or in any directory below it. The MCP server never reads,
lists, searches or indexes them
([SECURITY.md](../SECURITY.md#file-access-policy-arkignore)):

```gitignore
secrets/
config/*.local.yaml
.env
```

### Git worktrees

A server serves the one root it was started on, from its files as they are
now, and refuses paths outside it: a server started on one checkout does not
see a sibling worktree's files or uncommitted changes. To work in several Git
worktrees, give each one a server of its own; each keeps its cache in its own
`<worktree>/.ark/index`. The guidance of `ark instruction` tells the agent to
check the root its server reports against the worktree it works in — the
directory of a top-level file given by its relative name to
`get_files_arklite`, or else `RootPath` of `get_repository_map` with
`path: "."`, `format: "json"` and `maxPackages: 1`, compared as paths with symlinks and letter case resolved — and
not to use another checkout's results. Nothing
switches a client's server automatically, and the agent is told to ask you
before any MCP configuration changes.

| Client | A server per worktree |
|---|---|
| Claude Code (project) | The `.mcp.json` entry serves the project Claude Code starts in: start the session in the worktree (with the file committed, copied, or written there by `ark setup claude`). A session started in another checkout keeps that checkout's server. |
| Cursor, Copilot in VS Code, Copilot CLI | The project file holds the absolute `--root` of the directory `setup` ran in: run `ark setup <client>` at the top of each worktree and open that folder. A file committed or copied from another checkout names that checkout: `setup` reports the conflict, and `--force` replaces Ark's entry in this worktree's file only. |
| Claude Code and Cursor (`--global`), Cline CLI, Codex | The user-level entry `ark` names one root: `setup` in a second worktree stops on the conflict, and `--force` would repoint every session that uses it. Add a second server under its own name, with the same command and the worktree's absolute `--root`, through the client (Codex: `codex mcp add ark-feature -- ark mcp-server --root /abs/path/to/worktree`; Claude Code: `claude mcp add-json --scope user ark-feature '{"type":"stdio","command":"ark","args":["mcp-server","--root","/abs/path/to/worktree"]}'`) or by hand; `setup` keeps entries of other names. |

Some clients have a per-project scope `setup` does not write, which gives a
worktree its own `ark` entry without a second name: Claude Code's local scope
(`claude mcp add --scope local …`, run in the worktree) and Codex's project
`.codex/config.toml` (trusted projects) — see the client's documentation.
Reload or restart the client after adding or changing a server. A client
that prefixes tool names with the server's name shows a second server's
tools under that name (Claude Code: `mcp__ark-feature__find_symbol`), not
the `mcp__ark__` names `ark instruction claude` prints.

A worktree placed **inside** the repository (`.worktrees/feature`, or a
client's own worktree directory such as `.claude/worktrees/`) is part of the
root's files: the root's server indexes and reads it (`.gitignore` does not
change that), so its declarations show up beside the root's. Keep worktrees
outside the repository, or list their directory in the root's `.arkignore`:

```gitignore
.worktrees/
.claude/worktrees/
```

## 4. Teach the agent to use Ark

`setup` makes the tools available; the agent still has to know when to use
them. `ark instruction <target>` prints short Markdown guidance — which tool
answers which question and how to treat uncertain results — for you to add to
the agent's instruction file:

```bash
ark instruction claude >> CLAUDE.md        # review the file first
ark instruction codex  >> AGENTS.md
mkdir -p .clinerules && ark instruction cline > .clinerules/ark.md
ark instruction copilot-vscode >> .github/copilot-instructions.md
```

For agents that use skills, `ark skill` generates richer task guidance
([CLI reference](cli.md#skill)).

## 5. First session

Ask the agent questions that need structure, not text search:

- "Give me an overview of this repository." → `get_repository_map`
- "Where is the order placement logic?" → `search_context`
- "Who calls `Place`, and what breaks if I change `store.Save`?" →
  `get_callers`, `analyze_change_impact`
- "Show me what I need to safely modify `Place`." → `get_context`

Read the answers with their confidence: `exact` and `strong` edges are what
Ark could establish; `candidates` and `unattributed` counts mark what it could
not ([Resolution model](resolution-model.md)).

The first graph query builds the index; on large repositories this takes
seconds ([Performance](performance.md)). Later queries reuse it.

## 6. Upgrade

Upgrade the binary the way you installed it (`brew upgrade ark`, a new
archive, or `go install …@main`), then restart the agent so it reloads the
tool list. The MCP configuration written by an earlier `setup` keeps working
(except the Claude Code `--global` and Cline files of earlier versions, which
their clients never read; see
[Configuration written by earlier versions](#configuration-written-by-earlier-versions));
re-running `ark setup <client>` prints "Ark is already configured for …
No changes required." when nothing needs to change.

The extraction cache is versioned per language: entries a new version cannot
trust are re-extracted automatically. Deleting `<root>/.ark/index` is always
safe. Version-specific notes are in the release notes under
[`.github/release/`](../.github/release/).

There is no `ark setup` uninstall command: remove the `ark` entry from the
client configuration and delete `<root>/.ark/`.
