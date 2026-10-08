
# Ark

> Yet another alternate \[directory | repository\] text generator tool — with code intelligence MCP tools

**ark** recursively scans a directory and produces a clean, human‑readable dump of the tree and file contents. It also provides **code intelligence** features via Tree-sitter for symbol extraction and MCP (Model Context Protocol) server support. Perfect for

* 📚 sharing codebases with LLMs
* 🧪 static‑analysis pipelines
* 🗂️ snapshotting source trees
* 🔍 **code intelligence** — extract symbols, navigate definitions
* 🛰️ **MCP server** — serve codebase context to AI agents

It supports **plaintext**, **markdown**, **XML**, and **arklite** outputs, full UTF‑8 handling (with optional skip), extensive filtering, and **Tree-sitter powered** code analysis.

---

## 🚀 Quick Start

### 1. Install

```bash
go install github.com/magicdrive/ark@latest
```

Using Homebrew:

```bash
brew install magicdrive/tap/ark
```

Or download a pre-built binary from [Releases](https://github.com/magicdrive/ark/releases).

---

### 2. Generate a codebase dump

```bash
ark <dirname>                # creates ark-output.txt in the cwd
```

---

### 3. Set up Ark for your coding agent

Choose your coding agent and run `ark setup` once in your project root:

```bash
cd /your/project

ark setup claude   # Claude Code
# or
ark setup cursor   # Cursor
# or
ark setup codex    # Codex
# or
ark setup cline    # Cline CLI
# or
ark setup copilot-vscode  # GitHub Copilot in VS Code, Chat / Agent mode (this repository)
# or
ark setup copilot-cli  # GitHub Copilot CLI (this repository)
```

This registers the Ark MCP server in the client's configuration. For **Claude Code**
it additionally generates a project skill / `/<name>` slash command.

Then **restart (or reload) your client** and approve the Ark MCP server when prompted.

#### Agent integration matrix

Only clients Ark actually tests a `setup` path for are listed as supported.

| Client       | Setup command       | Config written                                   |
|--------------|---------------------|--------------------------------------------------|
| Claude Code  | `ark setup claude`  | `.mcp.json` (project) / `~/.claude/settings.json` (`--global`) |
| Cursor       | `ark setup cursor`  | `.cursor/mcp.json` (project) / `~/.cursor/mcp.json` (`--global`) |
| Codex        | `ark setup codex`   | Codex user config, via the official `codex` CLI  |
| Cline        | `ark setup cline`   | `~/.cline/mcp.json` (Cline **CLI**; see note below) |
| GitHub Copilot (VS Code) | `ark setup copilot-vscode` | `.vscode/mcp.json` (project only; see note below) |
| GitHub Copilot CLI | `ark setup copilot-cli` | `.github/mcp.json` (project only; see note below) |

> **Cline scope:** v4.1 supports the **Cline CLI** configuration at `~/.cline/mcp.json` only.
> The MCP settings used by Cline's VS Code / Cursor / Windsurf extensions
> (`.../globalStorage/.../cline_mcp_settings.json`) are **out of scope** — Ark never
> probes OS/editor-specific storage paths. Configure the IDE extension manually if needed.

> **Copilot (VS Code) scope:** `ark setup copilot-vscode` configures **GitHub Copilot Chat / Agent mode in VS Code**
> for the current repository only (`.vscode/mcp.json`, top-level `servers`). `--global` is not
> supported (the VS Code user-level path is not officially documented). It does **not** configure
> the Copilot CLI, the GitHub-hosted Copilot agent, or GitHub repository settings, and it never
> touches the portable `.mcp.json` (used by Claude Code). Copilot CLI setup is not currently
> managed by `ark setup copilot-vscode` — that is what `copilot-cli` below is for.
>
> The generated `--root` is the **absolute path of your repository** (VS Code's documentation does not
> guarantee workspace-variable substitution in `.vscode/mcp.json` `args`, nor its meaning in multi-root
> workspaces), so the file is machine-specific: do not commit/share it as-is, or have each developer
> run `ark setup copilot-vscode` locally. `command` is `ark` (resolved on `PATH`) unless you pass `--ark-path`.

> **Copilot CLI scope:** `ark setup copilot-cli` (a different client from `copilot-vscode`) configures the
> **GitHub Copilot CLI** for the current repository only: `.github/mcp.json` (`mcpServers`, a `local`
> entry). `--global` is not supported (`~/.copilot/mcp-config.json` is not managed), and the
> `copilot mcp` command is not used. Copilot CLI gives a `.mcp.json` server of the same name
> **precedence over `.github/mcp.json`**, so if `<root>/.mcp.json` already defines `mcpServers.ark`,
> setup is **refused** (also with `--force`; nothing is changed). A closer nested `.mcp.json`
> defining `ark` outranks it too, which Ark cannot detect at setup time; likewise running
> `ark setup claude` *after* `copilot-cli` creates a root `.mcp.json` `ark` entry that takes
> precedence. As above, `--root` is an absolute path: each developer should run
> `ark setup copilot-cli` locally rather than committing the file.

After setup you can use the MCP tools directly — `mcp__ark__find_symbol`,
`mcp__ark__get_symbols`, etc. — and, with Claude Code, the generated `/<name>` slash command.

> **`--force` means "replace Ark's entry", not "overwrite your config".**
> `--force` replaces only Ark's own MCP entry. It never deletes unrelated MCP servers,
> discards unknown fields, repairs malformed config, or overwrites other client settings.

> **Tip:** `setup` connects Ark MCP to your agent; `instruction` tells the agent how to use it effectively. To
> instruct Claude Code, run `ark instruction claude` and add the output to your project's `CLAUDE.md` (see
> [`ark instruction`](#-instruction--agent-usage-instructions), which also covers `codex`, `cursor`, `cline`,
> `copilot-vscode` and `copilot-cli`). A ready-to-use Claude template is at
> [`misc/CLAUDE.md.example`](misc/CLAUDE.md.example).

---

## 🧰 Basic Usage

```text
ark [OPTIONS] <dirname>
ark setup <client> [OPTIONS]
ark mcp-server [OPTIONS]
ark mcp-init [OPTIONS]
ark syntax <file> [OPTIONS]
ark symbol <file> [OPTIONS]
ark skill [OPTIONS]
```

---

## 📂 Sub‑commands

| Command      | Description                                      |
|--------------|--------------------------------------------------|
| `setup <client>` | Configure Ark for a supported coding agent (claude, cursor, codex, cline, copilot-vscode, copilot-cli). |
| `mcp-server` | Run Ark as an MCP server (stdio or HTTP).        |
| `mcp-init`   | Add ark MCP config to `.mcp.json`.              |
| `syntax`     | Parse file and output AST using Tree-sitter.     |
| `symbol`     | Extract symbols (functions, types, etc.) from file. |
| `skill`      | Generate Ark MCP skill for Claude Code / OpenAI. |
| `instruction <target>` | Print agent instructions for using Ark MCP (target: `claude`, `codex`, `cursor`, `cline`, `copilot-vscode`, `copilot-cli`). |

---

## ⚙️ General Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--help` | `-h` | Show help and exit | – |
| `--version` | `-v` | Show version | – |
| `--output-filename <file>` | `-o` | Name of the output file | `ark-output.txt` |
| `--scan-buffer <size>` | `-b` | Read buffer size (`10M`, `500K`, …) | `10M` |
| `--output-format <fmt>` | `-f` | `txt`, `md`, `xml`, `arklite` | `txt` |
| `--mask-secrets <on/off>` | `-m` | Detect & mask secrets | `on` |
| `--allow-gitignore <on/off>` | `-a` | Obey `.gitignore` rules | `on` |
| `--additionally-ignorerule <file>` | `-A` | Extra ignore‑rule file | – |
| `--with-line-number <on/off>` | `-n` | Prepend line numbers | `on` |
| `--ignore-dotfile <on/off>` | `-d` | Skip dotfiles | `off` |
| `--pattern-regex <regexp>` | `-x` | Include paths matching regexp | – |
| `--include-ext <exts>` | `-i` | Include only ext(s) (`go,ts,html`) | – |
| `--exclude-dir-regex <regexp>` | `-g` | Exclude dirs matching regexp | – |
| `--exclude-file-regex <regexp>` | `-G` | Exclude files matching regexp | – |
| `--exclude-ext <exts>` | `-e` | Exclude ext(s) | – |
| `--exclude-dir <names>` | `-E` | Exclude dirs by name | – |
| `--compless` | `-c` | Compress result with **arklite** | – |
| `--skip-non-utf8` | `-s` | Ignore non‑UTF‑8 files | – |
| `--silent` | `-S` | Suppress logs / progress | – |
| `--delete-comments` | `-D` | Strip comments (language‑aware) | – |

---

## ⚡ setup — One-command Agent Setup

`ark setup <client>` is the fastest way to integrate Ark into any project. It
registers the Ark MCP server in the target client's configuration (and, for
Claude Code, generates the project skill / slash command).

