# Ark v6.11.1 — Worktree-aware agent guidance

Ark v6.11.1 teaches coding agents to use the Ark MCP server of the repository or Git worktree they are actually working in. An Ark server answers for the one root it was started on; an agent working in a second worktree — a common setup for parallel agents — could take a server on another checkout for the current one and act on answers that do not describe its files. `ark instruction` now tells every supported agent how to check which root its server serves, how to work with one server per worktree without disturbing existing configuration, and what to do when no server for its worktree is available.

Nothing about the server changes: no new tool, option or protocol; the index, the cache and `ark setup` work as in v6.11.0.

## Highlights

### Multi-agent, worktree-aware Ark MCP guidance

`ark instruction <target>` — for `claude`, `codex`, `cursor`, `cline`, `copilot-vscode` and `copilot-cli` alike, from one vendor-neutral source — adds three rules to the existing guidance:

- **Correct MCP root selection.** A server answers for the root it was started on, from that root's files as they are now. Once a session, the agent checks that this root is the repository or worktree it works in (`git rev-parse --show-toplevel`), through what the server itself reports: `get_repository_map` with `format: "json"` and `detail: "minimal"` holds it as `RootPath`.
- **Separate MCP instances for independent worktrees.** To work in another worktree, the agent uses an Ark server started on it (`ark mcp-server --root <worktree>`), registered under a name of its own beside the existing one.
- **No cross-worktree index confusion.** Results of a server started on another checkout describe that checkout, even where they look current; a path outside a server's root is refused. Without a server for its worktree, the agent says so and runs the Ark CLI there (`ark symbol <file>`) or reads the files directly — it does not substitute another checkout's results.

### Safe configuration practices

The guidance tells the agent not to repoint, overwrite or remove a server entry other sessions use, and that a client sees a new or changed entry only after reloading its MCP configuration. [Getting started → Git worktrees](https://github.com/magicdrive/ark/blob/v6.11.1/docs/getting-started.md#git-worktrees) shows, for every client `ark setup` supports, how a worktree gets its own server:

- **Claude Code** (project `.mcp.json`): the entry serves the project the session starts in — start it in the worktree.
- **Cursor, Copilot in VS Code, Copilot CLI** (project files with an absolute root): run `ark setup <client>` in each worktree. A file copied from another checkout is reported as a conflict, never silently kept.
- **User-level entries** (Claude Code and Cursor with `--global`, Cline CLI, Codex): one entry `ark` names one root, so a second worktree gets a second entry under its own name, added by hand; `setup` keeps it and refuses to repoint the first without `--force`.

### Existing Ark-first workflow preserved

The tool-by-question guidance is unchanged, and now also says to read source files directly for implementation details and to use grep or ripgrep to corroborate Ark's answers or where Ark cannot answer. A single-checkout user's agent only adds one cheap root check per session.

## Changed

- `ark instruction` output (all targets) and `misc/CLAUDE.md.example`.
- Documentation: Git worktrees in Getting started; `RootPath` of `get_repository_map`'s JSON in the MCP tool reference; `ark instruction` in the CLI reference.

## Compatibility

- No CLI flag, MCP tool, argument, response field or configuration format was added, removed or renamed; `ark setup` writes what it wrote in v6.11.0.
- Compatibility across supported clients: the guidance names no client, configuration file or client command; the client-specific steps are in the documentation. Configuration generation and setup behavior in a second worktree are tested for all six clients; the clients themselves were not run.
- The cache format is unchanged; existing caches are reused.

## Known Limitations

- An agent follows the guidance as prompt text; nothing switches a client's MCP connection automatically. Adding a server for a worktree is a client configuration change and needs the client to reload it.
- `ark setup` registers one server named `ark` per configuration; a second server in a user-level configuration is added by hand.
- Under a name other than `ark`, Claude Code shows the tools as `mcp__<name>__<tool>`, not the `mcp__ark__` names `ark instruction claude` prints.

## Installation / Upgrade

```sh
brew upgrade ark            # or: brew install magicdrive/tap/ark
# or, without Homebrew (macOS, Linux):
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh               # sh install.sh --upgrade to upgrade
```
