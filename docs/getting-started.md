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
| Claude Code | `ark setup claude` | `.mcp.json` (project), `~/.claude/settings.json` (`--global`); also a project skill and slash command |
| Cursor | `ark setup cursor` | `.cursor/mcp.json` (project), `~/.cursor/mcp.json` (`--global`) |
| Codex | `ark setup codex` | Codex's own configuration, through the `codex` CLI (`codex mcp add`) |
| Cline CLI | `ark setup cline` | `~/.cline/mcp.json` |
| GitHub Copilot in VS Code | `ark setup copilot-vscode` | `.vscode/mcp.json` (project only) |
| GitHub Copilot CLI | `ark setup copilot-cli` | `.github/mcp.json` (project only) |

Only these clients have a tested `setup` path. Others can be configured by
hand ([below](#manual-configuration)).

Client-specific notes:

- **Cline:** only the Cline CLI file `~/.cline/mcp.json` is managed. The
  settings of Cline's editor extensions live in editor-specific storage that
  Ark does not touch; configure them by hand.
- **Copilot in VS Code** and **Copilot CLI:** project scope only (`--global`
  is refused). The generated `--root` is the absolute path of the
  repository, so the file is specific to your machine: do not commit it; let
  each developer run `setup`. Neither command configures the GitHub-hosted
  Copilot agent.
- **Copilot CLI** prefers a `.mcp.json` server of the same name over
  `.github/mcp.json`. If the repository's `.mcp.json` already defines `ark`,
  `ark setup copilot-cli` refuses (even with `--force`) and changes nothing.
  Running `ark setup claude` afterwards creates such an entry.

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

### Manual configuration

The server command is always `ark mcp-server --root <repository>`.

Claude Code (`.mcp.json` or `~/.claude/settings.json`):

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
resolves to the project. Use an absolute `--root` in the global file or to
serve another directory.

Cursor (`.cursor/mcp.json`) and Cline CLI (`~/.cline/mcp.json`):

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
tool list. The MCP configuration written by an earlier `setup` keeps working;
re-running `ark setup <client>` prints "Ark is already configured for …
No changes required." when nothing needs to change.

The extraction cache is versioned per language: entries a new version cannot
trust are re-extracted automatically. Deleting `<root>/.ark/index` is always
safe. Version-specific notes are in the release notes under
[`.github/release/`](../.github/release/).

There is no `ark setup` uninstall command: remove the `ark` entry from the
client configuration and delete `<root>/.ark/`.