```bash
cd /your/project
ark setup cursor
ark setup claude --name my-project   # --name only affects the Claude skill
ark setup codex --global
```

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `<client>` | – | Target agent: `claude`, `cursor`, `codex`, `cline`, `copilot-vscode` (GitHub Copilot in VS Code) or `copilot-cli` (GitHub Copilot CLI); both project only | – |
| `--name <name>` | `-n` | Claude skill/slash-command name (**Claude only**) | directory name |
| `--ark-path <path>` | `-p` | Path to the `ark` binary (validated at setup time) | auto-detect (`ark` on `PATH`) |
| `--root <dir>` | `-r` | Repository root to serve | `$PWD` |
| `--global` | `-g` | Use the client's **user-level** MCP configuration | project scope |
| `--force` | `-f` | Replace an existing **Ark-owned** entry on conflict | – |

### Safety contract

`ark setup` is designed to be *boring to install*:

- **Idempotent** — running it repeatedly makes no further changes once configured
  (equivalent config → no-op, the file is not even rewritten).
- **Conflict-safe** — if an existing Ark entry differs from what you request, setup
  fails and tells you to re-run with `--force`. It never silently overwrites.
- **Preserving** — unrelated MCP servers and unknown fields are always kept.
- **Never repairs** — a malformed/unparseable config is reported, never overwritten
  (even with `--force`).
