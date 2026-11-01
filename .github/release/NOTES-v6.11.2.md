# Ark v6.11.2 — Setup for the files Claude Code and Cline read

Ark v6.11.2 fixes two `ark setup` targets that wrote configuration their client never reads: `ark setup claude --global` (and `ark mcp-init --global`) and `ark setup cline`. If you used either, re-run it after upgrading — see [Migration](#migration). It also makes the linear-time tests of several language providers reliable on loaded CI runners, without weakening what they detect.

No MCP tool, argument or response changes; the index and the cache are unchanged.

## Fixed

### `ark setup claude --global` registers Ark where Claude Code reads it

Claude Code reads user-scoped MCP servers from the top-level `mcpServers` of `~/.claude.json` (`$CLAUDE_CONFIG_DIR/.claude.json` when that is set; an older, non-empty `.config.json` in `~/.claude` or `$CLAUDE_CONFIG_DIR` instead when present — Ark follows Claude Code there too). Ark used to write `~/.claude/settings.json`, which Claude Code does not read MCP servers from, so the entry never took effect.

`ark setup claude --global` now registers Ark through the official CLI — `claude mcp add-json --scope user ark …`, and `claude mcp remove --scope user ark` first for `--force` — and reads `~/.claude.json` back to confirm the entry. Ark never writes `~/.claude.json` itself: the file also holds Claude Code's account and project state.

- An equivalent entry is left alone, and `claude` is not started.
- A different `ark` entry is a conflict; `--force` replaces it, and puts the previous entry back if the replacement fails.
- If `~/.claude.json` is empty, malformed, a symlink, not a regular file or over 16 MiB, setup stops without starting `claude`: Claude Code replaces a configuration it cannot read with a default one, which would drop your other servers.
- The `claude` CLI must be on `PATH`. Without it, setup changes nothing and prints the command to run yourself. With `CLAUDE_CODE_CUSTOM_OAUTH_URL` set, Claude Code keeps its state in another file; setup then only prints the command.
- The entry names the repository by its absolute path, as before.

The project scope (`ark setup claude`, `.mcp.json`) and the Ark skill are unchanged.

### `ark setup cline` writes Cline's MCP settings file

Ark used to write `~/.cline/mcp.json`, which no Cline version reads. `ark setup cline` now writes the file the Cline CLI reads, found the way Cline finds it: `$CLINE_MCP_SETTINGS_PATH`, else `$CLINE_DATA_DIR/settings/cline_mcp_settings.json`, else `$CLINE_DIR/data/settings/cline_mcp_settings.json`, else `~/.cline/data/settings/cline_mcp_settings.json`. A relative value of these variables is refused, since Cline resolves it against its own working directory. If you start Cline with `--config` or `--data-dir`, set `CLINE_DIR` or `CLINE_DATA_DIR` to the same directory when you run setup.

An entry in the nested form Cline's own commands write (`{"transport": {"type": "stdio", …}, "disabled": false}`) is recognized as equivalent to the flat form Ark writes. A relative home directory is refused, as a relative variable is. The safety contract is unchanged: no symlinks, a malformed file is never overwritten, the write is atomic, and other servers and unknown fields are kept.

### `ark mcp-init --global` is refused

It wrote `~/.claude/settings.json` too. It now writes nothing and points to `ark setup claude --global`. `ark mcp-init` without `--global` is unchanged.

## Migration

Only users of `ark setup claude --global`, `ark mcp-init --global` or `ark setup cline` are affected; those entries were never loaded. After upgrading:

1. Re-run `ark setup claude --global` or `ark setup cline` in the repository it should serve.
2. Each run reports what it finds in the old file — an `ark` entry, other servers, or a file it could not inspect — and leaves the file unchanged. Delete the `ark` entry (or the old file) by hand.
3. Servers you added to `~/.cline/mcp.json` yourself were not loaded either: move them to Cline's MCP settings file. Other servers in `~/.claude/settings.json` can be added with `claude mcp add-json --scope user`.

[Getting started → Configuration written by earlier versions](https://github.com/magicdrive/ark/blob/v6.11.2/docs/getting-started.md#configuration-written-by-earlier-versions) has the details. Cursor, Codex, Copilot in VS Code and Copilot CLI are unchanged.

## CI reliability

- The linear-scaling tests of the Java, Rust and Zig providers, the parser probe and XML now measure the two input sizes alternately and fail only when the time exceeds its bound in each of three attempts (`internal/testutil/scaling`). A burst of load on a shared runner no longer fails them; a quadratic cost exceeds the bound in every attempt. The bounds and inputs are unchanged: ten times the time for four times the input, and for XML three times the time plus 20 ms for twice the input.
- Java extraction and resolution also bound the ratio of their allocations, which does not depend on load.
- The CSS reader and tokenizer count their work, and `TestReaderStepsLinear` bounds the steps per token and the decodes per byte on the worst-case stylesheets — a deterministic check that catches a quadratic reading the previous 10-second bound could not. The CSS extractor's own scans are not counted; the 10-second bound, which covers them and catches a hang, now applies without the race detector only.

The counters add one integer increment per step; extraction results are unchanged.

## Verified

- `ark setup claude --global` with Claude Code 2.1.316 in a temporary home: the entry is listed as "User config" and connects; a second run changes nothing; a conflict, `--force`, `CLAUDE_CONFIG_DIR`, a malformed `~/.claude.json` and other servers in it behave as described above.
- `ark setup claude --global` with an older `~/.claude/.config.json`: Ark registers the entry there, where Claude Code reads it, and `~/.claude.json` is not created.
- `ark setup cline` with the Cline CLI 3.0.74: Cline reads the entry Ark writes, and reads an entry in the nested form that Ark treats as equivalent. The search order of the settings file and the form `cline mcp add` writes were read from the CLI's source; `CLINE_MCP_SETTINGS_PATH` is not in Cline's documentation.

## Known Limitations

- `ark setup claude --global` needs the `claude` CLI; without it, run the printed command yourself.
- `cline mcp add` itself was not run (it needs a terminal); the form it writes was read from the CLI's source.
- When `--force` replaces a Claude Code entry and fails, the previous entry, `env` included, is put back with `claude mcp add-json`, which takes it as a command-line argument; other users of the machine may see the arguments of a running process. Claude's error output of that call is not shown.
- The command setup prints for you to run is quoted for a POSIX shell. On Windows, run it in a POSIX shell or adapt the quoting. With an npm installation of Claude Code or Codex on Windows (`claude.cmd`, `codex.cmd`), a repository path containing `&`, `|`, `<`, `>`, `^` or `%` may not reach the CLI intact; use a path without them, or run the printed command yourself.
- Windows was tested in CI only, not with the clients themselves.
- A linear-time test detects a quadratic cost only where, on the smaller input, that cost is large enough against the input's other costs (at least the linear part, and more where a cost does not grow with the input); a quadratic step that allocates nothing and is cheaper than that is not detected, as before. In three of the four XML cases the extraction stops at its symbol limit on both inputs, so their time grows little with the input; the parser's quadratic cost is still detected (one long line).
- A test with a quadratic regression now takes about three times as long to fail, as every attempt is measured.

## Installation / Upgrade

```sh
brew upgrade ark            # or: brew install magicdrive/tap/ark
# or, without Homebrew (macOS, Linux):
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh               # sh install.sh --upgrade to upgrade
```
