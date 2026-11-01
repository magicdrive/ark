# Ark v6.11.3 — Secure Claude Code MCP configuration recovery

Ark v6.11.3 is a security fix for `ark setup claude --global --force`. It no longer passes an existing Claude Code MCP entry that may hold credentials to another process's command line.

No other behavior changes: no new option, MCP tool or configuration format; the other clients and Claude Code's project scope (`.mcp.json`) are unchanged.

## Security

### `--force` no longer puts a credential-holding entry on a command line

In v6.11.2, `ark setup claude --global --force` replaced a conflicting `ark` entry in Claude Code's user scope by removing it and adding Ark's, and if that failed, it put the previous entry back with `claude mcp add-json --scope user ark '<entry>'`. `claude mcp add-json` accepts an entry only as a command-line argument — it has no standard-input or file option (checked with Claude Code 2.1.316) — and a process's arguments can be read by other users and processes on the same machine (the process list, `/proc/<pid>/cmdline`, process-audit logs). An entry with an `env`, headers, a URL with credentials or any other field could therefore expose them for as long as the restoring call ran.

Ark v6.11.3 never passes such an entry to `claude`:

- `--force` replaces only an `ark` entry of the form Ark writes: `"command"` an `ark` executable, `"args"` `["mcp-server", "--root", <path>]`, and nothing else but `"type": "stdio"` and an empty `"env"`. Such an entry holds nothing beyond the command line `ark mcp-server` itself runs with, so putting it back on failure exposes nothing new.
- Any other existing `ark` entry is treated as secret as a whole. Setup refuses it, with or without `--force`, before it starts `claude`, and changes nothing. The message says why and how to proceed — remove the entry yourself with `claude mcp remove --scope user ark` and re-run, or register Ark under another name — and does not show the entry.
- Just before removing an entry, setup reads the file again. If the entry changed since setup first read it (a running Claude Code, or you), setup stops without changing anything.
- If the replacement and the restoration both fail, setup says so (the entry is then gone, not hidden); Claude's error output for the restoring call is not shown.

Ark itself never writes Claude Code's state file, and no temporary file or environment variable carries an existing entry.

## Changed

- `ark setup claude --global`: a conflict with an `ark` entry not of the form Ark writes is refused even with `--force` (see above). A conflict with an entry of that form behaves as before.
- Documentation: Getting started (Claude Code `--global`, safety contract) and Troubleshooting (the new messages).

## Verified

- Unit and integration tests with a fake `claude` CLI that records its arguments. Dummy credentials are placed in `env`, a bearer header, a URL, unknown fields, extra arguments, a non-Ark command, several places at once and a non-object entry. For each, with every failure (add, verification, remove, restoration, no `claude`) and with and without `--force`, the tests confirm:
  - no dummy value reaches `claude`'s arguments, Ark's error or its output;
  - `claude` is not started;
  - the file, other servers included, is unchanged byte for byte.
- A concurrent change between the read and the remove, and an unreadable file, change nothing.
- Mutations of the new checks are all detected.
- Claude Code 2.1.316: `claude mcp add-json` takes no entry from standard input.

## Known Limitations

- An existing `ark` entry not of the form Ark writes can no longer be replaced with `--force`; remove it with `claude mcp remove --scope user ark` first.
- The check before the remove narrows, but cannot close, the window in which another process can change the file; Claude Code offers no way to remove an entry only if it is unchanged.
- Codex (`ark setup codex --force`) still passes a previous entry's command and arguments to `codex mcp add` when it restores one (never its environment). Codex's configuration is outside this fix.

## Installation / Upgrade

```sh
brew upgrade ark            # or: brew install magicdrive/tap/ark
# or, without Homebrew (macOS, Linux):
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh               # sh install.sh --upgrade to upgrade
```