- **Atomic** — config files are replaced via a temp file + rename, with a
  concurrent-modification (lost-update) check and a verify-after-write step.

`ark setup` (with no client) remains a **deprecated** alias for `ark setup claude`
during v4.x and prints a warning.

### Manual configuration

If `ark setup` cannot run (managed machine, read-only config, unusual install), add
the Ark MCP server yourself. The command is always `ark mcp-server --root <path>`.

**Claude Code** — `.mcp.json` (project) or `~/.claude/settings.json` (global):

```json
{
  "mcpServers": {
    "ark": { "type": "stdio", "command": "ark",
             "args": ["mcp-server", "--root", "${CLAUDE_PROJECT_DIR:-.}/"], "env": {} }
  }
}
```

**Cursor** — `.cursor/mcp.json` (project) or `~/.cursor/mcp.json` (global):

```json
{
  "mcpServers": {
    "ark": { "command": "ark", "args": ["mcp-server", "--root", "/abs/path/to/repo"], "env": {} }
  }
}
```

**Cline CLI** — `~/.cline/mcp.json`: same shape as Cursor.

**GitHub Copilot (VS Code)** — `.vscode/mcp.json`: servers live under `servers`, with `"type": "stdio"`:

```json
{
  "servers": {
    "ark": { "type": "stdio", "command": "ark", "args": ["mcp-server", "--root", "/abs/path/to/repo"], "env": {} }
  }
}
```

**Codex** — register via the official CLI: `codex mcp add ark -- ark mcp-server --root /abs/path/to/repo`.

### Troubleshooting

| Symptom | Fix |
|---------|-----|
| `ark command ... not found on PATH` | Install `ark` on `PATH`, or pass `--ark-path /full/path/to/ark`. |
| Client does not detect Ark | Fully restart/reload the client so it re-reads MCP config. |
| `found a different Ark MCP configuration` | Intended config differs from existing; re-run with `--force`. |
| `cannot parse <file>` | The config is malformed; fix it by hand — Ark will not touch a broken file. |
| `the Codex CLI (codex) was not found` | Install Codex (`codex`), or configure manually (see above). |
| `root directory does not exist` | Pass a `--root` that exists; Ark validates it up front. |
| Permission denied | The config file/dir is not writable; fix permissions or use `--global`. |

---

## 🔧 mcp‑init Options

`ark mcp-init` adds (or updates) the ark MCP server entry in `.mcp.json` for the current project, so you can use ark tools in Claude Code without manual configuration.

