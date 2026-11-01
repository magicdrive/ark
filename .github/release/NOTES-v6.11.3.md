# Ark v6.11.3 — Secure Claude Code MCP configuration recovery

Ark v6.11.3 is a security fix for `ark setup claude --global --force`. It no longer passes an existing Claude Code MCP entry that may hold credentials to another process's command line.

No new option, MCP tool or configuration format; the other clients and Claude Code's project scope (`.mcp.json`) are unchanged. What changes for Claude Code `--global` is listed under [Changed](#changed).

## Security

### `--force` no longer puts a credential-holding entry on a command line

In v6.11.2, `ark setup claude --global --force` replaced a conflicting `ark` entry in Claude Code's user scope by removing it and adding Ark's, and if that failed, it put the previous entry back with `claude mcp add-json --scope user ark '<entry>'`. `claude mcp add-json` accepts an entry only as a command-line argument — it has no standard-input or file option (checked with Claude Code 2.1.316) — and a process's arguments can be read by other users and processes on the same machine (the process list, `/proc/<pid>/cmdline`, process-audit logs). An entry with an `env`, headers, a URL with credentials or any other field could therefore expose them for as long as the restoring call ran.

Ark v6.11.3 never passes such an entry to `claude`:

- `--force` replaces only an `ark` entry of the form Ark writes: `"command"` an executable named `ark` or `ark.exe` (or the one `--ark-path` names), `"args"` exactly `["mcp-server", "--root", <path>]`, and nothing else but `"type": "stdio"` and an `"env"` of `{}`. Such an entry holds nothing beyond the command line `ark mcp-server` itself runs with, so putting it back on failure exposes nothing new.
- Any other existing `ark` entry is treated as secret as a whole. Setup refuses it, with or without `--force`, before it starts `claude`, and changes nothing. The message says why and how to proceed — remove the entry yourself with `claude mcp remove --scope user ark` and re-run — and does not show the entry.
- Just before removing an entry, setup reads the file again. If the entry changed since setup first read it (a running Claude Code, or you), setup stops without changing anything.
- If the replacement and the restoration both fail, setup says so (the entry is then gone, not hidden).
- Claude's error output is shown only when it is one line of one of its known short messages (an entry already exists, no such entry, an invalid configuration). Claude reports a malformed configuration file with the text it could not parse, which may hold a credential of any server; such output is not shown.

Ark itself never writes Claude Code's state file, and no temporary file or environment variable carries an existing entry.

## Changed

- `ark setup claude --global`: a conflict with an `ark` entry not of the form Ark writes is refused even with `--force` (see above), and the conflict message no longer shows such an entry's command and arguments. This includes entries that worked with `--force` before: one with an extra argument (`--no-cache`), `--root=<path>`, a command under another name than `ark` written with an `--ark-path` other than the one given now, or `"env": null`. A conflict with an entry of the form Ark writes behaves as before.
- Claude's error output from `claude mcp add-json` and `claude mcp remove` is shown only for its known short messages (see above).
- Documentation: Getting started (Claude Code `--global`, safety contract) and Troubleshooting (the new messages).

## Verified

- Unit and integration tests with a fake `claude` CLI that records its arguments. Dummy credentials are placed in `env`, a bearer header, a URL, unknown fields, extra arguments, a non-Ark command, several places at once and a non-object entry. For each, with every failure (add, verification, remove, restoration, no `claude`) and with and without `--force`, the tests confirm:
  - no dummy value reaches `claude`'s arguments, Ark's error or its output;
  - `claude` is not started;
  - the file, other servers included, is unchanged byte for byte.
- A concurrent change between the read and the remove (including a file that became malformed), and an unreadable file, change nothing.
- Mutations of the new checks, made in the implementation and by three independent audits, are detected by the tests; the two that first survived (the `mcp-server` argument check, a failed reread) are covered now. One that survives (the reread comparing normalized instead of exact entries) is equivalent: the entry put back is the one first read either way.
- Claude Code 2.1.316: `claude mcp add-json` takes no entry from standard input.

## Known Limitations

- An existing `ark` entry not of the form Ark writes can no longer be replaced with `--force`; remove it with `claude mcp remove --scope user ark` first.
- The check before the remove narrows, but cannot close, the window in which another process can change the file; Claude Code offers no way to remove an entry only if it is unchanged. If another process writes a different `ark` entry within that window (well under a second), `claude mcp remove` removes it, and setup reports success, or "restored" with the entry it first read; the other entry is not kept (Claude Code keeps no copy of a removed entry). Nothing of it reaches a command line or Ark's output.
- The conflict messages of the other clients and of Claude Code's project scope still show an existing entry's command and arguments.
- Codex (`ark setup codex --force`) still passes a previous entry's command and arguments to `codex mcp add` when it restores one (never its environment). Codex's configuration is outside this fix.

## Installation / Upgrade

```sh
brew upgrade ark            # or: brew install magicdrive/tap/ark
# or, without Homebrew (macOS, Linux):
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh               # sh install.sh --upgrade to upgrade
```
