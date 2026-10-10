# CLI reference

`ark` is one binary with a repository-dump mode (the default command) and
subcommands for code intelligence and agent integration. `ark --help` prints
the authoritative option list; this page explains it.

```text
ark [OPTIONS] <dirname>          # repository dump
ark setup <client> [OPTIONS]     # connect Ark to a coding agent
ark mcp-server [OPTIONS]         # run the MCP server
ark mcp-init [OPTIONS]           # write a Claude Code .mcp.json entry (older path; prefer setup)
ark instruction <target>         # print agent usage instructions
ark skill [SUBCOMMAND] [OPTIONS] # generate agent skills
ark syntax <file> [OPTIONS]      # print a file's syntax tree
ark symbol <file> [OPTIONS]      # print a file's symbols
ark --version
```

- [Repository dump](#repository-dump)
- [`setup`](#setup)
- [`mcp-server`](#mcp-server)
- [`mcp-init`](#mcp-init)
- [`instruction`](#instruction)
- [`skill`](#skill)
- [`syntax` and `symbol`](#syntax-and-symbol)
- [Shell completions](#shell-completions)

## Repository dump

`ark <dirname>` walks a directory and writes one file containing the tree and
the content of every included file. It is useful for handing a small
repository, or a filtered part of a large one, to a model in one piece.

```bash
ark .                               # writes ark-output.txt
ark -f md -i go,ts src/             # Markdown, only .go and .ts files
ark -f arklite -E vendor,dist .     # compact format, skip two directories
```

| Option | Alias | Description | Default |
|---|---|---|---|
| `--output-filename <file>` | `-o` | Output file name | `ark-output.txt`, `.md`, `.xml` or `.arklite.txt` by format |
| `--output-format <fmt>` | `-f` | `txt`, `md`, `xml` or `arklite` | `txt` |
| `--scan-buffer <size>` | `-b` | Line scan buffer (`10M`, `500K`, …) | `10M` |
| `--mask-secrets <on\|off>` | `-m` | Mask detected secrets | `on` |
| `--allow-gitignore <on\|off>` | `-a` | Apply `.gitignore` rules | `on` |
| `--additionally-ignorerule <file>` | `-A` | Extra ignore-rule file | – |
| `--with-line-number <on\|off>` | `-n` | Prefix lines with numbers (`txt` format only) | `off` |
| `--ignore-dotfile <on\|off>` | `-d` | Skip dotfiles | `off` |
| `--pattern-regex <regexp>` | `-x` | Include only paths matching the regexp | – |
| `--include-ext <exts>` | `-i` | Include only these extensions (`go,ts`) | – |
| `--exclude-dir-regex <regexp>` | `-g` | Exclude directories matching the regexp | – |
| `--exclude-file-regex <regexp>` | `-G` | Exclude files matching the regexp | – |
| `--exclude-ext <exts>` | `-e` | Exclude these extensions | – |
| `--exclude-dir <names>` | `-E` | Exclude directories by name | – |
| `--compless` | `-c` | Compress the result with arklite | – |
| `--skip-non-utf8` | `-s` | Skip files that are not UTF-8 | – |
| `--silent` | `-S` | No progress output | – |
| `--delete-comment` | `-D` | Strip comments (language-aware) | – |

Regular expressions use Go's [`regexp`](https://pkg.go.dev/regexp) syntax.
Size strings accept `K` and `M` suffixes.

A `.arkignore` file uses `.gitignore` syntax. The ignore rules belong to the
dumped directory: the dump reads every `.arkignore` and `.gitignore` at and
below it — not above it, and not in the directory it is run from — so
`ark /path/to/repo` selects the same files from anywhere
([Ignore rules](#ignore-rules)).

```gitignore
.idea/
.vscode/
*.code-workspace
```

### Ignore rules

The dump and the MCP server share one reading of ignore files:

- **Root.** Rules come from the ignore files at and below the processed
  directory (the dump's target, the server's `--root`), never from the
  working directory or a directory above it. Each file's patterns are
  relative to its own directory; `--additionally-ignorerule` files are
  relative to the root.
- **Syntax.** `.gitignore` syntax: `*`, `**`, `?`, a leading `/` anchors, a
  trailing `/` names a directory, `!` re-includes what an earlier pattern of
  the same source excluded, matching is case-sensitive. A path is ignored
  when it or a directory above it matches.
- **Two sources.** `.arkignore` files (with `--additionally-ignorerule`) and
  `.gitignore` files are separate rules: a path is ignored when either
  ignores it, and a `!` in one never re-includes what the other ignores. A
  directory may hold both. `-a off` (`allowGitignore: false` for an MCP file
  tool) leaves `.gitignore` out; `.arkignore` always applies.
- **Symlinks.** A symlink is listed but not followed into a directory; a
  dangling or looping link is skipped; a link to a file inside the root is
  ignored when its target is. The dump reads a file link that leads outside
  the root; the MCP server does not unless started with
  `--allow-external-symlinks on`.
- **What differs.** The rules mean the same in both; what they control does
  not: the dump selects files to write, while the MCP server's access
  policy is the `.arkignore` source alone — `.gitignore` never changes what
  an agent may read
  ([SECURITY.md](../SECURITY.md#file-access-policy-arkignore)).
- An ignore file the dump cannot read (an `.arkignore`, or a `.gitignore`
  unless `-a off`) stops it with `ignore rules: …` and a non-zero exit
  before anything is written: no output file is created or changed, and
  nothing is printed on standard output. Every ignore file is read and
  compiled before the dump starts. Other read errors during the dump (an
  unreadable source file) also exit non-zero, but may leave a partial output
  file.

### Output formats

Every format starts with a short preamble that explains the layout to a
model (shortened to `…` below). The examples dump this directory:

```text
example_project
├── main.go        package main / func main() { println("hello") }
└── sub
    └── sub.txt    hello world
```

<details>
<summary>Plain text (<code>-f txt</code>, the default)</summary>

```text
Project: example_project
Root: example_project
…
--- BEGIN FILE DUMP ---


example_project
├── main.go
└── sub/
    └── sub.txt


=== example_project/main.go ===
package main
func main() { println("hello") }

=== example_project/sub/sub.txt ===
hello world
```
</details>

<details>
<summary>Markdown (<code>-f md</code>)</summary>

````markdown
# Project: example_project
…
# Project Tree

```
example_project
├── main.go
└── sub/
    └── sub.txt

```

---

# File: example_project/main.go
```go
package main
func main() { println("hello") }
```
…
````
</details>

<details>
<summary>XML (<code>-f xml</code>)</summary>

```xml
<?xml version="1.0" encoding="UTF-8"?>
<ProjectDump>
<Meta>
<ProjectName>example_project</ProjectName>
<RootDir>/abs/path/example_project</RootDir>
…
</Meta>

<Tree>
<![CDATA[
├── main.go
└── sub/
    └── sub.txt
]]>
</Tree>
<directory name="sub">
<file name="sub.txt" language="text">
<![CDATA[
hello world

]]>
</file>
</directory>
<file name="main.go" language="go">
…
```
</details>

<details>
<summary>Arklite (<code>-f arklite</code>)</summary>

Arklite puts each file on one line: `@path`, then the content with newlines
written as `␤` (U+2424). Comments are stripped. The preamble explains how to
restore the files.

```text
# Arklite Format Overview

**Project:** example_project
**Path:** /abs/path/example_project
…
## Directory Tree (JSON)
{"name":"example_project","type":"directory","children":[{"name":"main.go","type":"file"},{"name":"sub","type":"directory","children":[{"name":"sub.txt","type":"file"}]}]}

## File Dump
@main.go
package main␤func main() { println("hello") }
@sub/sub.txt
hello world
```
</details>

## `setup`

Registers the Ark MCP server in a coding agent's configuration. The client
list, scopes and safety rules are in
[Getting started → Connect a coding agent](getting-started.md#3-connect-a-coding-agent).

| Option | Alias | Description | Default |
|---|---|---|---|
| `<client>` | – | `claude`, `cursor`, `codex`, `cline`, `copilot-vscode`, `copilot-cli` | – |
| `--root <dir>` | `-r` | Repository root to serve | current directory |
| `--global` | `-g` | Use the client's user-level configuration (not for `copilot-vscode` / `copilot-cli`) | project |
| `--force` | `-f` | Replace an existing Ark entry (never other entries) | – |
| `--ark-path <path>` | `-p` | Path of the `ark` binary to configure | `ark` found on `PATH` |
| `--name <name>` | `-n` | Name of the generated Claude skill / slash command (Claude only) | directory name |

`ark setup` without a client is a deprecated alias of `ark setup claude`.

## `mcp-server`

Runs the MCP server. Agents normally start it themselves from the
configuration `setup` wrote.

| Option | Alias | Description | Default |
|---|---|---|---|
| `--root <dir>` | `-r` | Repository root. A relative path is resolved against the launch directory once, at startup; a missing or non-directory root stops the server | current directory |
| `--type <stdio\|http>` | `-t` | Transport. `http` listens on `localhost:<port>`, endpoint `/mcp` | `stdio` |
| `--http-port <port>` | `-p` | HTTP port | `8522` |
| `--no-cache` | – | Do not use the extraction cache in `<root>/.ark/index` | cache on |
| `--mask-secrets <on\|off>` | `-m` | Mask detected secrets in every MCP response; `off` disables masking and logs a warning ([SECURITY.md](../SECURITY.md#secret-masking)) | `on` |
| `--allow-external-symlinks <on\|off>` | – | Read files through symlinks in the repository that lead outside it; `.arkignore` still applies, and paths outside the root stay refused ([Symlink policy](../SECURITY.md#symlink-policy)) | `off` |
| `--scan-buffer`, `--allow-gitignore`, `--additionally-ignorerule`, `--ignore-dotfile`, `--pattern-regex`, `--include-ext`, `--exclude-dir-regex`, `--exclude-file-regex`, `--exclude-ext`, `--exclude-dir`, `--skip-non-utf8`, `--delete-comment` | | As in [repository dump](#repository-dump). They are the defaults of the file tools (`list_files`, `search_in_files`, `get_file_content`, …); a tool argument of the same name overrides them per request | |

The filter options do not narrow the code-intelligence index: graph tools
index every file of a supported language under the requested `path`, skipping
only directories whose name starts with `.`, `vendor` and `node_modules`
([Operations](operations.md#what-the-index-covers)).

`ark mcp-server --help` and `--version` print and exit.

## `mcp-init`

Writes an Ark entry into Claude Code's `.mcp.json` (or, with `--global`,
`~/.claude/settings.json`). `ark setup claude` supersedes it and adds the
safety contract described in [Getting started](getting-started.md#safety-contract).

| Option | Alias | Description | Default |
|---|---|---|---|
| `--root <dir>` | `-r` | Root directory to serve | current directory |
| `--name <name>` | `-n` | MCP server name | `ark` |
| `--global` | `-g` | Write `~/.claude/settings.json` | `.mcp.json` |
| `--force` | `-f` | Overwrite an existing entry | – |
| `--ark-path <path>` | `-p` | Path of the `ark` binary | auto-detect |

## `instruction`

`ark instruction <target>` prints Markdown that tells an agent which Ark tool
to use for which question and how to treat uncertain results. It writes no
file.

| Target | Agent | Where that agent reads repository instructions |
|---|---|---|
| `claude` | Claude Code | `CLAUDE.md` |
| `codex` | OpenAI Codex | `AGENTS.md` |
| `cursor` | Cursor | `AGENTS.md` |
| `cline` | Cline | `.clinerules/ark.md` |
| `copilot-vscode` | GitHub Copilot in VS Code | `.github/copilot-instructions.md` |
| `copilot-cli` | GitHub Copilot CLI | `.github/copilot-instructions.md` |

```bash
ark instruction claude >> CLAUDE.md      # review the file first to avoid duplicates
mkdir -p .clinerules && ark instruction cline > .clinerules/ark.md
```

The guidance is the same for every target. Only `claude` renders tool names
as Claude Code exposes them (`mcp__ark__<tool>`); other targets get the bare
names. An instruction file is prompt text for the model, not an enforced
policy.

## `skill`

Generates reusable, task-oriented guidance for agents that support skills.

| Command | Description |
|---|---|
| `ark skill` | Detect existing skills and generate the appropriate type |
| `ark skill init` | Repository skill: build/test commands detected from `go.mod`, `package.json`, `Makefile`, …, a language summary and a conventions file |
| `ark skill add-explorer` | Explorer skill: Ark tool guidance only, to sit beside existing skills |
| `ark skill update [--dry-run] [--force]` | Refresh Ark-managed files, keeping user edits unless `--force` |
| `ark skill inspect` | Show detected skills and the repository analysis |

Options: `--name <name>`, `--output <dir>` (default `skills/<name>`),
`--archive` (also write a ZIP), `--force`.

A skill contains `SKILL.md` (with `ark-managed: true` metadata),
`agents/openai.yaml`, `agents/claude-code.md` and, for a repository skill,
`references/conventions.md`. `agents/claude-code.md` is also installed as a
Claude Code slash command under `.claude/commands/`. Generated skills grant
19 of Ark's 21 MCP tools (all but `find_references` and
`get_language_support`).

## `syntax` and `symbol`

Inspect one file without starting a server.

```bash
ark symbol internal/mcp/tools.go
ark symbol app.ts --format json
ark syntax main.go
ark syntax script.py --lang python
```

| Option | Description | Default |
|---|---|---|
| `--lang <language>` | `go`, `typescript`, `tsx`, `javascript`, `python`, `php`, `terraform` | detected from the extension |
| `--format <text\|json>` | Output format | `text` |

## Shell completions

```sh
source misc/completions/ark-completion.sh          # Bash and Zsh
cp misc/completions/fish/ark.fish ~/.config/fish/completions/
```

Release archives contain the same files under `completions/`. The
completions cover every subcommand and flag and the finite flag values; tests
in `internal/completion` fail when a completion file drifts from the CLI.