```bash
# Add ark MCP to the current project
ark mcp-init

# Add to global Claude Code settings (~/.claude/settings.json)
ark mcp-init --global

# Specify a custom root directory
ark mcp-init --root /path/to/project

# Overwrite an existing entry
ark mcp-init --force
```

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--ark-path <path>` | `-p` | Path to ark binary | auto-detect |
| `--root <dir>` | `-r` | Root directory to serve | `$PWD` |
| `--name <name>` | `-n` | MCP server name | `ark` |
| `--global` | `-g` | Write to `~/.claude/settings.json` | `.mcp.json` (project-local) |
| `--force` | `-f` | Overwrite existing entry | – |

---

## 🛰  mcp‑server Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--root <dir>` | `-r` | Serve directory root | `$PWD` |
| `--type <stdio\|http>` | `-t` | HTTP listen port | `stdio` |
| `--http-port <port>` | `-p` | HTTP listen port | `8522` |
| `--scan-buffer <size>` | `-b` | Read buffer size (`10M`, `500K`, …) | `10M` |
| `--mask-secrets <on/off>` | `-m` | Detect & mask secrets | `on` |
| `--allow-gitignore <on/off>` | `-a` | Obey `.gitignore` rules | `on` |
| `--additionally-ignorerule <file>` | `-A` | Extra ignore‑rule file | – |
| `--ignore-dotfile <on/off>` | `-d` | Skip dotfiles | `off` |
| `--pattern-regex <regexp>` | `-x` | Include paths matching regexp | – |
| `--include-ext <exts>` | `-i` | Include only ext(s) (`go,ts,html`) | – |
| `--exclude-dir-regex <regexp>` | `-g` | Exclude dirs matching regexp | – |
| `--exclude-file-regex <regexp>` | `-G` | Exclude files matching regexp | – |
| `--exclude-ext <exts>` | `-e` | Exclude ext(s) | – |
| `--exclude-dir <names>` | `-E` | Exclude dirs by name | – |
| `--skip-non-utf8` | `-s` | Ignore non‑UTF‑8 files | – |
| `--delete-comments` | `-D` | Strip comments (language‑aware) | – |
| `--no-cache` | – | Disable persistent index cache | – |

---

## 🔍 syntax Options

| Option | Description | Default |
|--------|-------------|---------|
| `--lang <language>` | Language (go, typescript, tsx, javascript, python, php) | auto-detect |
| `--format <text\|json>` | Output format | `text` |
| `-h, --help` | Show help | – |

```bash
ark syntax main.go                # Parse Go file
ark syntax app.ts --format json   # Parse TypeScript, JSON output
ark syntax script.py --lang python
```

---

## 🏷️ symbol Options

| Option | Description | Default |
|--------|-------------|---------|
| `--lang <language>` | Language (go, typescript, tsx, javascript, python, php) | auto-detect |
| `--format <text\|json>` | Output format | `text` |
| `-h, --help` | Show help | – |

```bash
ark symbol main.go                # Extract symbols from Go file
ark symbol app.ts --format json   # Extract symbols, JSON output
ark symbol script.py --lang python
```

---

## 📜 instruction — Agent usage instructions

`ark instruction <target>` prints a short Markdown instruction that tells a coding agent how to use Ark MCP
effectively (which tool to prefer for which question, when whole-file reads make sense, how to treat
uncertain results). The guidance itself is the same for every target; only its presentation changes —
`claude` renders each tool name the way Claude Code exposes it (`mcp__ark__<tool>`), since that is the one
agent whose model-visible MCP tool naming is officially documented. Every other target gets the guidance's
bare tool names unchanged: Ark does not invent a tool-naming convention where none is documented.

| Target | Agent / surface | Suggested destination |
|--------|------------------|------------------------|
| `claude` | Claude Code | `CLAUDE.md` |
| `codex` | OpenAI Codex (CLI / IDE extension / cloud) | `AGENTS.md` |
| `cursor` | Cursor | `AGENTS.md` |
| `cline` | Cline | `.clinerules/ark.md` |
| `copilot-vscode` | GitHub Copilot in VS Code | `.github/copilot-instructions.md` |
| `copilot-cli` | GitHub Copilot CLI | `.github/copilot-instructions.md` |

"Suggested destination" is where each agent's own documentation says it looks for repository-local
instructions — not the only mechanism that agent supports, and not something Ark writes for you.

```bash
ark instruction claude                      # print to stdout
ark instruction codex > ark-instruction.md
ark instruction codex >> AGENTS.md          # review AGENTS.md first to avoid duplicating an existing Ark section
ark instruction cursor >> AGENTS.md
mkdir -p .clinerules && ark instruction cline > .clinerules/ark.md
ark instruction copilot-vscode >> .github/copilot-instructions.md
ark instruction copilot-cli >> .github/copilot-instructions.md
```

It only prints text: Ark never creates or edits any of these files, and nothing but the instruction goes to
stdout. Unsupported targets (e.g. `agents`, `copilot` — neither is a target; see below) fail with the list
of supported ones. `ark setup claude` shows the same Claude instruction after setup; `ark skill` (reusable
skill / slash-command artifacts) is a separate feature.

An instruction file is context for the agent's model, not an enforced policy boundary — treat it the same
way you would treat any other prompt text.

`ark instruction <target>` and `ark setup <client>` currently name the same six agents, but they are
independent registries for independent concerns: `setup` connects Ark's MCP server to a client; `instruction`
teaches an agent how to use it. Either list can change without the other.

**Why no `agents` or `copilot` target:** `AGENTS.md` is a destination that several targets happen to share,
not an agent identity — Claude Code, for one, does not reliably read it (it is skipped whenever a `CLAUDE.md`
is present). And `copilot` alone is ambiguous between `copilot-vscode` and `copilot-cli`, which read from the
same file but are different setup surfaces. `ark instruction` always names the agent, never the file format.

---

## 🎯 skill Command

Ark skills provide **task-oriented guidance for using Ark MCP effectively**: which tool fits which goal
(repository map, symbol context, graph relations, change impact, search), how the tools combine, when to
stop exploring, and how to treat ambiguous or uncertain results. They are richer than
[`ark instruction`](#-instruction--agent-usage-instructions), which prints the same short standing guidance
for whichever agent you target; both teach the same usage model.

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `skill` | Auto mode - detect existing skills and generate appropriate type |
| `skill init` | Generate Repository Skill (full repo-specific skill) |
| `skill add-explorer` | Add Explorer Skill as companion to existing skills |
| `skill update` | Update Ark-managed skills (preserves user files) |
| `skill inspect` | Show detected skills and repository analysis |

### Options

| Option | Description | Default |
|--------|-------------|---------|
| `--name <name>` | Skill name | auto-determined |
| `--output <dirname>` | Output directory | `skills/<name>` |
| `--archive` | Create ZIP archive | – |
| `--force` | Overwrite existing skill | – |
| `-h, --help` | Show help | – |

### Update Options

| Option | Description |
|--------|-------------|
| `--force` | Force update even if user modifications detected |
| `--dry-run` | Show what would be updated without making changes |

```bash
ark skill                              # Auto mode
ark skill init                         # Generate Repository Skill
ark skill add-explorer                 # Add Explorer Skill
ark skill update                       # Update Ark-managed skills
ark skill update --dry-run             # Preview update
ark skill inspect                      # Show detected skills
```

### Skill Types

**Repository Skill** (`skill init`): Full project-specific skill including:
- Build/test commands detected from go.mod, package.json, Makefile, etc.
- Project language analysis
- Conventions reference file

**Explorer Skill** (`skill add-explorer`): Lightweight companion skill for code navigation:
- Task-oriented guidance for using Ark MCP (goal → tool, uncertainty handling)
- Works alongside existing project skills

### Generated Files

Skills include YAML frontmatter for safe updates:
- `SKILL.md` - Skill documentation with `ark-managed: true` metadata
- `agents/openai.yaml` - OpenAI/Cline agent configuration
- `agents/claude-code.md` - Claude Code custom agent (uses `mcp__ark__*` tool names)
- `references/conventions.md` - Project conventions (Repository Skill only)

`ark skill update` refreshes `SKILL.md` and `agents/openai.yaml`; `agents/claude-code.md` and the installed slash command are written when a skill is generated.

The `agents/claude-code.md` file is also automatically installed to `.claude/commands/` as a Claude Code slash command that uses all 19 Ark MCP tools.

---

## 📝 Arguments

| Argument | Description |
|----------|-------------|
| `<dirname>` | Directory to scan |
| `<byte-string>` | Size string (`10M`, `100K`, …) |
| `<extension>` | File extension (`go`, `ts`, `html`) |
| `<regexp>` | Go `regexp` syntax pattern |

---

## 📦 Output Examples

<details>
<summary>Plaintext <code>(--output-format txt)</code></summary>

```text
example_project
├── main.go
└── sub
    └── sub.txt

=== sub/sub.txt ===
hello world
```
</details>

<details>
<summary>Markdown <code>(--output-format md)</code></summary>

````markdown
# Project Tree
```
example_project
├── main.go
└── sub
    └── sub.txt
```

---

# File: sub/sub.txt
```txt
hello world
```
````
</details>

<details>
<summary>XML <code>(--output-format xml)</code></summary>

```xml
<?xml version="1.0" encoding="UTF-8"?>
<ProjectDump>
  <Description>
    <ProjectName>example_project</ProjectName>
    <ProjectPath>/abs/path/example_project</ProjectPath>
  </Description>
  <Tree><![CDATA[
example_project
├── main.go
└── sub
    └── sub.txt
  ]]></Tree>
  <Files>
    <File path="main.go"><![CDATA[
package main
func main() { println("hello") }
    ]]></File>
    <File path="sub/sub.txt"><![CDATA[
hello world
    ]]></File>
  </Files>
</ProjectDump>
```
</details>

<details>
<summary>Arklite <code>(--output-format arklite)</code></summary>

```
# Arklite Format: example_project (/abs/path/example_project)

## Directory Tree (JSON)
{"name":"example_project","type":"directory","children":[{"name":"main.go","type":"file"},{"name":"sub","type":"directory","children":[{"name":"sub.txt","type":"file"}]}]}

## File Dump
@main.go
package main␤func main(){␤println("hello")␤}
@sub/sub.txt
hello world
```
</details>

---

## 🤔 What is Arklite?

Arklite is a compact single‑line‑per‑file format tuned for LLM token efficiency:

1. Natural‑language header (project + path)  
2. JSON directory tree  
3. File dump (`@path` + content with `␤` for newlines)

---

## 🗂 Example `.arkignore`

```gitignore
# VCS
.git/
.hg/
.svn/

# IDEs / editors
.idea/
.vscode/
*.code-workspace
*.sublime-*
```

---

## 🧩 Shell Completions

```sh
# Bash & Zsh (one script for both)
source misc/completions/ark-completion.sh
# Fish
mkdir -p ~/.config/fish/completions
cp misc/completions/fish/ark.fish ~/.config/fish/completions/
```

Completions cover the subcommands, every flag, the finite flag values
(`--lang`, `--format`, `--type`, `on`/`off`, ...) and `ark setup <client>`
(`claude`, `cursor`, `codex`, `cline`, `copilot-vscode`, `copilot-cli`). Standalone per-shell files are in
`misc/completions/{bash,zsh,fish}/`. Tests (`internal/completion`) fail if a
completion file drifts from the CLI, the setup client registry or the language
registry.

---

## ✨ Why Ark?

### 🎯 Symbol-First Code Exploration

Instead of dumping entire files, **extract only what you need**:

```bash
$ ark symbol internal/mcp/tools.go
File: internal/mcp/tools.go (go)
Symbols: 11

  struct ToolsHandler [exported] (line 15-18)
  function NewToolsHandler [exported] (line 21-26)
  method ListTools (ToolsHandler) [exported] (line 29-251)
  method CallTool (ToolsHandler) [exported] (line 254-279)
  ...
```

**Benefits:**
- 📉 **Token-efficient** — No need to read entire files
- 🎯 **Precise** — Jump directly to the definition you need
- 🔍 **Discoverable** — `[exported]` markers show API surface at a glance

### 🚀 Pure Go + Tree-sitter = Best of Both Worlds

- **No CGO required** — Cross-compile anywhere, single static binary
- **Real parsing** — Not regex hacks, actual AST-based symbol extraction
- **Multi-language** — Go, TypeScript, TSX, JavaScript, Python, PHP (and growing!)

### 🌐 Language Support

Ark advertises only the capabilities it actually tests. Levels build up:
Parse → Symbols → References → Resolution → Graph → Context.

| Language   | Parse | Symbols | References | Resolution | Graph | Context |
|------------|:-----:|:-------:|:----------:|:----------:|:-----:|:-------:|
| Go         |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |    ✓    |
| TypeScript |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |    ✓    |
| TSX        |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |    ✓    |
| JavaScript |   ✓   |    ✓    |     ✓      |            |       |         |
| Python     |   ✓   |    ✓    |     ✓      |            |       |         |
| PHP        |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |         |

A checkmark means the canonical Language Registry advertises that level as the
language's certified support level; `get_language_support` reports it at runtime.
PHP's `get_context` path is implemented and covered by dedicated context-quality
tests, but PHP is advertised at **Graph** level because several resolver
precision areas (function and constant names, framework dispatch) remain
intentionally conservative — so the Context cell is left unchecked rather than
overstating certification.

#### TypeScript / TSX — static code intelligence

Ark statically extracts **symbols** with stable containment (classes,
interfaces, type aliases, enums, functions, `const` components, and every class /
interface **member**: constructors, instance / static / abstract methods,
properties, constructor parameter properties, arrow-function fields; a
getter/setter pair is one property symbol), **module bindings** (named, aliased,
default, namespace and type-only imports), **export tables** (local, aliased,
default, named re-export, `export *`, `export * as ns`, barrel chains),
**references** (calls, `this.m()`, static and namespace-qualified calls,
construction, type references, JSX components) and **typed relations**
(`extends` / `implements`) into a typed symbol graph and agent-oriented context.

Resolution is evidence-based and conservative. Explicit repository-local
relative imports (`./user`, `../domain/user`, `./user.ts`, `./user/index`)
resolve deterministically through aliases and barrel chains (bounded and
cycle-safe) to the **defining** symbol. A member call resolves only when its
receiver type is proven structurally — `this`, an explicit type annotation,
`const x = new T()`, or a typed field / constructor parameter property; a
receiver without such evidence (`repo.save()` with an unannotated `repo`) stays
`Candidate`/`Unresolved` even when only one `save` exists. Intrinsic JSX
elements (`<div />`) are never repository references.

| | Status |
|---|---|
| **Certified** (tested end-to-end: graph adversarial fixtures, context-quality scenarios with recall 1.00 and no false Exact / fabricated edge, MCP, cache, fuzz, determinism) | relative-import resolution, aliases, default / namespace / type-only imports, barrels, member resolution under proven receiver types, `this` / static members, `extends` / `implements`, JSX component references |
| **Intentionally unresolved** (honest `Candidate` / `Unresolved`, never a guess) | external packages (`zod`, `react`, `node:fs`), path aliases (`@/foo`, tsconfig `paths`), variable receivers without proven type, inherited-member lookup, declaration merging (same name as interface + class), computed / dynamic access (`a[k]()`), `.js`-suffixed specifiers when both `.ts` and `.tsx` exist, anonymous default exports |
| **Not implemented** | return-type propagation and type inference (`const u = repo.find()`), control-flow narrowing, compiler-equivalent overload resolution, `.d.ts` / `.mts` / `package.json` resolution, `tsconfig` interpretation, `namespace` bodies, enum members, destructured declarations, framework semantics (React / Next / Nest / Angular), decorator / DI inference |

**Module resolution is not compiler-equivalent.** Ark assigns deterministic
priority only within the repository-local, config-independent lexical subset it
explicitly supports (`./user` → `user.ts`, `user.tsx`, `user/index.ts`,
`user/index.tsx`, in that order; `.js` / `.jsx` substitutes are deliberately
unranked and stay ambiguous when both a `.ts` and a `.tsx` exist). Ark does not
interpret `tsconfig`, `moduleResolution`, `moduleSuffixes` or `package.json`.
Projects whose resolution depends on those settings may resolve differently from
Ark's repository-local lexical subset.

Same-named static and instance members of one class share one symbol identity,
and a non-adjacent getter/setter pair uses the first accessor as its span.
Files without any `import` / `export` are treated as scripts (globals) and keep
the legacy proximity rules. Ark performs **pure static analysis** and never
executes repository code, Node, npm / yarn / pnpm / bun, `tsc`, `tsserver`,
package scripts, or repository configuration.

#### PHP — static code intelligence

Ark statically extracts PHP **symbols** (namespaces, classes, interfaces,
traits, enums, functions, constants, methods, constructors, properties,
class constants, enum cases, promoted properties), **imports** (plain / aliased
/ grouped / function / const `use`), **references** (function / static /
instance / nullsafe / `$this` calls, construction including `new self` /
`new parent`, class-constant reads, type references and `Foo::class`
class-strings),
and **typed relations** (`extends` / `implements` / trait `use`) into a typed
symbol graph and agent-oriented context — while **preserving uncertainty** for
dynamic or ambiguous constructs.

**Known limitations (by design):** dynamic calls / construction (`$obj->$m()`,
`$fn()`, `new $c()`) are not guessed — they are reported as unresolved
references of the calling symbol (`get_callees` `unresolved`), and only
`$c = Foo::class; new $c()` with `$c` never rebound resolves; a class-string
passed to a container (`$this->container->make(Foo::class)`) is a type use of
`Foo`, never a call to or construction of `Foo` or of whatever a binding
substitutes; a receiver's type comes only from a declaration
(a typed parameter, a constructor-injected property) or from a single
`$x = new T()` / `$c = T::class` statement whose block contains the use — an
assignment inside a branch, loop, `try` or condition and used after it is not
evidence; there is
no Composer / PSR-4 / autoload resolution; class names are resolved from the
file's own `namespace` / `use` / `use … as` / fully-qualified syntax by exact
identity match (a class that is not declared in the repository, e.g. a vendor
class, stays `Unresolved` and is never matched to a same-named repository class;
a name declared more than once is a `Candidate`); inherited and trait members
(including `self::` / `static::` / `parent::`) are resolved structurally — own
declaration → traits → nearest parent → interfaces — through repository-declared
types only, stopping at an honest `Candidate` / `Unresolved` when a participant
is outside the repository or ambiguous, a trait adaptation (`insteadof` / `as`)
names the member, or the member is private to a supertype; function and
constant names remain intentionally conservative; no framework
(Laravel/Symfony/…) semantics.
Ark performs **pure static analysis** and never executes repository code,
Composer, or any PHP tooling.

### 🤖 LLM-Optimized Workflow

Ark provides **19 MCP tools** covering the full code-intelligence stack:

| Tool | Description |
|------|-------------|
| `get_directory_tree` | Understand project layout |
| `get_symbols` | List functions/types in a file |
| `find_symbol` | Search for a symbol by name across the repo |
| `get_symbol` | Get source code of one specific function/type |
| `search_in_files` | Full-text or regex search across files |
| `list_files` | Filter-aware file listing |
| `get_file_content` | Read a whole file |
| `get_file_info` | File metadata (size, lines, language) |
| `get_project_stats` | Language breakdown, file counts |
| `get_files_arklite` | Multiple files in compressed format |
| `get_context` | Token-budgeted, relevance-ranked context for a symbol (target always included) |
| `find_references` | Find all usages of a symbol across the repo |
| `get_relations` | Explore import/dependency relations between files |
| `get_callers` | Find symbols that call a given symbol |
| `get_callees` | Find symbols called by a given symbol |
| `get_repository_map` | Compact logical map of the repo for LLM orientation |
| `analyze_change_impact` | Estimate impact of changing a symbol |
| `search_code` | Structural search by kind, name, type usage, etc. |
| `get_language_support` | List supported languages and their feature levels |

The core navigation pattern:

```
get_directory_tree   →   Understand project structure
        ↓
    find_symbol      →   Locate "where is Foo?"
        ↓
    get_symbols      →   List what's in a file
        ↓
    get_symbol       →   Extract exact source code
        ↓
    get_context      →   Token-budgeted context for safe modification
```

This approach **dramatically reduces token usage** compared to reading entire files, while maintaining full context awareness.

### 📦 Instant Skill Generation

```bash
$ ark skill --name my-project-explorer
✅ Created my-project-explorer/SKILL.md
✅ Created my-project-explorer/agents/openai.yaml
```

One command generates everything needed to teach ChatGPT or Cline how to efficiently explore your codebase.

---

## 📎 See Also

* Project home — <https://github.com/magicdrive/ark>
* Architecture and design invariants — [ARCHITECTURE.md](ARCHITECTURE.md)

## Author

© 2025 - 2026Hiroshi IKEGAMI

## License

Released under the [MIT License](LICENSE)
